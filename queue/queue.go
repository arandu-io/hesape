package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

// Handler does the work.
//
// The Grant is rebuilt from the job's tenant and action, so a handler reaches
// repositories the same way a service does. There is no unauthorized path into
// the database from a worker, which is the whole point of the Grant existing.
//
// It answers Illuminate\Queue\CallQueuedHandler: the payload names the work and
// something has to turn that name into code. Here the name is a string and the
// registry is the [Worker], because resolving a class out of a serialized
// payload is what ADR 0001 refused a container for.
type Handler interface {
	Handle(ctx context.Context, g auth.Grant, j *jobs.Job) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, g auth.Grant, j *jobs.Job) error

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, g auth.Grant, j *jobs.Job) error {
	return f(ctx, g, j)
}

// Queue is what a driver implements.
//
// It is Illuminate\Contracts\Queue\Queue plus ClearableQueue, with the two
// differences a compiler makes worth having: every push takes an auth.Grant, so
// the tenant comes from the Grant and not from an argument somebody can get
// wrong (RULE 14), and Pop takes a count and a lease rather than returning one
// job at a time -- a worker with a concurrency of four asks for four.
//
// Pop rather than a channel of jobs: the job has to stay in the store,
// invisible to other workers, until it is settled. A worker that dies mid-job
// must not lose it, and that is not something a channel can offer.
type Queue interface {
	// Push adds a job to the queue named on it.
	//
	// Inside database.Transaction the DatabaseQueue joins it, which is the
	// property that driver exists for: the job is committed by the same
	// transaction as the row it describes, so it cannot refer to a write that
	// rolled back.
	Push(ctx context.Context, g auth.Grant, j jobs.Job) error

	// PushOn adds a job to a named queue, ignoring the one on the job.
	PushOn(ctx context.Context, g auth.Grant, queue string, j jobs.Job) error

	// Later adds a job that becomes eligible after delay.
	Later(ctx context.Context, g auth.Grant, delay time.Duration, j jobs.Job) error

	// Bulk adds many jobs. A driver that can do it in one round trip does; the
	// contract only promises that either all of them arrive or an error says
	// otherwise.
	Bulk(ctx context.Context, g auth.Grant, js []jobs.Job) error

	// Pop takes up to n jobs off a queue and hides them for the lease.
	//
	// Jobs whose lease expires become visible again -- which is what makes a
	// worker crash recoverable and delivery at-least-once.
	//
	// The returned jobs carry Attempts INCLUDING this delivery: a job handed
	// over for the first time has Attempts == 1. A driver that returns the
	// count from before the delivery makes the worker park a job one attempt
	// early, and with MaxAttempts of 2 it parks on the first failure and never
	// retries at all.
	//
	// Each job carries the queue it came off, so the worker settles it by
	// calling Release, Delete or Fail on the job rather than on the driver.
	Pop(ctx context.Context, queue string, n int, lease time.Duration) ([]*jobs.Job, error)

	// Size is how many jobs the queue holds, waiting or in flight.
	Size(ctx context.Context, queue string) (int, error)

	// PendingSize is how many jobs are waiting. It feeds the health check: a
	// queue that only grows is a worker that is not running.
	//
	// A job whose lease is still valid is running, not waiting.
	PendingSize(ctx context.Context, queue string) (int, error)

	// CreationTimeOfOldestPendingJob is when the oldest waiting job became
	// eligible, or the zero time when nothing is waiting.
	//
	// A stopped worker looks exactly like an idle one, and this is what tells
	// them apart. A job scheduled for the future returns a time in the future,
	// so a caller measuring lag with time.Since gets a negative number and can
	// treat it as no lag at all.
	CreationTimeOfOldestPendingJob(ctx context.Context, queue string) (time.Time, error)

	// Clear removes every job on a queue and returns how many went.
	//
	// It answers Illuminate\Contracts\Queue\ClearableQueue.
	Clear(ctx context.Context, queue string) (int, error)

	// Failed lists the jobs that gave up, most recent failure first.
	Failed(ctx context.Context, limit int) ([]jobs.Job, error)

	// Retry puts a failed job back in line with its attempts reset.
	//
	// Without it the only way out of a dead letter queue is SQL by hand, which
	// is how it becomes a table nobody touches.
	Retry(ctx context.Context, uuid string) error
}

// ErrNoConnection is returned when a connection nobody registered is asked for.
var ErrNoConnection = fmt.Errorf("queue: no such connection")

// Manager holds the configured connections and hands them out by name.
//
// It answers Illuminate\Queue\QueueManager, minus the half of it that is the
// container: there are no connectors and no lazy resolution from config, because
// the application constructs its queues in bootstrap/app.go and passes them here
// (ADR 0001). What is left is the part anybody actually calls -- `aru work
// --connection=redis` has to turn a string into a queue, and the default
// connection has to have a name.
type Manager struct {
	def         string
	connections map[string]Queue
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{connections: map[string]Queue{}}
}

// Extend registers a connection under a name.
//
// The first one registered becomes the default, so an application with one
// queue never has to say which it means.
func (m *Manager) Extend(name string, q Queue) *Manager {
	if m.def == "" {
		m.def = name
	}
	m.connections[name] = q
	return m
}

// DefaultConnection is the name Connection uses when asked for "".
func (m *Manager) DefaultConnection() string { return m.def }

// SetDefaultConnection names the connection Connection uses when asked for "".
func (m *Manager) SetDefaultConnection(name string) { m.def = name }

// Connection returns a registered queue. An empty name means the default.
func (m *Manager) Connection(name string) (Queue, error) {
	if name == "" {
		name = m.def
	}
	q, known := m.connections[name]
	if !known {
		return nil, fmt.Errorf("%w: %q. Register it with Extend in bootstrap/app.go", ErrNoConnection, name)
	}
	return q, nil
}

// Connections lists the registered names, for `aru queue:monitor` to print.
func (m *Manager) Connections() []string {
	out := make([]string, 0, len(m.connections))
	for name := range m.connections {
		out = append(out, name)
	}
	return out
}
