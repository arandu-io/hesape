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
	"github.com/arandu-io/hesape/notifications"
)

// NotificationTableCommand writes the migration that creates the notifications
// table.
//
// It is Illuminate\Notifications\Console\NotificationTableCommand, which answers
// to `make:notifications-table` and, in Laravel, also to
// `notifications:table`. Only the first name is here: two names for one command
// is the second way RULE 9 refuses.
//
// It writes a file and runs nothing. A migration that ran itself would be N
// replicas racing each other at boot (RULE 16), and emitting a file is what lets
// somebody read it before it reaches production.
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

// NewNotificationTableCommand returns the command, writing into directory.
//
// It has no PHP counterpart: there the command is constructed by the container
// and finds the migration path through the Application (ADR 0001).
func NewNotificationTableCommand(directory string) *NotificationTableCommand {
	return &NotificationTableCommand{Directory: directory}
}

// Command is the registry entry for make:notifications-table.
//
// It has no PHP counterpart: a Laravel command declares its name in a $signature
// property and is discovered by scanning. A command missing from the registry
// here does not exist, and one in it with a broken Run does not build.
func (c *NotificationTableCommand) Command() console.Command {
	return console.Command{
		Name:        "make:notifications-table",
		Description: "create a migration for the notifications database table",
		Run:         c.Handle,
	}
}

// MigrationTableName is the table the migration creates.
//
// It is NotificationTableCommand::migrationTableName, which is protected there
// and exported here because the name is what a caller checks a generated file
// against.
func (c *NotificationTableCommand) MigrationTableName() string { return notifications.Table }

// MigrationStub is the SQL the migration is written with.
//
// It is NotificationTableCommand::migrationStubFile, which names a stub file
// shipped inside the PHP package. There is no stub file here: the SQL is
// [notifications.Migrations], so the table this writes and the table a Go
// migration creates cannot drift (RULE 9).
func (c *NotificationTableCommand) MigrationStub() string {
	migrations := notifications.Migrations()
	if len(migrations) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("-- Notifications, for notifications.TableStore.\n")
	b.WriteString("--\n")
	b.WriteString("-- The key column is notification_key and not key: KEY is reserved in MySQL.\n")
	b.WriteString("-- tenant is first in the index because every read is scoped by it (RULE 14).\n\n")
	b.WriteString(strings.TrimSpace(migrations[0].Up))
	b.WriteString("\n")
	return b.String()
}

// MigrationName is the file the migration is written as.
//
// It has no PHP counterpart of its own: MigrationGeneratorCommand::
// createBaseMigration builds the same name inline. Stamped with the time so
// migrations sort in the order they were created, which is the order they have
// to run in.
func (c *NotificationTableCommand) MigrationName() string {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return now().UTC().Format("2006_01_02_150405") + "_create_" + c.MigrationTableName() + "_table.sql"
}

// MigrationExists reports whether a migration for the table is already there.
//
// It is MigrationGeneratorCommand::migrationExists, which
// NotificationTableCommand inherits. The glob is on the suffix, so a migration
// written yesterday under a different timestamp still counts.
func (c *NotificationTableCommand) MigrationExists() (bool, error) {
	matches, err := filepath.Glob(filepath.Join(c.directory(), "*_create_"+c.MigrationTableName()+"_table.sql"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// Handle writes the migration.
//
// It is MigrationGeneratorCommand::handle, which NotificationTableCommand
// inherits: an existing migration is an error rather than an overwrite, because
// the one that is there may already have run somewhere.
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

	if _, err := file.WriteString(c.MigrationStub()); err != nil {
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
