package schema

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// SqliteSchemaState is the SchemaState that shells out to the sqlite3 command.
type SqliteSchemaState struct {
	*BaseSchemaState
}

// NewSqliteSchemaState builds a SqliteSchemaState for connection, using
// processFactory to build the sqlite3 commands it runs.
func NewSqliteSchemaState(connection Connection, processFactory ProcessFactory) *SqliteSchemaState {
	return &SqliteSchemaState{BaseSchemaState: NewBaseSchemaState(connection, processFactory)}
}

// sqliteInternalTables matches the CREATE TABLE statement for one of SQLite's
// own bookkeeping tables in a .schema dump.
//
// SQLite's own bookkeeping tables come back from .schema and must not go into
// the dump: sqlite_sequence is recreated by the engine the first time an
// AUTOINCREMENT column is written, and a dump that tries to create it fails on
// load.
var sqliteInternalTables = regexp.MustCompile(`(?is)CREATE TABLE sqlite_.+?\);[\r\n]+`)

// sqliteMigrationRow matches the lines of a .dump worth keeping: the inserts
// and the comments around them.
var sqliteMigrationRow = regexp.MustCompile(`(?i)^\s*(--|INSERT\s)`)

// Dump writes the connection's schema, plus the rows of the migration table,
// to path.
//
// Two commands: the first writes the schema; the second appends the rows of
// the migration table, so that a database restored from the dump knows which
// migrations it already has and does not try to run them again. Without the
// second half the squashed schema is correct and the migrator immediately
// replays every migration on top of it.
//
// The dump has to be written by a program whose arguments came out of a
// database configuration, and assembling a shell line from that
// configuration is the injection the doc comment on MakeProcess is about.
func (s *SqliteSchemaState) Dump(ctx context.Context, g auth.Grant, connection Connection, path string) error {
	schema, err := s.runSqlite(ctx, ".schema --indent")
	if err != nil {
		return err
	}

	body := sqliteInternalTables.ReplaceAll(schema, nil)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("schema: writing the SQLite dump to %s: %w", path, err)
	}

	hasTable, err := s.HasMigrationTable(ctx, g)
	if err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	return s.appendMigrationData(ctx, path)
}

// appendMigrationData dumps the migration table with sqlite3 and appends its
// insert statements to the schema file at path.
func (s *SqliteSchemaState) appendMigrationData(ctx context.Context, path string) error {
	dumped, err := s.runSqlite(ctx, fmt.Sprintf(".dump '%s'", s.GetMigrationTable()))
	if err != nil {
		return err
	}

	var kept []string
	for _, line := range splitLines(string(dumped)) {
		if line == "" || !sqliteMigrationRow.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("schema: appending the migration rows to %s: %w", path, err)
	}
	defer file.Close()

	if _, err := file.WriteString(strings.Join(kept, "\n") + "\n"); err != nil {
		return fmt.Errorf("schema: appending the migration rows to %s: %w", path, err)
	}
	return nil
}

// Load runs the schema file at path against the connection.
//
// An in-memory database has no file for sqlite3 to open, and a second process
// opening ":memory:" would get its own empty database and load the schema into
// something nobody can see. So the statements are run through the connection
// that already holds the database.
func (s *SqliteSchemaState) Load(ctx context.Context, g auth.Grant, path string) error {
	database := s.GetConnection().GetConfig("database")

	if isSqliteMemory(database) {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("schema: reading the SQLite schema file %s: %w", path, err)
		}
		return s.GetConnection().Statement(ctx, string(contents))
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("schema: reading the SQLite schema file %s: %w", path, err)
	}
	defer file.Close()

	command := s.MakeProcess(ctx, "sqlite3", database)
	command.Stdin = file
	command.Env = s.processEnvironment(nil)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("schema: loading %s with sqlite3: %w: %s", path, err, bytes.TrimSpace(output))
	}
	return nil
}

// runSqlite runs one sqlite3 dot-command against the configured database and
// returns what it wrote to standard output.
func (s *SqliteSchemaState) runSqlite(ctx context.Context, dotCommand string) ([]byte, error) {
	command := s.MakeProcess(ctx, "sqlite3", s.GetConnection().GetConfig("database"), dotCommand)
	command.Env = s.processEnvironment(nil)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("schema: sqlite3 %s: %w: %s", dotCommand, err, bytes.TrimSpace(stderr.Bytes()))
	}
	s.Output(stdout.String())
	return stdout.Bytes(), nil
}

// isSqliteMemory reports whether database names an in-memory SQLite database:
// the literal ":memory:" or a mode=memory query parameter.
func isSqliteMemory(database string) bool {
	return database == ":memory:" ||
		strings.Contains(database, "?mode=memory") ||
		strings.Contains(database, "&mode=memory")
}

// splitLines splits text on any of \r\n, \n or \r.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}
