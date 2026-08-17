package database_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// TestAMigrationStatementIsReboundForPostgres: the migrations component writes
// "?" like every other query in this framework, and PostgreSQL wants "$1". The
// adapter is where that translation happens, because it is the only side of the
// pair that knows which engine it is talking to.
func TestAMigrationStatementIsReboundForPostgres(t *testing.T) {
	handle, state := newFakeDB()
	conn := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

	migrationConn := database.ForMigrations(conn)
	_, err := migrationConn.Statement(context.Background(),
		`INSERT INTO migrations (id, migration, batch) VALUES (?, ?, ?)`,
		[]any{1, "2026_01_01_000000_first", 1})
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}

	if !state.sawStatement("VALUES ($1, $2, $3)") {
		t.Fatalf("the statement reached the driver unbound: %q", state.statements())
	}
}

// TestAMigrationStatementIsLeftAloneForMySQL: MySQL and SQLite already spell a
// placeholder "?", so the same call goes through untouched.
func TestAMigrationStatementIsLeftAloneForMySQL(t *testing.T) {
	handle, state := newFakeDB()
	conn := database.NewConnection(handle, "app", "", map[string]any{"driver": "mysql"})

	migrationConn := database.ForMigrations(conn)
	_, err := migrationConn.Statement(context.Background(),
		`INSERT INTO migrations (id, migration, batch) VALUES (?, ?, ?)`,
		[]any{1, "2026_01_01_000000_first", 1})
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}

	if !state.sawStatement("VALUES (?, ?, ?)") {
		t.Fatalf("the statement was rewritten: %q", state.statements())
	}
}

// TestASchemaStatementIsUntouched: a CREATE TABLE carries no placeholder, and
// rebinding one is a rewrite of SQL somebody wrote by hand.
func TestASchemaStatementIsUntouched(t *testing.T) {
	handle, state := newFakeDB()
	conn := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

	create := `CREATE TABLE widgets (id VARCHAR(255) PRIMARY KEY)`
	migrationConn := database.ForMigrations(conn)
	if _, err := migrationConn.Statement(context.Background(), create, nil); err != nil {
		t.Fatalf("Statement: %v", err)
	}

	if !state.sawStatement(create) {
		t.Fatalf("the schema statement was rewritten: %q", state.statements())
	}
}
