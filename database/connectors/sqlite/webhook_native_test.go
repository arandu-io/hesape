package sqlite_test

// The webhook Module's schema against a real database: its seven migrations
// apply and roll back cleanly beside the webhook_deliveries table Manager
// keeps, and never create, touch or drop that table.

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/arandu-io/hesape/database/connectors/sqlite"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/webhook"
)

// nativeTables are the tables the Module's migrations own.
var nativeTables = []string{
	"webhook_server_endpoints", "webhook_client_calls",
	"webhook_server_deliveries", "webhook_server_delivery_attempts",
}

func nativeHandle(t *testing.T) (*database.DB, migrations.Connection) {
	t.Helper()
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := database.Wrap(raw, database.DialectSQLite)
	conn := database.ForMigrations(database.NewConnection(raw, "", "", map[string]any{"driver": "sqlite", "name": "sqlite"}))
	for _, migration := range queue.NewDatabaseQueue(db).Migrations() {
		if err := migration.Up(context.Background(), conn); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return db, conn
}

func tableExists(t *testing.T, db *database.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&n); err != nil {
		t.Fatalf("looking for %s: %v", table, err)
	}
	return n == 1
}

func nativeModule(t *testing.T, db *database.DB) *webhook.Module {
	t.Helper()
	module, err := webhook.New(webhook.Config{Key: []byte(webhookSecret)}, db, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return module
}

func TestTheNativeMigrationsCreateNoManagerTable(t *testing.T) {
	db, conn := nativeHandle(t)
	for _, migration := range nativeModule(t, db).Migrations() {
		if err := migration.Up(context.Background(), conn); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	for _, table := range nativeTables {
		if !tableExists(t, db, table) {
			t.Errorf("%s was not created", table)
		}
	}
	if tableExists(t, db, "webhook_deliveries") {
		t.Fatal("the Module's migrations created webhook_deliveries")
	}
}

func TestTheNativeMigrationsApplyAndRollBackBesideManagersTable(t *testing.T) {
	db, conn := nativeHandle(t)
	ctx := context.Background()
	if err := (webhook.CreateDeliveriesTable{}).Up(ctx, conn); err != nil {
		t.Fatalf("creating webhook_deliveries: %v", err)
	}
	g := auth.SystemGrant(webhook.ActionDispatch, "tenant-a")
	if _, err := webhook.NewDatabaseStore(db).Create(ctx, g, webhook.ManagerDelivery{
		ID: "delivery-1", EventID: "event-1", EndpointID: "endpoint-1", URL: "https://hooks.example.test",
		Headers: map[string]string{}, Body: []byte(`{}`),
	}); err != nil {
		t.Fatalf("storing a Manager delivery: %v", err)
	}

	declared := nativeModule(t, db).Migrations()
	for round := 0; round < 2; round++ {
		for _, migration := range declared {
			if err := migration.Up(ctx, conn); err != nil {
				t.Fatalf("round %d: applying %s: %v", round, migration.GetName(), err)
			}
		}
		for _, table := range nativeTables {
			if !tableExists(t, db, table) {
				t.Fatalf("round %d: %s was not created", round, table)
			}
		}
		for i := len(declared) - 1; i >= 0; i-- {
			reversible, ok := declared[i].(migrations.ReversibleMigration)
			if !ok {
				t.Fatalf("%s cannot be rolled back", declared[i].GetName())
			}
			if err := reversible.Down(ctx, conn); err != nil {
				t.Fatalf("round %d: rolling back %s: %v", round, declared[i].GetName(), err)
			}
		}
		for _, table := range nativeTables {
			if tableExists(t, db, table) {
				t.Fatalf("round %d: %s survived the rollback", round, table)
			}
		}
		if count := countRows(t, db, "webhook_deliveries"); count != 1 {
			t.Fatalf("round %d: webhook_deliveries holds %d rows after the Module's migrations, want its 1", round, count)
		}
	}
}
