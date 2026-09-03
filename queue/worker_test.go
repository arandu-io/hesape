package queue_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

// deadline is how long a test waits for the worker before it says the worker
// never got there. It is a failure deadline and never a synchronisation: every
// step these tests take is a channel.
const deadline = 5 * time.Second

// storeQueue is a driver that answers the way a store does rather than the way
// a map does: a settlement on a cancelled context fails, which is what
// db.ExecContext returns and what an in-memory fake cannot show.
//
// The whole shutdown question is invisible to a driver that ignores its
// context, because there the write that a cancelled shutdown killed still
// appears to have happened.
type storeQueue struct {
	queue.NullQueue

	mu       sync.Mutex
	ready    []jobs.Job
	deleted  []string
	released map[string]time.Duration
	parked   []string
	pops     int
	// afterPop runs once a batch has been handed over, with its jobs already
	// reserved. It is where a test puts the signal that has to land in that gap.
	afterPop func()
}

func newStoreQueue(js ...jobs.Job) *storeQueue {
	return &storeQueue{ready: js, released: map[string]time.Duration{}}
}

func (q *storeQueue) GetConnectionName() string { return "store" }

func (q *storeQueue) Pop(ctx context.Context, _ string, n int, _ time.Duration) ([]*jobs.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	q.mu.Lock()
	q.pops++
	if n > len(q.ready) {
		n = len(q.ready)
	}
	out := make([]*jobs.Job, 0, n)
	for _, j := range q.ready[:n] {
		// Like the real drivers: the delivery is counted when the job is handed
		// over, so Attempts includes this one.
		j.Attempts++
		out = append(out, jobs.Popped(q, "store", j))
	}
	q.ready = q.ready[n:]
	after := q.afterPop
	q.mu.Unlock()

	if len(out) > 0 && after != nil {
		after()
	}
	return out, nil
}

func (q *storeQueue) DeleteJob(ctx context.Context, j *jobs.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deleted = append(q.deleted, j.UUID)
	return nil
}

func (q *storeQueue) ReleaseJob(ctx context.Context, j *jobs.Job, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.released[j.UUID] = delay
	return nil
}

func (q *storeQueue) FailJob(ctx context.Context, j *jobs.Job, _ error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.parked = append(q.parked, j.UUID)
	return nil
}

// settled is what became of the jobs this driver handed over.
func (q *storeQueue) settled() (deleted []string, released map[string]time.Duration, parked []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	released = map[string]time.Duration{}
	for id, delay := range q.released {
		released[id] = delay
	}
	return append([]string(nil), q.deleted...), released, append([]string(nil), q.parked...)
}

// reserved is how many jobs are still out on a lease: handed over and never
// settled. It is the number a shutdown must not leave above zero.
func (q *storeQueue) reserved(handed int) int {
	deleted, released, parked := q.settled()
	return handed - len(deleted) - len(released) - len(parked)
}

func (q *storeQueue) popCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pops
}

func (q *storeQueue) waiting() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ready)
}

func newJob(t *testing.T, name string) jobs.Job {
	t.Helper()
	j, err := jobs.New(grant(), "", name, map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// runDaemon starts the worker and returns the channel its exit arrives on.
func runDaemon(w *queue.Worker, ctx context.Context) <-chan error {
	stopped := make(chan error, 1)
	go func() { stopped <- mustDaemon(w, ctx) }()
	return stopped
}

func awaitStop(t *testing.T, stopped <-chan error) {
	t.Helper()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Daemon: %v", err)
		}
	case <-time.After(deadline):
		t.Fatal("the worker never returned after the shutdown signal")
	}
}

func await(t *testing.T, what string, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(deadline):
		t.Fatalf("%s never happened", what)
	}
}

// TestAJobInFlightIsSettledAfterTheShutdownSignal: the deploy case.
//
// SIGTERM cancels the worker's context, and the delete that says the work is
// done used to ride it. Cancelled, the delete fails, the job stays reserved
// until its lease expires -- five minutes by default -- and comes back with an
// attempt already spent. On every deploy, for every job in flight.
func TestAJobInFlightIsSettledAfterTheShutdownSignal(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep:         time.Millisecond,
		ShutdownGrace: time.Minute,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	deleted, released, parked := q.settled()
	if len(deleted) != 1 || deleted[0] != j.UUID {
		t.Fatalf("the job was not deleted after the shutdown: deleted=%v released=%v parked=%v",
			deleted, released, parked)
	}
	if left := q.reserved(1); left != 0 {
		t.Errorf("%d job(s) are still reserved after the shutdown", left)
	}
}

// TestAJobThatFailsDuringTheShutdownIsPutBack is the other half: the release
// after a failure rode the cancelled context too, so the job was neither
// deleted nor put back -- reserved, invisible, and waiting out its lease.
func TestAJobThatFailsDuringTheShutdownIsPutBack(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep:         time.Millisecond,
		ShutdownGrace: time.Minute,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return errors.New("the payment gateway is down")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	deleted, released, parked := q.settled()
	delay, back := released[j.UUID]
	if !back {
		t.Fatalf("the job was not put back after the shutdown: deleted=%v released=%v parked=%v",
			deleted, released, parked)
	}
	if delay <= 0 {
		t.Errorf("the job came back with no backoff (%s), so a failing handler spins", delay)
	}
}

// TestTheWorkerReservesNothingAfterTheShutdownSignal: draining is finishing
// what is in flight, not taking more. A worker that popped again during the
// drain would leave the batch it took reserved when the process exits.
func TestTheWorkerReservesNothingAfterTheShutdownSignal(t *testing.T) {
	first, second := newJob(t, "invoice.send"), newJob(t, "invoice.send")
	q := newStoreQueue(first, second)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Concurrency:   1,
		Sleep:         time.Millisecond,
		ShutdownGrace: time.Minute,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	if pops := q.popCount(); pops != 1 {
		t.Errorf("the worker asked for jobs %d times; the signal landed after the first", pops)
	}
	if waiting := q.waiting(); waiting != 1 {
		t.Errorf("%d job(s) left on the queue, want the one that was never reserved", waiting)
	}
	deleted, _, _ := q.settled()
	if len(deleted) != 1 || deleted[0] != first.UUID {
		t.Errorf("the job in flight was not the only one settled: %v", deleted)
	}
}

// TestABatchIsNotAbandonedWhenTheSignalLandsAfterThePop: the gap between the
// reservation and the run.
//
// Every job of a batch is hidden by the same lease at the moment it is popped.
// The loop that started them used to check the context between jobs and break,
// so a signal landing in that gap left the rest of the batch reserved and
// unrun: invisible to every worker, and nothing anywhere recording that they
// were owed.
func TestABatchIsNotAbandonedWhenTheSignalLandsAfterThePop(t *testing.T) {
	first, second := newJob(t, "invoice.send"), newJob(t, "invoice.send")
	q := newStoreQueue(first, second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The signal, exactly between the reservation and the run.
	q.afterPop = cancel

	var ran atomic.Int32
	w := queue.NewWorker(q, queue.WorkerOptions{
		Concurrency:   2,
		Sleep:         time.Millisecond,
		ShutdownGrace: time.Minute,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		ran.Add(1)
		return nil
	})

	awaitStop(t, runDaemon(w, ctx))

	if got := ran.Load(); got != 2 {
		t.Errorf("%d of the 2 reserved jobs ran", got)
	}
	if left := q.reserved(2); left != 0 {
		deleted, released, parked := q.settled()
		t.Errorf("%d reserved job(s) were abandoned by the shutdown: deleted=%v released=%v parked=%v",
			left, deleted, released, parked)
	}
}

// TestTheDrainDeadlineEndsAJobThatOutlivesIt: the drain is bounded, and running
// out of it is not the same as losing the job.
//
// A handler that keeps working past the grace is cancelled the way the timeout
// cancels one, and its failure settles the job on a context the shutdown does
// not reach -- so the process exits and the work is back on the queue.
func TestTheDrainDeadlineEndsAJobThatOutlivesIt(t *testing.T) {
	const grace = 50 * time.Millisecond

	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)

	running := make(chan struct{})
	ended := make(chan time.Time, 1)
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep:         time.Millisecond,
		ShutdownGrace: grace,
	})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ *jobs.Job) error {
		close(running)
		<-ctx.Done()
		ended <- time.Now()
		return ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	signalled := time.Now()
	cancel()

	var cancelledAt time.Time
	select {
	case cancelledAt = <-ended:
	case <-time.After(deadline):
		t.Fatal("the handler was never cancelled, so the grace does not bound the drain")
	}
	awaitStop(t, stopped)

	if waited := cancelledAt.Sub(signalled); waited < grace {
		t.Errorf("the handler was cancelled %s after the signal, before the %s grace: "+
			"the shutdown killed it rather than draining it", waited, grace)
	}
	_, released, _ := q.settled()
	if _, back := released[j.UUID]; !back {
		t.Errorf("the job was not put back when the grace ran out; it is reserved until its lease expires")
	}
}
