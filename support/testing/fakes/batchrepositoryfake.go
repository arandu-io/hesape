package fakes

import (
	"sync"
	"time"
)

// BatchRepositoryFake answers
// Illuminate\Support\Testing\Fakes\BatchRepositoryFake: the batch store a
// faked bus writes to, which keeps the batches in memory and never touches a
// database.
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

// NewBatchRepositoryFake answers BatchRepositoryFake's implicit constructor.
func NewBatchRepositoryFake() *BatchRepositoryFake {
	return &BatchRepositoryFake{batches: map[string]*BatchFake{}}
}

// Get answers BatchRepositoryFake::get: every batch, in the order it was
// stored.
//
// The PHP takes a limit and a cursor and ignores both, and so does this.
func (r *BatchRepositoryFake) Get(limit int, before any) []*BatchFake {
	r.mu.Lock()
	defer r.mu.Unlock()

	batches := make([]*BatchFake, 0, len(r.order))
	for _, id := range r.order {
		batches = append(batches, r.batches[id])
	}
	return batches
}

// Find answers BatchRepositoryFake::find: the batch with that id, or nil when
// there is none, which is the PHP's null.
func (r *BatchRepositoryFake) Find(batchID string) *BatchFake {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batches[batchID]
}

// Store answers BatchRepositoryFake::store: a BatchFake is made for the
// pending batch and kept under a fresh ordered id.
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

// IncrementTotalJobs answers BatchRepositoryFake::incrementTotalJobs, and does
// nothing, as the PHP does.
func (r *BatchRepositoryFake) IncrementTotalJobs(batchID string, amount int) {}

// DecrementPendingJobs answers BatchRepositoryFake::decrementPendingJobs, and
// hands back empty counts, as the PHP does.
func (r *BatchRepositoryFake) DecrementPendingJobs(batchID string, jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// IncrementFailedJobs answers BatchRepositoryFake::incrementFailedJobs, and
// hands back empty counts, as the PHP does.
func (r *BatchRepositoryFake) IncrementFailedJobs(batchID string, jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// MarkAsFinished answers BatchRepositoryFake::markAsFinished: the batch is
// stamped with the moment it finished, and an id that is not stored is
// ignored, as the PHP ignores it.
func (r *BatchRepositoryFake) MarkAsFinished(batchID string) {
	r.mu.Lock()
	batch := r.batches[batchID]
	r.mu.Unlock()

	if batch != nil {
		batch.Finish(time.Now())
	}
}

// Cancel answers BatchRepositoryFake::cancel.
func (r *BatchRepositoryFake) Cancel(batchID string) {
	r.mu.Lock()
	batch := r.batches[batchID]
	r.mu.Unlock()

	if batch != nil {
		batch.Cancel()
	}
}

// Delete answers BatchRepositoryFake::delete: the batch is forgotten entirely,
// which is what unset() does there.
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

// Transaction answers BatchRepositoryFake::transaction: the callback is called,
// with nothing around it, because there is no storage to open a transaction on.
func (r *BatchRepositoryFake) Transaction(callback func() any) any {
	return callback()
}

// RollBack answers BatchRepositoryFake::rollBack, and does nothing, as the PHP
// does. The name is the PHP's, capital B and all.
func (r *BatchRepositoryFake) RollBack() {}
