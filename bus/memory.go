package bus

import (
	"context"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Memory is a Store in process memory.
//
// It is the driver and the test double in one type, for the reason the queue
// gives for its own: a fake that is not the thing gets the interesting cases
// wrong, and the interesting cases here are all about ordering -- two failures
// at the same instant, a report arriving after the batch already stopped.
// Memory serializes exactly like the SQL store does, so a test that passes
// against it is a test about the rule and not about the mutex.
//
// It is not a second way to run batches in production. Counters in one
// process's heap are lost with the process and invisible to the replica next to
// it, so a batch dispatched here is a batch only this binary can finish.
type Memory struct {
	mu      sync.Mutex
	batches map[string]Batch // keyed by tenant and id
	now     func() time.Time
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{batches: map[string]Batch{}, now: func() time.Time { return time.Now().UTC() }}
}

var _ Store = (*Memory)(nil)

// key namespaces a batch by tenant, so two tenants holding the same id -- which
// nothing forbids, because an id is generated per tenant's work -- do not see
// each other's counters.
func key(tenant, id string) string { return tenant + "\x00" + id }

// Create persists a new batch.
func (m *Memory) Create(_ context.Context, g auth.Grant, b Batch) error {
	tenant, err := tenantOf(g)
	if err != nil {
		return err
	}
	b.TenantID = tenant
	if b.CreatedAt.IsZero() {
		b.CreatedAt = m.now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.batches[key(tenant, b.ID)] = b
	return nil
}

// Find returns a batch.
func (m *Memory) Find(_ context.Context, g auth.Grant, id string) (Batch, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return Batch{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[key(tenant, id)]
	if !ok {
		return Batch{}, notFound(id)
	}
	return b, nil
}

// Cancel marks the batch cancelled.
func (m *Memory) Cancel(_ context.Context, g auth.Grant, id string) error {
	tenant, err := tenantOf(g)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[key(tenant, id)]
	if !ok {
		return notFound(id)
	}
	if b.Cancelled() || b.Finished() {
		return nil
	}
	b.CancelledAt = m.now()
	m.batches[key(tenant, id)] = b
	return nil
}

// Record registers the outcome of one job.
func (m *Memory) Record(_ context.Context, g auth.Grant, id string, ok bool) (Recorded, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return Recorded{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	b, found := m.batches[key(tenant, id)]
	if !found {
		return Recorded{}, notFound(id)
	}
	b, r := advance(b, ok, m.now())
	m.batches[key(tenant, id)] = b
	return r, nil
}

// Prune deletes finished batches created before the cut.
func (m *Memory) Prune(_ context.Context, g auth.Grant, before time.Time) (int, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, b := range m.batches {
		if b.TenantID != tenant || !b.Finished() || !b.CreatedAt.Before(before) {
			continue
		}
		delete(m.batches, k)
		n++
	}
	return n, nil
}
