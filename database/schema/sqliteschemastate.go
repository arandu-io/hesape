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

// SqliteSchemaState answers Illuminate\Database\Schema\SqliteSchemaState: the
// SchemaState that shells out to the sqlite3 command.
//
// It is spelled the PHP's way. Illuminate writes SQLiteBuilder and
// SQLiteGrammar with three capitals and SqliteSchemaState with one, and ADR 0044
// says the name is the Illuminate name -- so the inconsistency is carried across
// rather than tidied up, because a reader who greps for the PHP class has to
// find it.
type SqliteSchemaState struct {
	*BaseSchemaState
}

// NewSqliteSchemaState answers SqliteSchemaState's inherited constructor.
func NewSqliteSchemaState(connection Connection, processFactory ProcessFactory) *SqliteSchemaState {
	return &SqliteSchemaState{BaseSchemaState: NewBaseSchemaState(connection, processFactory)}
}

// sqliteInternalTables answers the PHP's
// /CREATE TABLE sqlite_.+?\);[\r\n]+/is.
//
// SQLite's own bookkeeping tables come back from .schema and must not go into
// the dump: sqlite_sequence is recreated by the engine the first time an
// AUTOINCREMENT column is written, and a dump that tries to create it fails on
// load.
var sqliteInternalTables = regexp.MustCompile(`(?is)CREATE TABLE sqlite_.+?\);[\r\n]+`)

// sqliteMigrationRow answers the PHP's /^\s*(--|INSERT\s)/iu: the lines of a
// .dump worth keeping, which are the inserts and the comments around them.
var sqliteMigrationRow = regexp.MustCompile(`(?i)^\s*(--|INSERT\s)`)

// Dump answers SqliteSchemaState::dump.
//
// Two commands, as in the PHP. The first writes the schema; the second appends
// the rows of the migration table, so that a database restored from the dump
// knows which migrations it already has and `aru migrate` does not try to run
// them again. Without the second half the squashed schema is correct and the
// migrator immediately replays every migration on top of it.
//
// The PHP redirects with a shell (`> $path`); this opens the file and wires the
// process's standard output to it. The dump has to be written by a program whose
// arguments came out of a database configuration, and assembling a shell line
// from that configuration is the injection the doc comment on MakeProcess is
// about.
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

// appendMigrationData answers SqliteSchemaState::appendMigrationData.
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

// Load answers SqliteSchemaState::load.
//
// An in-memory database has no file for sqlite3 to open, and a second process
// opening ":memory:" would get its own empty database and load the schema into
// something nobody can see. So the statements are run through the connection
// that already holds the database, which is what the PHP's getPdo()->exec does.
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
// answers what it wrote to standard output.
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

// isSqliteMemory answers the PHP's three-way check for an in-memory database.
func isSqliteMemory(database string) bool {
	return database == ":memory:" ||
		strings.Contains(database, "?mode=memory") ||
		strings.Contains(database, "&mode=memory")
}

// splitLines answers the PHP's preg_split("/\r\n|\n|\r/", ...).
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}
