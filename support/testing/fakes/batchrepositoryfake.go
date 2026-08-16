package fakes

import (
	"sync"
	"time"
)

// BatchRepositoryFake is the batch store a faked bus writes to. It keeps the
// batches in memory and never touches a database.
//
// It is safe to use from a test that calls t.Parallel: every batch is stored
// and read under a mutex.
type BatchRepositoryFake struct {
	mu sync.Mutex
	// batches keeps the batches by id, and order keeps the ids in the order
	// they were stored, because Get hands them back and a store that reorders
	// itself between runs cannot be asserted on.
	batches map[string]*BatchFake
	order   []string
}

// NewBatchRepositoryFake returns an empty batch store.
func NewBatchRepositoryFake() *BatchRepositoryFake {
	return &BatchRepositoryFake{batches: map[string]*BatchFake{}}
}

// Get returns every batch, in the order it was stored. The limit and the
// cursor are ignored: the store is small enough to hand back whole.
func (r *BatchRepositoryFake) Get(limit int, before any) []*BatchFake {
	r.mu.Lock()
	defer r.mu.Unlock()

	batches := make([]*BatchFake, 0, len(r.order))
	for _, id := range r.order {
		batches = append(batches, r.batches[id])
	}
	return batches
}

// Find returns the batch with that id, or nil when the store holds none.
func (r *BatchRepositoryFake) Find(batchID string) *BatchFake {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batches[batchID]
}

// Store makes a [BatchFake] for the pending batch and keeps it under a fresh
// ordered id, which sorts in the order the batches were stored.
//
// Every job counts as pending, none as failed, which is the state a batch is
// in the moment it is dispatched.
func (r *BatchRepositoryFake) Store(batch *PendingBatchFake) *BatchFake {
	id := orderedUUID()

	stored := NewBatchFake(
		id,
		batch.Name,
		len(batch.Jobs),
		len(batch.Jobs),
		0,
		nil,
		batch.Options,
		time.Now(),
		time.Time{},
		time.Time{},
	)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches[id] = stored
	r.order = append(r.order, id)
	return stored
}

// IncrementTotalJobs does nothing: a fake batch's counts are fixed when it is
// stored.
func (r *BatchRepositoryFake) IncrementTotalJobs(batchID string, amount int) {}

// DecrementPendingJobs does nothing and returns empty counts.
func (r *BatchRepositoryFake) DecrementPendingJobs(batchID string, jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// IncrementFailedJobs does nothing and returns empty counts.
func (r *BatchRepositoryFake) IncrementFailedJobs(batchID string, jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// MarkAsFinished stamps the batch with the moment it finished. An id the
// store does not hold is ignored.
func (r *BatchRepositoryFake) MarkAsFinished(batchID string) {
	r.mu.Lock()
	batch := r.batches[batchID]
	r.mu.Unlock()

	if batch != nil {
		batch.Finish(time.Now())
	}
}

// Cancel marks the batch cancelled. An id the store does not hold is
// ignored.
func (r *BatchRepositoryFake) Cancel(batchID string) {
	r.mu.Lock()
	batch := r.batches[batchID]
	r.mu.Unlock()

	if batch != nil {
		batch.Cancel()
	}
}

// Delete forgets the batch entirely, dropping it from the order too.
func (r *BatchRepositoryFake) Delete(batchID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.batches[batchID]; !ok {
		return
	}
	delete(r.batches, batchID)
	for i, id := range r.order {
		if id == batchID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Transaction calls the callback with nothing around it: there is no storage
// to open a transaction on.
func (r *BatchRepositoryFake) Transaction(callback func() any) any {
	return callback()
}

// RollBack does nothing: there is no transaction to roll back.
func (r *BatchRepositoryFake) RollBack() {}
