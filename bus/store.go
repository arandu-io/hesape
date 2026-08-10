package bus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// Store is where batch counters live.
//
// It is a port with two implementations in this package: SQL, which is the one
// an application runs, and Memory, which is the one a test runs. Every method
// takes a Grant and every statement filters by the tenant it carries, reads
// included -- a batch is a row about a customer's work, and RULE 17 does not
// have a "but it is only a counter" clause.
//
// The interface is small because a batch is small: it is created once, read
// often, decremented once per job and deleted eventually. Anything richer would
// be a query builder over one table.
type Store interface {
	// Create persists a new batch. It is called once, by PendingBatch.Dispatch,
	// before the first job is pushed -- a job that arrives before its batch
	// exists has nothing to report to.
	Create(ctx context.Context, g auth.Grant, b Batch) error

	// Find returns a batch, or an error wrapping database.ErrNotFound.
	Find(ctx context.Context, g auth.Grant, id string) (Batch, error)

	// Cancel marks the batch cancelled, so the jobs still queued skip their
	// work. Cancelling a batch that is already cancelled or already finished
	// changes nothing and is not an error: the caller wanted it stopped and it
	// is.
	Cancel(ctx context.Context, g auth.Grant, id string) error

	// Record registers the outcome of one job and reports what that did to the
	// batch. It is the only thing that moves a counter.
	Record(ctx context.Context, g auth.Grant, id string, ok bool) (Recorded, error)

	// Prune deletes finished batches created before the cut, and returns how
	// many went. A batch is a row that nothing reads once its callbacks have
	// run, and a table nobody prunes is a table that only grows.
	Prune(ctx context.Context, g auth.Grant, before time.Time) (int, error)
}

// Recorded is what one call to Store.Record changed.
//
// The two booleans exist so a callback fires once instead of once per delivery.
// Delivery is at-least-once: the same job can report twice, two jobs can fail at
// the same moment on two machines, and a batch of ten thousand will do both. The
// store is the only place that can answer "was this the call that finished it",
// because it is the only place that saw the counter before and after.
type Recorded struct {
	// Batch is the batch as it stands after this call.
	Batch Batch
	// FirstFailure is true on the call that took Failed from zero to one, and
	// on no other. It is what fires Catch.
	FirstFailure bool
	// Finished is true on the call that stopped the batch, and on no other. It
	// is what fires Then and Finally.
	Finished bool
}

// ErrNoTenant is returned when a Grant carries no usable tenant.
//
// It is an error rather than a default. A batch with no tenant would be counted
// against every customer at once, and the callbacks would run under a Grant
// nobody issued.
var ErrNoTenant = errors.New("bus: the Grant carries no tenant, and a batch without one cannot be scoped")

// tenantOf reads the tenant a statement must filter by.
func tenantOf(g auth.Grant) (string, error) {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return "", ErrNoTenant
	}
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: %q cannot be one", ErrNoTenant, tenant)
	}
	return tenant, nil
}

// advance applies one job outcome to a batch and says what it changed.
//
// Both stores call it, so the rule for when a callback fires is written once.
// Written twice it would drift, and the drift would show up as a report emailed
// twice by one deployment and never by the other.
//
// A batch that has already stopped keeps counting -- the numbers on the
// dashboard stay true for the jobs that were still in flight -- but reports
// nothing: its callbacks have run.
func advance(b Batch, ok bool, now time.Time) (Batch, Recorded) {
	wasFinished := b.Finished()
	firstFailure := !ok && b.Failed == 0

	if b.Pending > 0 {
		b.Pending--
	}
	if !ok {
		b.Failed++
	}
	if !wasFinished && b.stopped() {
		b.FinishedAt = now
	}

	return b, Recorded{
		Batch:        b,
		FirstFailure: firstFailure && !wasFinished,
		Finished:     !wasFinished && b.Finished(),
	}
}

// notFound is the one shape of "no such batch" in this package.
//
// It wraps database.ErrNotFound rather than introducing a sentinel of its own,
// so errors.Is keeps answering the question every caller actually asks -- and so
// an HTTP handler turns it into a 404 without learning a second name for the
// same thing.
func notFound(id string) error {
	return fmt.Errorf("%w: batch %s", database.ErrNotFound, id)
}
