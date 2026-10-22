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

	g := viewingGrant(t)
	events := map[string]any{
		"now":     &sentNow{queuedInvoiceSent{InvoiceID: "inv-17"}},
		"rescued": &rescuedNow{sentNow{queuedInvoiceSent{InvoiceID: "inv-17"}}},
		"queued":  &queuedInvoiceSent{Connection: "sync", InvoiceID: "inv-17"},
	}
	for name, event := range events {
		if err := manager.Queue(ctx, g, event); !errors.Is(err, auth.ErrReadOnly) {
			t.Errorf("Queue of the %s event with a viewing Grant answered %v, want auth.ErrReadOnly", name, err)
		}
	}
	if err := broadcasting.NewBroadcastEvent(&queuedInvoiceSent{InvoiceID: "inv-17"}).Handle(ctx, g, queuedBroadcastFactory{driver: driver}); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Handle with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
	if got := driver.all(); len(got) != 0 {
		t.Fatalf("a viewing Grant published %d broadcasts", len(got))
	}
}
