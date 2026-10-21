package middleware

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/queue/jobs"
)

// FinalAttempt is what [ReportToBatch] asks of the worker it runs under:
// whether a failure is the one the job is parked for. queue.Worker answers it.
//
// It is declared here because the worker is built on this package and this
// package cannot import it back.
type FinalAttempt interface {
	IsFinalAttempt(j *jobs.Job, cause error) bool
}

// ReportToBatch reports every job the worker runs to the batch and the chain
// it belongs to, which is the bus.Handled call a handler otherwise has to make
// on every path out of it -- and the handler that forgot it left its batch
// pending forever.
//
// A job that succeeded is recorded as a success and pushes the next link of
// its chain. A job that failed is recorded only on its final attempt, the one
// the worker parks it for: a failure the worker retries is not yet a failure
// of the batch, and recording it would fire Catch for a job that succeeds on
// its second delivery. A job an inner middleware released has not run, and is
// not reported. A job the bus did not write is handed on untouched.
//
// What reporting fails with is joined to the handler's own error, as the
// call written by hand returned it.
type ReportToBatch struct {
	batches bus.BatchRepository
	queue   bus.Queue
	final   FinalAttempt
}

var _ Middleware = (*ReportToBatch)(nil)

// NewReportToBatch returns the middleware over the repository the batches
// live in, the queue their callbacks and the next links of a chain are pushed
// on, and the worker it runs under.
//
// The worker is the one whose options carry it, so it is added once the worker
// exists:
//
//	w := queue.NewWorker(q, opts)
//	opts.Middleware = append(opts.Middleware, middleware.NewReportToBatch(batches, queue.NewBusAdapter(q), w))
//	w.SetOptions(opts)
func NewReportToBatch(batches bus.BatchRepository, q bus.Queue, final FinalAttempt) *ReportToBatch {
	return &ReportToBatch{batches: batches, queue: q, final: final}
}

// Handle runs the job, then reports its outcome to its batch and its chain.
func (m *ReportToBatch) Handle(ctx context.Context, j *jobs.Job, next func(context.Context) error) error {
	err := next(ctx)

	batchable, decodeErr := bus.Batched(j.Payload, nil)
	if decodeErr != nil {
		// A payload the bus cannot read is a job in no batch.
		return err
	}
	switch {
	case err == nil && j.IsReleased():
		return nil
	case err == nil:
		return bus.Handled(ctx, jobs.GrantFor(j), m.batches, m.queue, batchable, nil)
	case m.final.IsFinalAttempt(j, err):
		return errors.Join(err, bus.Handled(ctx, jobs.GrantFor(j), m.batches, m.queue, batchable, err))
	default:
		return err
	}
}
