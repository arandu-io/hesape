package schema_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/schema"
)

// configuredConn is a schema.Connection carrying a database configuration, so a
// schema state can be asked what command it would build from one.
type configuredConn struct {
	*conn
	config map[string]string
	driver string
	maria  bool
}

func newConfiguredConn(driver string, config map[string]string) *configuredConn {
	return &configuredConn{conn: newConn(), config: config, driver: driver}
}

func (c *configuredConn) GetConfig(option string) string { return c.config[option] }
func (c *configuredConn) GetDriverName() string          { return c.driver }
func (c *configuredConn) IsMaria() bool                  { return c.maria }

// recorder is a schema.ProcessFactory that records every command it was asked
// for and runs a harmless stand-in instead.
//
// The stand-in is `cat` reading nothing, which exits zero and writes the given
// output. What is under test is the command line each state assembles and where
// it sends the bytes -- not whether pg_dump is installed on the machine running
// the tests, which it usually is not.
type recorder struct {
	commands [][]string
	stdout   string
	envs     [][]string
}

func (r *recorder) factory(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.commands = append(r.commands, append([]string{name}, args...))

	command := exec.CommandContext(ctx, "printf", "%s", r.stdout)
	r.envs = append(r.envs, nil)
	return command
}

func (r *recorder) last() []string {
	if len(r.commands) == 0 {
		return nil
	}
	return r.commands[len(r.commands)-1]
}

func joined(command []string) string { return strings.Join(command, " ") }

// TestSqliteSchemaStateDumpStripsInternalTables is the reason the dump is
// filtered at all: sqlite_sequence comes back from .schema, and a dump that
// tries to create it fails on load, because the engine owns that table.
func TestSqliteSchemaStateDumpStripsInternalTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")

	factory := &recorder{stdout: "CREATE TABLE sqlite_sequence(name,seq);\nCREATE TABLE users (id integer);\n"}
	connection := newConfiguredConn("sqlite", map[string]string{"database": "/tmp/app.sqlite"})
	connection.scalar = nil

	state := schema.NewSqliteSchemaState(connection, factory.factory)

	if err := state.Dump(context.Background(), grant(), connection, path); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the dump: %v", err)
	}

	if strings.Contains(string(written), "sqlite_sequence") {
		t.Errorf("the dump kept SQLite's own table:\n%s", written)
	}
	if !strings.Contains(string(written), "CREATE TABLE users") {
		t.Errorf("the dump lost the application's table:\n%s", written)
	}

	if got := joined(factory.commands[0]); !strings.Contains(got, "sqlite3 /tmp/app.sqlite .schema --indent") {
		t.Errorf("command was %q, want the sqlite3 .schema dot-command", got)
	}
}

// TestSqliteSchemaStateLoadUsesTheConnectionInMemory is the branch that would
// otherwise load the schema into a database nobody can see: a second process
// opening ":memory:" gets its own.
func TestSqliteSchemaStateLoadUsesTheConnectionInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := os.WriteFile(path, []byte("create table users (id integer);"), 0o600); err != nil {
		t.Fatalf("writing the schema file: %v", err)
	}

	factory := &recorder{}
	connection := newConfiguredConn("sqlite", map[string]string{"database": ":memory:"})

	state := schema.NewSqliteSchemaState(connection, factory.factory)

	if err := state.Load(context.Background(), grant(), path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(factory.commands) != 0 {
		t.Errorf("an in-memory load shelled out to %v, and the second process cannot see the database", factory.commands)
	}
	if len(connection.statements) != 1 || !strings.Contains(connection.statements[0], "create table users") {
		t.Errorf("statements were %v, want the schema file run through the connection", connection.statements)
	}
}

// TestPostgresDumpCommandCarriesNoOwnerAndNoACL pins the two flags that decide
// whether the dump loads anywhere but the machine it came from.
func TestPostgresDumpCommandCarriesNoOwnerAndNoACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")

	factory := &recorder{}
	connection := newConfiguredConn("pgsql", map[string]string{
		"host": "db.internal", "port": "5432",
		"username": "app", "password": "s3cret", "database": "acme",
	})

	state := schema.NewPostgresSchemaState(connection, factory.factory)

	if err := state.Dump(context.Background(), grant(), connection, path); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	got := joined(factory.commands[0])
	for _, want := range []string{
		"pg_dump", "--no-owner", "--no-acl", "--schema-only",
		"--host=db.internal", "--port=5432", "--username=app", "--dbname=acme",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("command %q is missing %q", got, want)
		}
	}

	// The password is the one value that must not be an argument: `ps` shows
	// arguments to every user on the machine.
	if strings.Contains(got, "s3cret") {
		t.Errorf("the password reached the command line: %q", got)
	}
}

// TestPostgresLoadPicksTheToolByExtension pins the branch that chooses psql or
// pg_restore by file extension: getting it wrong reports a corrupt file when
// the file is fine.
func TestPostgresLoadPicksTheToolByExtension(t *testing.T) {
	connection := newConfiguredConn("pgsql", map[string]string{"database": "acme", "username": "app"})

	for path, want := range map[string]string{
		"/tmp/schema.sql":  "psql",
		"/tmp/schema.dump": "pg_restore",
	} {
		factory := &recorder{}
		state := schema.NewPostgresSchemaState(connection, factory.factory)

		if err := state.Load(context.Background(), grant(), path); err != nil {
			t.Fatalf("Load(%s): %v", path, err)
		}
		if got := factory.last()[0]; got != want {
			t.Errorf("Load(%s) ran %q, want %q", path, got, want)
		}
	}
}

// TestMySqlDumpOmitsGtidPurgedOnMaria is the option MariaDB's client does not
// have, and it refuses the whole command over it rather than ignoring it.
func TestMySqlDumpOmitsGtidPurgedOnMaria(t *testing.T) {
	connection := newConfiguredConn("mysql", map[string]string{
		"host": "db.internal", "port": "3306",
		"username": "app", "password": "s3cret", "database": "acme",
	})

	for _, maria := range []bool{false, true} {
		connection.maria = maria

		factory := &recorder{}
		state := schema.NewMySqlSchemaState(connection, factory.factory)
		path := filepath.Join(t.TempDir(), "schema.sql")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("seeding the dump file: %v", err)
		}

		if err := state.Dump(context.Background(), grant(), connection, path); err != nil {
			t.Fatalf("Dump(maria=%v): %v", maria, err)
		}

		got := joined(factory.commands[0])
		has := strings.Contains(got, "--set-gtid-purged=OFF")
		if has == maria {
			t.Errorf("maria=%v produced %q; --set-gtid-purged=OFF is MySQL only", maria, got)
		}
		if strings.Contains(got, "s3cret") {
			t.Errorf("the password reached the command line: %q", got)
		}
	}
}

// TestMySqlDumpPrefersTheSocket is the pair that cannot both be given: mysqldump
// takes whichever it sees, so passing both makes the connection depend on
// argument order.
func TestMySqlDumpPrefersTheSocket(t *testing.T) {
	connection := newConfiguredConn("mysql", map[string]string{
		"host": "db.internal", "port": "3306", "username": "app",
		"database": "acme", "unix_socket": "/var/run/mysqld.sock",
	})

	factory := &recorder{}
	state := schema.NewMySqlSchemaState(connection, factory.factory)
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("seeding the dump file: %v", err)
	}

	if err := state.Dump(context.Background(), grant(), connection, path); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	got := joined(factory.commands[0])
	if !strings.Contains(got, "--socket=/var/run/mysqld.sock") {
		t.Errorf("command %q did not use the configured socket", got)
	}
	if strings.Contains(got, "--host=") {
		t.Errorf("command %q passed both a socket and a host", got)
	}
}

// TestRefreshDatabaseFileRefusesOtherDrivers checks the guard RefreshDatabaseFile
// must make explicitly, because it lives on the one Builder rather than on a
// SQLite-only type: truncating a file is not a statement the database can
// refuse, so nothing downstream would catch it.
func TestRefreshDatabaseFileRefusesOtherDrivers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.sqlite")
	if err := os.WriteFile(path, []byte("not empty"), 0o600); err != nil {
		t.Fatalf("seeding the database file: %v", err)
	}

	postgres := schema.NewBuilder(newConfiguredConn("pgsql", map[string]string{"database": path}))
	if err := postgres.RefreshDatabaseFile(context.Background(), grant(), path); err == nil {
		t.Fatal("RefreshDatabaseFile emptied a file on a Postgres connection")
	}

	if contents, _ := os.ReadFile(path); string(contents) != "not empty" {
		t.Errorf("the file was truncated anyway: %q", contents)
	}

	sqlite := schema.NewBuilder(newConfiguredConn("sqlite", map[string]string{"database": path}))
	if err := sqlite.RefreshDatabaseFile(context.Background(), grant(), ""); err != nil {
		t.Fatalf("RefreshDatabaseFile: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the database file: %v", err)
	}
	if len(contents) != 0 {
		t.Errorf("the file still holds %q", contents)
	}
}

// TestRefreshDatabaseFileRefusesInMemory: there is no file to empty, and the
// configured path would be ":memory:" -- a name os.WriteFile would happily
// create in the working directory.
func TestRefreshDatabaseFileRefusesInMemory(t *testing.T) {
	builder := schema.NewBuilder(newConfiguredConn("sqlite", map[string]string{"database": ":memory:"}))

	if err := builder.RefreshDatabaseFile(context.Background(), grant(), ""); err == nil {
		t.Fatal("RefreshDatabaseFile accepted an in-memory database")
	}
	if _, err := os.Stat(":memory:"); err == nil {
		t.Error("a file named :memory: was created in the working directory")
	}
}
