// Package conformance is the suite every connector has to pass against a real
// server.
//
// It exists because of a defect that shipped: every test in this project ran
// against SQLite, which accepts `id TEXT PRIMARY KEY`. MySQL does not -- it
// stores TEXT off-page and refuses it in a key without a prefix length -- so
// `aru migrate` failed on the first statement of the first migration, the one
// that creates its own tracking table. Nothing caught it, because nothing ever
// spoke to a MySQL server.
//
// One suite rather than one per connector, for the same reason there is one
// Repository contract: a claim that holds on SQLite and not on MySQL is not a
// claim this framework can make. What is asserted here is the portable subset
// itself -- the types, the keys and the round trips that are meant to be the
// same everywhere. It is what turns "swapping the driver duplicates nothing"
// from an assertion into a measurement.
//
// A connector's test calls Run with a DSN taken from the environment and skips
// when it is empty, so `go test ./...` still passes with nothing installed:
//
//	func TestConformance(t *testing.T) {
//	    name, err := database.DriverName(database.DialectMySQL)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    conformance.Run(t, database.DialectMySQL, name, os.Getenv("ARANDU_TEST_MYSQL_DSN"))
//	}
//
// The DSN is the driver's own, not a database.Config: the point is to exercise
// the driver, and asking the caller to build the string keeps this package from
// having an opinion about how each one spells a connection.
//
// There is nothing here about the Grant. Authorization is Go code that runs
// before the statement is built, so it behaves the same on the three engines,
// and the collection's own tests cover it against SQLite. What cannot be
// covered that way is the SQL, which is all this suite looks at.
package conformance

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
)

// Run executes the suite against a live server.
//
// It skips when dsn is empty. driverName is what the connector registered,
// which the caller reads back from database.DriverName rather than hardcoding.
func Run(t *testing.T, dialect database.Dialect, driverName, dsn string) {
	t.Helper()
	if dsn == "" {
		t.Skipf("no DSN for %s: set the environment variable to run this suite against a real server", dialect)
	}

	sqldb, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatalf("opening %s: %v", dialect, err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		t.Fatalf("no server answered at the %s DSN: %v", dialect, err)
	}

	db := database.Wrap(sqldb, dialect)

	t.Run("the migrations table can be created", func(t *testing.T) {
		testMigrationsTable(t, dialect, db)
	})
	t.Run("a generated schema applies", func(t *testing.T) {
		testGeneratedSchema(t, db)
	})
	t.Run("a decimal keeps its digits", func(t *testing.T) {
		testDecimalRoundTrip(t, db)
	})
	t.Run("a timestamp comes back as it went in", func(t *testing.T) {
		testTimestampRoundTrip(t, db)
	})
	t.Run("a transaction rolls back", func(t *testing.T) {
		testTransactionRollback(t, db)
	})
}

// conformanceMigrationPath is the group the suite's own migration registers
// under, so a run picks up this one and nothing an application registered.
const conformanceMigrationPath = "database/conformance"

// conformanceMigration is the schema change the suite applies: one table, one
// key column, nothing an engine could disagree about except the key type.
type conformanceMigration struct{ migrations.BaseMigration }

// GetName returns the migration's name.
func (conformanceMigration) GetName() string { return "2026_01_01_000000_conformance" }

// Up creates the table.
func (conformanceMigration) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx,
		`CREATE TABLE `+table("noop")+` (id `+database.KeyText+` PRIMARY KEY)`, nil)
	return err
}

// Down drops it.
func (conformanceMigration) Down(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, `DROP TABLE `+table("noop"), nil)
	return err
}

func init() { migrations.Register(conformanceMigration{}, conformanceMigrationPath) }

// testMigrationsTable is the statement MySQL rejected. The migrator creates its
// tracking table before it does anything else, so a failure here means the
// engine is unusable end to end -- not that one migration is wrong.
func testMigrationsTable(t *testing.T, dialect database.Dialect, db *database.DB) {
	ctx := context.Background()
	drop(t, db, migrations.DefaultTable)
	drop(t, db, table("noop"))
	t.Cleanup(func() { drop(t, db, table("noop")) })

	migrator := newMigrator(dialect, db)
	if err := migrator.GetRepository().CreateRepository(ctx); err != nil {
		t.Fatalf("creating the tracking table: %v", err)
	}

	applied, err := migrator.Run(ctx, []string{conformanceMigrationPath}, migrations.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d migrations, want 1", len(applied))
	}

	// A second call is a no-op, which is what makes `aru migrate` safe to run
	// on every deploy.
	again, err := migrator.Run(ctx, []string{conformanceMigrationPath}, migrations.Options{})
	if err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("re-applied %v", again)
	}

	// The rollback is the other half: it reads the tracking table back and
	// deletes the row, which is where a placeholder that was never rebound
	// shows up.
	rolledBack, err := migrator.Rollback(ctx, []string{conformanceMigrationPath}, migrations.Options{})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(rolledBack) != 1 {
		t.Errorf("rolled back %v, want the one migration", rolledBack)
	}
}

// newMigrator wires a Migrator over the suite's connection.
//
// The migrations component reaches a connection through a resolver rather than
// being handed one, because a migration may name the connection it runs on. The
// suite has exactly one, so the resolver holds exactly one.
func newMigrator(dialect database.Dialect, db *database.DB) *migrations.Migrator {
	connection := database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(dialect),
		"name":   conformanceConnection,
	})

	inner := database.NewConnectionResolver(map[string]database.ConnectionInterface{
		conformanceConnection: connection,
	})
	inner.SetDefaultConnection(conformanceConnection)

	resolver := database.MigrationResolver{Resolver: inner}
	repository := migrations.NewDatabaseMigrationRepository(resolver, migrations.DefaultTable)

	return migrations.NewMigrator(repository, resolver, nil)
}

// conformanceConnection is the name the suite's single connection is registered
// under.
const conformanceConnection = "conformance"

// testGeneratedSchema applies the shape `aru make:module` emits: a text primary
// key, a tenant column, a composite UNIQUE over two text columns and an index
// over a third. Every one of those is a place TEXT would have failed.
func testGeneratedSchema(t *testing.T, db *database.DB) {
	ctx := context.Background()
	name := table("widget")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	ddl := fmt.Sprintf(`CREATE TABLE %s (
		id         %s PRIMARY KEY,
		tenant_id  %s NOT NULL,
		reference  %s NOT NULL,
		notes      TEXT,
		total      INTEGER NOT NULL,
		created_at TIMESTAMP NOT NULL,
		UNIQUE (tenant_id, reference)
	)`, name, database.KeyText, database.KeyText, database.KeyText)

	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("the generated schema does not apply:\n%s\n%v", ddl, err)
	}
	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`CREATE INDEX %s_tenant_created_idx ON %s (tenant_id, created_at, id)`, name, name)); err != nil {
		t.Fatalf("the tenant index does not apply: %v", err)
	}

	// The UNIQUE has to hold, or it is decoration: two tenants may share a
	// reference, one tenant may not repeat it.
	insert := fmt.Sprintf(
		`INSERT INTO %s (id, tenant_id, reference, total, created_at) VALUES (?, ?, ?, ?, ?)`, name)
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := db.ExecContext(ctx, insert, "1", "tenant-a", "REF-1", 100, now); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.ExecContext(ctx, insert, "2", "tenant-b", "REF-1", 100, now); err != nil {
		t.Errorf("two tenants cannot share a reference, and they must: %v", err)
	}
	if _, err := db.ExecContext(ctx, insert, "3", "tenant-a", "REF-1", 100, now); err == nil {
		t.Error("one tenant repeated a reference and the UNIQUE did not stop it")
	}
}

// testDecimalRoundTrip is the four-byte REAL. In PostgreSQL a float64 written
// through REAL comes back rounded to about seven digits, silently, on read.
func testDecimalRoundTrip(t *testing.T, db *database.DB) {
	ctx := context.Background()
	name := table("rate")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY, value DOUBLE PRECISION NOT NULL)`, name, database.KeyText)); err != nil {
		t.Fatalf("DOUBLE PRECISION is not accepted: %v", err)
	}

	// Enough digits that a four-byte float cannot hold them.
	const want = 0.1234567890123456
	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, value) VALUES (?, ?)`, name), "1", want); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got float64
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT value FROM %s WHERE id = ?`, name), "1").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("the value came back as %.17g, want %.17g -- the column is losing digits", got, want)
	}
}

// testTimestampRoundTrip: every timestamp this framework writes is UTC, and a
// driver that hands one back in the session's zone makes every comparison the
// queue and the scheduler do wrong by the offset.
func testTimestampRoundTrip(t *testing.T, db *database.DB) {
	ctx := context.Background()
	name := table("moment")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY, at TIMESTAMP NOT NULL)`, name, database.KeyText)); err != nil {
		t.Fatalf("create: %v", err)
	}

	want := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, at) VALUES (?, ?)`, name), "1", want); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got time.Time
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT at FROM %s WHERE id = ?`, name), "1").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if !got.UTC().Equal(want) {
		t.Errorf("the timestamp came back as %s, want %s", got.UTC(), want)
	}
}

// testTransactionRollback: the outbox depends on it. An event stored next to a
// row that then rolled back is worse than no event.
func testTransactionRollback(t *testing.T, db *database.DB) {
	ctx := context.Background()
	name := table("ledger")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	if _, err := db.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY)`, name, database.KeyText)); err != nil {
		t.Fatalf("create: %v", err)
	}

	refused := fmt.Errorf("the rule said no")
	err := database.Transaction(ctx, db, func(ctx context.Context) error {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (id) VALUES (?)`, name), "1"); err != nil {
			return err
		}
		return refused
	})
	if err == nil {
		t.Fatal("the transaction reported success after fn failed")
	}

	var count int
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, name)).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("%d rows survived a rolled back transaction", count)
	}
}

// table prefixes a name, so the suite never collides with a real table in a
// database somebody pointed it at by accident.
func table(name string) string { return "arandu_conformance_" + name }

func drop(t *testing.T, db *database.DB, name string) {
	t.Helper()
	// A missing table is not an error here: this runs before the create.
	_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+name)
}
