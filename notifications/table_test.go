package notifications_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/notifications"
)

func openTable(t *testing.T) (*notifications.TableStore, *fakeTable) {
	t.Helper()
	dsn, table := newTable()
	handle, err := sql.Open("arandu-fake-notifications", dsn)
	if err != nil {
		t.Fatalf("opening the fake table: %v", err)
	}
	t.Cleanup(func() { handle.Close() })
	return notifications.NewTableStore(database.Wrap(handle, database.DialectSQLite)), table
}

func TestTableStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, _ := openTable(t)
	ada := user{id: "u1"}
	send := grantFor(t, notifications.ActionSend, "acme")
	list := grantFor(t, notifications.ActionList, "acme")

	first, err := store.Save(ctx, send, record(ada, "billing.invoice-paid"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Save(ctx, send, record(ada, "billing.invoice-failed")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rows, err := store.For(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("read %d rows, want 2", len(rows))
	}
	// Newest first.
	if rows[0].Key != "billing.invoice-failed" {
		t.Fatalf("the newest row is %s", rows[0].Key)
	}
	if string(rows[0].Data) != `{"invoice":"2026-114"}` {
		t.Fatalf("the payload did not survive the round trip: %s", rows[0].Data)
	}
	if !rows[0].Unread() {
		t.Fatal("a fresh row came back read")
	}

	if err := store.MarkAsRead(ctx, grantFor(t, notifications.ActionRead, "acme"), first.ID); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	unread, err := store.Unread(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("unread = %d, want 1", len(unread))
	}

	// Marking it again is not an error and does not move the timestamp.
	if err := store.MarkAsRead(ctx, grantFor(t, notifications.ActionRead, "acme"), first.ID); err != nil {
		t.Fatalf("marking a read notification read again: %v", err)
	}

	if err := store.Delete(ctx, grantFor(t, notifications.ActionDelete, "acme"), first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows, err = store.For(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("after the delete there are %d rows, want 1", len(rows))
	}
}

func TestTableStoreScopesEveryStatementByTenant(t *testing.T) {
	ctx := context.Background()
	store, table := openTable(t)
	ada := user{id: "u1"}

	if _, err := store.Save(ctx, grantFor(t, notifications.ActionSend, "globex"), record(ada, "billing.invoice-paid")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rows, err := store.For(ctx, grantFor(t, notifications.ActionList, "acme"), ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("acme read %d of globex's notifications", len(rows))
	}

	// Every statement carries the tenant. A read path that forgets it is the
	// leak RULE 17 exists to prevent, and it is invisible in a test that only
	// looks at results.
	for _, statement := range table.statements() {
		if strings.HasPrefix(statement, "INSERT") {
			if !strings.Contains(statement, "tenant,") {
				t.Fatalf("a row is written without a tenant: %s", statement)
			}
			continue
		}
		if !strings.Contains(statement, "tenant = ?") {
			t.Fatalf("a statement runs without a tenant in its WHERE: %s", statement)
		}
	}
}

func TestTableStoreMissingRow(t *testing.T) {
	ctx := context.Background()
	store, _ := openTable(t)

	err := store.MarkAsRead(ctx, grantFor(t, notifications.ActionRead, "acme"), "nope")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("want database.ErrNotFound, got %v", err)
	}
	err = store.Delete(ctx, grantFor(t, notifications.ActionDelete, "acme"), "nope")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("want database.ErrNotFound, got %v", err)
	}
}

func TestTableStoreClampsTheLimitIntoTheStatement(t *testing.T) {
	ctx := context.Background()
	store, table := openTable(t)

	if _, err := store.For(ctx, grantFor(t, notifications.ActionList, "acme"), user{id: "u1"}, 10_000); err != nil {
		t.Fatalf("For: %v", err)
	}
	last := table.statements()[len(table.statements())-1]
	if !strings.Contains(last, "LIMIT 500") {
		t.Fatalf("the limit was not clamped: %s", last)
	}
}
