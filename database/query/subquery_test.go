package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// countTenantFilters reports how many times the tenant column is compared in a
// statement. A statement with a subquery has to carry one per query, not one in
// total.
//
// The column arrives qualified -- "users"."tenant_id" -- because a query with a
// join has to say which table it means, so the count is on the quoted column
// and its comparison rather than on the bare name.
func countTenantFilters(sql string) int {
	return strings.Count(sql, `"`+query.TenantColumn+`" = ?`)
}

// A `where exists (select ... from invoices)` compiles the inner select whole.
// Filtering only the outer query says nothing about which invoices were looked
// at: the customer row belongs to this tenant, and the reason it came back may
// be an invoice belonging to another. That is a leak whose outer query is
// perfectly correct, which is why it survives review.
func TestASubqueryInWhereExistsCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.WhereExists(func(sub *query.Builder) {
		sub.From("invoices").WhereColumn("invoices.user_id", "=", "users.id")
	}, "and", false)

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(connection.calls) == 0 {
		t.Fatal("no statement reached the connection")
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2 -- one for the outer query and one for the subquery:\n%s", got, sql)
	}
}

// whereIn over a subquery is the same leak with a different shape, and the more
// common of the two: "the users who have an invoice over a thousand".
//
// The harness grammar compiles an In from its Values and has no form for one
// carrying a query, so the statement cannot be read for the filter here. What
// can be read is the binding list: scoping the subquery contributes the
// tenant, and the value is there or the subquery was not scoped.
func TestASubqueryInWhereInCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	sub := query.NewBuilder(connection, b.GetGrammar(), b.GetProcessor()).
		From("invoices").Select("user_id").Where("total", ">", 1000)
	b.Wheres = append(b.Wheres, query.Where{
		Type: "In", Column: "id", Query: sub, Boolean: "and",
	})

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	tenants := 0
	for _, binding := range connection.calls[0].bindings {
		if binding == "acme" {
			tenants++
		}
	}
	if tenants != 2 {
		t.Errorf("the tenant is bound %d time(s), want 2 -- once for the outer query and once for the subquery:\n%v", tenants, connection.calls[0].bindings)
	}
}

// A subquery inside a parenthesised group has to be reached as well. The group
// is the same query and is already under the outer filter, so it must NOT be
// scoped itself -- but what it holds must be.
func TestASubqueryInsideAGroupIsReached(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.Where(func(group *query.Builder) {
		group.Where("state", "=", "active").WhereExists(func(sub *query.Builder) {
			sub.From("invoices")
		}, "or", false)
	})

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2 -- the group is not scoped again, but the subquery inside it is:\n%s", got, sql)
	}
}

// Scoping a subquery adds a binding to it, and every value declared after that
// clause has to move along with it. If the flat list is left as it was, the
// values slide onto the wrong placeholders -- which is not an error, it is a
// query that runs and answers wrongly.
func TestScopingASubqueryKeepsTheBindingsLinedUpWithTheirPlaceholders(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.Where("state", "=", "active").
		WhereExists(func(sub *query.Builder) {
			sub.From("invoices").Where("total", ">", 1000)
		}, "and", false).
		WhereRaw("created_at > ?", "2026-01-01")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	call := connection.calls[0]
	if got, want := strings.Count(call.sql, "?"), len(call.bindings); got != want {
		t.Fatalf("the statement has %d placeholders and %d bindings:\n%s\n%v", got, want, call.sql, call.bindings)
	}

	// The last value declared must still be the last one bound: it is the one
	// the inserted tenant would have pushed off the end.
	last := call.bindings[len(call.bindings)-1]
	if last != "2026-01-01" {
		t.Errorf("the last binding is %v, want 2026-01-01 -- the values slid when the subquery gained one:\n%v", last, call.bindings)
	}
}

// WhereBindings is what makes the rebuild possible, and it is only correct
// while it knows every clause type that carries a value. A clause it does not
// know about loses its values in silence, so this pins the whole set against
// the flat list the builder kept.
func TestWhereBindingsRebuildsExactlyWhatTheBuilderCollected(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.Where("state", "=", "active").
		WhereIn("role", []any{"admin", "owner"}).
		WhereBetween("age", 18, 65).
		WhereNull("deleted_at").
		WhereRaw("created_at > ?", "2026-01-01").
		Where(func(group *query.Builder) { group.Where("plan", "=", "pro") })

	flat := b.GetBindings()
	rebuilt := b.WhereBindings()

	if len(flat) != len(rebuilt) {
		t.Fatalf("the builder collected %d bindings and the clauses rebuild %d:\n%v\n%v", len(flat), len(rebuilt), flat, rebuilt)
	}
	for i := range flat {
		if flat[i] != rebuilt[i] {
			t.Errorf("binding %d is %v rebuilt and %v collected", i, rebuilt[i], flat[i])
		}
	}
}

// A Grant with no tenant reaches nothing, subquery or not.
func TestASubqueryIsNotAWayPastTheTenantCheck(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)
	b.WhereExists(func(sub *query.Builder) { sub.From("invoices") }, "and", false)

	if _, err := b.Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("a query with a subquery ran without a tenant")
	}
	if len(connection.calls) != 0 {
		t.Errorf("a statement reached the connection anyway: %v", connection.calls)
	}
}
