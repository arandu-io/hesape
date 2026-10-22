package webhook

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

type allowManagerDispatch struct{}

func (allowManagerDispatch) Can(context.Context, auth.Subject, auth.Action, struct{}) error {
	return nil
}

// creatingStore counts the deliveries a Manager asked its Store to write,
// whatever the transaction around them later decided.
type creatingStore struct {
	*memoryStore
	creates int
}

func (s *creatingStore) Create(ctx context.Context, g auth.Grant, delivery ManagerDelivery) (bool, error) {
	s.creates++
	return s.memoryStore.Create(ctx, g, delivery)
}

// TestTheManagerRecordsNothingForAViewingGrant: a store the application
// supplies need not be the database, so the delivery was handed to it before
// anything asked whether the Grant could write, and a store with no rollback
// kept it after the queue refused.
func TestTheManagerRecordsNothingForAViewingGrant(t *testing.T) {
	store := &creatingStore{memoryStore: newMemoryStore()}
	target := &memoryQueue{store: store.memoryStore}
	manager, err := newManager(store, target, NewStaticSecret([]byte(testSecret)), ManagerOptions{HTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatal(err)
	}
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: "tenant-a", Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowManagerDispatch{}, viewed, ActionDispatch, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	err = manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]ManagerEndpoint{{ID: "accounting", URL: "https://hooks.example.test/invoices", SecretRef: "accounting-key"}})
	if !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("Dispatch with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
	if store.creates != 0 || len(target.jobs) != 0 {
		t.Fatalf("a viewing Grant handed the store %d deliveries and queued %d jobs", store.creates, len(target.jobs))
	}
}
