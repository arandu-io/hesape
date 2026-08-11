package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/database"
)

const tenant = "acme"

func grant() auth.Grant { return auth.SystemGrant("invoice.import", tenant) }

// pushed is one call to Queue.Push, kept whole so a test can assert on the
// envelope as it would arrive at a worker.
type pushed struct {
	Queue   string
	Name    string
	Payload []byte
}

// recorder is a Queue that keeps what it was given.
//
// It is what the narrow Queue interface buys: a batch is exercised end to end
// without a driver, a table or a running worker, and what the test asserts on
// is the bytes a worker would actually receive.
type recorder struct {
	mu   sync.Mutex
	jobs []pushed
	fail error
	// failFrom makes the nth push onwards fail, to exercise a batch that could
	// not be fully dispatched.
	failFrom int
}

func (r *recorder) Push(_ context.Context, _ auth.Grant, queue, name string, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil && len(r.jobs) >= r.failFrom {
		return r.fail
	}
	r.jobs = append(r.jobs, pushed{Queue: queue, Name: name, Payload: payload})
	return nil
}

func (r *recorder) all() []pushed {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pushed, len(r.jobs))
	copy(out, r.jobs)
	return out
}

func (r *recorder) names() []string {
	var out []string
	for _, j := range r.all() {
		out = append(out, j.Name)
	}
	return out
}

func (r *recorder) count(name string) int {
	n := 0
	for _, j := range r.all() {
		if j.Name == name {
			n++
		}
	}
	return n
}

// row is the arguments of the job used throughout.
type row struct {
	N int `json:"n"`
}

func dispatch(t *testing.T, p *bus.PendingBatch) (bus.Batch, *bus.Memory, *recorder) {
	t.Helper()
	store, queue := bus.NewMemory(), &recorder{}
	b, err := p.Dispatch(context.Background(), grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return b, store, queue
}

// handle plays one delivery: read the envelope, report the outcome.
func handle(t *testing.T, store bus.BatchRepository, queue bus.Queue, j pushed, cause error) row {
	t.Helper()
	var r row
	m, err := bus.Batched(j.Payload, &r)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if err := bus.Handled(context.Background(), grant(), store, queue, m, cause); err != nil {
		t.Fatalf("Handled: %v", err)
	}
	return r
}

func TestDispatchPushesEveryJobWithItsArguments(t *testing.T) {
	t.Parallel()

	b, _, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		OnQueue("imports"))

	if b.TotalJobs != 2 || b.PendingJobs != 2 || b.FailedJobs != 0 {
		t.Fatalf("counters = %d/%d/%d, want 2/2/0", b.TotalJobs, b.PendingJobs, b.FailedJobs)
	}
	if b.TenantID != tenant {
		t.Fatalf("tenant = %q, want %q", b.TenantID, tenant)
	}

	jobs := queue.all()
	if len(jobs) != 2 {
		t.Fatalf("pushed %d jobs, want 2", len(jobs))
	}
	for i, j := range jobs {
		if j.Queue != "imports" {
			t.Errorf("job %d on queue %q, want imports", i, j.Queue)
		}
		var got row
		m, err := bus.Batched(j.Payload, &got)
		if err != nil {
			t.Fatalf("Batched: %v", err)
		}
		if m.BatchID != b.ID {
			t.Errorf("job %d belongs to batch %q, want %q", i, m.BatchID, b.ID)
		}
		if got.N != i+1 {
			t.Errorf("job %d carries n=%d, want %d", i, got.N, i+1)
		}
	}
}

func TestThenFiresOnceWhenEveryJobSucceeded(t *testing.T) {
	t.Parallel()

	b, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Then("import.done", row{N: 0}).
		Finally("import.over", nil).
		Catch("import.failed", nil))

	jobs := queue.all()
	handle(t, store, queue, jobs[0], nil)
	if queue.count("import.done") != 0 {
		t.Fatal("Then fired with a job still pending")
	}
	handle(t, store, queue, jobs[1], nil)

	if got := queue.count("import.done"); got != 1 {
		t.Errorf("Then fired %d times, want 1", got)
	}
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times, want 1", got)
	}
	if got := queue.count("import.failed"); got != 0 {
		t.Errorf("Catch fired %d times on a batch with no failure, want 0", got)
	}

	after, err := store.Find(context.Background(), grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !after.Finished() || after.PendingJobs != 0 || after.Progress() != 100 {
		t.Errorf("batch = %+v, want finished, nothing pending, 100%%", after)
	}
}

func TestCallbacksCarryNoEnvelope(t *testing.T) {
	t.Parallel()

	_, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Then("import.done", row{N: 7}))

	handle(t, store, queue, queue.all()[0], nil)

	var done pushed
	for _, j := range queue.all() {
		if j.Name == "import.done" {
			done = j
		}
	}
	var got row
	m, err := bus.Batched(done.Payload, &got)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	// A callback that belonged to the batch it closes would report to a
	// counter that has already reached zero.
	if m.BatchID != "" {
		t.Errorf("the callback belongs to batch %q, want none", m.BatchID)
	}
	if got.N != 7 {
		t.Errorf("the callback carries n=%d, want 7", got.N)
	}
}

func TestFirstFailureStopsTheBatchAndFiresCatchOnce(t *testing.T) {
	t.Parallel()

	b, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Add("invoice.import", row{N: 3}).
		Then("import.done", nil).
		Catch("import.failed", nil).
		Finally("import.over", nil))

	jobs := queue.all()
	boom := errors.New("row 1 is not a row")
	handle(t, store, queue, jobs[0], boom)

	if got := queue.count("import.failed"); got != 1 {
		t.Errorf("Catch fired %d times, want 1", got)
	}
	if got := queue.count("import.done"); got != 0 {
		t.Errorf("Then fired on a failed batch")
	}
	// Finally waits for every job to report, whichever way each went: two of
	// the three are still owed.
	if got := queue.count("import.over"); got != 0 {
		t.Errorf("Finally fired %d times with two jobs still owed, want 0", got)
	}

	// The two jobs still queued are delivered and must skip their work: the
	// first failure of a batch that does not allow them cancels it.
	m, err := bus.Batched(jobs[1].Payload, nil)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	working, err := m.Batching(context.Background(), grant(), store)
	if err != nil {
		t.Fatalf("Batching: %v", err)
	}
	if working {
		t.Error("a job of a stopped batch was not told to skip")
	}

	// They still report, and reporting must not fire Catch a second time.
	handle(t, store, queue, jobs[1], boom)
	handle(t, store, queue, jobs[2], nil)
	if got := queue.count("import.failed"); got != 1 {
		t.Errorf("Catch fired %d times after the later reports, want 1", got)
	}
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times once every job had reported, want 1", got)
	}

	after, err := store.Find(context.Background(), grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if after.FailedJobs != 2 || after.PendingJobs != 2 {
		t.Errorf("counters = failed %d, pending %d; want 2 and 2", after.FailedJobs, after.PendingJobs)
	}
}

func TestAllowFailuresKeepsTheBatchGoing(t *testing.T) {
	t.Parallel()

	_, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		AllowFailures().
		Then("import.done", nil).
		Catch("import.failed", nil).
		Finally("import.over", nil))

	jobs := queue.all()
	handle(t, store, queue, jobs[0], errors.New("bad row"))

	if got := queue.count("import.failed"); got != 1 {
		t.Errorf("Catch fired %d times, want 1", got)
	}
	if got := queue.count("import.over"); got != 0 {
		t.Fatal("the batch finished on a failure it was told to allow")
	}

	handle(t, store, queue, jobs[1], nil)
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times, want 1", got)
	}
	if got := queue.count("import.done"); got != 0 {
		t.Error("Then fired on a batch that had a failure")
	}
}

// TestCancelStopsTheWorkNotTheBookkeeping is Illuminate's behaviour, and it is
// worth a test of its own because it surprises people: cancelling tells the
// jobs still queued to skip, and skipping is *succeeding*, so the counter still
// reaches zero and Then still fires.
//
// Batch::recordSuccessfulJob has no cancelled check -- see the clone. A batch
// that must not report success when it is cancelled says so in the job that
// Then names, which is a job like any other and can ask.
func TestCancelStopsTheWorkNotTheBookkeeping(t *testing.T) {
	t.Parallel()

	b, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Then("import.done", nil).
		Finally("import.over", nil))

	ctx := context.Background()
	if err := store.Cancel(ctx, grant(), b.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Cancelling twice is the caller getting what they asked for.
	if err := store.Cancel(ctx, grant(), b.ID); err != nil {
		t.Fatalf("Cancel again: %v", err)
	}

	jobs := queue.all()
	m, err := bus.Batched(jobs[0].Payload, nil)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	working, err := m.Batching(ctx, grant(), store)
	if err != nil {
		t.Fatalf("Batching: %v", err)
	}
	if working {
		t.Error("a job of a cancelled batch was not told to skip")
	}

	for _, j := range jobs {
		handle(t, store, queue, j, nil)
	}

	if got := queue.count("import.done"); got != 1 {
		t.Errorf("Then fired %d times, want 1", got)
	}
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times, want 1", got)
	}
}

func TestConcurrentReportsFireEachCallbackOnce(t *testing.T) {
	t.Parallel()

	const n = 50
	p := bus.NewBatch("import").Then("import.done", nil).Finally("import.over", nil)
	for i := range n {
		p = p.Add("invoice.import", row{N: i})
	}
	_, store, queue := dispatch(t, p)

	jobs := queue.all()
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j pushed) {
			defer wg.Done()
			m, err := bus.Batched(j.Payload, nil)
			if err != nil {
				t.Errorf("Batched: %v", err)
				return
			}
			if err := bus.Handled(context.Background(), grant(), store, queue, m, nil); err != nil {
				t.Errorf("Handled: %v", err)
			}
		}(j)
	}
	wg.Wait()

	if got := queue.count("import.done"); got != 1 {
		t.Errorf("Then fired %d times under %d concurrent reports, want 1", got, n)
	}
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times, want 1", got)
	}
}

func TestBatchingLeavesAPayloadItDidNotWriteAlone(t *testing.T) {
	t.Parallel()

	var got row
	m, err := bus.Batched([]byte(`{"n":9}`), &got)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if m.BatchID != "" || len(m.Chained) != 0 {
		t.Errorf("a job pushed on its own reported membership: %+v", m)
	}
	if got.N != 9 {
		t.Errorf("n = %d, want 9", got.N)
	}

	// And Handled on it does nothing, so a handler needs one shape, not two.
	if err := bus.Handled(context.Background(), grant(), nil, nil, m, nil); err != nil {
		t.Errorf("Handled on a job that belongs to nothing: %v", err)
	}
}

func TestBatchingRefusesAnEnvelopeFromAnotherFormat(t *testing.T) {
	t.Parallel()

	_, err := bus.Batched([]byte(`{"bus":99,"body":{"n":1}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "envelope format 99") {
		t.Fatalf("err = %v, want it to name the format it could not read", err)
	}
}

func TestDispatchRefusesABatchWithNothingInIt(t *testing.T) {
	t.Parallel()

	_, err := bus.NewBatch("import").Dispatch(context.Background(), grant(), bus.NewMemory(), &recorder{})
	if !errors.Is(err, bus.ErrEmptyBatch) {
		t.Fatalf("err = %v, want ErrEmptyBatch", err)
	}
}

func TestDispatchRefusesAGrantWithNoTenant(t *testing.T) {
	t.Parallel()

	_, err := bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Dispatch(context.Background(), auth.Grant{}, bus.NewMemory(), &recorder{})
	if !errors.Is(err, bus.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

func TestDispatchReportsAPayloadItCannotSerialize(t *testing.T) {
	t.Parallel()

	_, err := bus.NewBatch("import").
		Add("invoice.import", make(chan int)).
		Add("invoice.import", row{N: 2}).
		Dispatch(context.Background(), grant(), bus.NewMemory(), &recorder{})
	if err == nil || !strings.Contains(err.Error(), "invoice.import") {
		t.Fatalf("err = %v, want the name of the job that could not be serialized", err)
	}
}

func TestAHalfPushedBatchIsCancelled(t *testing.T) {
	t.Parallel()

	store := bus.NewMemory()
	queue := &recorder{fail: errors.New("the queue is down"), failFrom: 2}

	_, err := bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Add("invoice.import", row{N: 3}).
		Dispatch(context.Background(), grant(), store, queue)
	if err == nil {
		t.Fatal("a batch whose third job could not be pushed was reported as dispatched")
	}

	// The two already queued must skip their work rather than do a third of an
	// import.
	m, err := bus.Batched(queue.all()[0].Payload, nil)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	working, err := m.Batching(context.Background(), grant(), store)
	if err != nil {
		t.Fatalf("Batching: %v", err)
	}
	if working {
		t.Error("a job of an abandoned batch was not told to skip")
	}
}

func TestRecordingAgainstAMissingBatchIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := bus.NewMemory().DecrementPendingJobs(context.Background(), grant(), "nope", "job-1")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("err = %v, want database.ErrNotFound", err)
	}
}

func TestOneTenantCannotSeeAnothersBatch(t *testing.T) {
	t.Parallel()

	b, store, _ := dispatch(t, bus.NewBatch("import").Add("invoice.import", row{N: 1}))

	other := auth.SystemGrant("invoice.import", "globex")
	if _, err := store.Find(context.Background(), other, b.ID); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("err = %v, want database.ErrNotFound", err)
	}
}

func TestPruneTakesFinishedBatchesOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := bus.NewMemory()

	done, err := bus.NewBatch("done").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if err := store.MarkAsFinished(ctx, grant(), done.ID); err != nil {
		t.Fatalf("MarkAsFinished: %v", err)
	}
	running, err := bus.NewBatch("running").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	n, err := store.Prune(ctx, grant(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d batches, want 1", n)
	}
	if _, err := store.Find(ctx, grant(), running.ID); err != nil {
		t.Errorf("the unfinished batch was pruned: %v", err)
	}
}

func TestProgressRoundsToTheNearestPercent(t *testing.T) {
	t.Parallel()

	// Two of three succeeded: 66.67, which Illuminate's (int) round() reports
	// as 67.
	b := bus.Batch{TotalJobs: 3, PendingJobs: 1}
	if got := b.Progress(); got != 67 {
		t.Errorf("Progress = %d, want 67", got)
	}
	if got := (bus.Batch{}).Progress(); got != 0 {
		t.Errorf("Progress of an empty batch = %d, want 0", got)
	}
}

// TestEnvelopeIsJSON guards the wire format. A worker running the previous
// binary reads what this one writes, so the shape is a contract and not an
// implementation detail.
func TestEnvelopeIsJSON(t *testing.T) {
	t.Parallel()

	_, _, queue := dispatch(t, bus.NewBatch("import").Add("invoice.import", row{N: 1}))

	var got map[string]any
	if err := json.Unmarshal(queue.all()[0].Payload, &got); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	if got["bus"] != float64(1) {
		t.Errorf("bus = %v, want 1", got["bus"])
	}
	if _, ok := got["batch"]; !ok {
		t.Error("the envelope carries no batch")
	}
	if _, ok := got["body"]; !ok {
		t.Error("the envelope carries no body")
	}
}

// otherTenant is a second customer, for the tests that prove one cannot see or
// silence the other's work.
func otherTenant() auth.Grant { return auth.SystemGrant("invoice.import", "globex") }
