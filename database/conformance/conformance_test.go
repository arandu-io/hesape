package conformance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// What is testable here without a server, and what is not.
//
// This package is a harness: what it asserts is the behaviour of three engines,
// and that cannot be measured by anything but those engines. Run is covered by
// the connectors that call it against a live server.
//
// What is measurable offline is the harness's own contract -- the skip that lets
// `go test ./...` pass with nothing installed, the table prefix that keeps it
// away from real data, the migration it applies, and the wiring it builds to
// apply it with. Each of those has failed before in a way a server would not
// have caught, because a suite that never ran reports nothing.

// stubDriver is a database/sql driver that is registered and never connected.
//
// sql.Open does not dial, so a handle over this is enough to build the values
// the wiring is made of. Any attempt to actually use it fails, which is the
// point: nothing below this line may reach a server.
type stubDriver struct{}

func (stubDriver) Open(string) (driver.Conn, error) {
	return nil, driver.ErrBadConn
}

func init() { sql.Register("arandu_conformance_stub", stubDriver{}) }

// newStubDB returns a database handle that is valid and unusable.
func newStubDB(t *testing.T) *database.DB {
	t.Helper()

	sqldb, err := sql.Open("arandu_conformance_stub", "")
	if err != nil {
		t.Fatalf("opening the stub driver: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	return database.Wrap(sqldb, database.DialectSQLite)
}

// TestRunSkipsWithoutADSN is the contract that lets this package be imported by
// a connector whose server is not installed.
//
// Without it `go test ./...` fails on a developer machine with no MySQL, and the
// suite gets deleted rather than fixed. The skip has to happen before anything
// is opened, so the assertion is that the subtest was skipped and not that it
// merely passed.
func TestRunSkipsWithoutADSN(t *testing.T) {
	var skipped, returned bool

	t.Run("no dsn", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		Run(t, database.DialectMySQL, "arandu_conformance_stub", "")
		returned = true
	})

	if returned {
		t.Fatal("Run returned instead of skipping, so a machine with no server fails the build")
	}
	if !skipped {
		t.Fatal("Run did not skip on an empty DSN")
	}
}

// TestTableNamesArePrefixed: the suite drops and creates the tables it names, and
// it is pointed at a live database by an environment variable. A name without
// the prefix is somebody's data.
func TestTableNamesArePrefixed(t *testing.T) {
	for _, name := range []string{"noop", "widget", "rate", "moment", "ledger"} {
		got := table(name)
		if !strings.HasPrefix(got, "arandu_conformance_") {
			t.Errorf("table(%q) = %q, and this suite drops what it names", name, got)
		}
		if got == name {
			t.Errorf("table(%q) returned the bare name", name)
		}
	}
}

// recordingConnection is a migrations.Connection that records the statements it
// was given and runs none of them.
type recordingConnection struct{ statements []string }

func (c *recordingConnection) GetName() string { return conformanceConnection }

func (c *recordingConnection) Schema() *schema.Builder { return nil }

func (c *recordingConnection) Statement(_ context.Context, sql string, _ []any) (bool, error) {
	c.statements = append(c.statements, sql)
	return true, nil
}

func (c *recordingConnection) Select(context.Context, string, []any) ([]map[string]any, error) {
	return nil, nil
}

// TestTheSuitesMigrationCreatesAndDropsThePrefixedTable.
//
// This migration is the statement the suite exists for: MySQL refused
// `id TEXT PRIMARY KEY`, and nothing caught it because nothing spoke to MySQL.
// What is checkable without a server is that the statement still declares the
// key with the portable type rather than TEXT, and still names the prefixed
// table -- a change to either turns the live run into a test of something else.
func TestTheSuitesMigrationCreatesAndDropsThePrefixedTable(t *testing.T) {
	conn := &recordingConnection{}
	migration := conformanceMigration{}

	if err := migration.Up(context.Background(), conn); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := migration.Down(context.Background(), conn); err != nil {
		t.Fatalf("Down: %v", err)
	}

	if len(conn.statements) != 2 {
		t.Fatalf("the migration ran %d statements: %#v", len(conn.statements), conn.statements)
	}

	up, down := conn.statements[0], conn.statements[1]
	if !strings.Contains(up, table("noop")) {
		t.Errorf("Up names %q, want the prefixed table", up)
	}
	if !strings.Contains(up, database.KeyText) {
		t.Errorf("Up declares the key as %q, and TEXT in a key is what this suite was written for", up)
	}
	if !strings.Contains(down, "DROP TABLE") || !strings.Contains(down, table("noop")) {
		t.Errorf("Down is %q, want a drop of the prefixed table", down)
	}
}

// TestTheSuitesMigrationIsRegisteredUnderItsOwnPath.
//
// The path is what keeps a live run from applying an application's migrations
// along with this one. Registered under the default group instead, the suite
// would apply whatever the binary linked -- against somebody's database.
func TestTheSuitesMigrationIsRegisteredUnderItsOwnPath(t *testing.T) {
	own := migrations.Registered(conformanceMigrationPath)
	if len(own) != 1 {
		t.Fatalf("%d migrations registered under %q, want exactly this suite's own",
			len(own), conformanceMigrationPath)
	}
	if got := own[0].GetName(); got != (conformanceMigration{}).GetName() {
		t.Fatalf("the migration registered under %q is %q", conformanceMigrationPath, got)
	}

	for _, migration := range migrations.Registered(migrations.DefaultPath) {
		if migration.GetName() == (conformanceMigration{}).GetName() {
			t.Fatal("the suite's migration is also in the default group, so an application run would apply it")
		}
	}
}

// TestNewMigratorResolvesTheSuitesOwnConnection.
//
// The migrations component reaches a connection through a resolver, and a
// resolver with no default answers the empty name -- which resolves to nothing
// and fails on the first statement, against a live server, in a suite whose
// whole job is to say which engine is at fault.
func TestNewMigratorResolvesTheSuitesOwnConnection(t *testing.T) {
	migrator := newMigrator(database.DialectSQLite, newStubDB(t))

	if migrator == nil {
		t.Fatal("newMigrator answered nothing")
	}
	if migrator.GetRepository() == nil {
		t.Fatal("the migrator has no repository, so nothing would record what it applied")
	}

	// The migrator's own connection name is left empty, and that is what makes
	// the resolver's default the answer: every migration in this suite names no
	// connection, so every one of them resolves through the empty string.
	if got := migrator.GetConnection(); got != "" {
		t.Fatalf("the migrator names %q as its own connection, and this suite sets none", got)
	}

	conn, err := migrator.ResolveConnection("")
	if err != nil {
		t.Fatalf("resolving the connection a migration that names none runs on: %v", err)
	}
	if conn == nil {
		t.Fatal("a migration that names no connection resolved to nothing")
	}
	if got := conn.GetName(); got != conformanceConnection {
		t.Fatalf("the empty name resolved to %q, want the suite's own connection", got)
	}

	named, err := migrator.ResolveConnection(conformanceConnection)
	if err != nil {
		t.Fatalf("resolving %q: %v", conformanceConnection, err)
	}
	if named == nil {
		t.Fatalf("%q resolved to nothing", conformanceConnection)
	}
}
