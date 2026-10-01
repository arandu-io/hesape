package sqlite_test

// The webhook engine against a real database: the delivery snapshot and the
// queue job commit or roll back together, a redelivered event inserts nothing,
// and a stale claim cannot settle a delivery.
//
// It lives in this module rather than beside the webhook package because it
// needs a driver, and the root module carries none: a driver imported by one
// of its tests is a requirement every consumer of the collection downloads.
// This module already owns SQLite, so the test costs nothing here.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/webhook"
)

// webhookSecret is a signing key of the length the static secret requires.
const webhookSecret = "01234567890123456789012345678901"

func TestDatabaseDispatchCommitsSnapshotAndJobTogether(t *testing.T) {
	db := webhookDatabase(t)
	manager, err := webhook.NewManager(db, webhook.NewStaticSecret([]byte(webhookSecret)), webhook.ManagerOptions{})
	if err != nil {
		t.Fatalf("webhook.NewManager() error = %v", err)
	}
	g := auth.SystemGrant(webhook.ActionDispatch, "tenant-a")
	err = manager.Dispatch(context.Background(), g,
		webhook.Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{"id":"inv-1"}`)},
		[]webhook.Endpoint{
			{ID: "accounting", URL: "https://accounting.example.test/hook"},
			{ID: "audit", URL: "https://audit.example.test/hook"},
		},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got := countRows(t, db, "webhook_deliveries"); got != 2 {
		t.Fatalf("delivery rows = %d", got)
	}
	if got := countRows(t, db, "jobs"); got != 2 {
		t.Fatalf("job rows = %d", got)
	}
}

func TestDatabaseManagerUsesConfiguredActionForStoreAndJob(t *testing.T) {
	db := webhookDatabase(t)
	const customAction auth.Action = "whatsapp.runtime"
	manager, err := webhook.NewManager(db, webhook.NewStaticSecret([]byte(webhookSecret)), webhook.ManagerOptions{
		Action: customAction, QueueName: "whatsapp-webhooks", DeliveryJobName: "whatsapp.webhook.deliver",
	})
	if err != nil {
		t.Fatalf("webhook.NewManager() error = %v", err)
	}
	grant := auth.SystemGrant(customAction, "tenant-a")
	if err := manager.Dispatch(context.Background(), grant,
		webhook.Event{ID: "event-legacy", Name: "message.sent", Payload: []byte(`{"id":"message-1"}`)},
		[]webhook.Endpoint{{ID: "application", URL: "https://hooks.example.test/messages"}},
	); err != nil {
		t.Fatalf("Dispatch() with configured action error = %v", err)
	}
	var action, queueName, jobName string
	if err := db.QueryRowContext(context.Background(), `SELECT action, queue, name FROM jobs`).
		Scan(&action, &queueName, &jobName); err != nil {
		t.Fatalf("reading configured job: %v", err)
	}
	if action != string(customAction) || queueName != "whatsapp-webhooks" || jobName != "whatsapp.webhook.deliver" {
		t.Fatalf("stored job route = action %q, queue %q, name %q", action, queueName, jobName)
	}
}

func TestDatabaseDispatchRollsBackSnapshotWhenJobInsertFails(t *testing.T) {
	db := webhookDatabase(t)
	if _, err := db.ExecContext(context.Background(), `CREATE TRIGGER refuse_webhook_job
		BEFORE INSERT ON jobs BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END`); err != nil {
		t.Fatalf("creating failure trigger: %v", err)
	}
	manager, err := webhook.NewManager(db, webhook.NewStaticSecret([]byte(webhookSecret)), webhook.ManagerOptions{})
	if err != nil {
		t.Fatalf("webhook.NewManager() error = %v", err)
	}
	err = manager.Dispatch(context.Background(), auth.SystemGrant(webhook.ActionDispatch, "tenant-a"),
		webhook.Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]webhook.Endpoint{{ID: "accounting", URL: "https://accounting.example.test/hook"}},
	)
	if err == nil {
		t.Fatal("Dispatch() error = nil")
	}
	if got := countRows(t, db, "webhook_deliveries"); got != 0 {
		t.Fatalf("delivery rows after rollback = %d", got)
	}
}

func TestDatabaseDispatchIsIdempotent(t *testing.T) {
	db := webhookDatabase(t)
	manager, err := webhook.NewManager(db, webhook.NewStaticSecret([]byte(webhookSecret)), webhook.ManagerOptions{})
	if err != nil {
		t.Fatalf("webhook.NewManager() error = %v", err)
	}
	g := auth.SystemGrant(webhook.ActionDispatch, "tenant-a")
	event := webhook.Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)}
	endpoint := webhook.Endpoint{ID: "accounting", URL: "https://accounting.example.test/hook"}
	if err := manager.Dispatch(context.Background(), g, event, []webhook.Endpoint{endpoint}); err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	if err := manager.Dispatch(context.Background(), g, event, []webhook.Endpoint{endpoint}); err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	if got := countRows(t, db, "webhook_deliveries"); got != 1 {
		t.Fatalf("delivery rows = %d", got)
	}
	if got := countRows(t, db, "jobs"); got != 1 {
		t.Fatalf("job rows = %d", got)
	}
}

func TestDatabaseClaimFencingRejectsStaleSettlement(t *testing.T) {
	db := webhookDatabase(t)
	store := webhook.NewDatabaseStore(db)
	g := auth.SystemGrant(webhook.ActionDispatch, "tenant-a")
	now := time.Now().UTC()
	created, err := store.Create(context.Background(), g, webhook.Delivery{
		ID: "delivery-1", EventID: "event-1", EventName: "invoice.paid",
		EndpointID: "accounting", URL: "https://accounting.example.test/hook",
		Headers: map[string]string{}, Body: []byte(`{}`), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !created {
		t.Fatalf("Create() = %v, %v", created, err)
	}
	first, claimed, err := store.Claim(context.Background(), g, "delivery-1", -time.Second)
	if err != nil || !claimed {
		t.Fatalf("first Claim() = %v, %v", claimed, err)
	}
	second, claimed, err := store.Claim(context.Background(), g, "delivery-1", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("second Claim() = %v, %v", claimed, err)
	}
	if err := store.Complete(context.Background(), g, first, webhook.Result{StatusCode: 204}); !errors.Is(err, webhook.ErrStaleClaim) {
		t.Fatalf("stale Complete() error = %v", err)
	}
	if err := store.Complete(context.Background(), g, second, webhook.Result{StatusCode: 204}); err != nil {
		t.Fatalf("current Complete() error = %v", err)
	}
}

func TestMigrationRendersForSQLitePostgresAndMySQL(t *testing.T) {
	db := webhookDatabase(t)
	for _, driver := range []string{"sqlite", "pgsql", "mysql"} {
		conn := database.NewConnection(db.Unwrap(), "", "", map[string]any{"driver": driver, "name": driver})
		migrationConn := database.ForMigrations(conn)
		pretender, ok := migrationConn.(migrations.PretendingConnection)
		if !ok {
			t.Fatalf("%s migration connection cannot pretend", driver)
		}
		statements, err := pretender.Pretend(context.Background(), func() error {
			return (webhook.CreateDeliveriesTable{}).Up(context.Background(), migrationConn)
		})
		if err != nil {
			t.Fatalf("%s migration render error = %v", driver, err)
		}
		if len(statements) < 3 {
			t.Fatalf("%s migration statements = %d", driver, len(statements))
		}
	}
}

func TestIdempotentInsertRendersForSQLitePostgresAndMySQL(t *testing.T) {
	tests := []struct {
		name    string
		grammar query.Grammar
		marker  string
	}{
		{"sqlite", grammars.NewSQLiteGrammar(), "insert or ignore"},
		{"postgres", grammars.NewPostgresGrammar(), "on conflict do nothing"},
		{"mysql", grammars.NewMySQLGrammar(), "insert ignore"},
	}
	values := []map[string]any{{"id": "delivery-1", "tenant_id": "tenant-a"}}
	for _, test := range tests {
		builder := query.NewBuilder(nil, test.grammar, nil).From("webhook_deliveries")
		statement := strings.ToLower(test.grammar.CompileInsertOrIgnore(builder, values))
		if !strings.Contains(statement, test.marker) {
			t.Errorf("%s insert = %q", test.name, statement)
		}
	}
}

func webhookDatabase(t *testing.T) *database.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := database.Wrap(raw, database.DialectSQLite)
	conn := database.NewConnection(raw, "", "", map[string]any{"driver": "sqlite", "name": "sqlite"})
	migrationConn := database.ForMigrations(conn)
	for _, migration := range append((webhook.NewDatabaseStore(db)).Migrations(), queue.NewDatabaseQueue(db).Migrations()...) {
		if err := migration.Up(context.Background(), migrationConn); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return db
}

func countRows(t *testing.T, db *database.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return count
}
