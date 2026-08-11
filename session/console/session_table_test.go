package console_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/session/console"
)

func TestTheMigrationIsWrittenOnceAndNotTwice(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "migrations")
	command := console.NewSessionTableCommand(nil, dir)
	ctx := context.Background()

	path, err := command.Handle(ctx)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if path == "" {
		t.Fatal("nothing was written")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"CREATE TABLE", console.TableName, "last_activity", "payload", "user_id"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the migration does not mention %q:\n%s", want, body)
		}
	}
	// The sweep deletes on last_activity, and an unindexed sweep of the session
	// table is a full scan on the busiest table in the schema.
	if !strings.Contains(string(body), "sessions_last_activity_index") {
		t.Fatalf("the migration does not index last_activity:\n%s", body)
	}

	// Running it again is what somebody does when they are not sure whether
	// they ran it. A second migration creating the same table is a deploy that
	// fails at the worst moment.
	second, err := command.Handle(ctx)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if second != "" {
		t.Fatalf("a second migration was written: %s", second)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the directory holds %d files", len(entries))
	}
}

func TestTheMigrationNameSortsByWhenItWasMade(t *testing.T) {
	command := console.NewSessionTableCommand(nil, t.TempDir())

	name := command.MigrationName()
	if !strings.HasSuffix(name, "_create_"+console.TableName+"_table.sql") {
		t.Fatalf("got %q", name)
	}
	// A migration's file name is what decides the order it runs in, so the stamp
	// has to be at the front.
	if len(name) < 19 || name[4] != '_' {
		t.Fatalf("the name is not stamped: %q", name)
	}
}

func TestTheCommandIsAValueTheRegistryTakes(t *testing.T) {
	command := console.NewSessionTableCommand(nil, t.TempDir())

	c := command.Command()
	if c.Name != "make:session-table" {
		t.Fatalf("got %q", c.Name)
	}
	if c.Run == nil {
		t.Fatal("the command runs nothing")
	}
	if strings.HasSuffix(c.Description, ".") {
		t.Fatalf("the description ends in a full stop: %q", c.Description)
	}
}
