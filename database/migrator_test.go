package database_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// TestNewSingleConnectionResolverHoldsTheOneConnectionAsTheDefault: an application with one
// database reaches it by the empty name and by the name it was registered
// under, and the connection describes the engine the handle speaks -- db, the
// migrator and the wipe all read the driver off it.
func TestNewSingleConnectionResolverHoldsTheOneConnectionAsTheDefault(t *testing.T) {
	handle, _ := newFakeDB()
	db := database.Wrap(handle, database.DialectPostgres)

	resolver := database.NewSingleConnectionResolver(db, "")
	if got := resolver.GetDefaultConnection(); got != database.DefaultConnectionName {
		t.Fatalf("default connection = %q, want %q", got, database.DefaultConnectionName)
	}

	for _, name := range []string{"", database.DefaultConnectionName} {
		connection, err := resolver.Connection(name)
		if err != nil {
			t.Fatalf("Connection(%q): %v", name, err)
		}
		concrete, ok := connection.(*database.Connection)
		if !ok {
			t.Fatalf("Connection(%q) = %T, want *database.Connection", name, connection)
		}
		if got := concrete.GetDriverName(); got != string(database.DialectPostgres) {
			t.Fatalf("driver = %q, want %q: the commands would describe another engine", got, database.DialectPostgres)
		}
	}
}

// TestNewSingleConnectionResolverRegistersUnderTheNameItIsGiven: a project whose migrations
// name their connection keeps that name, and the default name is then not a
// second spelling of it.
func TestNewSingleConnectionResolverRegistersUnderTheNameItIsGiven(t *testing.T) {
	handle, _ := newFakeDB()
	resolver := database.NewSingleConnectionResolver(database.Wrap(handle, database.DialectSQLite), "reports")

	if got := resolver.GetDefaultConnection(); got != "reports" {
		t.Fatalf("default connection = %q, want reports", got)
	}
	if _, err := resolver.Connection(database.DefaultConnectionName); err == nil {
		t.Fatal("the connection answers to a name it was not registered under")
	}
}

// TestNewMigratorRecordsInTheTableItIsGiven: the tracking table is the
// compatibility of every database already migrated, so the name passed is the
// name the repository creates and reads.
func TestNewMigratorRecordsInTheTableItIsGiven(t *testing.T) {
	handle, state := newFakeDB()
	migrator := database.NewMigrator(database.Wrap(handle, database.DialectSQLite), "arandu_migrations")

	if err := migrator.GetRepository().CreateRepository(context.Background()); err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if !state.sawStatement("EXISTS arandu_migrations (") {
		t.Fatalf("the repository was created somewhere else: %q", state.statements())
	}
}

// TestNewMigratorDefaultsTheTable: an empty name is the component's own
// default rather than a table with no name.
func TestNewMigratorDefaultsTheTable(t *testing.T) {
	handle, state := newFakeDB()
	migrator := database.NewMigrator(database.Wrap(handle, database.DialectSQLite), "")

	if err := migrator.GetRepository().CreateRepository(context.Background()); err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if !state.sawStatement("EXISTS migrations (") {
		t.Fatalf("the repository was not created in the default table: %q", state.statements())
	}
}

// TestNewMigratorRunsOnTheDatabaseItWasGiven: a migration that names no
// connection runs on the default one, and the default one is db.
func TestNewMigratorRunsOnTheDatabaseItWasGiven(t *testing.T) {
	handle, state := newFakeDB()
	migrator := database.NewMigrator(database.Wrap(handle, database.DialectSQLite), "")

	if got := migrator.GetConnection(); got != database.DefaultConnectionName {
		t.Fatalf("migrator connection = %q, want %q", got, database.DefaultConnectionName)
	}
	connection, err := migrator.ResolveConnection("")
	if err != nil {
		t.Fatalf("ResolveConnection: %v", err)
	}
	if _, err := connection.Statement(context.Background(), "CREATE TABLE widgets (id INTEGER)", nil); err != nil {
		t.Fatalf("Statement: %v", err)
	}
	if !state.sawStatement("CREATE TABLE widgets") {
		t.Fatalf("the statement did not reach the database: %q", state.statements())
	}
}

// TestWiperReachesTheConnectionItIsNamed: migrate:fresh passes --database
// through, and a wipe that fell back to the default for a name it does not
// know would drop the tables of a database nobody named.
func TestWiperReachesTheConnectionItIsNamed(t *testing.T) {
	handle, state := newFakeDB()
	wipe := database.Wiper(database.NewSingleConnectionResolver(database.Wrap(handle, database.DialectSQLite), ""))

	if err := wipe(context.Background(), "elsewhere"); err == nil {
		t.Fatal("a wipe of a connection nobody registered succeeded")
	}
	if got := state.statements(); len(got) != 0 {
		t.Fatalf("a refused wipe reached the database: %q", got)
	}

	if err := wipe(context.Background(), ""); err != nil {
		t.Fatalf("wipe of the default connection: %v", err)
	}
	if !state.sawStatement("select 'main'") {
		t.Fatalf("the wipe did not ask the default connection for the schemas it searches: %q", state.statements())
	}
}
