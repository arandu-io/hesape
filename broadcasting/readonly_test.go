package broadcasting_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/queue"
)

// viewingGrant is a Grant issued to a customer an operator is only viewing as.
func viewingGrant(t *testing.T) auth.Grant {
	t.Helper()
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: queuedTenant, Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: queuedTenant})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowAll{}, viewed, "invoice.sent", "")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

type sentNow struct{ queuedInvoiceSent }

func (sentNow) ShouldBroadcastNow() bool { return true }

type rescuedNow struct{ sentNow }

func (rescuedNow) ShouldRescue() bool { return true }

// TestAViewingGrantBroadcastsNothing: the read-only mode of an impersonation
// held only in the database, so an operator viewing a customer's account could
// publish events into the customer's channels as the customer.
func TestAViewingGrantBroadcastsNothing(t *testing.T) {
	assertNothingBroadcast(t, viewingGrant(t), auth.ErrReadOnly)
}

// readsOnly allows everything and says none of its actions write.
type readsOnly struct{ allowAll }

func (readsOnly) Writes(auth.Action) bool { return false }

// TestAReadGrantBroadcastsNothing: a Grant a policy issued for an action it
// says does not write publishes nothing, queued or not.
func TestAReadGrantBroadcastsNothing(t *testing.T) {
	g, err := auth.Authorize(context.Background(), readsOnly{}, auth.Subject{ID: "bea", Tenant: queuedTenant}, "invoice.view", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNothingBroadcast(t, g, auth.ErrReadGrant)
}

// assertNothingBroadcast queues and handles broadcasts with g, wants each
// refused with want, and wants no driver to have published.
func assertNothingBroadcast(t *testing.T, g auth.Grant, want error) {
	t.Helper()
	ctx := context.Background()
	driver := &queuedBroadcastRecorder{}
	worker := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
	queues := queue.NewQueueManager()
	adapter := broadcasting.NewQueueAdapter(queues, queuedBroadcastFactory{driver: driver})
	worker.Handle(broadcasting.BroadcastJobName, adapter)
	queues.Extend("sync", queue.NewSyncQueue(worker))
	manager := broadcasting.NewBroadcastManager(broadcasting.Config{
		Default:     "recorder",
		Connections: map[string]broadcasting.ConnectionConfig{"recorder": {Driver: "recorder"}},
	}, nil, adapter, nil)
	manager.Extend("recorder", func(broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) { return driver, nil })

	events := map[string]any{
		"now":     &sentNow{queuedInvoiceSent{InvoiceID: "inv-17"}},
		"rescued": &rescuedNow{sentNow{queuedInvoiceSent{InvoiceID: "inv-17"}}},
		"queued":  &queuedInvoiceSent{Connection: "sync", InvoiceID: "inv-17"},
	}
	for name, event := range events {
		if err := manager.Queue(ctx, g, event); !errors.Is(err, want) {
			t.Errorf("Queue of the %s event answered %v, want %v", name, err, want)
		}
	}
	if err := broadcasting.NewBroadcastEvent(&queuedInvoiceSent{InvoiceID: "inv-17"}).Handle(ctx, g, queuedBroadcastFactory{driver: driver}); !errors.Is(err, want) {
		t.Errorf("Handle answered %v, want %v", err, want)
	}
	if got := driver.all(); len(got) != 0 {
		t.Fatalf("a Grant that cannot write published %d broadcasts", len(got))
	}
}
