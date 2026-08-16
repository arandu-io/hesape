package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
)

const tenant = "tenant-1"

// TestStoreRefusesOutsideATransaction is the guarantee this package exists for.
//
// An event written next to a row that then rolled back makes the rest of the
// system react to something that did not happen. An event written after the
// commit is one crash away from never leaving. Both are worse than an error at
// the call site.
func TestStoreRefusesOutsideATransaction(t *testing.T) {
	outbox := events.NewOutbox(&handle{})

	err := outbox.Store(context.Background(), grant(tenant), []events.Event{
		{Name: "customer.created", Aggregate: "customer", AggregateID: "1"},
	})
	if !errors.Is(err, events.ErrNoTransaction) {
		t.Fatalf("err = %v, want ErrNoTransaction", err)
	}
}

// TestStoringNothingIsNotAnError: a write that produced no event is the common
// case, and forcing every caller to check the length first would put the same
// three lines in every service.
func TestStoringNothingIsNotAnError(t *testing.T) {
	outbox := events.NewOutbox(&handle{})

	if err := outbox.Store(context.Background(), grant(tenant), nil); err != nil {
		t.Fatalf("storing no events: %v", err)
	}
}

// TestStoreWritesTheGrantIntoTheRow: who authorized it, which action and which
// tenant are the audit trail, and they are in the row rather than in a second
// table.
func TestStoreWritesTheGrantIntoTheRow(t *testing.T) {
	db, table := newFakeOutbox()
	t.Cleanup(func() { _ = db.Close() })
	outbox := events.NewOutbox(db)

	err := outbox.Store(context.Background(), grant(tenant), []events.Event{
		{Name: "invoice.paid", Aggregate: "invoice", AggregateID: "inv-1", Payload: map[string]int{"amount": 1250}},
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	rows := table.stored()
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.TenantID != tenant {
		t.Errorf("tenant = %q, want %q", got.TenantID, tenant)
	}
	if got.Action != "outbox.store" {
		t.Errorf("action = %q, want the action the Grant was issued for", got.Action)
	}
	if got.ID == "" {
		t.Error("the row has no id: the consumer deduplicates on it")
	}
	if got.OccurredAt.IsZero() {
		t.Error("an event with no OccurredAt was stored without one, and the relay orders by it")
	}
	if got.Payload != `{"amount":1250}` {
		t.Errorf("payload = %s", got.Payload)
	}
}

// TestStoreRefusesAGrantWithNoTenant guards the one place an event becomes a
// row: a relay reading a row with no tenant would not know who to deliver it
// to.
func TestStoreRefusesAGrantWithNoTenant(t *testing.T) {
	db, _ := newFakeOutbox()
	t.Cleanup(func() { _ = db.Close() })

	err := events.NewOutbox(db).Store(context.Background(), auth.Grant{}, []events.Event{
		{Name: "invoice.paid"},
	})
	if err == nil {
		t.Fatal("an event was stored against no tenant")
	}
}

// TestStoreRefusesAnEventWithNoName: the name is what routes it, and a row
// nothing subscribes to is a write nobody will ever notice went missing.
func TestStoreRefusesAnEventWithNoName(t *testing.T) {
	db, _ := newFakeOutbox()
	t.Cleanup(func() { _ = db.Close() })

	err := events.NewOutbox(db).Store(context.Background(), grant(tenant), []events.Event{
		{Aggregate: "invoice", AggregateID: "inv-1"},
	})
	if err == nil {
		t.Fatal("an event with no name was stored")
	}
}

// TestRecorderClearsWhatItHandsOver: an entity stored twice must not emit the
// same event twice.
func TestRecorderClearsWhatItHandsOver(t *testing.T) {
	var r events.Recorder
	r.Record(events.Event{Name: "invoice.paid"})
	r.Record(events.Event{Name: "invoice.closed"})

	first := r.PullEvents()
	if len(first) != 2 {
		t.Fatalf("pulled %d events, want 2", len(first))
	}
	if second := r.PullEvents(); len(second) != 0 {
		t.Fatalf("pulled %d events the second time, want 0", len(second))
	}
}

// TestTheRecorderKeepsOrder: events are a sequence of facts, and two events
// about the same aggregate only make sense in the order they happened.
func TestTheRecorderKeepsOrder(t *testing.T) {
	var r events.Recorder
	for _, name := range []string{"a", "b", "c"} {
		r.Record(events.Event{Name: name})
	}
	for i, e := range r.PullEvents() {
		if want := []string{"a", "b", "c"}[i]; e.Name != want {
			t.Errorf("event %d = %q, want %q", i, e.Name, want)
		}
	}
}

func TestDecodeReadsThePayloadBack(t *testing.T) {
	stored := events.Stored{Name: "invoice.paid", Payload: `{"amount":1250,"currency":"BRL"}`}

	var payload struct {
		Amount   int    `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := stored.Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload.Amount != 1250 || payload.Currency != "BRL" {
		t.Fatalf("payload = %+v", payload)
	}
}

// TestTheEventCarriesWhenItHappened: OccurredAt is the domain's clock, and a
// relay that reorders by insertion time would deliver a correction before the
// thing it corrects.
func TestTheEventCarriesWhenItHappened(t *testing.T) {
	when := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	e := events.Event{Name: "invoice.paid", OccurredAt: when}

	if !e.OccurredAt.Equal(when) {
		t.Fatal("OccurredAt was not kept")
	}
}

func grant(tenant string) auth.Grant {
	return auth.SystemGrant("outbox.store", tenant)
}

// TestTheDiagnosisIsSilentWhenNothingIsWrong: a diagnosis that always says
// something is a diagnosis nobody reads, and the error page has limited room
// before people stop looking at it.
func TestTheDiagnosisIsSilentWhenNothingIsWrong(t *testing.T) {
	if got := events.NewModule().Diagnose(context.Background()); len(got) != 0 {
		t.Fatalf("a module with no relay diagnosed %v", got)
	}
}

// TestAModuleWithNoRelayIsHealthy: storing without publishing is a real state,
// not a broken one. Storing is what cannot be recovered later; publishing can
// start the day there is something to publish to.
func TestAModuleWithNoRelayIsHealthy(t *testing.T) {
	if err := events.NewModule().Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if err := events.NewModule().Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := events.NewModule().Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
