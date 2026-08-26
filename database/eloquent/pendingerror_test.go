package eloquent

import (
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// The two methods that do not go through prepare.
//
// Every chainable on this builder holds its error instead of returning it, so
// that a chain is a chain, and prepare is where that error is read back. Two
// executing methods skip prepare -- ForceDelete because "force" is about the
// soft delete and it scopes the tenant itself, FromQuery because the SQL is the
// caller's -- and both used to skip the error with it.
//
// What that produced is the worst shape a bug can have: the statement is well
// formed, the driver accepts it, and the row is gone. A DELETE issued from a
// builder that was invalidated three calls earlier is a DELETE nobody asked
// for.

// TestForceDeleteRefusesABuilderThatAlreadyFailed invalidates the builder with
// WithTrashed on a model that does not soft delete, then force-deletes.
func TestForceDeleteRefusesABuilderThatAlreadyFailed(t *testing.T) {
	model, conn := newUserModel()
	model.SoftDeletes = false

	// WithTrashed on a model with no soft deletes fails the builder rather than
	// returning an error, because it has to stay chainable.
	builder := model.NewQuery().WithTrashed()

	affected, err := builder.ForceDelete(auth.SystemGrant("users.write", "acme"))
	if err == nil {
		t.Fatal("ForceDelete ran on a failed builder and reported no error")
	}
	if affected != 0 {
		t.Errorf("affected: got %d, want 0", affected)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("a statement reached the connection: %v", conn.sqls())
	}
}

// TestFromQueryRefusesABuilderThatAlreadyFailed is the same property on the
// read side. It matters less than the delete and fails the same way.
func TestFromQueryRefusesABuilderThatAlreadyFailed(t *testing.T) {
	model, conn := newUserModel()
	model.SoftDeletes = false

	builder := model.NewQuery().WithTrashed()

	rows, err := builder.FromQuery(auth.SystemGrant("users.read", "acme"),
		`SELECT * FROM users WHERE tenant_id = ?`, []any{"acme"})
	if err == nil {
		t.Fatal("FromQuery ran on a failed builder and reported no error")
	}
	if rows != nil {
		t.Errorf("rows: got %v, want nil", rows)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("a statement reached the connection: %v", conn.sqls())
	}
}
