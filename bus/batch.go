package bus

import "time"

// Batch is a group of jobs dispatched together and counted as one.
//
// The counters are the whole point. Total never moves; Pending falls by one for
// every job that reports back, whatever it reported; Failed rises for the ones
// that reported an error. When Pending reaches zero the batch is finished, and
// finishing is what fires the callbacks.
//
// A Batch value is a snapshot. It is read from the store, it is already stale
// when it arrives, and nothing decides anything by mutating it -- Store.Record
// is the only thing that moves a counter, and it moves it in the store.
type Batch struct {
	// ID identifies the batch and travels inside the payload of every job in
	// it.
	ID string
	// Name is for people: it is what the batch is called on a dashboard and in
	// a log line. It is not unique and nothing looks a batch up by it.
	Name string
	// TenantID is who the batch belongs to. It comes from the Grant that
	// dispatched it, and every statement about the batch filters on it.
	TenantID string
	// Queue is where the jobs and the callbacks go when a step does not name
	// its own.
	Queue string
	// Total is how many jobs were dispatched. Pending is how many have not
	// reported yet. Failed is how many reported an error.
	Total   int
	Pending int
	Failed  int
	// AllowFailures decides what a failure does to the rest of the batch: with
	// it, the remaining jobs keep going and the batch finishes when the last
	// one reports; without it, the first failure finishes the batch and the
	// jobs still queued are told to stop by Member.Cancelled.
	//
	// Catch fires on the first failure either way. AllowFailures is about the
	// other jobs, not about whether anyone is told.
	AllowFailures bool
	// Then runs when every job succeeded. Catch runs on the first failure.
	// Finally runs when the batch stops, whichever way it stopped. Each fires
	// at most once, and each is a job rather than a function -- see the package
	// doc for why.
	Then    Step
	Catch   Step
	Finally Step
	// CreatedAt is when it was dispatched. CancelledAt and FinishedAt are zero
	// until they happen.
	CreatedAt   time.Time
	CancelledAt time.Time
	FinishedAt  time.Time
}

// Processed is how many jobs have reported back, successfully or not.
func (b Batch) Processed() int { return b.Total - b.Pending }

// Progress is Processed as a percentage, rounded down, 0 when the batch is
// empty.
//
// Rounded down on purpose: a progress bar that shows 100% while work is still
// running is the one number a user reads as "it is stuck".
func (b Batch) Progress() int {
	if b.Total <= 0 {
		return 0
	}
	return b.Processed() * 100 / b.Total
}

// Finished reports whether the batch has stopped, successfully or not.
func (b Batch) Finished() bool { return !b.FinishedAt.IsZero() }

// Cancelled reports whether the batch was cancelled.
//
// A cancelled batch is not finished: the jobs already in the queue still get
// delivered, still report back, and the batch finishes when the last one does.
// Cancelling stops the work, not the bookkeeping.
func (b Batch) Cancelled() bool { return !b.CancelledAt.IsZero() }

// HasFailures reports whether any job in the batch failed.
func (b Batch) HasFailures() bool { return b.Failed > 0 }

// stopped reports whether this state is terminal.
//
// Pending at zero is the ordinary end. A failure with AllowFailures off is the
// other one: the batch is done even though jobs remain, because nothing they
// could report would change the outcome.
func (b Batch) stopped() bool {
	return b.Pending <= 0 || (b.Failed > 0 && !b.AllowFailures)
}
