package fakes

import (
	"sync"
	"time"
)

// UpdatedBatchJobCounts is what a batch's counters became after a job finished
// or failed.
//
// The batch fakes always return it empty, because a fake batch never runs a
// job.
type UpdatedBatchJobCounts struct {
	// PendingJobs is how many jobs of the batch have yet to run.
	PendingJobs int
	// FailedJobs is how many jobs of the batch failed.
	FailedJobs int
}

// AllJobsHaveRanExactlyOnce reports whether nothing is pending and nothing
// failed.
func (c UpdatedBatchJobCounts) AllJobsHaveRanExactlyOnce() bool {
	return c.PendingJobs == 0 && c.FailedJobs == 0
}

// BatchFake is the batch a faked bus hands back. It remembers what was added
// to it and never runs anything.
//
// It is safe to use from a test that calls t.Parallel: the fields a method
// writes are written under a mutex, and Add is the one a job that fans out
// calls from more than one goroutine.
type BatchFake struct {
	mu sync.Mutex

	// ID identifies the batch.
	ID string
	// Name is what the batch is called, and may be empty.
	Name string
	// TotalJobs is how many jobs the batch holds.
	TotalJobs int
	// PendingJobs is how many have yet to run.
	PendingJobs int
	// FailedJobs is how many failed.
	FailedJobs int
	// FailedJobIDs identifies the jobs that failed.
	FailedJobIDs []string
	// Options are the batch's settings, keyed by name.
	Options map[string]any
	// CreatedAt is when the batch was made.
	CreatedAt time.Time
	// CancelledAt is when the batch was cancelled, and is zero until it is.
	CancelledAt time.Time
	// FinishedAt is when the batch finished, and is zero until it does.
	FinishedAt time.Time

	// added holds the jobs [BatchFake.Add] was handed.
	added []any
	// deleted records that [BatchFake.Delete] was called.
	deleted bool
}

// NewBatchFake builds a batch carrying the given counts and stamps.
func NewBatchFake(
	id string,
	name string,
	totalJobs int,
	pendingJobs int,
	failedJobs int,
	failedJobIDs []string,
	options map[string]any,
	createdAt time.Time,
	cancelledAt time.Time,
	finishedAt time.Time,
) *BatchFake {
	return &BatchFake{
		ID:           id,
		Name:         name,
		TotalJobs:    totalJobs,
		PendingJobs:  pendingJobs,
		FailedJobs:   failedJobs,
		FailedJobIDs: failedJobIDs,
		Options:      options,
		CreatedAt:    createdAt,
		CancelledAt:  cancelledAt,
		FinishedAt:   finishedAt,
	}
}

// Fresh returns the batch itself: nothing stored it anywhere it could go
// stale.
func (b *BatchFake) Fresh() *BatchFake {
	return b
}

// Add remembers the jobs, grows the total and returns the batch.
func (b *BatchFake) Add(jobs ...any) *BatchFake {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, jobs...)
	b.TotalJobs += len(jobs)
	return b
}

// Added returns a copy of the jobs [BatchFake.Add] was handed.
func (b *BatchFake) Added() []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]any(nil), b.added...)
}

// RecordSuccessfulJob does nothing: a fake batch never runs a job.
func (b *BatchFake) RecordSuccessfulJob(jobID string) {}

// DecrementPendingJobs does nothing: the counts are fixed when the batch is
// made.
func (b *BatchFake) DecrementPendingJobs(jobID string) {}

// RecordFailedJob does nothing: a fake batch never runs a job.
func (b *BatchFake) RecordFailedJob(jobID string, err error) {}

// IncrementFailedJobs does nothing and returns empty counts.
func (b *BatchFake) IncrementFailedJobs(jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// Cancel stamps the batch as cancelled at this moment.
func (b *BatchFake) Cancel() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.CancelledAt = time.Now()
}

// Cancelled reports whether the batch was cancelled.
func (b *BatchFake) Cancelled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.CancelledAt.IsZero()
}

// Delete marks the batch deleted. It stays in memory, so
// [BatchFake.Deleted] can still be asked.
func (b *BatchFake) Delete() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = true
}

// Deleted reports whether the batch was deleted.
func (b *BatchFake) Deleted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deleted
}

// Finish stamps the batch as finished at the given moment.
func (b *BatchFake) Finish(at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.FinishedAt = at
}

// Finished reports whether the batch has finished.
func (b *BatchFake) Finished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.FinishedAt.IsZero()
}
