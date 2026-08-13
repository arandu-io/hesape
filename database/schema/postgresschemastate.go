package schema

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// PostgresSchemaState answers
// Illuminate\Database\Schema\PostgresSchemaState: the SchemaState that shells
// out to pg_dump, pg_restore and psql.
type PostgresSchemaState struct {
	*BaseSchemaState
}

// NewPostgresSchemaState answers PostgresSchemaState's inherited constructor.
func NewPostgresSchemaState(connection Connection, processFactory ProcessFactory) *PostgresSchemaState {
	return &PostgresSchemaState{BaseSchemaState: NewBaseSchemaState(connection, processFactory)}
}

// Dump answers PostgresSchemaState::dump.
//
// Two pg_dump runs into one file: the schema, then the rows of the migration
// table appended to it. The second is what stops a restored database from
// replaying every migration it already has.
//
// --no-owner and --no-acl are the ones worth naming. Without them the dump
// carries the roles of the machine it came from, and loading it on another
// machine fails on a role that does not exist there -- which makes the squashed
// schema unusable exactly where it is most wanted, on a fresh checkout.
func (s *PostgresSchemaState) Dump(ctx context.Context, g auth.Grant, connection Connection, path string) error {
	schemaOnly := append(s.baseDumpCommand(), "--schema-only")
	if err := s.dumpInto(ctx, path, false, schemaOnly); err != nil {
		return err
	}

	hasTable, err := s.HasMigrationTable(ctx, g)
	if err != nil {
		return err
	}
	if !hasTable {
		return nil
	}

	dataOnly := append(s.baseDumpCommand(), "-t", s.GetMigrationTable(), "--data-only")
	return s.dumpInto(ctx, path, true, dataOnly)
}

// dumpInto runs one pg_dump and sends its standard output to path, truncating
// or appending as the PHP's `>` and `>>` do.
func (s *PostgresSchemaState) dumpInto(ctx context.Context, path string, appending bool, args []string) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appending {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fmt.Errorf("schema: writing the Postgres dump to %s: %w", path, err)
	}
	defer file.Close()

	var stderr bytes.Buffer
	command := s.MakeProcess(ctx, args[0], args[1:]...)
	command.Stdout = file
	command.Stderr = &stderr
	command.Env = s.processEnvironment(s.baseVariables())

	if err := command.Run(); err != nil {
		return fmt.Errorf("schema: pg_dump into %s: %w: %s", path, err, bytes.TrimSpace(stderr.Bytes()))
	}
	s.Output(stderr.String())
	return nil
}

// Load answers PostgresSchemaState::load.
//
// The tool is chosen by the file's extension, as the PHP chooses it: a .sql file
// is plain text and goes through psql, anything else is pg_dump's own archive
// format and goes through pg_restore. Handing one to the other fails with a
// message about the file being corrupt, which is a misleading thing to read
// when the file is fine and only the reader is wrong.
func (s *PostgresSchemaState) Load(ctx context.Context, g auth.Grant, path string) error {
	var args []string

	if strings.HasSuffix(path, ".sql") {
		args = append([]string{"psql", "--file=" + path}, s.connectionFlags()...)
	} else {
		args = append([]string{"pg_restore", "--no-owner", "--no-acl", "--clean", "--if-exists"}, s.connectionFlags()...)
		args = append(args, path)
	}

	command := s.MakeProcess(ctx, args[0], args[1:]...)
	command.Env = s.processEnvironment(s.baseVariables())

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("schema: loading %s with %s: %w: %s", path, args[0], err, bytes.TrimSpace(output))
	}
	return nil
}

// GetMigrationTable answers PostgresSchemaState::getMigrationTable, which
// qualifies the table with its schema.
//
// Postgres needs the qualification because pg_dump's -t matches against the
// search path, and a migrations table in a schema that is not first on that
// path would be dumped as nothing at all -- silently, since matching no table
// is not an error.
func (s *PostgresSchemaState) GetMigrationTable() string {
	schema, table, err := NewBuilder(s.GetConnection()).ParseSchemaAndTable(s.migrationTable, "public")
	if err != nil || schema == "" {
		return s.BaseSchemaState.GetMigrationTable()
	}
	return schema + "." + s.GetConnection().GetTablePrefix() + table
}

// baseDumpCommand answers PostgresSchemaState::baseDumpCommand.
func (s *PostgresSchemaState) baseDumpCommand() []string {
	return append([]string{"pg_dump", "--no-owner", "--no-acl"}, s.connectionFlags()...)
}

// connectionFlags is the host, port, user and database every one of these
// commands takes. The PHP writes them into the shell string with ${:VAR}
// placeholders; they are plain arguments here, because no shell parses them.
func (s *PostgresSchemaState) connectionFlags() []string {
	connection := s.GetConnection()
	return []string{
		"--host=" + s.configuredHost(),
		"--port=" + connection.GetConfig("port"),
		"--username=" + connection.GetConfig("username"),
		"--dbname=" + connection.GetConfig("database"),
	}
}

// baseVariables answers PostgresSchemaState::baseVariables, reduced to the one
// value that has to travel in the environment rather than on the command line.
//
// PGPASSWORD is read by every libpq client. A password passed as an argument
// would be readable by every process on the machine through `ps`, which is the
// reason the PHP keeps it out of the command line too.
func (s *PostgresSchemaState) baseVariables() map[string]string {
	return map[string]string{"PGPASSWORD": s.GetConnection().GetConfig("password")}
}
