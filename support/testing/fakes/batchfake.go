package fakes

import (
	"sync"
	"time"
)

// UpdatedBatchJobCounts is the little of Illuminate\Bus\UpdatedBatchJobCounts
// that the batch fakes hand back: what a batch's counters became after a job
// finished or failed.
//
// The real one lives in the bus package, which is another package's to write;
// this is the shape the fakes return, and the fakes always return it empty,
// because a fake batch never runs a job.
type UpdatedBatchJobCounts struct {
	// PendingJobs answers UpdatedBatchJobCounts::$pendingJobs.
	PendingJobs int
	// FailedJobs answers UpdatedBatchJobCounts::$failedJobs.
	FailedJobs int
}

// AllJobsHaveRanExactlyOnce answers
// UpdatedBatchJobCounts::allJobsHaveRanExactlyOnce.
func (c UpdatedBatchJobCounts) AllJobsHaveRanExactlyOnce() bool {
	return c.PendingJobs == 0 && c.FailedJobs == 0
}

// BatchFake answers Illuminate\Support\Testing\Fakes\BatchFake: the batch a
// faked bus hands back, which remembers what was added to it and never runs
// anything.
//
// It is safe to use from a test that calls t.Parallel: the fields a method
// writes are written under a mutex, and Add is the one a job that fans out
// calls from more than one goroutine.
type BatchFake struct {
	mu sync.Mutex

	// ID answers Batch::$id. The initialism is upper case because Go spells
	// it that way.
	ID string
	// Name answers Batch::$name.
	Name string
	// TotalJobs answers Batch::$totalJobs.
	TotalJobs int
	// PendingJobs answers Batch::$pendingJobs.
	PendingJobs int
	// FailedJobs answers Batch::$failedJobs.
	FailedJobs int
	// FailedJobIDs answers Batch::$failedJobIds.
	FailedJobIDs []string
	// Options answers Batch::$options.
	Options map[string]any
	// CreatedAt answers Batch::$createdAt.
	CreatedAt time.Time
	// CancelledAt answers Batch::$cancelledAt. The zero time is PHP's null.
	CancelledAt time.Time
	// FinishedAt answers Batch::$finishedAt. The zero time is PHP's null.
	FinishedAt time.Time

	// added answers BatchFake::$added, the jobs Add was handed.
	added []any
	// deleted answers BatchFake::$deleted.
	deleted bool
}

// NewBatchFake answers BatchFake::__construct.
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

// Fresh answers BatchFake::fresh: itself, because nothing stored it anywhere
// it could go stale.
func (b *BatchFake) Fresh() *BatchFake {
	return b
}

// Add answers BatchFake::add: the jobs are remembered and the total grows.
func (b *BatchFake) Add(jobs ...any) *BatchFake {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, jobs...)
	b.TotalJobs += len(jobs)
	return b
}

// Added answers BatchFake::$added, the jobs Add was handed.
//
// PHP reads the public property; a Go field named Added and a method named Add
// would be two names for one thing on the same type, so the jobs are read
// through this and written through Add.
func (b *BatchFake) Added() []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]any(nil), b.added...)
}

// RecordSuccessfulJob answers BatchFake::recordSuccessfulJob, and does nothing,
// as the PHP does.
func (b *BatchFake) RecordSuccessfulJob(jobID string) {}

// DecrementPendingJobs answers BatchFake::decrementPendingJobs, and does
// nothing, as the PHP does.
func (b *BatchFake) DecrementPendingJobs(jobID string) {}

// RecordFailedJob answers BatchFake::recordFailedJob, and does nothing, as the
// PHP does.
func (b *BatchFake) RecordFailedJob(jobID string, err error) {}

// IncrementFailedJobs answers BatchFake::incrementFailedJobs, and hands back
// empty counts, as the PHP does.
func (b *BatchFake) IncrementFailedJobs(jobID string) UpdatedBatchJobCounts {
	return UpdatedBatchJobCounts{}
}

// Cancel answers BatchFake::cancel: the batch is marked cancelled now.
func (b *BatchFake) Cancel() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.CancelledAt = time.Now()
}

// Cancelled answers Batch::cancelled.
func (b *BatchFake) Cancelled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.CancelledAt.IsZero()
}

// Delete answers BatchFake::delete: the batch is marked deleted, and stays in
// memory so that Deleted can answer.
func (b *BatchFake) Delete() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = true
}

// Deleted answers BatchFake::deleted.
func (b *BatchFake) Deleted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deleted
}

// Finish answers the finishedAt stamp BatchRepositoryFake::markAsFinished
// writes. It is not a method of the PHP BatchFake -- there the repository
// assigns the public property -- and it exists because a Go field written from
// another type would be written without the lock the rest of the type takes.
func (b *BatchFake) Finish(at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.FinishedAt = at
}

// Finished answers Batch::finished.
func (b *BatchFake) Finished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.FinishedAt.IsZero()
}
