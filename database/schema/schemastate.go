package schema

import (
	"context"
	"os/exec"

	"github.com/arandu-io/hesape/auth"
)

// SchemaState answers Illuminate\Database\Schema\SchemaState.
//
// It is the pair of operations behind `schema:dump` and the squashed migration:
// write the whole schema to a file, and load one back. Both shell out to the
// engine's own tool -- mysqldump, pg_dump, sqlite3 -- because no driver can
// produce a dump another server will accept.
//
// PHP declares dump and load abstract and leaves the rest concrete. Go has no
// abstract class, so the two are an interface and the concrete half is
// BaseSchemaState, which a driver's state embeds. Symfony's Process becomes
// os/exec, and the process factory keeps its place so a test can substitute a
// command that runs nothing.
type SchemaState interface {
	// Dump answers SchemaState::dump: it writes the connection's schema to path.
	Dump(ctx context.Context, g auth.Grant, connection Connection, path string) error

	// Load answers SchemaState::load: it runs the schema file at path against
	// the connection.
	Load(ctx context.Context, g auth.Grant, path string) error
}

// BaseSchemaState is the concrete half of Illuminate's abstract SchemaState: the
// connection, the migration table's name, the process factory and the output
// handler. A driver's schema state embeds it and adds Dump and Load.
type BaseSchemaState struct {
	connection     Connection
	migrationTable string
	processFactory func(ctx context.Context, name string, args ...string) *exec.Cmd
	output         func(line string)
}

// NewBaseSchemaState answers SchemaState::__construct.
//
// PHP takes an optional Filesystem, which Go's os package makes unnecessary. A
// nil processFactory takes exec.CommandContext, and a nil output handler
// discards, which is the empty closure the PHP installs.
func NewBaseSchemaState(connection Connection, processFactory func(ctx context.Context, name string, args ...string) *exec.Cmd) *BaseSchemaState {
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

// MakeProcess answers SchemaState::makeProcess.
//
// The PHP builds the command from a shell string; this takes the program and
// its arguments apart, because a shell string assembled from a database
// configuration is an injection with a familiar shape.
func (s *BaseSchemaState) MakeProcess(ctx context.Context, name string, args ...string) *exec.Cmd {
	return s.processFactory(ctx, name, args...)
}

// HasMigrationTable answers SchemaState::hasMigrationTable.
func (s *BaseSchemaState) HasMigrationTable(ctx context.Context, g auth.Grant) (bool, error) {
	return NewBuilder(s.connection).HasTable(ctx, g, s.migrationTable)
}

// GetMigrationTable answers SchemaState::getMigrationTable.
func (s *BaseSchemaState) GetMigrationTable() string {
	return s.connection.GetTablePrefix() + s.migrationTable
}

// WithMigrationTable answers SchemaState::withMigrationTable.
func (s *BaseSchemaState) WithMigrationTable(table string) *BaseSchemaState {
	s.migrationTable = table
	return s
}

// HandleOutputUsing answers SchemaState::handleOutputUsing.
func (s *BaseSchemaState) HandleOutputUsing(output func(line string)) *BaseSchemaState {
	if output == nil {
		output = func(string) {}
	}
	s.output = output
	return s
}

// Output hands one line to the installed output handler. PHP calls the
// callable directly because it is a property; a Go field holding a function is
// reachable only from this package, so the call has a name.
func (s *BaseSchemaState) Output(line string) { s.output(line) }

// GetConnection returns the connection the state works against.
func (s *BaseSchemaState) GetConnection() Connection { return s.connection }
