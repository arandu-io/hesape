package console

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/filesystem"
)

// MigrationStub is the migration [SessionTableCommand] writes.
//
// Three of its columns stay null here: the handler does not fill user_id,
// ip_address or user_agent, because there is no container to fetch the
// guard and the request out of -- see
// [session.DatabaseSessionHandler]. They are in the table anyway, because an
// application that wants them writes them itself and adding a column later
// costs a second migration.
//
// last_activity is indexed because garbage collection deletes on it, and an
// unindexed sweep of the session table is a full scan on the busiest table in
// the schema.
const MigrationStub = `-- Sessions, for session.DatabaseSessionHandler.
CREATE TABLE IF NOT EXISTS sessions (
    id            VARCHAR(40)  NOT NULL PRIMARY KEY,
    user_id       BIGINT       NULL,
    ip_address    VARCHAR(45)  NULL,
    user_agent    TEXT         NULL,
    payload       TEXT         NOT NULL,
    last_activity BIGINT       NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_user_id_index ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_last_activity_index ON sessions (last_activity);
`

// TableName is the table the stub creates and the handler reads.
const TableName = "sessions"

// SessionTableCommand writes the migration that creates the sessions table,
// registered as `make:session-table`. It writes a file and runs nothing: a
// migration that ran itself would be N replicas racing each other at boot,
// and the whole point of emitting a file is that somebody reads it before
// it reaches production.
type SessionTableCommand struct {
	files     *filesystem.Filesystem
	directory string
	// now is the clock the file name is stamped from. It is a field so a test
	// gets the same name twice.
	now func() time.Time
}

// NewSessionTableCommand returns the command, writing into directory, which
// the caller provides directly: there is no application object here to
// derive a default database path from.
//
// files may be nil, in which case a plain [filesystem.Filesystem] is used.
func NewSessionTableCommand(files *filesystem.Filesystem, directory string) *SessionTableCommand {
	if files == nil {
		files = filesystem.NewFilesystem()
	}
	return &SessionTableCommand{files: files, directory: directory, now: time.Now}
}

// Command returns the name, description and behaviour of this command as a
// value, which is the whole of hesape/console's design: a command missing
// from the registry does not exist, and one in it with a broken Run does
// not build.
//
// Only one name is registered for this command: two names for one command
// are two ways to do one thing.
func (c *SessionTableCommand) Command() console.Command {
	return console.Command{
		Name:        "make:session-table",
		Description: "create a migration for the session database table",
		Run: func(ctx context.Context, o *console.IO) error {
			path, err := c.Handle(ctx)
			if err != nil {
				return err
			}
			if path == "" {
				o.Comment("A migration for the %s table already exists.", TableName)
				return nil
			}
			o.Info("Created %s", path)
			return nil
		},
	}
}

// Handle writes the migration and returns the path it wrote, or "" when one
// is already there.
//
// Already there is not an error: running the generator twice is what
// somebody does when they are not sure whether they ran it, and answering
// with a failing exit code would put a red step in a pipeline over a table
// that is correctly already described.
func (c *SessionTableCommand) Handle(ctx context.Context) (string, error) {
	if err := c.files.EnsureDirectoryExists(c.directory, 0o755, true); err != nil {
		return "", err
	}
	exists, err := c.MigrationExists(TableName)
	if err != nil {
		return "", err
	}
	if exists {
		return "", nil
	}

	path := filepath.Join(c.directory, c.MigrationName())
	if err := c.files.Put(path, []byte(MigrationStub), false); err != nil {
		return "", err
	}
	return path, nil
}

// MigrationName is the file the migration is written as. Stamped with the
// time so migrations sort in the order they were created, which is the
// order they have to run in. The extension is .sql, because a migration
// here is statements rather than a class.
func (c *SessionTableCommand) MigrationName() string {
	return fmt.Sprintf("%s_create_%s_table.sql", c.now().UTC().Format("2006_01_02_150405"), TableName)
}

// MigrationExists reports whether a migration for the table has already
// been written into the directory. There is only one glob: nothing here
// ships a migration that creates this table under another name.
func (c *SessionTableCommand) MigrationExists(table string) (bool, error) {
	matches, err := c.files.Glob(filepath.Join(c.directory, "*_create_"+table+"_table.sql"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}
