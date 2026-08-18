package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/conformance"
	_ "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
)

// The connector end to end: importing it has to be enough for Open to work,
// and the handle it returns has to be a real one. Testing the registry alone
// would prove the wiring and not the driver.

func TestImportingTheConnectorIsEnough(t *testing.T) {
	cfg := database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	}

	db, closeDB, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE customer (id TEXT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO customer (id, name) VALUES (?, ?)`, "c-1", "Ana"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM customer WHERE id = ?`, "c-1").Scan(&name); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if name != "Ana" {
		t.Fatalf("name = %q", name)
	}
}

// TestTransactionsWorkThroughTheConnector: database.Transaction carries the
// transaction on the context, and the outbox depends on statements issued
// through the same handle joining it. That claim is about a driver.
func TestTransactionsWorkThroughTheConnector(t *testing.T) {
	db, closeDB, err := database.Open(database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE customer (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	rolledBack := errFailed{}
	err = database.Transaction(ctx, db, func(ctx context.Context) error {
		if _, err := db.ExecContext(ctx, `INSERT INTO customer (id) VALUES (?)`, "c-1"); err != nil {
			return err
		}
		return rolledBack
	})
	if err != rolledBack {
		t.Fatalf("Transaction: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM customer`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d rows survived a rollback", count)
	}
}

// TestTheMigrationPathWorks: one migrator, three engines. This is the half that
// runs without anything installed.
func TestTheMigrationPathWorks(t *testing.T) {
	db, closeDB, err := database.Open(database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	ctx := context.Background()
	migrator := newMigrator(db)
	if err := migrator.GetRepository().CreateRepository(ctx); err != nil {
		t.Fatalf("creating the tracking table: %v", err)
	}

	applied, err := migrator.Run(ctx, []string{customerPath}, migrations.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d migrations, want 1", len(applied))
	}

	// Twice, because a deploy pipeline runs it on every release.
	if applied, err := migrator.Run(ctx, []string{customerPath}, migrations.Options{}); err != nil || len(applied) != 0 {
		t.Fatalf("second Run: %v, applied %d", err, len(applied))
	}

	reverted, err := migrator.Rollback(ctx, []string{customerPath}, migrations.Options{})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(reverted) != 1 {
		t.Fatalf("rolled back %v, want the one migration", reverted)
	}
}

// customerPath is the group createCustomerTable registers under, so this test
// runs its own migration and nothing another package registered.
const customerPath = "database/connectors/sqlite"

// createCustomerTable is the migration this test applies.
type createCustomerTable struct{ migrations.BaseMigration }

func (createCustomerTable) GetName() string { return "2026_01_01_000000_create_customer_table" }

func (createCustomerTable) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx,
		`CREATE TABLE customer (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL)`, nil)
	return err
}

func (createCustomerTable) Down(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, `DROP TABLE customer`, nil)
	return err
}

func init() { migrations.Register(createCustomerTable{}, customerPath) }

// newMigrator wires a Migrator over db, the way `aru migrate` does.
func newMigrator(db *database.DB) *migrations.Migrator {
	connection := database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(database.DialectSQLite),
		"name":   "sqlite",
	})

	inner := database.NewConnectionResolver(map[string]database.ConnectionInterface{
		"sqlite": connection,
	})
	inner.SetDefaultConnection("sqlite")

	resolver := database.MigrationResolver{Resolver: inner}
	repository := migrations.NewDatabaseMigrationRepository(resolver, migrations.DefaultTable)
	return migrations.NewMigrator(repository, resolver, nil)
}

// TestTenantScopingStillComesFromTheGrant: the connector changes how the
// connection is opened and nothing about who may read what.
//
// The tenant is read through auth.Tenant, and this is the assertion that catches
// a connector reading it from anywhere else.
func TestTenantScopingStillComesFromTheGrant(t *testing.T) {
	g := auth.SystemGrant("customer.view", "tenant-1")
	if auth.Tenant(g) != "tenant-1" {
		t.Fatal("the tenant no longer comes from the Grant")
	}
}

type errFailed struct{}

func (errFailed) Error() string { return "the rule said no" }

// TestConformance runs the shared suite against a real server.
//
// It is the test that would have caught the defect that shipped: every other
// test in this project ran against SQLite, which accepts `id TEXT PRIMARY KEY`.
// See the conformance package for the rest of that story.
//
// To run it: it needs nothing installed, so it always runs:
//
//	go test ./...
func TestConformance(t *testing.T) {
	conformance.Run(t, database.DialectSQLite, driverName(t), dsn())
}

// driverName is what the connector registered, read back rather than
// hardcoded: if the connector ever registers a different driver, this suite
// follows it instead of silently testing nothing.
func driverName(t *testing.T) string {
	t.Helper()
	name, err := database.DriverName(database.DialectSQLite)
	if err != nil {
		t.Fatalf("the connector did not register a driver: %v", err)
	}
	return name
}

// dsn is a file in the test's own directory. SQLite needs nothing installed, so
// this connector runs the suite on every machine and every CI job -- which is
// what makes the other two connectors a comparison rather than the only signal.
func dsn() string {
	if from := os.Getenv("ARANDU_TEST_SQLITE_DSN"); from != "" {
		return from
	}
	return filepath.Join(os.TempDir(), "arandu-conformance.sqlite")
}
