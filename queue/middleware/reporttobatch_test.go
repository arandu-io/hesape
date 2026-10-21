package middleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/arandu-io/hesape/queue/middleware"
)

// final answers whether a failure is the last one, as the worker would.
type final bool

func (f final) IsFinalAttempt(*jobs.Job, error) bool { return bool(f) }

// batched dispatches a batch of two with a Then and a Catch, and returns the
// store, what the dispatch and the callbacks pushed, the batch and its two
// jobs as a worker would pop them.
func batched(t *testing.T) (*bus.Memory, *pushRecorder, bus.Batch, [2]*jobs.Job) {
	t.Helper()
	ctx, g := context.Background(), grantFor(tenant)
	store, pushed := bus.NewMemory(), &pushRecorder{}
	b, err := bus.NewBatch("import").
		Add("invoice.import", nil).
		Add("invoice.import", nil).
		Then("import.done", nil).
		Catch("import.failed", nil).
		Dispatch(ctx, g, store, pushed)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var popped [2]*jobs.Job
	for i := range popped {
		j, _ := jobs.New(g, "", "invoice.import", nil)
		j.Payload = pushed.payloads[i]
		popped[i] = jobs.Popped(&recorder{}, "test", j)
	}
	pushed.payloads = nil
	return store, pushed, b, popped
}

func find(t *testing.T, store *bus.Memory, id string) bus.Batch {
	t.Helper()
	b, err := store.Find(context.Background(), grantFor(tenant), id)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	return b
}

// TestReportToBatchRecordsEverySuccess: the handler reports nothing, and the
// batch still finishes and fires Then when its last job succeeds.
func TestReportToBatchRecordsEverySuccess(t *testing.T) {
	store, pushed, b, popped := batched(t)
	m := middleware.NewReportToBatch(store, pushed, final(false))

	for _, j := range popped {
		if err := m.Handle(context.Background(), j, func(context.Context) error { return nil }); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if got := find(t, store, b.ID); got.PendingJobs != 0 {
		t.Fatalf("pending = %d after every job succeeded, want 0", got.PendingJobs)
	}
	if len(pushed.payloads) != 1 {
		t.Fatalf("pushed %d callbacks, want Then once", len(pushed.payloads))
	}
}

// TestReportToBatchWaitsForTheFinalFailure: a failure the worker retries is
// not a failure of the batch yet, and the one it parks for is -- once.
func TestReportToBatchWaitsForTheFinalFailure(t *testing.T) {
	store, pushed, b, popped := batched(t)
	broken := errors.New("the row does not parse")
	failing := func(context.Context) error { return broken }

	err := middleware.NewReportToBatch(store, pushed, final(false)).Handle(context.Background(), popped[0], failing)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the handler's", err)
	}
	if got := find(t, store, b.ID); got.FailedJobs != 0 || len(pushed.payloads) != 0 {
		t.Fatalf("a retried failure was recorded: failed=%d pushed=%d", got.FailedJobs, len(pushed.payloads))
	}

	err = middleware.NewReportToBatch(store, pushed, final(true)).Handle(context.Background(), popped[0], failing)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the handler's", err)
	}
	if got := find(t, store, b.ID); got.FailedJobs != 1 || len(pushed.payloads) != 1 {
		t.Fatalf("the final failure: failed=%d pushed=%d, want 1 and Catch", got.FailedJobs, len(pushed.payloads))
	}
}

// TestReportToBatchLeavesAReleasedJobAlone: a job an inner middleware put back
// has not run, so it is neither a success nor a failure yet.
func TestReportToBatchLeavesAReleasedJobAlone(t *testing.T) {
	store, pushed, b, popped := batched(t)
	m := middleware.NewReportToBatch(store, pushed, final(false))

	err := m.Handle(context.Background(), popped[0], func(ctx context.Context) error {
		return popped[0].Release(ctx, 0)
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := find(t, store, b.ID); got.PendingJobs != 2 {
		t.Fatalf("pending = %d after a release, want 2", got.PendingJobs)
	}
}

// TestReportToBatchHandsOnAJobInNoBatch: the middleware is safe on a worker
// that also runs jobs pushed on their own.
func TestReportToBatchHandsOnAJobInNoBatch(t *testing.T) {
	j, _ := job(t, tenant, "invoice.send")
	var jobRan bool
	m := middleware.NewReportToBatch(bus.NewMemory(), &pushRecorder{}, final(true))
	if err := m.Handle(context.Background(), j, ran(&jobRan)); err != nil || !jobRan {
		t.Fatalf("Handle = %v, ran = %v", err, jobRan)
	}
}
