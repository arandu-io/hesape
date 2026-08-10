package queue_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/queue"
)

const tenant = "11111111-1111-4111-8111-111111111111"

func grant() auth.Grant { return auth.SystemGrant("invoice.send", tenant) }

// TestAJobWithoutATenantIsRefused is RULE 14 at the queue. A job with no tenant
// cannot be scoped, and everything the handler touches would read across
// customers.
func TestAJobWithoutATenantIsRefused(t *testing.T) {
	_, err := queue.New(auth.SystemGrant("invoice.send", ""), "", "invoice.send", nil)
	if !errors.Is(err, queue.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

func TestAJobWithoutANameIsRefused(t *testing.T) {
	if _, err := queue.New(grant(), "", "", nil); !errors.Is(err, queue.ErrNoName) {
		t.Fatalf("err = %v, want ErrNoName", err)
	}
}

// TestTheJobCarriesTheGrantThatPushedIt: the audit trail, and what the worker
// reissues the work under. A worker that invented its own Grant would be a way
// to reach the database with permissions nobody granted.
func TestTheJobCarriesTheGrantThatPushedIt(t *testing.T) {
	j, err := queue.New(grant(), "", "invoice.send", map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if j.TenantID != tenant || j.Action != "invoice.send" {
		t.Fatalf("job = %+v", j)
	}
	if j.Queue != queue.DefaultQueue {
		t.Errorf("queue = %q, want the default", j.Queue)
	}
	if j.ID == "" {
		t.Error("the job has no id, and the id is what a handler deduplicates on")
	}

	g := queue.GrantFor(j)
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

func TestThePayloadSurvives(t *testing.T) {
	j, err := queue.New(grant(), "mail", "invoice.send", map[string]any{"id": "i-1", "amount": 1250})
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
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, queue.Job) error { return nil })

	defer func() {
		if recover() == nil {
			t.Fatal("a second handler for the same name was accepted")
		}
	}()
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, queue.Job) error { return nil })
}

// fakeQueue is the in-memory driver the worker tests run against. The real
// drivers live in github.com/arandu-io/queue and are tested there, against a
// real store -- this one exists to drive the loop.
type fakeQueue struct {
	mu       sync.Mutex
	ready    []queue.Job
	acked    []queue.Job
	failures []failure
}

type failure struct {
	job   queue.Job
	cause error
	park  bool
}

func (q *fakeQueue) Push(_ context.Context, _ auth.Grant, j queue.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ready = append(q.ready, j)
	return nil
}

func (q *fakeQueue) Reserve(_ context.Context, _ string, n int, _ time.Duration) ([]queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.ready) == 0 {
		return nil, nil
	}
	if n > len(q.ready) {
		n = len(q.ready)
	}
	out := make([]queue.Job, 0, n)
	for _, j := range q.ready[:n] {
		// Like the real drivers: the delivery is counted when the job is
		// handed over, so Attempts includes this one.
		j.Attempts++
		out = append(out, j)
	}
	q.ready = q.ready[n:]
	return out, nil
}

func (q *fakeQueue) Ack(_ context.Context, j queue.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, j)
	return nil
}

func (q *fakeQueue) Fail(_ context.Context, j queue.Job, cause error, _ time.Time, park bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failures = append(q.failures, failure{job: j, cause: cause, park: park})
	return nil
}

func (q *fakeQueue) Parked(context.Context, int) ([]queue.Job, error) { return nil, nil }
func (q *fakeQueue) Retry(context.Context, string) error              { return nil }
func (q *fakeQueue) Pending(context.Context, string) (int, error)     { return 0, nil }
func (q *fakeQueue) Oldest(context.Context, string) (time.Duration, error) {
	return 0, nil
}

func (q *fakeQueue) state() ([]queue.Job, []failure) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]queue.Job(nil), q.acked...), append([]failure(nil), q.failures...)
}

func TestTheWorkerRunsAndAcknowledges(t *testing.T) {
	q := &fakeQueue{}
	j, err := queue.New(grant(), "", "invoice.send", map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Push(context.Background(), grant(), j)

	var got queue.Job
	var gotGrant auth.Grant
	done := make(chan struct{})

	w := queue.NewWorker(q, queue.WorkerOptions{Poll: time.Millisecond})
	w.HandleFunc("invoice.send", func(_ context.Context, g auth.Grant, j queue.Job) error {
		got, gotGrant = j, g
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job never ran")
	}
	cancel()

	if got.ID != j.ID {
		t.Errorf("ran %s, want %s", got.ID, j.ID)
	}
	// The handler receives a Grant for the tenant that pushed it, which is what
	// lets it reach a repository at all.
	if gotGrant.Subject().Tenant != tenant {
		t.Errorf("the handler got a Grant for %q", gotGrant.Subject().Tenant)
	}

	waitFor(t, func() bool {
		acked, _ := q.state()
		return len(acked) == 1
	}, "the job was not acknowledged")
}

// TestAFailedJobIsRetriedThenParked: a worker stuck on one bad payload stops
// draining everything behind it.
func TestAFailedJobIsRetriedThenParked(t *testing.T) {
	q := &fakeQueue{}
	j, _ := queue.New(grant(), "", "invoice.send", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{Poll: time.Millisecond, MaxAttempts: 2})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, queue.Job) error {
		return errors.New("the invoice has no address")
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		_, failures := q.state()
		return len(failures) == 1
	}, "the failure was not recorded")

	_, failures := q.state()
	if failures[0].park {
		t.Error("the first failure parked the job instead of scheduling a retry")
	}
	if failures[0].cause == nil || failures[0].cause.Error() != "the invoice has no address" {
		t.Errorf("the reason was not passed to the driver: %v", failures[0].cause)
	}

	// The second attempt reaches MaxAttempts and parks.
	failed := j
	failed.Attempts = 1
	_ = q.Push(context.Background(), grant(), failed)

	waitFor(t, func() bool {
		_, failures := q.state()
		return len(failures) == 2 && failures[1].park
	}, "the job was not parked after the last attempt")
}

// TestAJobWithNoHandlerParksImmediately: no amount of retrying registers a
// handler, and a job retried forever is one nobody ever looks at.
func TestAJobWithNoHandlerParksImmediately(t *testing.T) {
	q := &fakeQueue{}
	j, _ := queue.New(grant(), "", "report.monthly", nil)
	_ = q.Push(context.Background(), grant(), j)

	w := queue.NewWorker(q, queue.WorkerOptions{Poll: time.Millisecond})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, queue.Job) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	defer cancel()

	waitFor(t, func() bool {
		_, failures := q.state()
		return len(failures) == 1 && failures[1-1].park
	}, "a job with no handler was not parked")
}

// TestTheWorkerStopsOnCancel: shutdown has to be clean, or a deploy leaves
// workers holding leases nobody will release.
func TestTheWorkerStopsOnCancel(t *testing.T) {
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{Poll: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- w.Run(ctx) }()

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
// Reserve hides the whole batch for one Lease, and the batch used to run one
// job at a time. With Concurrency 4, Lease 5m and a two-minute handler, the
// fourth job started at minute six -- past its own lease, visible again to
// another worker, and running in both. At-least-once became exactly-twice for
// the tail of every batch.
//
// The handler here waits for all four to arrive. Serially, none of them ever
// does, and the test times out instead of passing slowly.
func TestTheBatchRunsInParallel(t *testing.T) {
	const batch = 4

	q := &fakeQueue{}
	for range batch {
		j, err := queue.New(grant(), "", "slow.thing", nil)
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
		Poll:        time.Millisecond,
	})
	w.HandleFunc("slow.thing", func(ctx context.Context, _ auth.Grant, _ queue.Job) error {
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
	go func() { done <- w.Run(ctx) }()

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
	j, err := queue.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatal(err)
	}

	seen := make(chan *log.Collector, 1)
	w := queue.NewWorker(q, queue.WorkerOptions{Poll: time.Millisecond})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ queue.Job) error {
		seen <- log.FromContext(ctx)
		return nil
	})

	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

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
	j, err := queue.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatal(err)
	}

	recorder := log.NewRecorder(8)
	w := queue.NewWorker(q, queue.WorkerOptions{Poll: time.Millisecond, Recorder: recorder})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ queue.Job) error {
		// Something a person would want to see on the page.
		log.FromContext(ctx).RecordEvent("invoice.rendered", nil)
		return nil
	})

	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

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
