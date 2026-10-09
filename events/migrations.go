package events

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// Both migrations are reversible, and the assertion is here rather than
// discovered at rollback: the Migrator tests for Down with a type assertion, so
// a Down with the wrong signature would leave a rollback that silently does
// nothing.
var (
	_ migrations.ReversibleMigration = createOutboxTable{}
	_ migrations.ReversibleMigration = addOutboxDeadLetter{}
)

// createOutboxTable is the outbox and the two indexes the relay reads it by.
type createOutboxTable struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
//
// It is the name an application that migrated the table before this package
// declared it has already recorded, so the table is created once whichever
// module brought it.
func (createOutboxTable) GetName() string { return "2026_07_31_000001_create_outbox_table" }

// Up creates the table and its indexes.
//
// Portable types only: TEXT, INTEGER and TIMESTAMP mean the same thing on
// SQLite, Postgres and MySQL. jsonb would be one engine's spelling, and the
// payload is written and read as JSON text either way.
func (createOutboxTable) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, "outbox", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.Text("event")
		table.Text("aggregate")
		table.Text("aggregate_id")
		table.Text("payload")
		table.Text("authorized_by")
		table.Text("action")
		table.Timestamp("occurred_at")
		table.Timestamp("published_at").Nullable()
		table.BigInteger("attempts").Default(0)
		table.Text("last_error").Nullable()

		// The relay reads unpublished events oldest first. A partial index would
		// be tighter, and MySQL does not have one; the two leading columns give
		// the same scan on every engine.
		table.Index([]string{"published_at", "occurred_at"}, "idx_outbox_pending")

		// Deduplication is the consumer's job, and the id is the key it
		// deduplicates on. Delivery is at-least-once: the same event can arrive
		// twice, and that is the price of never losing one.
		table.Index([]string{"tenant_id", "occurred_at"}, "idx_outbox_tenant")
	})
}

// Down drops the table, which takes its indexes with it.
func (createOutboxTable) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, "outbox")
}

// addOutboxDeadLetter is where an event goes after it has failed too often.
//
// A separate migration rather than an edit to the one above, because the first
// one has already run somewhere. The column is nullable, so the previous binary
// keeps working during a rollout -- it simply never writes it.
type addOutboxDeadLetter struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (addOutboxDeadLetter) GetName() string { return "2026_07_31_000002_add_outbox_dead_letter" }

// Up adds the column and the index that reads around it.
func (addOutboxDeadLetter) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, "outbox", func(table *schema.Blueprint) {
		// Nullable, and that is the rollout rule rather than a preference: a
		// NOT NULL column added to a table that has rows fails on every row
		// already there.
		table.Timestamp("failed_at").Nullable()

		// The relay reads pending events on every tick, and "pending" now means
		// neither published nor parked.
		table.Index([]string{"failed_at", "published_at", "occurred_at"}, "idx_outbox_unfinished")
	})
}

// Down drops the index before the column it names.
func (addOutboxDeadLetter) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, "outbox", func(table *schema.Blueprint) {
		table.DropIndex("idx_outbox_unfinished")
		table.DropColumn("failed_at")
	})
}
