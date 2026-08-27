package model

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
)

// One builder, two Grants.
//
// A *Builder[T] is a value a caller can hold and run more than once, and
// prepare short-circuits on a flag (`if b.prepared`). Those two facts together
// are the shape of a tenant leak: if the flag ever lets the second run reuse the
// first run's scoped query, the second Grant reads the first Grant's rows and
// nothing says so -- the statement is well formed, the driver is happy, and the
// wrong customer's data comes back.
//
// The design intends otherwise. prepare scopes a clone and leaves the caller's
// builder untouched, so each run starts from an unscoped query. That intent is
// written down beside scopeToTenant. Nothing tested it.
//
// These tests are the assertion the intent was missing. They are written
// against the current code deliberately: a guard that only arrives with the
// refactor proves nothing about the refactor, because it cannot fail before it.

// TestOneBuilderUnderTwoGrantsScopesEachRunSeparately runs the same builder for
// two tenants and reads the bindings of both statements.
func TestOneBuilderUnderTwoGrantsScopesEachRunSeparately(t *testing.T) {
	model, conn := newUserModel()

	// One builder, held by the caller, as a repository or a scope helper would
	// hold it.
	builder := model.NewQuery().Where("name", "Ada")

	conn.queue()
	if _, err := builder.Get(context.Background(), auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("Get for acme: %v", err)
	}

	conn.queue()
	if _, err := builder.Get(context.Background(), auth.SystemGrant("users.read", "globex")); err != nil {
		t.Fatalf("Get for globex: %v", err)
	}

	if got := len(conn.statements); got != 2 {
		t.Fatalf("statements: got %d, want 2 -- one per Get", got)
	}

	first, second := conn.statements[0], conn.statements[1]

	if !slices.Contains(first.Bindings, any("acme")) {
		t.Errorf("first statement is not scoped to acme: %v", first.Bindings)
	}
	if slices.Contains(first.Bindings, any("globex")) {
		t.Errorf("first statement carries the second tenant: %v", first.Bindings)
	}

	// The half that matters. A leak here reads every acme row under a globex
	// grant, or -- worse, because it looks like nothing at all -- both filters
	// at once, which returns no rows and reads as an empty table.
	if !slices.Contains(second.Bindings, any("globex")) {
		t.Errorf("second statement is not scoped to globex: %v", second.Bindings)
	}
	if slices.Contains(second.Bindings, any("acme")) {
		t.Errorf("second statement still carries the first tenant: %v", second.Bindings)
	}
}

// TestRunningABuilderDoesNotScopeTheBuilderTheCallerKept is the same property
// read from the other side: after a run, the caller's own builder must still be
// unscoped, so the next Grant starts from nothing.
//
// It is a separate test because it fails differently. The one above catches a
// second statement carrying the wrong tenant; this one catches a builder that
// accumulates a filter per run -- which stays correct for two runs under one
// tenant and goes wrong on the first run under a second.
func TestRunningABuilderDoesNotScopeTheBuilderTheCallerKept(t *testing.T) {
	model, conn := newUserModel()

	builder := model.NewQuery().Where("name", "Ada")

	conn.queue()
	if _, err := builder.Get(context.Background(), auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// The caller's builder, not the clone prepare scoped.
	if builder.prepared {
		t.Error("running the builder marked the caller's own builder prepared")
	}

	for _, where := range builder.query.Wheres {
		if where.Column == "users.tenant_id" || where.Column == "tenant_id" {
			t.Fatalf("the tenant filter landed on the builder the caller kept: %+v", where)
		}
	}
}

// Who puts the tenant on a relation's own table.
//
// Two layers used to, and the statement said so: `... and "posts"."tenant_id" =
// ? and "posts"."tenant_id" = ?`, bound [1 acme acme]. Both clauses were right,
// which is why it survived -- the rows were correct and the only cost was a
// reader having to prove the second copy redundant.
//
// It is prepare that owns it. The relation layer asks the builder first
// (concerns.OwnTenantScoper) and the typed builder answers yes, because prepare
// filters on the column the model itself declares. The relation layer only knows
// the default name, so on a model that renamed the column it would have written
// a second filter on a column that is not there, and on a shared table a filter
// on a column that exists nowhere.
//
// The three tests below are one property read from three sides, and they are
// separate because they fail differently. Deleting the filter from prepare
// leaves the first two green and breaks the third; deleting the claim from the
// builder leaves all three green and puts the second clause back. Nothing here
// can end in a statement with no tenant filter, which is the failure the split
// has to be safe against.

// TestARelationNamesTheTenantOnceAndNamesIt is the statement itself: the filter
// is there, and there is one of it.
func TestARelationNamesTheTenantOnceAndNamesIt(t *testing.T) {
	users, _ := newUserModel()
	posts, postsConn := newPostModel()

	parent, err := users.NewFromBuilder(map[string]any{"id": int64(1), "tenant_id": "acme"})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	postsConn.queue()
	if _, err := HasManyOf(parent, posts, "", "").Get(context.Background(), auth.SystemGrant("posts.read", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	last := postsConn.last()
	if got := strings.Count(last.SQL, `"posts"."tenant_id"`); got != 1 {
		t.Errorf(`the relation names "posts"."tenant_id" %d times, want 1: %s`, got, last.SQL)
	}

	tenants := 0
	for _, binding := range last.Bindings {
		if binding == "acme" {
			tenants++
		}
	}
	if tenants != 1 {
		t.Errorf("the relation binds the tenant %d times, want 1: %v", tenants, last.Bindings)
	}
}

// TestTheTypedBuilderPromisesToScopeItsOwnTable reads the claim the relation
// layer trusts. It is one line of production code and it is load bearing: with
// it false, the clause comes back twice; with it true and prepare not
// delivering, it never comes at all.
func TestTheTypedBuilderPromisesToScopeItsOwnTable(t *testing.T) {
	posts, _ := newPostModel()

	scoper, ok := posts.NewQuery().Ref().(concerns.OwnTenantScoper)
	if !ok {
		t.Fatal("the typed builder no longer answers OwnTenantScoper, so a relation over it writes the filter itself -- and only ever under the default column name")
	}
	if !scoper.ScopesOwnTableByTenant() {
		t.Fatal("the typed builder says it does not scope its own table, which prepare does")
	}
}

// TestPrepareIsWhatPutsTheTenantOnTheOwnTable is the promise paid.
//
// It reaches under the relation deliberately: ScopeTenant is asked to scope the
// typed builder and answers by adding nothing at all, so whatever tenant filter
// the statement ends up carrying can only have come from prepare. A change that
// dropped it there would leave this test looking at a statement with no tenant
// in it, which is the failure this split has to be caught by.
func TestPrepareIsWhatPutsTheTenantOnTheOwnTable(t *testing.T) {
	posts, conn := newPostModel()

	builder := posts.NewQuery()
	g := auth.SystemGrant("posts.read", "acme")

	scoped, err := concerns.ScopeTenant(builder.Ref(), posts.Ref(), g)
	if err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}
	for _, where := range scoped.GetQuery().Wheres {
		if where.Column == "posts.tenant_id" {
			t.Fatalf("the relation layer wrote the own-table filter after promising not to: %+v", where)
		}
	}

	conn.queue()
	if _, err := builder.Get(context.Background(), g); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(conn.last().SQL, `"posts"."tenant_id" = ?`) {
		t.Fatalf("nothing put the tenant on the statement: %s", conn.last().SQL)
	}
}
