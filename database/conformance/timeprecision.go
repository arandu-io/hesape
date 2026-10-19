package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/events"
)

// schemaBuilder is the schema builder a migration would write through, over
// the suite's connection.
func schemaBuilder(dialect database.Dialect, db *database.DB) *schema.Builder {
	connection := database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(dialect),
		"name":   conformanceConnection,
	})
	return database.ForMigrations(connection).Schema()
}

// microsecond is an instant with a fraction no column at whole seconds keeps.
func microsecond() time.Time {
	return time.Date(2026, 10, 19, 12, 34, 56, 123456000, time.UTC)
}

// testBlueprintTimestampKeepsTheMicrosecond: a column declared with no
// precision keeps the fraction of the second, so two rows written within one
// second keep their order and a value reads back as the instant written.
// PostgreSQL rounded a value to the nearest second in a timestamp(0), and
// MySQL truncated it.
func testBlueprintTimestampKeepsTheMicrosecond(t *testing.T, dialect database.Dialect, db *database.DB) {
	ctx := context.Background()
	name := table("instant")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	if err := schemaBuilder(dialect, db).Create(ctx, name, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.Timestamp("at")
		table.Timestamps()
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := microsecond()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, at, created_at) VALUES (?, ?, ?)`, name), "1", want, want); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var at, created time.Time
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT at, created_at FROM %s WHERE id = ?`, name), "1").Scan(&at, &created); err != nil {
		t.Fatalf("select: %v", err)
	}
	if !at.UTC().Equal(want) || !created.UTC().Equal(want) {
		t.Errorf("the instants came back as %s and %s, want %s", at.UTC(), created.UTC(), want)
	}
}

// testWideningKeepsTheRows: a table created in whole seconds is widened in
// place -- its rows, its nullability and its index stay -- and keeps the
// fraction from then on. On PostgreSQL the widening rewrites neither the table
// nor its index.
func testWideningKeepsTheRows(t *testing.T, dialect database.Dialect, db *database.DB) {
	ctx := context.Background()
	name := table("seconds")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	builder := schemaBuilder(dialect, db)
	if err := builder.Create(ctx, name, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.Timestamp("at", 0)
		table.Timestamp("seen_at", 0).Nullable()
		table.Index([]string{"at"}, name+"_at_index")
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	whole := microsecond().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, at) VALUES (?, ?)`, name), "1", whole); err != nil {
		t.Fatalf("insert: %v", err)
	}

	filenodes := func() (string, string) {
		if dialect != database.DialectPostgres {
			return "", ""
		}
		var table, index string
		if err := db.QueryRowContext(ctx, `SELECT pg_relation_filenode(?::regclass)::text, pg_relation_filenode(?::regclass)::text`,
			name, name+"_at_index").Scan(&table, &index); err != nil {
			t.Fatalf("reading the file nodes: %v", err)
		}
		return table, index
	}
	tableBefore, indexBefore := filenodes()

	altered, err := builder.WidenTimePrecision(ctx, name, schema.MaxTimePrecision)
	if err != nil {
		t.Fatalf("widening: %v", err)
	}
	if dialect == database.DialectSQLite {
		if altered != nil {
			t.Fatalf("SQLite altered %v, and has nothing to widen", altered)
		}
	} else if len(altered) != 2 {
		t.Fatalf("altered %v, want at and seen_at", altered)
	}
	if again, err := builder.WidenTimePrecision(ctx, name, schema.MaxTimePrecision); err != nil || again != nil {
		t.Fatalf("widening a second time altered %v (%v), want nothing", again, err)
	}
	if tableAfter, indexAfter := filenodes(); tableAfter != tableBefore || indexAfter != indexBefore {
		t.Errorf("the widening rewrote the table (%s to %s) or its index (%s to %s)", tableBefore, tableAfter, indexBefore, indexAfter)
	}

	var at time.Time
	var seen *time.Time
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT at, seen_at FROM %s WHERE id = ?`, name), "1").Scan(&at, &seen); err != nil {
		t.Fatalf("select: %v", err)
	}
	if !at.UTC().Equal(whole) || seen != nil {
		t.Errorf("the row came back as %s and %v, want %s and nothing", at.UTC(), seen, whole)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, at) VALUES (?, NULL)`, name), "2"); err == nil {
		t.Error("the widening dropped NOT NULL from at")
	}
	want := microsecond()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, at) VALUES (?, ?)`, name), "3", want); err != nil {
		t.Fatalf("insert after widening: %v", err)
	}
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT at FROM %s WHERE id = ?`, name), "3").Scan(&at); err != nil {
		t.Fatalf("select: %v", err)
	}
	if !at.UTC().Equal(want) {
		t.Errorf("after widening the instant came back as %s, want %s", at.UTC(), want)
	}
}

// testTheOutboxKeepsTheOrderOfOneStore: every event of one Store carries one
// occurred_at, and the relay reads them back in the order they were stored,
// on its migrated table, with a lag that is never negative.
func testTheOutboxKeepsTheOrderOfOneStore(t *testing.T, dialect database.Dialect, db *database.DB) {
	ctx := context.Background()
	drop(t, db, "outbox")
	t.Cleanup(func() { drop(t, db, "outbox") })

	conn := database.ForMigrations(database.NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(dialect),
		"name":   conformanceConnection,
	}))
	for _, m := range events.NewModule().Migrations() {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("%s: %v", m.GetName(), err)
		}
	}
	if _, ok := events.NewModule().Migrations()[2].(migrations.WidenTimePrecision); !ok {
		t.Fatal("the outbox's third migration is not its widening")
	}

	outbox := events.NewOutbox(outboxHandle{db})
	names := []string{"order.placed", "order.paid", "order.shipped", "order.delivered", "order.closed"}
	list := make([]events.Event, 0, len(names))
	for _, name := range names {
		list = append(list, events.Event{Name: name, Aggregate: "order", AggregateID: "o-1"})
	}
	if err := database.Transaction(ctx, db, func(ctx context.Context) error {
		return outbox.Store(ctx, auth.SystemGrant("orders.write", "tenant-1"), list)
	}); err != nil {
		t.Fatalf("storing: %v", err)
	}

	pending, err := outbox.PendingAll(ctx, 10)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(pending) != len(names) {
		t.Fatalf("%d events pending, want %d", len(pending), len(names))
	}
	for i, stored := range pending {
		if stored.Name != names[i] {
			t.Fatalf("event %d is %s, want %s: the relay does not read one Store in the order it was written", i, stored.Name, names[i])
		}
	}
	if lag, err := outbox.Lag(ctx); err != nil || lag < 0 {
		t.Errorf("the lag is %s (%v), want zero or more", lag, err)
	}
}

// outboxHandle is the suite's database as the outbox reads it: the handle,
// and whether a context is inside a transaction on it.
type outboxHandle struct{ *database.DB }

// InTransaction reports whether ctx is inside a transaction on the handle.
func (h outboxHandle) InTransaction(ctx context.Context) bool {
	return database.InTransaction(ctx, h.DB)
}
