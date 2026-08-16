package failed

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// FailedJob is one job that gave up.
//
// It is the record a dead letter list holds: what the job was, whose it was,
// why it gave up and when.
type FailedJob struct {
	// ID identifies this failure. It is the job's own UUID, because the id is
	// minted by the application and a second one would only be a second thing
	// to quote at somebody.
	ID string
	// UUID is the job's identifier, which is the same string as ID. It is kept
	// because it is what a provider indexes and what `aru queue:retry` takes.
	UUID string
	// TenantID is who the work belonged to, and it is not optional: a failed
	// job list that crossed customers would be one customer reading another's
	// payloads.
	TenantID string
	// Connection is the queue connection it was on.
	Connection string
	// Queue is the queue it was on.
	Queue string
	// Name is what routes the job to a handler.
	Name string
	// Payload is the job's arguments, as they were stored.
	Payload []byte
	// Exception is why it gave up.
	Exception string
	// FailedAt is when.
	FailedAt time.Time
}

// FailedJobProvider is where a job goes when it gives up.
//
// Every method takes a context and an auth.Grant.
//
// The Grant is not decoration. A failed job carries a customer's payload, and
// "list the failed jobs" is a read like any other -- so it takes a Grant and
// every implementation filters by auth.Tenant(g). A provider that answered
// across tenants would be the one query in the collection that leaks, and it
// would leak the arguments of every job every customer ever queued.
type FailedJobProvider interface {
	// Log records a job that gave up, and returns the id it was recorded under.
	Log(ctx context.Context, g auth.Grant, job FailedJob) (string, error)

	// IDs is the identifiers of the failed jobs, newest first. An empty queue
	// means every queue.
	IDs(ctx context.Context, g auth.Grant, queue string) ([]string, error)

	// All is the failed jobs, newest first.
	All(ctx context.Context, g auth.Grant) ([]FailedJob, error)

	// Find is one failed job, or an error wrapping [ErrNotFound].
	Find(ctx context.Context, g auth.Grant, id string) (FailedJob, error)

	// Forget removes one failed job, and reports whether there was one.
	Forget(ctx context.Context, g auth.Grant, id string) (bool, error)

	// Flush removes the failed jobs older than age. A zero age removes all of
	// them, which is what `queue:flush` with no --hours does.
	Flush(ctx context.Context, g auth.Grant, age time.Duration) error
}

// CountableFailedJobProvider is a provider that can say how many.
//
// It is a second interface rather than a sixth method on [FailedJobProvider]:
// counting is what a monitor does every minute, and a provider that would have
// to load every row to answer should not pretend it can.
type CountableFailedJobProvider interface {
	// Count is how many failed jobs there are. An empty connection or queue
	// means every one.
	Count(ctx context.Context, g auth.Grant, connection, queue string) (int, error)
}

// PrunableFailedJobProvider is a provider that can drop old entries.
type PrunableFailedJobProvider interface {
	// Prune removes the entries that failed before this instant, and returns
	// how many went.
	Prune(ctx context.Context, g auth.Grant, before time.Time) (int, error)
}

// ErrNotFound is what Find wraps when there is no such failed job.
//
// It is an error rather than a zero value, because "no such job" and "a job
// with no payload" are different answers and a command that retries the second
// one silently does nothing.
var ErrNotFound = errors.New("queue/failed: no such failed job")

// ErrNoTenant is returned when a Grant carries no tenant.
//
// It mirrors jobs.ErrNoTenant and exists for the same reason: a query with no
// tenant to filter by is a query that reads every customer's failures.
var ErrNoTenant = errors.New("queue/failed: the Grant carries no tenant, and a failed job list cannot be scoped without one")

// tenantOf is the tenant a provider filters by, or an error.
func tenantOf(g auth.Grant) (string, error) {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return "", ErrNoTenant
	}
	return tenant, nil
}
