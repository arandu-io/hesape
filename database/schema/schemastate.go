package schema

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// SchemaState is the pair of operations behind `schema:dump` and the squashed
// migration:
// write the whole schema to a file, and load one back. Both shell out to the
// engine's own tool -- mysqldump, pg_dump, sqlite3 -- because no driver can
// produce a dump another server will accept.
//
// Dump and Load are the interface; everything else is BaseSchemaState, which a
// driver's state embeds. The process factory is a field so a test can substitute
// a command that runs nothing.
type SchemaState interface {
	// Dump writes the connection's schema to path.
	Dump(ctx context.Context, g auth.Grant, connection Connection, path string) error

	// Load runs the schema file at path against the connection.
	Load(ctx context.Context, g auth.Grant, path string) error
}

// The three concrete states implement the contract, checked here rather than
// at the first call site, so a state missing Dump or Load fails to compile
// instead of only failing when something tries to use it.
var (
	_ SchemaState = (*MySqlSchemaState)(nil)
	_ SchemaState = (*PostgresSchemaState)(nil)
	_ SchemaState = (*SqliteSchemaState)(nil)
)

// ProcessFactory builds the command a schema state runs.
//
// It is a named type rather than a bare signature because three schema states
// and their tests pass it around, and because it is the seam a test uses to
// substitute a command that touches no database.
type ProcessFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// BaseSchemaState is the half of a SchemaState no driver disagrees about: the
// connection, the migration table's name, the process factory and the output
// handler. A driver's schema state embeds it and adds Dump and Load.
type BaseSchemaState struct {
	connection     Connection
	migrationTable string
	processFactory ProcessFactory
	output         func(line string)
}

// NewBaseSchemaState builds a BaseSchemaState for connection, with the
// migration table named "migrations" until WithMigrationTable overrides it. A
// nil processFactory defaults to exec.CommandContext, and the output handler
// defaults to one that discards every line.
func NewBaseSchemaState(connection Connection, processFactory ProcessFactory) *BaseSchemaState {
	if processFactory == nil {
		processFactory = exec.CommandContext
	}
	return &BaseSchemaState{
		connection:     connection,
		migrationTable: "migrations",
		processFactory: processFactory,
		output:         func(string) {},
	}
}

// MakeProcess builds the *exec.Cmd for name and args through the installed
// process factory.
//
// It takes the program and its arguments apart rather than a shell string,
// because a shell string assembled from a database configuration is an
// injection with a familiar shape.
func (s *BaseSchemaState) MakeProcess(ctx context.Context, name string, args ...string) *exec.Cmd {
	return s.processFactory(ctx, name, args...)
}

// HasMigrationTable reports whether the connection's migration table exists.
func (s *BaseSchemaState) HasMigrationTable(ctx context.Context, g auth.Grant) (bool, error) {
	return NewBuilder(s.connection).HasTable(ctx, g, s.migrationTable)
}

// GetMigrationTable returns the migration table's name, with the connection's
// table prefix applied.
func (s *BaseSchemaState) GetMigrationTable() string {
	return s.connection.GetTablePrefix() + s.migrationTable
}

// WithMigrationTable sets the migration table's name and returns s.
func (s *BaseSchemaState) WithMigrationTable(table string) *BaseSchemaState {
	s.migrationTable = table
	return s
}

// HandleOutputUsing installs output as the handler for each line the
// underlying command writes; a nil output installs one that discards
// everything.
func (s *BaseSchemaState) HandleOutputUsing(output func(line string)) *BaseSchemaState {
	if output == nil {
		output = func(string) {}
	}
	s.output = output
	return s
}

// Output hands one line to the installed output handler. The handler is an
// unexported field holding a function, reachable only from this package, so
// the call needs a name outside it.
func (s *BaseSchemaState) Output(line string) { s.output(line) }

// GetConnection returns the connection the state works against.
func (s *BaseSchemaState) GetConnection() Connection { return s.connection }

// processEnvironment builds the environment a dump or load command needs: the
// values it must be told without them appearing on a command line.
//
// The ordinary arguments are passed as arguments -- no shell is involved at all,
// so there is nothing to interpolate and nothing to quote -- and the environment
// is used only for what the client programs read from it, which is the password.
// PGPASSWORD and MYSQL_PWD are how pg_dump and mysqldump take one without it
// being visible in `ps`.
//
// The parent environment is inherited, because these programs also read PGHOST,
// PGSSLMODE, HOME and the rest, and a schema dump that ignored the operator's
// environment would fail in ways that have nothing to do with the schema.
func (s *BaseSchemaState) processEnvironment(extra map[string]string) []string {
	environment := os.Environ()

	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		environment = append(environment, key+"="+extra[key])
	}
	return environment
}

// configuredHost returns the connection's configured host, taking the first
// entry when it is a comma-separated list.
//
// A read/write cluster is configured with a list of hosts, and a dump is one
// connection to one server, so this takes the first. GetConfig returns a
// string here rather than a list, so the split is done on the way past.
func (s *BaseSchemaState) configuredHost() string {
	host := s.connection.GetConfig("host")
	if first, _, found := strings.Cut(host, ","); found {
		return strings.TrimSpace(first)
	}
	return host
}
