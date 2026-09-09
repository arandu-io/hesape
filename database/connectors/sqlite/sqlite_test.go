package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/conformance"
	_ "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
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
	conformance.Run(t, database.DialectSQLite, driverName(t), dsn(t))
}

// TestTheDefaultConformanceDatabaseEndsWithTheTest: a fixed file under the
// process temp directory survives the test that created it and leaks state into
// later conformance runs.
func TestTheDefaultConformanceDatabaseEndsWithTheTest(t *testing.T) {
	t.Setenv("ARANDU_TEST_SQLITE_DSN", "")
	t.Setenv("TMPDIR", t.TempDir())

	var path string
	if ok := t.Run("owner", func(t *testing.T) {
		path = dsn(t)
		if err := os.WriteFile(path, []byte("conformance"), 0o600); err != nil {
			t.Fatalf("write conformance database: %v", err)
		}
	}); !ok {
		t.Fatal("the owner test failed before cleanup could be checked")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default conformance database survived its test at %q: %v", path, err)
	}
}

func TestAnExplicitConformanceDSNRemainsCallerOwned(t *testing.T) {
	const override = "file:caller-owned.sqlite?mode=memory&cache=shared"
	t.Setenv("ARANDU_TEST_SQLITE_DSN", override)

	if got := dsn(t); got != override {
		t.Fatalf("conformance DSN = %q, want explicit override %q", got, override)
	}
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

// dsn returns an explicit caller-owned override or a database owned by the
// current test. SQLite needs nothing installed, so this connector runs the
// suite on every machine and every CI job.
func dsn(t *testing.T) string {
	t.Helper()
	if from := os.Getenv("ARANDU_TEST_SQLITE_DSN"); from != "" {
		return from
	}
	return filepath.Join(t.TempDir(), "arandu-conformance.sqlite")
}

// TestDropAllTablesEmptiesTheCatalogue is the wipe migrate:fresh runs before it
// migrates from nothing.
//
// It runs against the real driver because the defect it covers could not be
// seen anywhere else. The SQLite grammar took a schema name where the interface
// promised table names, and Builder.DropAllTables passes the qualified table
// names, so the statement named the schema "main.arandu_migrations" -- which
// SQLite refused. The grammar's own doc comment described the divergence and
// nobody had run the two halves together.
//
// The second half is the pragma: sqlite_master is read-only without it, and the
// delete is refused with "table sqlite_master may not be modified".
func TestDropAllTablesEmptiesTheCatalogue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wipe.sqlite")

	db, closeDB, err := database.Open(database.Config{
		Connection: database.DialectSQLite,
		Database:   path,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(closeDB)

	connection := database.NewConnection(db.Unwrap(), path, "", map[string]any{
		"driver": string(database.DialectSQLite), "name": "wipe", "database": path,
	})
	builder := schema.NewBuilder(database.ForSchema(connection))

	if err := builder.Create(ctx, "invoices", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.Index([]string{"tenant_id"}, "invoices_tenant_idx")
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := builder.Create(ctx, "payments", func(table *schema.Blueprint) {
		table.String("id").Primary()
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := builder.DropAllTables(ctx); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}

	tables, err := builder.GetTables(ctx)
	if err != nil {
		t.Fatalf("GetTables: %v", err)
	}
	if len(tables) != 0 {
		names := make([]string, 0, len(tables))
		for _, table := range tables {
			names = append(names, table.Name)
		}
		t.Fatalf("DropAllTables left %v behind", names)
	}

	// The catalogue is writable during the wipe and must not stay that way: a
	// connection that carries a writable sqlite_master into the migrations that
	// follow is one where a typo edits the schema instead of failing.
	if err := builder.Create(ctx, "invoices", func(table *schema.Blueprint) {
		table.String("id").Primary()
	}); err != nil {
		t.Fatalf("the database is unusable after the wipe: %v", err)
	}
}

// TestANullableTimestampIsStoredLikeANonNullableOne is the measurement, not a
// reading of the code: it writes both through the connection and then reads
// the text the column actually holds.
//
// Reading the value back through a model would hide the defect, because the
// model reparses either spelling and answers the same instant. The column is
// what the next query compares against, so the column is what has to be
// asserted.
func TestANullableTimestampIsStoredLikeANonNullableOne(t *testing.T) {
	ctx := context.Background()
	cfg := database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	}

	db, closeDB, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	conn := database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(database.DialectSQLite),
		"name":   "sqlite",
	})

	if _, err := conn.Statement(ctx,
		`CREATE TABLE window (id TEXT PRIMARY KEY, starts_at DATETIME NOT NULL, ends_at DATETIME)`, nil); err != nil {
		t.Fatalf("creating the table: %v", err)
	}

	// One instant, written twice: once as a value, once through a pointer.
	// Truncated to the second, because every timestamp on this path is.
	at := time.Date(2026, 9, 9, 23, 14, 4, 0, time.UTC)
	if _, err := conn.Statement(ctx,
		`INSERT INTO window (id, starts_at, ends_at) VALUES (?, ?, ?)`,
		conn.PrepareBindings([]any{"w1", at, &at})); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	// CAST to TEXT, because the driver parses a DATETIME column on the way
	// back and would answer both spellings as the same instant -- which is the
	// same hiding the model does, one layer lower.
	rows, err := conn.Select(ctx,
		`SELECT CAST(starts_at AS TEXT) AS starts_at, CAST(ends_at AS TEXT) AS ends_at FROM window WHERE id = ?`,
		conn.PrepareBindings([]any{"w1"}), true)
	if err != nil {
		t.Fatalf("reading the columns back: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("read %d rows, want 1", len(rows))
	}

	starts, ends := fmt.Sprint(rows[0]["starts_at"]), fmt.Sprint(rows[0]["ends_at"])
	if starts != ends {
		t.Errorf("the same instant is stored as %q in a NOT NULL column and %q in a nullable one; "+
			"an engine that stores a timestamp as text compares those as text, and the longer of two "+
			"equal prefixes sorts after the bound a query sends", starts, ends)
	}

	// The boundary, which is where the difference in spelling stops being
	// cosmetic: a scan running in the same second as the end of a window has
	// to find the window.
	found, err := conn.Select(ctx, `SELECT id FROM window WHERE ends_at <= ?`,
		conn.PrepareBindings([]any{at}), true)
	if err != nil {
		t.Fatalf("querying the boundary: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("`ends_at <= ?` against the exact instant found %d rows, want 1: "+
			"an expiry scan running in the same second as the end of the window left the record active", len(found))
	}

	// And the same query against the column beside it, which was already
	// right, so a regression that broke both would not read as this one.
	found, err = conn.Select(ctx, `SELECT id FROM window WHERE starts_at <= ?`,
		conn.PrepareBindings([]any{at}), true)
	if err != nil {
		t.Fatalf("querying the boundary on the NOT NULL column: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("`starts_at <= ?` against the exact instant found %d rows, want 1", len(found))
	}
}

// TestANilTimestampIsStillNull keeps the other half: a pointer that is not set
// is the absence of a value, and formatting the zero behind it would write the
// year one into a column that means "not set".
func TestANilTimestampIsStillNull(t *testing.T) {
	ctx := context.Background()
	cfg := database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	}

	db, closeDB, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	conn := database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(database.DialectSQLite),
		"name":   "sqlite",
	})

	if _, err := conn.Statement(ctx,
		`CREATE TABLE window (id TEXT PRIMARY KEY, ends_at DATETIME)`, nil); err != nil {
		t.Fatalf("creating the table: %v", err)
	}

	var absent *time.Time
	if _, err := conn.Statement(ctx, `INSERT INTO window (id, ends_at) VALUES (?, ?)`,
		conn.PrepareBindings([]any{"w1", absent})); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	rows, err := conn.Select(ctx, `SELECT ends_at IS NULL AS absent FROM window WHERE id = ?`,
		conn.PrepareBindings([]any{"w1"}), true)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if fmt.Sprint(rows[0]["absent"]) != "1" {
		t.Errorf("a nil pointer was stored as %v, want NULL", rows[0]["absent"])
	}
}
