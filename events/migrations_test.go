package events_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/foundation"
)

// outboxColumns is every column the Outbox names in the statements it issues:
// the insert in Store, the reads in Pending, PendingAll, Parked and Lag, and the
// updates in MarkPublished, MarkFailed, Park and Retry. A column the outbox
// writes and the migration does not create is a Store that fails on the first
// event, in production, after the deploy.
var outboxColumns = []string{
	"id", "tenant_id", "event", "aggregate", "aggregate_id", "payload",
	"authorized_by", "action", "occurred_at", "published_at", "attempts",
	"last_error", "failed_at",
}

// TestTheModuleBringsTheOutboxTable: an application that registers this module
// and nothing from a bridge still has to end up with the table an Outbox
// writes to. Before, the module declared no migration, and the table existed
// only where something else copied it in.
func TestTheModuleBringsTheOutboxTable(t *testing.T) {
	var module foundation.Module = events.NewModule()

	migratable, ok := module.(foundation.Migratable)
	if !ok {
		t.Fatal("the module does not declare migrations, so the kernel collects no outbox table")
	}
	ms := migratable.Migrations()
	if len(ms) != 2 {
		t.Fatalf("%d migrations, want the table and its dead-letter column", len(ms))
	}

	var up []string
	for _, m := range ms {
		statements, err := migrations.UpStatements(context.Background(), m)
		if err != nil {
			t.Fatalf("%s: %v", m.GetName(), err)
		}
		up = append(up, statements...)
	}
	joined := strings.ToLower(strings.Join(up, "\n"))

	if !strings.Contains(joined, `create table "outbox"`) {
		t.Fatalf("no outbox table is created:\n%s", joined)
	}
	for _, column := range outboxColumns {
		if !strings.Contains(joined, `"`+column+`"`) {
			t.Errorf("the outbox writes %s, and no migration creates it", column)
		}
	}
	for _, index := range []string{"idx_outbox_pending", "idx_outbox_tenant", "idx_outbox_unfinished"} {
		if !strings.Contains(joined, `create index "`+index+`"`) {
			t.Errorf("%s is not created, and the relay scans the table without it", index)
		}
	}
}

// TestTheOutboxMigrationsKeepTheirNames: a migration's name is its identity in
// the migrations table. These are the names an application that migrated the
// outbox before this module declared it has already recorded; a different
// name runs the create again, on a table that exists.
func TestTheOutboxMigrationsKeepTheirNames(t *testing.T) {
	want := []string{
		"2026_07_31_000001_create_outbox_table",
		"2026_07_31_000002_add_outbox_dead_letter",
	}
	ms := events.NewModule().Migrations()
	if len(ms) != len(want) {
		t.Fatalf("%d migrations, want %d", len(ms), len(want))
	}
	for i, m := range ms {
		if m.GetName() != want[i] {
			t.Errorf("migration %d is %q, want %q", i, m.GetName(), want[i])
		}
	}
}

// TestTheOutboxMigrationsRollBack: both are reversible, and the Migrator finds
// Down by a type assertion, so a Down it cannot find is a rollback that
// silently does nothing.
func TestTheOutboxMigrationsRollBack(t *testing.T) {
	ms := events.NewModule().Migrations()
	if len(ms) != 2 {
		t.Fatalf("%d migrations, want the table and its dead-letter column", len(ms))
	}
	for _, m := range ms {
		if _, ok := m.(migrations.ReversibleMigration); !ok {
			t.Fatalf("%s is not reversible", m.GetName())
		}
	}

	create, err := migrations.DownStatements(context.Background(), ms[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(create) != 1 || !strings.Contains(strings.ToLower(create[0]), `drop table if exists "outbox"`) {
		t.Errorf("rolling back the create runs %q, want the table dropped", create)
	}

	deadLetter, err := migrations.DownStatements(context.Background(), ms[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(deadLetter) == 0 || !strings.Contains(strings.ToLower(deadLetter[0]), `drop index "idx_outbox_unfinished"`) {
		t.Errorf("rolling back the dead letter runs %q, want the index dropped before the column it names", deadLetter)
	}
}

// TestARelayDoesNotChangeTheTable: storing comes before publishing, so the
// table is the same whether or not this process runs the relay.
func TestARelayDoesNotChangeTheTable(t *testing.T) {
	withRelay := events.WithRelay(nil).Migrations()
	without := events.NewModule().Migrations()

	if len(withRelay) != len(without) {
		t.Fatalf("%d migrations with a relay, %d without", len(withRelay), len(without))
	}
	for i := range without {
		if withRelay[i].GetName() != without[i].GetName() {
			t.Errorf("migration %d differs: %q and %q", i, withRelay[i].GetName(), without[i].GetName())
		}
	}
}
