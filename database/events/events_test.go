package events_test

import (
	"testing"

	"github.com/arandu-io/hesape/database/events"
)

// fakeConnection answers the two interfaces the events take.
type fakeConnection struct{ name string }

func (c fakeConnection) GetName() string { return c.name }

func (c fakeConnection) PrepareBindings(bindings []any) []any { return bindings }

func (c fakeConnection) SubstituteBindingsIntoRawSQL(sql string, bindings []any) string {
	if len(bindings) == 0 {
		return sql
	}
	return sql + " /* bound */"
}

func TestConnectionEventReadsTheNameOffTheConnection(t *testing.T) {
	event := events.NewConnectionEstablished(fakeConnection{name: "primary"})

	if event.ConnectionName != "primary" {
		t.Fatalf("ConnectionName = %q; the PHP constructor reads it rather than taking it", event.ConnectionName)
	}
	if event.Connection == nil {
		t.Fatal("the event does not carry the connection")
	}
}

func TestConnectionEventSurvivesANilConnection(t *testing.T) {
	// A test constructs one of these by hand more often than the framework
	// does, and a panic in an event constructor is a panic in a listener.
	event := events.NewTransactionCommitted(nil)

	if event.ConnectionName != "" {
		t.Fatalf("ConnectionName = %q for a nil connection", event.ConnectionName)
	}
}

func TestQueryExecutedToRawSQL(t *testing.T) {
	event := events.NewQueryExecuted("select ?", []any{1}, 1.25, fakeConnection{name: "primary"}, "write")

	if event.ConnectionName != "primary" {
		t.Fatalf("ConnectionName = %q", event.ConnectionName)
	}
	if event.Time != 1.25 {
		t.Fatalf("Time = %v; it is milliseconds, which is what every threshold is compared in", event.Time)
	}
	if event.ToRawSQL() != "select ? /* bound */" {
		t.Fatalf("ToRawSQL = %q", event.ToRawSQL())
	}

	bare := events.NewQueryExecuted("select 1", nil, 0, nil, "")
	if bare.ToRawSQL() != "select 1" {
		t.Fatal("ToRawSQL on an event with no connection did not answer the query unchanged")
	}
}

func TestMigrationEvents(t *testing.T) {
	if got := events.NewMigrationsStarted("up", map[string]any{"pretend": true}); got.Method != "up" {
		t.Fatalf("Method = %q", got.Method)
	}
	if got := events.NewNoPendingMigrations("down"); got.Method != "down" {
		t.Fatalf("Method = %q", got.Method)
	}
	if got := events.NewMigrationSkipped("2026_01_01_000000_first"); got.MigrationName != "2026_01_01_000000_first" {
		t.Fatalf("MigrationName = %q", got.MigrationName)
	}
}

func TestSchemaAndPruneEvents(t *testing.T) {
	dumped := events.NewSchemaDumped(fakeConnection{name: "primary"}, "database/schema/pgsql-schema.sql")
	if dumped.ConnectionName != "primary" || dumped.Path == "" {
		t.Fatalf("SchemaDumped = %+v", dumped)
	}

	loaded := events.NewSchemaLoaded(fakeConnection{name: "primary"}, "database/schema/pgsql-schema.sql")
	if loaded.ConnectionName != "primary" {
		t.Fatalf("SchemaLoaded = %+v", loaded)
	}

	pruned := events.NewModelsPruned("Invoice", 12)
	if pruned.Model != "Invoice" || pruned.Count != 12 {
		t.Fatalf("ModelsPruned = %+v", pruned)
	}
}
