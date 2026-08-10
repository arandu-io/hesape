package notifications_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/notifications"
)

func record(to user, key notifications.Key) notifications.Record {
	return notifications.Record{
		NotifiableType: to.NotifiableType(),
		NotifiableID:   to.NotifiableID(),
		Key:            key,
		Data:           json.RawMessage(`{"invoice":"2026-114"}`),
	}
}

func TestMemoryStoreKeepsTenantsApart(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := user{id: "u1"}

	if _, err := store.Save(ctx, grantFor(t, notifications.ActionSend, "acme"), record(ada, "billing.invoice-paid")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Save(ctx, grantFor(t, notifications.ActionSend, "globex"), record(ada, "billing.invoice-paid")); err != nil {
		t.Fatalf("Save for the other tenant: %v", err)
	}

	// The same notifiable id exists in both tenants. A read scoped by the
	// Grant sees one row, and this is the assertion the whole design is for.
	rows, err := store.For(ctx, grantFor(t, notifications.ActionList, "acme"), ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("read %d rows, want 1: the tenant did not scope the read", len(rows))
	}
	if rows[0].Tenant != "acme" {
		t.Fatalf("tenant = %q", rows[0].Tenant)
	}
}

func TestMemoryStoreTakesTheTenantFromTheGrantAndNotFromTheRecord(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	row := record(user{id: "u1"}, "billing.invoice-paid")
	row.Tenant = "globex"

	saved, err := store.Save(ctx, grantFor(t, notifications.ActionSend, "acme"), row)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Tenant != "acme" {
		t.Fatalf("tenant = %q: a caller was able to write into another tenant", saved.Tenant)
	}
	if saved.ID == "" || saved.CreatedAt.IsZero() {
		t.Fatalf("the stored row was not completed: %+v", saved)
	}
	if !saved.Unread() {
		t.Fatal("a fresh notification is unread")
	}
}

func TestMemoryStoreReadsAndClears(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := user{id: "u1"}
	send := grantFor(t, notifications.ActionSend, "acme")
	list := grantFor(t, notifications.ActionList, "acme")
	read := grantFor(t, notifications.ActionRead, "acme")

	first, err := store.Save(ctx, send, record(ada, "billing.invoice-paid"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Save(ctx, send, record(ada, "billing.invoice-failed")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	unread, err := store.Unread(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 2 {
		t.Fatalf("unread = %d, want 2", len(unread))
	}

	if err := store.MarkAsRead(ctx, read, first.ID); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	unread, err = store.Unread(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("unread = %d, want 1", len(unread))
	}

	if err := store.MarkAllAsRead(ctx, read, ada); err != nil {
		t.Fatalf("MarkAllAsRead: %v", err)
	}
	unread, err = store.Unread(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("unread = %d, want 0", len(unread))
	}

	if err := store.Delete(ctx, grantFor(t, notifications.ActionDelete, "acme"), first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, err := store.For(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("after the delete there are %d rows, want 1", len(all))
	}
}

func TestMemoryStoreRefusesTheWrongGrant(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := user{id: "u1"}

	if _, err := store.Save(ctx, auth.Grant{}, record(ada, "billing.invoice-paid")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("the zero grant should not write, got %v", err)
	}
	if _, err := store.For(ctx, grantFor(t, notifications.ActionSend, "acme"), ada, 0); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a grant for sending should not list, got %v", err)
	}
	if err := store.Delete(ctx, grantFor(t, notifications.ActionRead, "acme"), "n1"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a grant for reading should not delete, got %v", err)
	}
}

func TestMemoryStoreRefusesARowWithNobodyBehindIt(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	send := grantFor(t, notifications.ActionSend, "acme")

	orphan := notifications.Record{Key: "billing.invoice-paid"}
	if _, err := store.Save(ctx, send, orphan); !errors.Is(err, notifications.ErrAnonymous) {
		t.Fatalf("a row with no notifiable should be refused, got %v", err)
	}

	bad := record(user{id: "u1"}, "Billing Invoice")
	if _, err := store.Save(ctx, send, bad); err == nil {
		t.Fatal("a row with an invalid key should be refused")
	}
}

func TestMemoryStoreDeleteOfAMissingRowIsNotFound(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()

	err := store.Delete(ctx, grantFor(t, notifications.ActionDelete, "acme"), "nope")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("want database.ErrNotFound, got %v", err)
	}
	err = store.MarkAsRead(ctx, grantFor(t, notifications.ActionRead, "acme"), "nope")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("want database.ErrNotFound, got %v", err)
	}
}

func TestMemoryStoreClampsTheLimit(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := user{id: "u1"}
	send := grantFor(t, notifications.ActionSend, "acme")
	for range notifications.DefaultLimit + 5 {
		if _, err := store.Save(ctx, send, record(ada, "billing.invoice-paid")); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	rows, err := store.For(ctx, grantFor(t, notifications.ActionList, "acme"), ada, 0)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(rows) != notifications.DefaultLimit {
		t.Fatalf("read %d rows with no limit asked for, want %d", len(rows), notifications.DefaultLimit)
	}
}
