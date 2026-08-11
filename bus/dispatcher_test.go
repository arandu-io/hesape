package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	busevents "github.com/arandu-io/hesape/bus/events"
	"github.com/arandu-io/hesape/events"
)

// job builds a Step the way an application would.
func job(t *testing.T, name string, payload any) bus.Step {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling the payload of %s: %v", name, err)
	}
	return bus.Step{Name: name, Payload: raw}
}

func TestDispatchRunsTheHandlerWhenThereIsNoQueue(t *testing.T) {
	t.Parallel()

	var got row
	d := bus.NewDispatcher(nil, nil).Map(map[string]bus.Handler{
		"invoice.import": func(_ context.Context, _ auth.Grant, payload []byte) error {
			return json.Unmarshal(payload, &got)
		},
	})

	if !d.HasCommandHandler("invoice.import") {
		t.Error("the handler that was just mapped is not registered")
	}
	if _, ok := d.GetCommandHandler("invoice.export"); ok {
		t.Error("a job nobody mapped has a handler")
	}
	if err := d.Dispatch(context.Background(), grant(), job(t, "invoice.import", row{N: 4})); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got.N != 4 {
		t.Errorf("the handler saw n=%d, want 4", got.N)
	}
}

func TestDispatchQueuesWhenThereIsAQueueAndDispatchNowDoesNot(t *testing.T) {
	t.Parallel()

	queue := &recorder{}
	ran := 0
	d := bus.NewDispatcher(queue, nil).Map(map[string]bus.Handler{
		"invoice.import": func(context.Context, auth.Grant, []byte) error { ran++; return nil },
	})

	ctx := context.Background()
	if err := d.Dispatch(ctx, grant(), job(t, "invoice.import", row{N: 1})); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if ran != 0 || len(queue.all()) != 1 {
		t.Errorf("Dispatch ran the handler %d times and queued %d jobs, want 0 and 1", ran, len(queue.all()))
	}

	if err := d.DispatchNow(ctx, grant(), job(t, "invoice.import", row{N: 2})); err != nil {
		t.Fatalf("DispatchNow: %v", err)
	}
	if err := d.DispatchSync(ctx, grant(), job(t, "invoice.import", row{N: 3})); err != nil {
		t.Fatalf("DispatchSync: %v", err)
	}
	if ran != 2 || len(queue.all()) != 1 {
		t.Errorf("after two immediate dispatches: ran %d, queued %d; want 2 and 1", ran, len(queue.all()))
	}
}

func TestDispatchNowSaysWhenNothingHandlesTheJob(t *testing.T) {
	t.Parallel()

	err := bus.NewDispatcher(nil, nil).DispatchNow(context.Background(), grant(), job(t, "invoice.import", nil))
	if err == nil || !strings.Contains(err.Error(), "invoice.import") {
		t.Fatalf("err = %v, want it to name the job with no handler", err)
	}
}

func TestPipeThroughWrapsEveryHandlerOutermostFirst(t *testing.T) {
	t.Parallel()

	var order []string
	d := bus.NewDispatcher(nil, nil).
		Map(map[string]bus.Handler{
			"invoice.import": func(context.Context, auth.Grant, []byte) error {
				order = append(order, "handler")
				return nil
			},
		}).
		PipeThrough([]bus.Pipe{
			func(next bus.Handler) bus.Handler {
				return func(ctx context.Context, g auth.Grant, p []byte) error {
					order = append(order, "first")
					return next(ctx, g, p)
				}
			},
			func(next bus.Handler) bus.Handler {
				return func(ctx context.Context, g auth.Grant, p []byte) error {
					order = append(order, "second")
					return next(ctx, g, p)
				}
			},
		})

	if err := d.DispatchNow(context.Background(), grant(), job(t, "invoice.import", nil)); err != nil {
		t.Fatalf("DispatchNow: %v", err)
	}
	if strings.Join(order, ",") != "first,second,handler" {
		t.Errorf("order = %v, want the first pipe outermost", order)
	}
}

func TestDispatchAfterResponseWaitsForTerminating(t *testing.T) {
	t.Parallel()

	ran := 0
	d := bus.NewDispatcher(nil, nil).Map(map[string]bus.Handler{
		"invoice.import": func(context.Context, auth.Grant, []byte) error { ran++; return nil },
	})

	ctx := context.Background()
	if err := d.DispatchAfterResponse(ctx, grant(), job(t, "invoice.import", nil)); err != nil {
		t.Fatalf("DispatchAfterResponse: %v", err)
	}
	if ran != 0 {
		t.Fatal("the job ran before the response went out")
	}
	if err := d.Terminating(ctx); err != nil {
		t.Fatalf("Terminating: %v", err)
	}
	if ran != 1 {
		t.Errorf("the job ran %d times after Terminating, want 1", ran)
	}
	// Terminating empties the list: a second call must not run it again.
	if err := d.Terminating(ctx); err != nil {
		t.Fatalf("Terminating again: %v", err)
	}
	if ran != 1 {
		t.Errorf("the job ran %d times over two Terminating calls, want 1", ran)
	}

	d.WithoutDispatchingAfterResponses()
	if err := d.DispatchAfterResponse(ctx, grant(), job(t, "invoice.import", nil)); err != nil {
		t.Fatalf("DispatchAfterResponse without holding: %v", err)
	}
	if ran != 2 {
		t.Errorf("WithoutDispatchingAfterResponses did not run the job at once: ran %d", ran)
	}
	d.WithDispatchingAfterResponses()
}

func TestDispatcherBatchAndFindBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := bus.NewMemory(), &recorder{}
	d := bus.NewDispatcher(queue, store)

	b, err := d.Batch(job(t, "invoice.import", row{N: 1}), job(t, "invoice.import", row{N: 2})).
		Name("import").
		Dispatch(ctx, grant(), d.Repository(), d.Queue())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got, err := d.FindBatch(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("FindBatch: %v", err)
	}
	if got.Name != "import" || got.TotalJobs != 2 {
		t.Errorf("batch = %+v, want import with two jobs", got)
	}
}

func TestBatchAddPutsMoreWorkInARunningBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	b, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Then("import.done", nil))

	// The one job reports, which takes the batch to zero and finishes it.
	handle(t, store, queue, queue.all()[0], nil)
	if queue.count("import.done") != 1 {
		t.Fatal("Then did not fire when the only job reported")
	}

	grown, err := b.Add(ctx, grant(), store, queue, bus.Step{Name: "invoice.import"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if grown.TotalJobs != 2 || grown.PendingJobs != 1 || grown.Finished() {
		t.Errorf("batch = %+v, want two jobs, one pending, running again", grown)
	}

	// And the added job reports to the same batch.
	var last pushed
	for _, j := range queue.all() {
		if j.Name == "invoice.import" {
			last = j
		}
	}
	m, err := bus.Batched(last.Payload, nil)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if m.BatchID != b.ID {
		t.Errorf("the added job belongs to %q, want %q", m.BatchID, b.ID)
	}
}

func TestBatchDeleteAndFresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	b, store, _ := dispatch(t, bus.NewBatch("import").Add("invoice.import", row{N: 1}))

	if _, err := b.Fresh(ctx, grant(), store); err != nil {
		t.Fatalf("Fresh: %v", err)
	}
	if err := b.Delete(ctx, grant(), store); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.Fresh(ctx, grant(), store); err == nil {
		t.Error("a deleted batch was still readable")
	}
}

func TestBatchToArrayCarriesTheCountersAndTheProgress(t *testing.T) {
	t.Parallel()

	b := bus.Batch{ID: "b1", Name: "import", TotalJobs: 4, PendingJobs: 1, FailedJobs: 1}
	got := b.ToArray()

	for key, want := range map[string]any{
		"id": "b1", "name": "import",
		"total_jobs": 4, "pending_jobs": 1, "processed_jobs": 3,
		"progress": 75, "failed_jobs": 1,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if got["cancelled_at"] != nil || got["finished_at"] != nil {
		t.Errorf("a running batch reported %v and %v", got["cancelled_at"], got["finished_at"])
	}
}

func TestProgressAndFailureCallbacksFireOnABatchThatAllowsFailures(t *testing.T) {
	t.Parallel()

	_, store, queue := dispatch(t, bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		AllowFailures().
		Progress("import.tick", nil).
		Failure("import.one-failed", nil))

	jobs := queue.all()
	handle(t, store, queue, jobs[0], errors.New("bad row"))
	handle(t, store, queue, jobs[1], nil)

	if got := queue.count("import.tick"); got != 2 {
		t.Errorf("Progress fired %d times over two reports, want 2", got)
	}
	if got := queue.count("import.one-failed"); got != 1 {
		t.Errorf("Failure fired %d times, want 1", got)
	}
}

func TestBeforeFiresOnceTheBatchIsStoredAndBeforeAnyJob(t *testing.T) {
	t.Parallel()

	_, _, queue := dispatch(t, bus.NewBatch("import").
		Before("import.starting", nil).
		Add("invoice.import", row{N: 1}))

	names := queue.names()
	if len(names) != 2 || names[0] != "import.starting" {
		t.Errorf("queued %v, want import.starting first", names)
	}
}

func TestPendingBatchReportsWhatWasDescribed(t *testing.T) {
	t.Parallel()

	p := bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Before("a", nil).Progress("b", nil).Then("c", nil).
		Catch("d", nil).Failure("e", nil).Finally("f", nil).
		OnQueue("imports").OnConnection("redis").AllowFailures()

	if p.Queue() != "imports" || p.Connection() != "redis" || !p.AllowsFailures() {
		t.Errorf("options = %+v", p.Options())
	}
	for name, got := range map[string][]bus.Step{
		"a": p.BeforeCallbacks(), "b": p.ProgressCallbacks(), "c": p.ThenCallbacks(),
		"d": p.CatchCallbacks(), "e": p.FailureCallbacks(), "f": p.FinallyCallbacks(),
	} {
		if len(got) != 1 || got[0].Name != name {
			t.Errorf("the callback %q is %+v", name, got)
		}
	}
	if len(p.Jobs()) != 1 {
		t.Errorf("%d jobs described, want 1", len(p.Jobs()))
	}
}

func TestDispatchIfAndDispatchUnless(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := bus.NewMemory(), &recorder{}

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		DispatchIf(ctx, grant(), store, queue, false)
	if err != nil || b.ID != "" {
		t.Errorf("DispatchIf(false) returned %+v (%v), want nothing", b, err)
	}
	b, err = bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		DispatchUnless(ctx, grant(), store, queue, false)
	if err != nil || b.ID == "" {
		t.Errorf("DispatchUnless(false) returned %+v (%v), want a batch", b, err)
	}
}

func TestDispatchAfterResponseStoresTheBatchAndQueuesLater(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := bus.NewMemory(), &recorder{}
	d := bus.NewDispatcher(queue, store)

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		DispatchAfterResponse(ctx, grant(), d)
	if err != nil {
		t.Fatalf("DispatchAfterResponse: %v", err)
	}
	if b.ID == "" {
		t.Fatal("the batch was not stored")
	}
	if len(queue.all()) != 0 {
		t.Error("a job was queued before the response went out")
	}

	if err := d.Terminating(ctx); err != nil {
		t.Fatalf("Terminating: %v", err)
	}
	if len(queue.all()) != 1 {
		t.Errorf("%d jobs queued after Terminating, want 1", len(queue.all()))
	}
}

func TestChainedBatchRunsTheRestOfTheChainAfterTheBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := bus.NewMemory(), &recorder{}

	inner := bus.NewBatch("inner").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2})

	links, err := bus.PrepareNestedBatches([]any{inner, job(t, "report.monthly", row{N: 9})})
	if err != nil {
		t.Fatalf("PrepareNestedBatches: %v", err)
	}
	if err := bus.NewChain().AddStep(links...).Dispatch(ctx, grant(), queue); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The first link is the batch, still wrapped as a job.
	first := queue.all()[0]
	if first.Name != bus.ChainedBatchJob {
		t.Fatalf("the first link is %q, want %q", first.Name, bus.ChainedBatchJob)
	}
	cb, err := bus.DecodeChainedBatch(first.Payload)
	if err != nil {
		t.Fatalf("DecodeChainedBatch: %v", err)
	}
	if _, err := cb.Handle(ctx, grant(), store, queue); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Its two jobs are queued, and the rest of the chain became its Finally.
	if got := queue.count("invoice.import"); got != 2 {
		t.Errorf("the batch queued %d jobs, want 2", got)
	}
	if got := queue.count("report.monthly"); got != 0 {
		t.Error("the link after the batch ran before the batch did")
	}

	for _, j := range queue.all() {
		if j.Name == "invoice.import" {
			handle(t, store, queue, j, nil)
		}
	}
	if got := queue.count("report.monthly"); got != 1 {
		t.Errorf("the link after the batch ran %d times, want 1", got)
	}
}

func TestQueueableCarriesTheDispatchSettings(t *testing.T) {
	t.Parallel()

	q := bus.Queueable{}.
		OnConnection("redis").
		OnQueue("imports").
		OnGroup("tenant-acme").
		WithDeduplicator("import-2026-08").
		Delay(30).
		AfterCommit().
		Through("throttle", "log").
		Chain(bus.Step{Name: "b"}).
		PrependToChain(bus.Step{Name: "a"}).
		AppendToChain(bus.Step{Name: "c"})

	if q.Connection != "redis" || q.Queue != "imports" || q.MessageGroup != "tenant-acme" {
		t.Errorf("routing = %+v", q)
	}
	if q.Deduplicator != "import-2026-08" || q.DelaySeconds != 30 {
		t.Errorf("delivery = %+v", q)
	}
	if q.DispatchAfterCommit == nil || !*q.DispatchAfterCommit {
		t.Error("AfterCommit did not stick")
	}
	if q = q.WithoutDelay().BeforeCommit(); q.DelaySeconds != 0 || *q.DispatchAfterCommit {
		t.Errorf("WithoutDelay and BeforeCommit did not undo their pair: %+v", q)
	}
	if len(q.Middleware) != 2 {
		t.Errorf("middleware = %v, want two", q.Middleware)
	}

	m := bus.Batchable{Queueable: q}
	if err := m.AssertHasChain("a", "b", "c"); err != nil {
		t.Error(err)
	}
	if err := m.AssertDoesntHaveChain(); err == nil {
		t.Error("AssertDoesntHaveChain passed on a job that has one")
	}
	if err := (bus.Batchable{}).AssertDoesntHaveChain(); err != nil {
		t.Error(err)
	}
	if err := m.AssertHasChain(); err == nil {
		t.Error("AssertHasChain accepted an empty expectation")
	}

	q = q.AllOnQueue("slow").AllOnConnection("sqs")
	if q.Queue != "slow" || q.ChainQueue != "slow" || q.Connection != "sqs" || q.ChainConnection != "sqs" {
		t.Errorf("AllOn... did not reach the chain: %+v", q)
	}
}

func TestWithFakeBatchGivesAHandlerABatchWithNoRepository(t *testing.T) {
	t.Parallel()

	m, b := bus.Batchable{}.WithFakeBatch(bus.Batch{Name: "import", TotalJobs: 3, PendingJobs: 1})
	if b.ID == "" || m.BatchID != b.ID {
		t.Fatalf("the fake batch is %+v and the job says %q", b, m.BatchID)
	}

	got, err := m.Batch(context.Background(), grant(), nil)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if got.Name != "import" || got.ProcessedJobs() != 2 {
		t.Errorf("batch = %+v, want import with two processed", got)
	}
	working, err := m.Batching(context.Background(), grant(), nil)
	if err != nil || !working {
		t.Errorf("Batching on a running fake batch = %v (%v), want true", working, err)
	}
}

func TestWithBatchIdPutsAJobInABatch(t *testing.T) {
	t.Parallel()

	m := bus.Batchable{}.WithBatchId("b-7")
	if m.BatchID != "b-7" {
		t.Errorf("BatchID = %q, want b-7", m.BatchID)
	}
}

func TestUniqueLockTakesAndGivesBackTheClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	lock := bus.NewUniqueLock(repo())
	j := bus.UniqueJob{Key: "report.monthly:2026-08", Name: "report.monthly", TTL: time.Minute}

	if !strings.HasPrefix(lock.GetKey(j), "bus:unique:") {
		t.Errorf("GetKey = %q, want it namespaced", lock.GetKey(j))
	}

	taken, err := lock.Acquire(ctx, grant(), j)
	if err != nil || !taken {
		t.Fatalf("Acquire = %v (%v), want true", taken, err)
	}
	again, err := lock.Acquire(ctx, grant(), j)
	if err != nil || again {
		t.Fatalf("Acquire while held = %v (%v), want false", again, err)
	}
	if err := lock.Release(ctx, grant(), j); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if taken, err := lock.Acquire(ctx, grant(), j); err != nil || !taken {
		t.Fatalf("Acquire after Release = %v (%v), want true", taken, err)
	}
}

// eventRecorder collects what the repository reports.
type eventRecorder struct{ list []events.Event }

func (r *eventRecorder) Record(e events.Event) { r.list = append(r.list, e) }

func (r *eventRecorder) names() []string {
	var out []string
	for _, e := range r.list {
		out = append(out, e.Name)
	}
	return out
}

func TestTheThreeBatchEventsAreRecorded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rec := &eventRecorder{}
	store, queue := bus.NewMemory(), &recorder{}

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		WithEvents(rec).
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got := rec.names(); len(got) != 1 || got[0] != busevents.BatchDispatched {
		t.Fatalf("recorded %v, want batch.dispatched", got)
	}
	p, ok := rec.list[0].Payload.(busevents.Payload)
	if !ok || p.ID != b.ID || p.Tenant != tenant || p.TotalJobs != 1 {
		t.Errorf("payload = %+v", rec.list[0].Payload)
	}
}
