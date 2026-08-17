package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/notifications"
)

// NotificationTableCommand writes the migration that creates the notifications
// table.
//
// It answers to `make:notifications-table`, and to that name only: two names
// for one command is two things to keep in step.
//
// It writes a file and runs nothing. A migration that ran itself would be N
// replicas racing each other at boot, and emitting a file is what lets somebody
// read it before it reaches production.
//
// A project that keeps its migrations as Go values passes
// [notifications.Migrations] to database.Migrate instead and never runs this.
// The two do not diverge: the SQL this writes is read off that.
type NotificationTableCommand struct {
	// Directory is where the migration is written. Empty means the working
	// directory's database/migrations, which is where the rest of them live.
	Directory string

	// Now returns the timestamp the file is named with. Nil means the process
	// clock, and a test passes a fixed one.
	Now func() time.Time
}

// NewNotificationTableCommand returns the command, writing into directory. An
// empty directory means database/migrations under the working directory.
func NewNotificationTableCommand(directory string) *NotificationTableCommand {
	return &NotificationTableCommand{Directory: directory}
}

// Command is the registry entry for make:notifications-table.
//
// A command is registered rather than discovered by scanning: one missing from
// the registry does not exist, and one in it with a broken Run does not build.
func (c *NotificationTableCommand) Command() console.Command {
	return console.Command{
		Name:        "make:notifications-table",
		Description: "create a migration for the notifications database table",
		Run:         c.Handle,
	}
}

// MigrationTableName is the table the migration creates. It is exported because
// the name is what a caller checks a generated file against.
func (c *NotificationTableCommand) MigrationTableName() string { return notifications.Table }

// MigrationStub is the SQL the migration is written with.
//
// It is read off [notifications.Migrations] rather than a stub file of its own,
// so the table this writes and the table a Go migration creates cannot drift.
// The statements come back from migrations.UpStatements, which runs the
// migration against a connection that records rather than executes, so nothing
// here reaches a server.
func (c *NotificationTableCommand) MigrationStub() (string, error) {
	declared := notifications.Migrations()
	if len(declared) == 0 {
		return "", nil
	}

	statements, err := migrations.UpStatements(context.Background(), declared[0])
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("-- Notifications, for notifications.TableStore.\n")
	b.WriteString("--\n")
	b.WriteString("-- The key column is notification_key and not key: KEY is reserved in MySQL.\n")
	b.WriteString("-- tenant is first in the index because every read is scoped by it.\n\n")
	for _, statement := range statements {
		b.WriteString(strings.TrimSpace(statement))
		// The file is read by a person and replayed by whatever applies it, so
		// every statement carries the terminator the migration itself does not
		// need.
		b.WriteString(";\n")
	}
	return b.String(), nil
}

// MigrationName is the file the migration is written as. It is stamped with the
// time so migrations sort in the order they were created, which is the order
// they have to run in.
func (c *NotificationTableCommand) MigrationName() string {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return now().UTC().Format("2006_01_02_150405") + "_create_" + c.MigrationTableName() + "_table.sql"
}

// MigrationExists reports whether a migration for the table is already there.
//
// The glob is on the suffix, so a migration written yesterday under a different
// timestamp still counts.
func (c *NotificationTableCommand) MigrationExists() (bool, error) {
	matches, err := filepath.Glob(filepath.Join(c.directory(), "*_create_"+c.MigrationTableName()+"_table.sql"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// Handle writes the migration.
//
// An existing migration is an error rather than an overwrite, because the one
// that is there may already have run somewhere.
func (c *NotificationTableCommand) Handle(_ context.Context, o *console.IO) error {
	flags := o.Flags()
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	exists, err := c.MigrationExists()
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("notifications: a migration for the %s table already exists in %s", c.MigrationTableName(), c.directory())
	}

	stub, err := c.MigrationStub()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(c.directory(), 0o755); err != nil {
		return err
	}

	path := filepath.Join(c.directory(), c.MigrationName())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("notifications: %s already exists", path)
		}
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(stub); err != nil {
		return err
	}

	o.Info("Migration created: %s", path)
	return nil
}

// directory is where the migration goes, with the default filled in.
func (c *NotificationTableCommand) directory() string {
	if c.Directory == "" {
		return filepath.Join("database", "migrations")
	}
	return c.Directory
}
