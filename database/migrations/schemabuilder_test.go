package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// The migration written with the Blueprint.
//
// The schema package has had a Blueprint with a hundred and thirty-nine methods
// and a grammar for three engines since it was written, and no migration could
// reach any of it: Up received a connection that ran strings. So every migration
// in the collection wrote CREATE TABLE by hand, and the portability the grammar
// exists to give was worked out in a comment instead -- there is one in examples
// explaining which of VARCHAR and TEXT survives a MySQL index.
//
// conn.Schema() is that reach. What these tests assert is that a migration
// written the standard way produces the statements it should, and that the
// escape is still there beside it.

// blueprintMigration is a migration written the way the generator will write
// them: a callback over the Blueprint, no SQL in sight.
type blueprintMigration struct{ migrations.BaseMigration }

func (blueprintMigration) GetName() string { return "2026_08_26_000001_create_invoices_table" }

func (blueprintMigration) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, "invoices", func(table *schema.Blueprint) {
		table.ID()
		table.String("number")
		table.String("email").Unique()
		table.Timestamps()
	})
}

func (blueprintMigration) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, "invoices")
}

// mixedMigration is one that uses both, which is the shape the escape exists
// for: the Blueprint for the table, a statement for the thing no Blueprint
// reaches.
type mixedMigration struct{ migrations.BaseMigration }

func (mixedMigration) GetName() string { return "2026_08_26_000002_create_invoices_search" }

func (mixedMigration) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, "documents", func(table *schema.Blueprint) {
		table.ID()
		table.Text("body")
	}); err != nil {
		return err
	}
	_, err := conn.Statement(ctx, `CREATE INDEX CONCURRENTLY documents_body_idx ON documents (body)`, nil)
	return err
}

// TestAMigrationWrittenWithTheBlueprintCompilesItsStatements is the reach
// itself: no SQL in the migration, and SQL out of it.
func TestAMigrationWrittenWithTheBlueprintCompilesItsStatements(t *testing.T) {
	statements, err := migrations.UpStatements(context.Background(), blueprintMigration{})
	if err != nil {
		t.Fatalf("UpStatements: %v", err)
	}
	if len(statements) == 0 {
		t.Fatal("a migration written with the Blueprint produced no statements; --pretend would print nothing and read like a migration that does nothing")
	}

	all := strings.ToLower(strings.Join(statements, "\n"))
	for _, want := range []string{"create table", "invoices", "number", "email", "created_at", "updated_at"} {
		if !strings.Contains(all, want) {
			t.Errorf("the statements do not carry %q:\n%s", want, strings.Join(statements, "\n"))
		}
	}
}

// TestTheBlueprintReachesTheReversal: Down is the same path, and a rollback
// that printed nothing would be a rollback nobody could review.
func TestTheBlueprintReachesTheReversal(t *testing.T) {
	statements, err := migrations.DownStatements(context.Background(), blueprintMigration{})
	if err != nil {
		t.Fatalf("DownStatements: %v", err)
	}
	all := strings.ToLower(strings.Join(statements, "\n"))
	if !strings.Contains(all, "drop table") || !strings.Contains(all, "invoices") {
		t.Errorf("the reversal does not drop the table:\n%s", strings.Join(statements, "\n"))
	}
}

// TestTheEscapeIsStillThere: conn.Statement did not go anywhere, and a migration
// may use both. CREATE INDEX CONCURRENTLY is the statement that makes the point
// -- no Blueprint reaches it, and PostgreSQL refuses it inside a transaction.
func TestTheEscapeIsStillThere(t *testing.T) {
	statements, err := migrations.UpStatements(context.Background(), mixedMigration{})
	if err != nil {
		t.Fatalf("UpStatements: %v", err)
	}

	all := strings.Join(statements, "\n")
	if !strings.Contains(strings.ToLower(all), "create table") {
		t.Errorf("the Blueprint half is missing:\n%s", all)
	}
	if !strings.Contains(all, "CREATE INDEX CONCURRENTLY") {
		t.Errorf("the escape half is missing:\n%s", all)
	}
}
