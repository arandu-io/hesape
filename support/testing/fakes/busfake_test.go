package fakes

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type chargeCard struct {
	Amount int

	chained    []any
	connection string
	queue      string
	delay      time.Duration
}

func (j *chargeCard) Chained() []any                      { return j.chained }
func (j *chargeCard) Chain(jobs []any)                    { j.chained = jobs }
func (j *chargeCard) AllOnConnection(connection string)   { j.connection = connection }
func (j *chargeCard) AllOnQueue(queue string)             { j.queue = queue }
func (j *chargeCard) Delay(delay time.Duration)           { j.delay = delay }
func (j *chargeCard) sameAmountAs(other *chargeCard) bool { return j.Amount == other.Amount }

type sendReceipt struct{ To string }

type chainedBatchJob struct{ batch *PendingBatchFake }

func (j chainedBatchJob) ToPendingBatch() *PendingBatchFake { return j.batch }

// spyBusDispatcher records what the fake forwarded.
type spyBusDispatcher struct {
	mu         sync.Mutex
	dispatched []any
	pipes      []any
	mapped     map[string]string
}

func (d *spyBusDispatcher) Dispatch(command any) any {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched = append(d.dispatched, command)
	return "handled"
}

func (d *spyBusDispatcher) DispatchSync(command any, handler any) any { return d.Dispatch(command) }
func (d *spyBusDispatcher) DispatchNow(command any, handler any) any  { return d.Dispatch(command) }
func (d *spyBusDispatcher) DispatchToQueue(command any) any           { return d.Dispatch(command) }
func (d *spyBusDispatcher) PipeThrough(pipes []any)                   { d.pipes = pipes }
func (d *spyBusDispatcher) HasCommandHandler(command any) bool        { return true }
func (d *spyBusDispatcher) GetCommandHandler(command any) any         { return "the handler" }
func (d *spyBusDispatcher) Map(commands map[string]string)            { d.mapped = commands }

func TestBusFakeAssertDispatchedCoversAllThreeWays(t *testing.T) {
	t.Parallel()

	// A job dispatched synchronously answers AssertDispatched, as it does in
	// the PHP: the three ledgers are counted together there.
	bus := NewBusFake(nil)
	bus.DispatchSync(sendReceipt{}, nil)

	r := &recorder{}
	bus.AssertDispatched(r, reflect.TypeFor[sendReceipt](), nil)
	assertPasses(t, r)

	bus = NewBusFake(nil)
	bus.DispatchAfterResponse(sendReceipt{})

	r = &recorder{}
	bus.AssertDispatched(r, reflect.TypeFor[sendReceipt](), nil)
	assertPasses(t, r)
}

func TestBusFakeAssertDispatchedNamesWhereEverythingWent(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.DispatchSync(sendReceipt{}, nil)

	r := &recorder{}
	bus.AssertDispatched(r, reflect.TypeFor[chargeCard](), nil)
	// The reader has to be told that a sendReceipt went out synchronously,
	// otherwise the message reads as if nothing happened at all.
	assertFails(t, r, "fakes.chargeCard", "fakes.sendReceipt dispatched synchronously")

	r = &recorder{}
	NewBusFake(nil).AssertDispatched(r, reflect.TypeFor[chargeCard](), nil)
	assertFails(t, r, "Nothing was dispatched")
}

func TestBusFakeAssertDispatchedSyncAndAfterResponse(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.Dispatch(sendReceipt{})

	// A job dispatched to the queue is not one dispatched synchronously.
	r := &recorder{}
	bus.AssertDispatchedSync(r, reflect.TypeFor[sendReceipt](), nil)
	assertFails(t, r, "not dispatched synchronously")

	r = &recorder{}
	bus.AssertDispatchedAfterResponse(r, reflect.TypeFor[sendReceipt](), nil)
	assertFails(t, r, "not dispatched after sending the response")

	bus.DispatchSync(sendReceipt{}, nil)
	bus.DispatchAfterResponse(sendReceipt{})

	r = &recorder{}
	bus.AssertDispatchedSync(r, reflect.TypeFor[sendReceipt](), 1)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertDispatchedAfterResponse(r, reflect.TypeFor[sendReceipt](), 1)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertDispatchedTimes(r, reflect.TypeFor[sendReceipt](), 3)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertNotDispatchedSync(r, reflect.TypeFor[sendReceipt](), nil)
	assertFails(t, r, "dispatched synchronously 1 time")

	r = &recorder{}
	bus.AssertNotDispatchedAfterResponse(r, reflect.TypeFor[sendReceipt](), nil)
	assertFails(t, r, "dispatched after sending the response 1 time")
}

func TestBusFakeAssertDispatchedOnce(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.Dispatch(sendReceipt{})

	r := &recorder{}
	bus.AssertDispatchedOnce(r, reflect.TypeFor[sendReceipt]())
	assertPasses(t, r)

	bus.Dispatch(sendReceipt{})
	r = &recorder{}
	bus.AssertDispatchedOnce(r, reflect.TypeFor[sendReceipt]())
	assertFails(t, r, "dispatched 2 times instead of 1 time")
}

func TestBusFakeAssertNothingDispatchedReadsTheQueueLedger(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)

	r := &recorder{}
	bus.AssertNothingDispatched(r)
	assertPasses(t, r)

	// A job dispatched synchronously is not one dispatched, there or here.
	bus.DispatchSync(sendReceipt{}, nil)
	r = &recorder{}
	bus.AssertNothingDispatched(r)
	assertPasses(t, r)

	bus.Dispatch(sendReceipt{})
	bus.Dispatch(sendReceipt{})
	r = &recorder{}
	bus.AssertNothingDispatched(r)
	assertFails(t, r, "fakes.sendReceipt dispatched 2 times")
}

func TestBusFakeAssertChained(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.Chain([]any{&chargeCard{Amount: 10}, sendReceipt{To: "a@x"}}).Dispatch()

	r := &recorder{}
	bus.AssertChained(r, []any{reflect.TypeFor[chargeCard](), reflect.TypeFor[sendReceipt]()})
	assertPasses(t, r)

	// A truth test in the first slot is asked about the job that led.
	r = &recorder{}
	bus.AssertChained(r, []any{
		func(job any) bool { return job.(*chargeCard).Amount == 10 },
		reflect.TypeFor[sendReceipt](),
	})
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertChained(r, []any{reflect.TypeFor[chargeCard](), reflect.TypeFor[chargeCard]()})
	assertFails(t, r, "the expected chain", "was not dispatched behind")

	// A chain of the wrong length never matches, however right the first link.
	r = &recorder{}
	bus.AssertChained(r, []any{reflect.TypeFor[chargeCard]()})
	assertFails(t, r, "was not dispatched behind")

	r = &recorder{}
	bus.AssertChained(r, nil)
	assertFails(t, r, "can not be empty")
}

func TestBusFakeAssertChainedFindsABatchInTheChain(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	inner := NewPendingBatchFake(bus, []any{sendReceipt{}})
	inner.Name = "receipts"
	bus.Chain([]any{&chargeCard{Amount: 10}, chainedBatchJob{batch: inner}}).Dispatch()

	r := &recorder{}
	bus.AssertChained(r, []any{
		reflect.TypeFor[chargeCard](),
		bus.ChainedBatch(func(batch *PendingBatchFake) bool { return batch.Name == "receipts" }),
	})
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertChained(r, []any{
		reflect.TypeFor[chargeCard](),
		bus.ChainedBatch(func(batch *PendingBatchFake) bool { return batch.Name == "invoices" }),
	})
	assertFails(t, r, "was not dispatched behind")
}

func TestBusFakeAssertDispatchedWithoutChain(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.Dispatch(&chargeCard{Amount: 10})

	r := &recorder{}
	bus.AssertDispatchedWithoutChain(r, reflect.TypeFor[chargeCard](), nil)
	assertPasses(t, r)

	bus = NewBusFake(nil)
	bus.Chain([]any{&chargeCard{Amount: 10}, sendReceipt{}}).Dispatch()

	r = &recorder{}
	bus.AssertDispatchedWithoutChain(r, reflect.TypeFor[chargeCard](), nil)
	assertFails(t, r, "chained [fakes.sendReceipt]")
}

func TestBusFakeChainWritesOntoTheFirstJob(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	chain := bus.Chain([]any{&chargeCard{Amount: 10}, sendReceipt{}})
	chain.Connection = "redis"
	chain.Queue = "payments"
	chain.Delay = time.Minute
	chain.Dispatch()

	dispatched := bus.Dispatched(reflect.TypeFor[chargeCard](), nil)
	if len(dispatched) != 1 {
		t.Fatalf("Dispatched = %d, want the leading job", len(dispatched))
	}
	job := dispatched[0].(*chargeCard)
	if job.connection != "redis" || job.queue != "payments" || job.delay != time.Minute {
		t.Errorf("the chain gave the job %q/%q/%s, want redis/payments/1m0s", job.connection, job.queue, job.delay)
	}
	if len(job.chained) != 1 {
		t.Errorf("the job carries %d chained jobs, want 1", len(job.chained))
	}

	// An empty chain dispatches nothing rather than a nil job.
	if got := bus.Chain(nil).Dispatch(); got != nil {
		t.Errorf("dispatching an empty chain answered %v, want nothing", got)
	}
}

func TestBusFakeBatches(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)

	r := &recorder{}
	bus.AssertNothingBatched(r)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertBatchCount(r, 0)
	assertPasses(t, r)

	batch := bus.Batch([]any{sendReceipt{}, sendReceipt{}})
	batch.Name = "receipts"
	stored := batch.Dispatch()

	if stored.Name != "receipts" || stored.TotalJobs != 2 || stored.PendingJobs != 2 {
		t.Errorf("the stored batch is %+v, want receipts with 2 jobs pending", stored)
	}
	if stored.ID == "" {
		t.Error("the stored batch should carry an id")
	}
	if found := bus.FindBatch(stored.ID); found != stored {
		t.Error("FindBatch should answer the batch that was stored")
	}
	if bus.FindBatch("nothing here") != nil {
		t.Error("FindBatch should answer nothing for an id that was never stored")
	}

	r = &recorder{}
	bus.AssertBatched(r, func(b *PendingBatchFake) bool { return b.Name == "receipts" })
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertBatched(r, func(b *PendingBatchFake) bool { return b.Name == "invoices" })
	assertFails(t, r, "the expected batch was not dispatched", "[receipts] of 2 jobs", "fakes.sendReceipt")

	r = &recorder{}
	bus.AssertBatchCount(r, 1)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertBatchCount(r, 2)
	assertFails(t, r, "was 1 instead of 2")

	r = &recorder{}
	bus.AssertNothingBatched(r)
	assertFails(t, r, "dispatched unexpectedly", "receipts")

	r = &recorder{}
	bus.AssertNothingPlaced(r)
	assertFails(t, r, "AssertNothingBatched")
}

func TestBusFakeDispatchFakeBatch(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	batch := bus.DispatchFakeBatch("nightly")

	if batch.Name != "nightly" || batch.TotalJobs != 0 {
		t.Errorf("DispatchFakeBatch gave %+v, want an empty batch named nightly", batch)
	}
	r := &recorder{}
	bus.AssertBatchCount(r, 1)
	assertPasses(t, r)
}

func TestBusFakeBatchesDispatchedAfterTheResponseAreRecordedToo(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	bus.Batch([]any{sendReceipt{}}).DispatchAfterResponse()

	r := &recorder{}
	bus.AssertBatchCount(r, 1)
	assertPasses(t, r)
}

func TestBusFakeExceptForwardsToTheRealDispatcher(t *testing.T) {
	t.Parallel()

	spy := &spyBusDispatcher{}
	bus := NewBusFake(spy).Except(reflect.TypeFor[sendReceipt]())

	bus.Dispatch(&chargeCard{})
	answer := bus.Dispatch(sendReceipt{})

	if len(spy.dispatched) != 1 {
		t.Fatalf("the real dispatcher got %d jobs, want 1", len(spy.dispatched))
	}
	if answer != "handled" {
		t.Errorf("Dispatch answered %v, want what the real dispatcher answered", answer)
	}
	if bus.HasDispatched(reflect.TypeFor[sendReceipt]()) {
		t.Error("the job Except named should not have been recorded")
	}
	if !bus.HasDispatched(reflect.TypeFor[chargeCard]()) {
		t.Error("the job that was not named should have been recorded")
	}
}

func TestBusFakeOnlyFakesTheJobsItWasNamed(t *testing.T) {
	t.Parallel()

	spy := &spyBusDispatcher{}
	bus := NewBusFake(spy, reflect.TypeFor[sendReceipt]())

	bus.Dispatch(sendReceipt{})
	bus.Dispatch(&chargeCard{})

	if !bus.HasDispatched(reflect.TypeFor[sendReceipt]()) {
		t.Error("the named job should have been recorded")
	}
	if len(spy.dispatched) != 1 {
		t.Errorf("the real dispatcher got %d jobs, want 1", len(spy.dispatched))
	}
}

func TestBusFakeAcceptsAClosureAsAJobToFake(t *testing.T) {
	t.Parallel()

	spy := &spyBusDispatcher{}
	bus := NewBusFake(spy, func(job any) bool {
		card, ok := job.(*chargeCard)
		return ok && card.Amount > 100
	})

	bus.Dispatch(&chargeCard{Amount: 500})
	bus.Dispatch(&chargeCard{Amount: 5})

	if got := len(bus.Dispatched(reflect.TypeFor[chargeCard](), nil)); got != 1 {
		t.Errorf("the fake recorded %d jobs, want the one over 100", got)
	}
	if len(spy.dispatched) != 1 {
		t.Errorf("the real dispatcher got %d jobs, want the one under 100", len(spy.dispatched))
	}
}

func TestBusFakeSerializeAndRestore(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil).SerializeAndRestore(true)
	original := &chargeCard{Amount: 10}
	bus.Dispatch(original)

	dispatched := bus.Dispatched(reflect.TypeFor[chargeCard](), nil)
	if len(dispatched) != 1 {
		t.Fatalf("Dispatched = %d, want 1", len(dispatched))
	}
	if dispatched[0] == any(original) {
		t.Error("the recorded job should be the copy that came back from the round trip")
	}
	if !dispatched[0].(*chargeCard).sameAmountAs(original) {
		t.Error("the round trip should keep what the queue would have kept")
	}
}

func TestBusFakeForwardsHandlerQuestions(t *testing.T) {
	t.Parallel()

	spy := &spyBusDispatcher{}
	bus := NewBusFake(spy)

	if !bus.HasCommandHandler(sendReceipt{}) {
		t.Error("HasCommandHandler should reach the real dispatcher")
	}
	if bus.GetCommandHandler(sendReceipt{}) != "the handler" {
		t.Error("GetCommandHandler should reach the real dispatcher")
	}
	bus.PipeThrough([]any{"a pipe"})
	if len(spy.pipes) != 1 {
		t.Error("PipeThrough should reach the real dispatcher")
	}
	bus.Map(map[string]string{"fakes.sendReceipt": "fakes.sendReceiptHandler"})
	if len(spy.mapped) != 1 {
		t.Error("Map should reach the real dispatcher")
	}

	// A fake with no dispatcher behind it answers rather than panicking.
	alone := NewBusFake(nil)
	if alone.HasCommandHandler(sendReceipt{}) {
		t.Error("a fake with no dispatcher should answer that there is no handler")
	}
	if alone.GetCommandHandler(sendReceipt{}) != nil {
		t.Error("a fake with no dispatcher should answer no handler")
	}
	alone.PipeThrough(nil)
	alone.Map(nil)
}

func TestBusFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	bus := NewBusFake(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Dispatch(sendReceipt{})
			bus.DispatchSync(sendReceipt{}, nil)
			bus.Batch([]any{sendReceipt{}}).Dispatch()
			bus.HasDispatched(reflect.TypeFor[sendReceipt]())
		}()
	}
	wg.Wait()

	r := &recorder{}
	bus.AssertDispatchedTimes(r, reflect.TypeFor[sendReceipt](), 100)
	assertPasses(t, r)

	r = &recorder{}
	bus.AssertBatchCount(r, 50)
	assertPasses(t, r)
}
