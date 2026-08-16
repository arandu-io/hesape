package queue_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/events"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/arandu-io/hesape/queue/middleware"
)

const tenant = "11111111-1111-4111-8111-111111111111"

func grant() auth.Grant { return auth.SystemGrant("invoice.send", tenant) }

// TestAJobWithoutATenantIsRefused checks the tenant at the queue. A job with no
// tenant cannot be scoped, and everything the handler touches would read across
// customers.
func TestAJobWithoutATenantIsRefused(t *testing.T) {
	_, err := jobs.New(auth.SystemGrant("invoice.send", ""), "", "invoice.send", nil)
	if !errors.Is(err, jobs.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

func TestAJobWithoutANameIsRefused(t *testing.T) {
	if _, err := jobs.New(grant(), "", "", nil); !errors.Is(err, jobs.ErrNoName) {
		t.Fatalf("err = %v, want ErrNoName", err)
	}
}

// TestTheJobCarriesTheGrantThatPushedIt: the audit trail, and what the worker
// reissues the work under. A worker that invented its own Grant would be a way
// to reach the database with permissions nobody granted.
func TestTheJobCarriesTheGrantThatPushedIt(t *testing.T) {
	j, err := jobs.New(grant(), "", "invoice.send", map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if j.TenantID != tenant || j.Action != "invoice.send" {
		t.Fatalf("job = %+v", j)
	}
	if j.Queue != jobs.DefaultQueue {
		t.Errorf("queue = %q, want the default", j.Queue)
	}
	if j.UUID == "" {
		t.Error("the job has no id, and the id is what a handler deduplicates on")
	}

	g := jobs.GrantFor(&j)
	if g.Subject().Tenant != tenant || g.Action() != "invoice.send" {
		t.Errorf("the rebuilt Grant does not match the push: %+v", g.Subject())
	}
	// The rebuilt Grant has to pass the check for the action it was issued for,
	// and only that one.
	if err := g.Check("invoice.send"); err != nil {
		t.Errorf("the rebuilt Grant fails its own action: %v", err)
	}
	if err := g.Check("invoice.delete"); err == nil {
		t.Error("the rebuilt Grant passed for an action it was not issued for")
	}
}

// TestAForgedJobIsRefused: New takes the tenant and the action from the Grant,
// but Push takes a Job and a Job is a struct anybody can fill in. Without this
// check the queue is the one way past the authorization the collection exists
// to enforce.
func TestAForgedJobIsRefused(t *testing.T) {
	forged := jobs.Job{
		UUID:     "j-1",
		Name:     "invoice.send",
		TenantID: "22222222-2222-4222-8222-222222222222",
	}
	if err := jobs.Authorized(grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Errorf("a job naming another tenant was accepted: %v", err)
	}

	escalated := jobs.Job{UUID: "j-2", Name: "invoice.send", Action: "invoice.delete"}
	if err := jobs.Authorized(grant(), escalated); !errors.Is(err, jobs.ErrForged) {
		t.Errorf("a job naming an action the Grant does not carry was accepted: %v", err)
	}
}

// TestADetachedJobCannotSettleItself: a job built to be pushed has no queue to
// release it back onto, and saying so beats a nil dereference in a handler.
func TestADetachedJobCannotSettleItself(t *testing.T) {
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Delete(context.Background()); !errors.Is(err, jobs.ErrDetached) {
		t.Errorf("Delete = %v, want ErrDetached", err)
	}
}

func TestThePayloadSurvives(t *testing.T) {
	j, err := jobs.New(grant(), "mail", "invoice.send", map[string]any{"id": "i-1", "amount": 1250})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var payload struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
	}
	if err := j.Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload.ID != "i-1" || payload.Amount != 1250 {
		t.Fatalf("payload = %+v", payload)
	}
	if j.Queue != "mail" {
		t.Errorf("queue = %q", j.Queue)
	}
}

// TestBackoffIsCapped: unbounded doubling means the eleventh attempt is next
// year, and a job nobody will ever see fail is worse than one that parks.
func TestBackoffIsCapped(t *testing.T) {
	if got := queue.ExponentialBackoff(1); got != 2*time.Second {
		t.Errorf("attempt 1 waits %s", got)
	}
	if got := queue.ExponentialBackoff(3); got != 8*time.Second {
		t.Errorf("attempt 3 waits %s", got)
	}
	for _, attempt := range []int{20, 100} {
		if got := queue.ExponentialBackoff(attempt); got != time.Hour {
			t.Errorf("attempt %d waits %s, want the cap", attempt, got)
		}
	}
	// Attempt zero is a caller mistake, not a reason to wait no time at all and
	// hammer the store.
	if got := queue.ExponentialBackoff(0); got <= 0 {
		t.Errorf("attempt 0 waits %s", got)
	}
}

// TestTwoHandlersForOneNamePanics: two handlers for one name is an import
// nobody meant to add, and finding out at boot beats finding out from work that
// silently went to the wrong place.
func TestTwoHandlersForOneNamePanics(t *testing.T) {
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error { return nil })

	defer func() {
		if recover() == nil {
			t.Fatal("a second handler for the same name was accepted")
		}
	}()
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error { return nil })
}

// fakeQueue is the in-memory driver the worker tests run against. What it
// proves is the loop; what the DatabaseQueue does to rows is proved against a
// database in database_test.go.
type fakeQueue struct {
	mu       sync.Mutex
	ready    []jobs.Job
	deleted  []jobs.Job
	released []released
	failed   []failure
}

type released struct {
	job   jobs.Job
	delay time.Duration
}

type failure struct {
	job   jobs.Job
	cause error
}

var (
	_ queue.Queue = (*fakeQueue)(nil)
	_ jobs.Driver = (*fakeQueue)(nil)
)

func (q *fakeQueue) Push(_ context.Context, g auth.Grant, j jobs.Job) error {
	if err := jobs.Authorized(g, j); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ready = append(q.ready, jobs.Prepare(g, j))
	return nil
}

func (q *fakeQueue) PushOn(ctx context.Context, g auth.Grant, name string, j jobs.Job) error {
	j.Queue = name
	return q.Push(ctx, g, j)
}

func (q *fakeQueue) Later(ctx context.Context, g auth.Grant, delay time.Duration, j jobs.Job) error {
	j.RunAt = time.Now().UTC().Add(delay)
	return q.Push(ctx, g, j)
}

func (q *fakeQueue) Bulk(ctx context.Context, g auth.Grant, js []jobs.Job) error {
	for _, j := range js {
		if err := q.Push(ctx, g, j); err != nil {
			return err
		}
	}
	return nil
}

func (q *fakeQueue) Pop(_ context.Context, _ string, n int, _ time.Duration) ([]*jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.ready) == 0 {
		return nil, nil
	}
	if n > len(q.ready) {
		n = len(q.ready)
	}
	out := make([]*jobs.Job, 0, n)
	for _, j := range q.ready[:n] {
		// Like the real drivers: the delivery is counted when the job is
		// handed over, so Attempts includes this one.
		j.Attempts++
		out = append(out, jobs.Popped(q, "fake", j))
	}
	q.ready = q.ready[n:]
	return out, nil
}

func (q *fakeQueue) Size(context.Context, string) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ready), nil
}

func (q *fakeQueue) PendingSize(ctx context.Context, name string) (int, error) {
	return q.Size(ctx, name)
}

func (q *fakeQueue) CreationTimeOfOldestPendingJob(context.Context, string) (time.Time, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.ready) == 0 {
		return time.Time{}, nil
	}
	return q.ready[0].RunAt, nil
}

func (q *fakeQueue) Clear(context.Context, string) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.ready)
	q.ready = nil
	return n, nil
}

func (q *fakeQueue) Failed(_ context.Context, limit int) ([]jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]jobs.Job, 0, len(q.failed))
	for _, f := range q.failed {
		out = append(out, f.job)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (q *fakeQueue) Retry(context.Context, string) error { return nil }

func (q *fakeQueue) ReleaseJob(_ context.Context, j *jobs.Job, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.released = append(q.released, released{job: *j, delay: delay})
	return nil
}

func (q *fakeQueue) DeleteJob(_ context.Context, j *jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deleted = append(q.deleted, *j)
	return nil
}

func (q *fakeQueue) FailJob(_ context.Context, j *jobs.Job, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, failure{job: *j, cause: cause})
	return nil
}

func (q *fakeQueue) state() (deleted []jobs.Job, rel []released, failed []failure) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]jobs.Job(nil), q.deleted...),
		append([]released(nil), q.released...),
		append([]failure(nil), q.failed...)
}

func TestTheWorkerRunsAndDeletes(t *testing.T) {
	q := &fakeQueue{}
	j, err := jobs.New(grant(), "", "invoice.send", map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Push(context.Background(), grant(), j)

	var got string
	var gotGrant auth.Grant
	done := make(chan struct{})

	w := queue.NewWorker(q, queue.WorkerOptions{Sleep: time.Millisecond})
	w.HandleFunc("invoice.send", func(_ context.Context, g auth.Grant, j *jobs.Job) error {
		got, gotGrant = j.UUID, g
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mustDaemon(w, ctx) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job never ran")
	}
	cancel()

	if got != j.UUID {
		t.Errorf("ran %s, want %s", got, j.UUID)
	}
	// The handler receives a Grant for the tenant that pushed it, which is what
	// lets it reach a repository at all.
	if gotGrant.Subject().Tenant != tenant {
		t.Errorf("the handler got a Grant for %q", gotGrant.Subject().Tenant)
	}

	waitFor(t, func() bool {
		deleted, _, _ := q.state()
		return len(deleted) == 1
	}, "the finished job was not deleted")
}

// TestAFailedJobIsReleasedThenParked: a worker stuck on one bad payload stops
// draining everything behind it.
func TestAFailedJobIsReleasedThenParked(t *testing.T) {
	q := &fakeQueue{}
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{Sleep: time.Millisecond, MaxTries: 2})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the invoice has no address")
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mustDaemon(w, ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		_, rel, _ := q.state()
		return len(rel) == 1
	}, "the first failure did not put the job back")

	_, rel, failed := q.state()
	if len(failed) != 0 {
		t.Error("the first failure parked the job instead of releasing it")
	}
	if rel[0].job.LastError != "the invoice has no address" {
		t.Errorf("the reason did not reach the driver: %q", rel[0].job.LastError)
	}
	if rel[0].delay <= 0 {
		t.Errorf("the job came back immediately, with no backoff: %s", rel[0].delay)
	}

	// The second attempt reaches the try limit and parks.
	second := j
	second.Attempts = 1
	_ = q.Push(context.Background(), grant(), second)

	waitFor(t, func() bool {
		_, _, failed := q.state()
		return len(failed) == 1
	}, "the job was not parked after the last attempt")
}

// TestAJobWithNoHandlerParksImmediately: no amount of retrying registers a
// handler, and a job retried forever is one nobody ever looks at.
func TestAJobWithNoHandlerParksImmediately(t *testing.T) {
	q := &fakeQueue{}
	j, _ := jobs.New(grant(), "", "report.monthly", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{Sleep: time.Millisecond})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mustDaemon(w, ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		_, _, failed := q.state()
		return len(failed) == 1
	}, "a job with no handler was not parked")
}

// TestTheWorkerStopsOnCancel: shutdown has to be clean, or a deploy leaves
// workers holding leases nobody will release.
func TestTheWorkerStopsOnCancel(t *testing.T) {
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{Sleep: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- mustDaemon(w, ctx) }()

	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not stop")
	}
}

func waitFor(t *testing.T, cond func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(message)
}

// TestTheBatchRunsInParallel is a bug an audit found.
//
// Pop hides the whole batch for one Lease, and the batch used to run one job at
// a time. With Concurrency 4, Lease 5m and a two-minute handler, the fourth job
// started at minute six -- past its own lease, visible again to another worker,
// and running in both. At-least-once became exactly-twice for the tail of every
// batch.
//
// The handler here waits for all four to arrive. Serially, none of them ever
// does, and the test times out instead of passing slowly.
func TestTheBatchRunsInParallel(t *testing.T) {
	const batch = 4

	q := &fakeQueue{}
	for range batch {
		j, err := jobs.New(grant(), "", "slow.thing", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Push(context.Background(), grant(), j); err != nil {
			t.Fatal(err)
		}
	}

	arrived := make(chan struct{}, batch)
	release := make(chan struct{})

	w := queue.NewWorker(q, queue.WorkerOptions{
		Concurrency: batch,
		Sleep:       time.Millisecond,
	})
	w.HandleFunc("slow.thing", func(ctx context.Context, _ auth.Grant, _ *jobs.Job) error {
		arrived <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- mustDaemon(w, ctx) }()

	for i := range batch {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatalf("only %d of %d jobs in the batch were running at once", i, batch)
		}
	}
	close(release)
	stop()
	<-done
}

// TestProductionBuildsNoCollector is the promise in the Collector's own doc
// comment: "zero cost, not low cost".
//
// The worker built one on every job and threw it away, so production paid to
// record every query with its bound arguments and its caller frames, and nobody
// could read any of it. The allocation is the symptom; retaining the arguments
// of every statement is the cost that matters.
func TestProductionBuildsNoCollector(t *testing.T) {
	q := &fakeQueue{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatal(err)
	}

	seen := make(chan *log.Collector, 1)
	w := queue.NewWorker(q, queue.WorkerOptions{Sleep: time.Millisecond})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ *jobs.Job) error {
		seen <- log.FromContext(ctx)
		return nil
	})

	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- mustDaemon(w, ctx) }()

	select {
	case col := <-seen:
		if col != nil {
			t.Error("a worker with no Recorder still built a Collector")
		}
	case <-ctx.Done():
		t.Fatal("the job never ran")
	}
	stop()
	<-done
}

// TestARecorderPutsTheJobOnTheConsole is the other half, and the promise doc 16
// makes: a task or a job is investigated on the same page as a request. The
// collector used to be created and dropped, so the console never showed one.
func TestARecorderPutsTheJobOnTheConsole(t *testing.T) {
	q := &fakeQueue{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatal(err)
	}

	recorder := log.NewRecorder(8)
	w := queue.NewWorker(q, queue.WorkerOptions{Sleep: time.Millisecond, Recorder: recorder})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ *jobs.Job) error {
		// Something a person would want to see on the page.
		log.FromContext(ctx).RecordEvent("invoice.rendered", nil)
		return nil
	})

	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- mustDaemon(w, ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for recorder.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	<-done

	recent := recorder.Recent(1)
	if len(recent) != 1 {
		t.Fatal("the finished job never reached the console")
	}
	if recent[0].Method != "job" || recent[0].Path != "invoice.send" {
		t.Errorf("the console entry reads %q %q, want job invoice.send", recent[0].Method, recent[0].Path)
	}
	if events := recent[0].Collector.Events(); len(events) != 1 || events[0].Name != "invoice.rendered" {
		t.Errorf("what the handler recorded did not survive: %+v", events)
	}
}

// TestAMiddlewareThatReleasedTheJobIsNotOverruled is why the worker asks
// DeletedOrReleased before it settles anything: WithoutOverlapping releases the
// job for ten seconds, and a worker that released it again for the backoff --
// or parked it -- would undo the decision the middleware just made.
func TestAMiddlewareThatReleasedTheJobIsNotOverruled(t *testing.T) {
	q := &fakeQueue{}
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep:    time.Millisecond,
		MaxTries: 1, // the worker would park on the first failure
		Middleware: []middleware.Middleware{
			middleware.Func(func(ctx context.Context, j *jobs.Job, _ func(context.Context) error) error {
				return j.Release(ctx, 10*time.Second)
			}),
		},
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		t.Error("the handler ran even though the middleware released the job")
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mustDaemon(w, ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		_, rel, _ := q.state()
		return len(rel) == 1
	}, "the middleware never released the job")

	deleted, rel, failed := q.state()
	if len(deleted) != 0 {
		t.Error("a released job was also deleted")
	}
	if len(failed) != 0 {
		t.Error("a released job was also parked")
	}
	if rel[0].delay != 10*time.Second {
		t.Errorf("the worker overrode the middleware's delay: %s", rel[0].delay)
	}
}

// TestAMiddlewareThatSkipsTheJobDeletesIt: a middleware that returns without
// touching the job says the work is handled, and handled work is done.
func TestAMiddlewareThatSkipsTheJobDeletesIt(t *testing.T) {
	q := &fakeQueue{}
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep: time.Millisecond,
		Middleware: []middleware.Middleware{
			middleware.Func(func(context.Context, *jobs.Job, func(context.Context) error) error {
				return nil
			}),
		},
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		t.Error("the handler ran even though the middleware skipped the job")
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mustDaemon(w, ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		deleted, _, _ := q.state()
		return len(deleted) == 1
	}, "the skipped job was not deleted")
}

// deliveryDriver is the driver the redelivery tests run against: a release puts
// the job back in line carrying the counters it now has, which is what a driver
// that writes them to a row does and what fakeQueue -- which only records the
// release -- cannot show.
//
// It is what the two counters that cross deliveries have to be proved against:
// a limit that is only ever compared inside one call of Worker.Process is a
// limit that has never been tested.
type deliveryDriver struct {
	ready    []jobs.Job
	deleted  []jobs.Job
	released []released
	failed   []failure
}

var _ jobs.Driver = (*deliveryDriver)(nil)

func (d *deliveryDriver) push(j jobs.Job) { d.ready = append(d.ready, jobRecord(j)) }

// jobRecord is what a driver stores: the columns, and nothing of the delivery.
// A real one writes a row and reads it back, so the released and deleted flags
// -- which say what happened to one delivery -- do not survive into the next.
func jobRecord(j jobs.Job) jobs.Job {
	return jobs.Job{
		UUID: j.UUID, Queue: j.Queue, Name: j.Name, DisplayName: j.DisplayName,
		TenantID: j.TenantID, Payload: j.Payload, AuthorizedBy: j.AuthorizedBy,
		Action: j.Action, RunAt: j.RunAt, Attempts: j.Attempts,
		LastError: j.LastError, Exceptions: j.Exceptions, Attributes: j.Attributes,
	}
}

// deliver hands over the next job, counting the delivery the way every real
// driver does: the claim increments attempts, so Attempts includes this one.
func (d *deliveryDriver) deliver() *jobs.Job {
	j := d.ready[0]
	d.ready = d.ready[1:]
	j.Attempts++
	return jobs.Popped(d, "delivery", j)
}

func (d *deliveryDriver) ReleaseJob(_ context.Context, j *jobs.Job, delay time.Duration) error {
	d.released = append(d.released, released{job: *j, delay: delay})
	// The row, as it now stands. The exception count is part of it, and a
	// driver that dropped it here is the defect these tests exist for.
	d.ready = append(d.ready, jobRecord(*j))
	return nil
}

func (d *deliveryDriver) DeleteJob(_ context.Context, j *jobs.Job) error {
	d.deleted = append(d.deleted, *j)
	return nil
}

func (d *deliveryDriver) FailJob(_ context.Context, j *jobs.Job, cause error) error {
	d.failed = append(d.failed, failure{job: *j, cause: cause})
	return nil
}

// alwaysFails is the handler the redelivery tests run: the failure is the same
// every time, so what changes between deliveries is only the bookkeeping.
func alwaysFails(*queue.Worker) queue.HandlerFunc {
	return func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the payment gateway is down")
	}
}

// TestMaxExceptionsIsCountedAcrossDeliveries: a job with MaxExceptions 2 on a
// worker with MaxTries 5, and a handler that always fails.
//
// The count has to survive the delivery. Counting it only in the in-memory job,
// with the release not writing it down, starts every delivery from zero and
// runs the job to MaxTries -- five deliveries where two were asked for.
func TestMaxExceptionsIsCountedAcrossDeliveries(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	j.Attributes.MaxExceptions = 2
	d.push(j)

	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 5})
	w.HandleFunc("invoice.send", alwaysFails(w))

	for delivery := 1; delivery <= 5; delivery++ {
		if len(d.ready) == 0 {
			t.Fatalf("the job stopped being delivered after %d deliveries", delivery-1)
		}
		_ = w.Process(ctx, d.deliver())
		if len(d.failed) == 0 {
			continue
		}
		if delivery != 2 {
			t.Fatalf("the job parked on delivery %d, want the 2nd: MaxExceptions is 2", delivery)
		}
		if got := d.failed[0].job.Exceptions; got != 2 {
			t.Errorf("the parked job counted %d exceptions, want 2", got)
		}
		return
	}
	t.Fatal("the job was never parked: MaxExceptions of 2 did not survive the release")
}

// TestAReleasedJobCarriesItsExceptionCount is the same defect one level down:
// whatever the worker counted has to reach the driver, or the next delivery
// reads a job that has never failed.
func TestAReleasedJobCarriesItsExceptionCount(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	d.push(j)

	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 5})
	w.HandleFunc("invoice.send", alwaysFails(w))

	_ = w.Process(ctx, d.deliver())
	if len(d.released) != 1 {
		t.Fatalf("the job was released %d times, want once", len(d.released))
	}
	if got := d.released[0].job.Exceptions; got != 1 {
		t.Errorf("the released job carries %d exceptions, want 1", got)
	}

	_ = w.Process(ctx, d.deliver())
	if got := d.released[1].job.Exceptions; got != 2 {
		t.Errorf("the second failure carries %d exceptions, want 2: the count is the job's, not the delivery's", got)
	}
}

// TestAJobWithUnlimitedTriesIsNotParkedByTheWorkersLimit checks the negative
// Tries.
//
// A Go int has to carry three answers: attributes.Attributes says a negative
// Tries is forever and a zero is "the worker decides". Handing the negative
// straight to "attempts >= maxTries" is true on the first delivery, so a job
// asking never to be parked would be parked at once.
func TestAJobWithUnlimitedTriesIsNotParkedByTheWorkersLimit(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	j.Attributes.Tries = -1
	d.push(j)

	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 3})
	w.HandleFunc("invoice.send", alwaysFails(w))

	for delivery := 1; delivery <= 4; delivery++ {
		_ = w.Process(ctx, d.deliver())
		if len(d.failed) != 0 {
			t.Fatalf("the job was parked on delivery %d, and it asked never to be parked", delivery)
		}
	}
	if len(d.released) != 4 {
		t.Fatalf("the job was released %d times in 4 deliveries", len(d.released))
	}
}

// TestAJobThatDeclaresNoTriesTakesTheWorkersLimit is the other half of the
// pair: the zero value still means "the worker decides", which is what a job
// that never mentioned tries has always meant.
func TestAJobThatDeclaresNoTriesTakesTheWorkersLimit(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	d.push(j)

	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 3})
	w.HandleFunc("invoice.send", alwaysFails(w))

	for delivery := 1; delivery <= 3; delivery++ {
		_ = w.Process(ctx, d.deliver())
	}
	if len(d.failed) != 1 {
		t.Fatalf("the job was parked %d times in 3 deliveries, want once on the 3rd", len(d.failed))
	}
}

// dispatchedTypes is the sequence of events a dispatcher saw, by type name.
// Order is what two of these tests are about, so the assertion is on the
// sequence and not on the set.
func dispatchedTypes(d *recordingDispatcher) []string {
	out := make([]string, 0, len(d.dispatched))
	for _, event := range d.dispatched {
		out = append(out, fmt.Sprintf("%T", event))
	}
	return out
}

func indexOfType(seen []string, want string) int {
	for i, name := range seen {
		if name == want {
			return i
		}
	}
	return -1
}

// TestEveryDeliveryDispatchesJobAttempted: the event was declared, documented
// as "dispatched after every delivery", and never dispatched by anything. A
// listener counting deliveries -- the one thing it exists for -- counted none.
func TestEveryDeliveryDispatchesJobAttempted(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		handler queue.HandlerFunc
		failed  bool
	}{
		{"a delivery that worked", func(context.Context, auth.Grant, *jobs.Job) error { return nil }, false},
		{"a delivery that failed", func(context.Context, auth.Grant, *jobs.Job) error {
			return errors.New("the payment gateway is down")
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &deliveryDriver{}
			j, err := jobs.New(grant(), "", "invoice.send", nil)
			if err != nil {
				t.Fatal(err)
			}
			d.push(j)

			bus := &recordingDispatcher{}
			w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 5}).SetEvents(bus)
			w.HandleFunc("invoice.send", tc.handler)

			_ = w.Process(ctx, d.deliver())

			var attempted []events.JobAttempted
			for _, event := range bus.dispatched {
				if e, is := event.(events.JobAttempted); is {
					attempted = append(attempted, e)
				}
			}
			if len(attempted) != 1 {
				t.Fatalf("JobAttempted was dispatched %d times for one delivery, want once", len(attempted))
			}
			if failed := attempted[0].Exception != nil; failed != tc.failed {
				t.Errorf("JobAttempted carries exception %v, want failed = %v", attempted[0].Exception, tc.failed)
			}
			if attempted[0].Successful() == tc.failed {
				t.Errorf("Successful() = %v on %s", attempted[0].Successful(), tc.name)
			}
			// It is the last word on the delivery.
			if seen := dispatchedTypes(bus); seen[len(seen)-1] != "events.JobAttempted" {
				t.Errorf("the events were %v, and JobAttempted is not the last", seen)
			}
		})
	}
}

// TestAParkedJobAnnouncesJobFailedBeforeJobExceptionOccurred is the order
// handleJobException has: the parking runs first and dispatches JobFailed, and
// the exception is announced after it.
//
// A listener that treats JobExceptionOccurred as "this one will be retried" --
// the only reading that makes it different from JobFailed -- is wrong on the
// last delivery of every job when the two go out in the other order.
func TestAParkedJobAnnouncesJobFailedBeforeJobExceptionOccurred(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	d.push(j)

	bus := &recordingDispatcher{}
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 1}).SetEvents(bus)
	w.HandleFunc("invoice.send", alwaysFails(w))

	_ = w.Process(ctx, d.deliver())
	if len(d.failed) != 1 {
		t.Fatalf("the job was not parked on its only delivery")
	}

	seen := dispatchedTypes(bus)
	failedAt := indexOfType(seen, "events.JobFailed")
	occurredAt := indexOfType(seen, "events.JobExceptionOccurred")
	if failedAt < 0 || occurredAt < 0 {
		t.Fatalf("the events were %v, want both JobFailed and JobExceptionOccurred", seen)
	}
	if failedAt > occurredAt {
		t.Errorf("the events were %v, want JobFailed before JobExceptionOccurred", seen)
	}
}

// TestAReleasedJobAnnouncesJobExceptionOccurredBeforeItIsReleased is the other
// path through the same method: nothing parked the job, so the event goes out
// and the release happens after it.
func TestAReleasedJobAnnouncesJobExceptionOccurredBeforeItIsReleased(t *testing.T) {
	ctx := context.Background()
	d := &deliveryDriver{}
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	d.push(j)

	bus := &recordingDispatcher{}
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{MaxTries: 5}).SetEvents(bus)
	w.HandleFunc("invoice.send", alwaysFails(w))

	_ = w.Process(ctx, d.deliver())

	seen := dispatchedTypes(bus)
	occurredAt := indexOfType(seen, "events.JobExceptionOccurred")
	releasedAt := indexOfType(seen, "events.JobReleasedAfterException")
	if occurredAt < 0 || releasedAt < 0 {
		t.Fatalf("the events were %v", seen)
	}
	if occurredAt > releasedAt {
		t.Errorf("the events were %v, want JobExceptionOccurred before JobReleasedAfterException", seen)
	}
	if indexOfType(seen, "events.JobFailed") >= 0 {
		t.Errorf("a released job announced JobFailed: %v", seen)
	}
}

// mustDaemon runs the worker's loop and keeps only the error, so the tests that
// only care that it stopped read the way they did when the loop returned one
// value. Worker.Daemon returns the exit status the process is about to hand the
// shell.
func mustDaemon(w *queue.Worker, ctx context.Context) error {
	_, err := w.Daemon(ctx)
	return err
}
