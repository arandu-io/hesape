package console_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
	notifconsole "github.com/arandu-io/hesape/notifications/console"
)

func run(t *testing.T, cmd console.Command, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	io := console.NewIO(cmd.Name, args, &out, &out, nil)
	err := cmd.Run(context.Background(), io)
	return out.String(), err
}

func TestNotificationTableWritesTheMigration(t *testing.T) {
	dir := t.TempDir()

	out, err := run(t, notifconsole.NewNotificationTableCommand(dir).Command())
	if err != nil {
		t.Fatalf("make:notifications-table: %v", err)
	}
	if !strings.Contains(out, "Migration created") {
		t.Fatalf("make:notifications-table said %q", out)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files written, want 1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "_create_notifications_table.sql") {
		t.Fatalf("the migration is called %q", entries[0].Name())
	}

	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The columns the store reads and writes. notification_key rather than key
	// because KEY is reserved in MySQL. The SQL is the grammar's, so it is read
	// case-insensitively: the migration is written with the Blueprint, and the
	// case of the keywords belongs to whichever engine it was rendered for.
	written := strings.ToLower(string(body))
	for _, want := range []string{
		"create table",
		"notifications",
		"notification_key",
		"notifiable_type",
		"notifiable_id",
		"tenant",
		"read_at",
	} {
		if !strings.Contains(written, want) {
			t.Fatalf("the migration does not contain %q:\n%s", want, written)
		}
	}
}

func TestNotificationTableRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	// A migration written under some earlier timestamp still counts: the glob
	// is on the suffix, because the one that is there may already have run.
	existing := filepath.Join(dir, "2026_01_01_000000_create_notifications_table.sql")
	if err := os.WriteFile(existing, []byte("-- already here\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := run(t, notifconsole.NewNotificationTableCommand(dir).Command()); err == nil {
		t.Fatal("make:notifications-table over an existing migration = nil, want a refusal")
	}

	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "-- already here\n" {
		t.Fatalf("the existing migration was rewritten to %q", string(body))
	}
}

func TestNotificationTableStubIsTheGoMigration(t *testing.T) {
	c := notifconsole.NewNotificationTableCommand(t.TempDir())

	if c.MigrationTableName() != "notifications" {
		t.Fatalf("the table is %q, want notifications", c.MigrationTableName())
	}
	stub, err := c.MigrationStub()
	if err != nil {
		t.Fatalf("MigrationStub: %v", err)
	}
	// The SQL is read off notifications.Migrations rather than a stub of its
	// own, so the two cannot describe different tables.
	rendered := strings.ToLower(stub)
	if !strings.Contains(rendered, "create table") || !strings.Contains(rendered, "notifications") {
		t.Fatalf("the stub does not create the table: %q", stub)
	}
	if !strings.Contains(stub, "notifications_recipient_idx") {
		t.Fatalf("the stub does not carry the index every read uses: %q", stub)
	}
}
