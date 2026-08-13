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

// MySqlSchemaState answers Illuminate\Database\Schema\MySqlSchemaState: the
// SchemaState that shells out to mysqldump and mysql.
//
// The name is the PHP's, capital S and lower q, rather than the MySQL spelling
// the grammar uses. ADR 0044 asks for the Illuminate name; Illuminate is
// inconsistent between MySqlSchemaState and MySqlGrammar on one side and
// SQLiteGrammar on the other, and copying the inconsistency is what makes a
// grep for the PHP class find this file.
type MySqlSchemaState struct {
	*BaseSchemaState
}

// NewMySqlSchemaState answers MySqlSchemaState's inherited constructor.
func NewMySqlSchemaState(connection Connection, processFactory ProcessFactory) *MySqlSchemaState {
	return &MySqlSchemaState{BaseSchemaState: NewBaseSchemaState(connection, processFactory)}
}

// autoIncrementState answers the PHP's /\s+AUTO_INCREMENT=[0-9]+/iu.
//
// mysqldump records where each table's counter had got to. Keeping it makes the
// dump differ on every run for reasons that are not schema changes, so the file
// churns in review and two developers who ran no migrations still get a diff.
var autoIncrementState = regexp.MustCompile(`(?i)\s+AUTO_INCREMENT=[0-9]+`)

// Dump answers MySqlSchemaState::dump.
//
// Three steps, as in the PHP: dump the structure, strip the auto-increment
// counters, then append the rows of the migration table so a restored database
// does not replay the migrations it already has.
func (s *MySqlSchemaState) Dump(ctx context.Context, g auth.Grant, connection Connection, path string) error {
	args := append(s.baseDumpCommand(), "--routines", "--result-file="+path, "--no-data")
	if err := s.executeDumpProcess(ctx, args, nil); err != nil {
		return err
	}

	if err := s.removeAutoIncrementingState(path); err != nil {
		return err
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

// removeAutoIncrementingState answers
// MySqlSchemaState::removeAutoIncrementingState.
func (s *MySqlSchemaState) removeAutoIncrementingState(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("schema: reading the MySQL dump at %s: %w", path, err)
	}
	if err := os.WriteFile(path, autoIncrementState.ReplaceAll(contents, nil), 0o600); err != nil {
		return fmt.Errorf("schema: rewriting the MySQL dump at %s: %w", path, err)
	}
	return nil
}

// appendMigrationData answers MySqlSchemaState::appendMigrationData.
func (s *MySqlSchemaState) appendMigrationData(ctx context.Context, path string) error {
	args := append(s.baseDumpCommand(),
		s.GetMigrationTable(),
		"--no-create-info", "--skip-extended-insert", "--skip-routines", "--compact", "--complete-insert")

	var stdout bytes.Buffer
	if err := s.executeDumpProcess(ctx, args, &stdout); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("schema: appending the migration rows to %s: %w", path, err)
	}
	defer file.Close()

	if _, err := file.Write(stdout.Bytes()); err != nil {
		return fmt.Errorf("schema: appending the migration rows to %s: %w", path, err)
	}
	return nil
}

// Load answers MySqlSchemaState::load.
func (s *MySqlSchemaState) Load(ctx context.Context, g auth.Grant, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("schema: reading the MySQL schema file %s: %w", path, err)
	}
	defer file.Close()

	args := append([]string{"mysql"}, s.connectionFlags()...)
	args = append(args, "--database="+s.GetConnection().GetConfig("database"))

	command := s.MakeProcess(ctx, args[0], args[1:]...)
	command.Stdin = file
	command.Env = s.processEnvironment(s.baseVariables())

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("schema: loading %s with mysql: %w: %s", path, err, bytes.TrimSpace(output))
	}
	return nil
}

// baseDumpCommand answers MySqlSchemaState::baseDumpCommand.
//
// --set-gtid-purged=OFF is added only for MySQL. MariaDB's mysqldump has no such
// option and refuses the whole command over it, which is why the PHP asks
// isMaria before adding it rather than letting executeDumpProcess retry.
func (s *MySqlSchemaState) baseDumpCommand() []string {
	args := append([]string{"mysqldump"}, s.connectionFlags()...)
	args = append(args,
		"--no-tablespaces", "--skip-add-locks", "--skip-comments",
		"--skip-set-charset", "--tz-utc", "--column-statistics=0")

	if !s.GetConnection().IsMaria() {
		args = append(args, "--set-gtid-purged=OFF")
	}
	return append(args, s.GetConnection().GetConfig("database"))
}

// connectionFlags answers MySqlSchemaState::connectionString.
//
// A configured Unix socket wins over host and port, because the two cannot both
// be given: mysqldump takes whichever it sees and the other is silently ignored,
// so passing both makes the connection depend on argument order.
//
// The SSL branches of the PHP are not here. They read PDO::MYSQL_ATTR_SSL_*
// out of the driver's options array, which is a PDO constant this ecosystem has
// no equivalent for -- the Go driver takes TLS through a registered config name
// in the DSN instead. An operator who needs a dump over TLS sets it in the
// environment the client already reads, which processEnvironment inherits.
func (s *MySqlSchemaState) connectionFlags() []string {
	connection := s.GetConnection()

	flags := []string{"--user=" + connection.GetConfig("username")}

	if socket := connection.GetConfig("unix_socket"); socket != "" {
		return append(flags, "--socket="+socket)
	}
	return append(flags,
		"--host="+s.configuredHost(),
		"--port="+connection.GetConfig("port"))
}

// baseVariables answers MySqlSchemaState::baseVariables, reduced to the value
// that must not appear on a command line.
//
// MYSQL_PWD is how the client takes a password without it being visible in
// `ps`. The PHP keeps it out of the command line for the same reason, by way of
// Symfony's ${:VAR} interpolation.
func (s *MySqlSchemaState) baseVariables() map[string]string {
	return map[string]string{"MYSQL_PWD": s.GetConnection().GetConfig("password")}
}

// executeDumpProcess answers MySqlSchemaState::executeDumpProcess: run the
// dump, and drop an option the installed client does not know rather than
// failing over it.
//
// Both options it retries without are recent additions that older and
// alternative clients reject outright. --column-statistics=0 is not understood
// before mysqldump 8.0, and --set-gtid-purged is a MySQL option MariaDB does not
// have; in each case the client exits without writing anything, and the message
// names the option. The PHP recurses with a depth limit of thirty; this drops
// each of the two at most once, which is the same thing with the recursion
// written out, and cannot loop.
func (s *MySqlSchemaState) executeDumpProcess(ctx context.Context, args []string, stdout *bytes.Buffer) error {
	err := s.runDump(ctx, args, stdout)
	if err == nil {
		return nil
	}

	for _, option := range []struct {
		flag    string
		mention []string
	}{
		{"--column-statistics=0", []string{"column-statistics", "column_statistics"}},
		{"--set-gtid-purged=OFF", []string{"set-gtid-purged"}},
	} {
		if !mentionsAny(err.Error(), option.mention) {
			continue
		}
		if stdout != nil {
			stdout.Reset()
		}
		args = withoutArg(args, option.flag)
		if err = s.runDump(ctx, args, stdout); err == nil {
			return nil
		}
	}
	return err
}

// runDump runs one mysqldump, sending its standard output to the buffer when
// one was given and letting --result-file place it otherwise.
func (s *MySqlSchemaState) runDump(ctx context.Context, args []string, stdout *bytes.Buffer) error {
	var stderr bytes.Buffer

	command := s.MakeProcess(ctx, args[0], args[1:]...)
	command.Stderr = &stderr
	command.Env = s.processEnvironment(s.baseVariables())
	if stdout != nil {
		command.Stdout = stdout
	}

	if err := command.Run(); err != nil {
		return fmt.Errorf("schema: mysqldump: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	s.Output(stderr.String())
	return nil
}

// mentionsAny answers Str::contains($message, $needles).
func mentionsAny(message string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

// withoutArg answers the PHP's str_replace on the assembled command line, done
// on the argument list instead -- which cannot accidentally match a substring
// of a password or a table name the way a search over the whole line can.
func withoutArg(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == flag {
			continue
		}
		out = append(out, arg)
	}
	return out
}
