package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/validation"
)

// The count is what validation.PresenceQuery names, so the method value builds
// the verifier with nothing written in between. Asserted here, where the test
// may import validation and the package does not.
var _ validation.PresenceQuery = (*database.Presence)(nil).Count

// presenceOver returns a presence count over a fake Postgres handle that
// answers every query with count.
func presenceOver(count int64, global ...string) (*database.Presence, *fakeDB) {
	handle, state := newFakeDB()
	state.answer([]string{"aggregate"}, []driver.Value{count})
	return database.NewPresence(database.Wrap(handle, database.DialectPostgres), global...), state
}

// argsOf is the values the one statement on the fake was sent with.
func argsOf(t *testing.T, state *fakeDB) []any {
	t.Helper()
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.args) != 1 {
		t.Fatalf("%d statements reached the database, want 1", len(state.args))
	}
	out := make([]any, len(state.args[0]))
	for i, a := range state.args[0] {
		out[i] = a.Value
	}
	return out
}

// TestPresenceCountsWithinTheGrantsTenant: a count is a read, so it is
// filtered by the tenant on the Grant -- `unique` asks whether this customer
// already holds the value, not whether somebody does.
func TestPresenceCountsWithinTheGrantsTenant(t *testing.T) {
	presence, state := presenceOver(1)

	count, err := presence.Count(context.Background(), auth.SystemGrant("invoice.create", "acme"),
		"invoices", "number", []any{"2026-114"}, nil, "", nil)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want the database's answer", count)
	}
	want := `select count(distinct "number") as aggregate from "invoices" where "tenant_id" = $1 and "number" in ($2)`
	if got := state.statements()[0]; got != want {
		t.Errorf("statement\n got: %s\nwant: %s", got, want)
	}
	if args := argsOf(t, state); fmt.Sprint(args) != "[acme 2026-114]" {
		t.Errorf("args = %v, want the Grant's tenant and the value", args)
	}
}

// TestPresenceCountsASharedTableWithoutTheFilter: the table an address has to
// be unique across is named once, and only it loses the filter.
func TestPresenceCountsASharedTableWithoutTheFilter(t *testing.T) {
	presence, state := presenceOver(0, "users")

	if _, err := presence.Count(context.Background(), auth.SystemGrant("user.create", "acme"),
		"users", "email", []any{"ada@example.com"}, nil, "", nil); err != nil {
		t.Fatalf("Count: %v", err)
	}
	want := `select count(distinct "email") as aggregate from "users" where "email" in ($1)`
	if got := state.statements()[0]; got != want {
		t.Errorf("statement\n got: %s\nwant: %s", got, want)
	}
}

// TestPresenceAppliesTheRuleConditions: the ignored row and the extra
// conditions, read the way the rule strings write them, in column order.
func TestPresenceAppliesTheRuleConditions(t *testing.T) {
	presence, state := presenceOver(0)

	_, err := presence.Count(context.Background(), auth.SystemGrant("user.update", "acme"),
		"members", "email", []any{"ada@example.com"}, 7, "member_id", map[string]string{
			"team":       "red",
			"status":     "!banned",
			"deleted_at": "NULL",
			"joined_at":  "NOT_NULL",
		})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	want := `select count(distinct "email") as aggregate from "members" where "tenant_id" = $1 and "email" in ($2) ` +
		`and "member_id" <> $3 and "deleted_at" is null and "joined_at" is not null and "status" != $4 and "team" = $5`
	if got := state.statements()[0]; got != want {
		t.Errorf("statement\n got: %s\nwant: %s", got, want)
	}
	if args := argsOf(t, state); fmt.Sprint(args) != "[acme ada@example.com 7 banned red]" {
		t.Errorf("args = %v", args)
	}
}

// TestPresenceRefusesAGrantWithNoTenant: nothing can be scoped without one, so
// no statement is built, and the rule fails closed on the error.
func TestPresenceRefusesAGrantWithNoTenant(t *testing.T) {
	presence, state := presenceOver(0, "users")

	_, err := presence.Count(context.Background(), auth.Grant{}, "users", "email", []any{"ada@example.com"}, nil, "", nil)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err = %v, want auth.ErrForbidden", err)
	}
	if n := len(state.statements()); n != 0 {
		t.Fatalf("%d statements reached the database with no tenant", n)
	}
}

// TestPresenceDrivesTheValidationRules: built into the verifier, the count is
// what decides `unique` and `exists`.
func TestPresenceDrivesTheValidationRules(t *testing.T) {
	g := auth.SystemGrant("user.create", "acme")
	validate := func(count int64, rules validation.Rules) bool {
		presence, _ := presenceOver(count)
		set, err := validation.Compile(rules)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		verifier := validation.NewDatabasePresenceVerifier(presence.Count)
		return validation.Make(validation.Data{"email": "ada@example.com"}, set, validation.WithPresence(g, verifier)).Passes()
	}

	if validate(1, validation.Rules{"email": "unique:members"}) {
		t.Error("unique passed on a value the tenant already holds")
	}
	if !validate(0, validation.Rules{"email": "unique:members"}) {
		t.Error("unique failed on a value nobody holds")
	}
	if !validate(1, validation.Rules{"email": "exists:members,email"}) {
		t.Error("exists failed on a value the tenant holds")
	}
}
