package eloquent

import (
	"context"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/auth"
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
