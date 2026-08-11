package fakes

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type sampleJob struct {
	Name    string
	chained []any
}

func (j sampleJob) Chained() []any { return j.chained }

type otherJob struct{ Name string }

type connectedJob struct{ Name string }

func (j connectedJob) Connection() string { return "redis" }

// spyQueue records what the fake handed to the real queue, which is the only
// way to see the Except path from outside.
type spyQueue struct {
	mu         sync.Mutex
	pushed     []any
	connection string
	raw        []RawPush
}

func (q *spyQueue) Connection(name string) Queue {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.connection = name
	return q
}

func (q *spyQueue) Push(job any, data any, queue string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pushed = append(q.pushed, job)
}

func (q *spyQueue) PushRaw(payload string, queue string, options map[string]any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.raw = append(q.raw, RawPush{Payload: payload, Queue: queue, Options: options})
}

func (q *spyQueue) Later(delay time.Duration, job any, data any, queue string) {
	q.Push(job, data, queue)
}

func (q *spyQueue) Bulk(jobs []any, data any, queue string) {
	for _, job := range jobs {
		q.Push(job, data, queue)
	}
}

func (q *spyQueue) Size(queue string) int { return 0 }

func (q *spyQueue) Pop(queue string) any { return nil }

func TestQueueFakeAssertPushed(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{Name: "a"}, nil, "")

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), nil)
	assertPasses(t, r)

	// A value of the type names it as well as the type does.
	r = &recorder{}
	queue.AssertPushed(r, sampleJob{}, nil)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[otherJob](), nil)
	assertFails(t, r, "otherJob", "not pushed", "1 job", "fakes.sampleJob")
}

func TestQueueFakeAssertPushedNamesWhatWasPushedInstead(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{Name: "a"}, nil, "invoices")
	queue.Push(otherJob{Name: "b"}, nil, "")

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[connectedJob](), nil)
	// The message has to carry both jobs and the queues they went on: a
	// failure that says only "was not pushed" leaves the reader guessing.
	assertFails(t, r, "fakes.sampleJob", "invoices", "fakes.otherJob", "default queue")
}

func TestQueueFakeAssertPushedWithACount(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{}, nil, "")
	queue.Push(sampleJob{}, nil, "")

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), 2)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), 3)
	assertFails(t, r, "pushed 2 times instead of 3 times")

	// Zero is a count like any other, not "no count given".
	r = &recorder{}
	queue.AssertPushedTimes(r, reflect.TypeFor[otherJob](), 0)
	assertPasses(t, r)
}

func TestQueueFakeAssertPushedRejectsACallbackItDoesNotKnow(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), "not a callback")
	assertFails(t, r, "the callback must be nil", "got string")
}

func TestQueueFakeTruthTestsSeeQueueAndData(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{Name: "a"}, "payload", "invoices")

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), func(job any) bool {
		return job.(sampleJob).Name == "a"
	})
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), func(job any, q string) bool {
		return q == "invoices"
	})
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), func(job any, q string, data any) bool {
		return data == "payload"
	})
	assertPasses(t, r)
}

func TestQueueFakeAssertPushedOn(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.PushOn("invoices", sampleJob{Name: "a"}, nil)

	r := &recorder{}
	queue.AssertPushedOn(r, "invoices", reflect.TypeFor[sampleJob](), nil)
	assertPasses(t, r)

	// The job was pushed, but somewhere else: the message says so rather than
	// claiming it was never pushed.
	r = &recorder{}
	queue.AssertPushedOn(r, "receipts", reflect.TypeFor[sampleJob](), nil)
	assertFails(t, r, "receipts", "It was pushed 1 time", "invoices")

	r = &recorder{}
	queue.AssertPushedOn(r, "receipts", reflect.TypeFor[otherJob](), nil)
	assertFails(t, r, "was not pushed at all")
}

func TestQueueFakeAssertNotPushedAndNothingPushed(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)

	r := &recorder{}
	queue.AssertNothingPushed(r)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertNotPushed(r, reflect.TypeFor[sampleJob](), nil)
	assertPasses(t, r)

	queue.Push(sampleJob{}, nil, "")

	r = &recorder{}
	queue.AssertNotPushed(r, reflect.TypeFor[sampleJob](), nil)
	assertFails(t, r, "unexpected [fakes.sampleJob] job was pushed 1 time")

	r = &recorder{}
	queue.AssertNothingPushed(r)
	assertFails(t, r, "pushed unexpectedly", "fakes.sampleJob")
}

func TestQueueFakeAssertCount(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{}, nil, "")
	queue.Push(otherJob{}, nil, "")

	r := &recorder{}
	queue.AssertCount(r, 2)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertCount(r, 1)
	assertFails(t, r, "expected 1 job to be pushed, but found 2")
}

func TestQueueFakeAssertPushedWithChain(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{Name: "first", chained: []any{otherJob{Name: "second"}}}, nil, "")

	r := &recorder{}
	queue.AssertPushedWithChain(r, reflect.TypeFor[sampleJob](), []any{reflect.TypeFor[otherJob]()}, nil)
	assertPasses(t, r)

	// A value in the chain is compared by value, as the PHP compares two
	// serialized jobs.
	r = &recorder{}
	queue.AssertPushedWithChain(r, reflect.TypeFor[sampleJob](), []any{otherJob{Name: "second"}}, nil)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushedWithChain(r, reflect.TypeFor[sampleJob](), []any{otherJob{Name: "wrong"}}, nil)
	assertFails(t, r, "the expected chain", "was not pushed behind")

	// An empty expected chain is the caller's mistake, and is named as one.
	r = &recorder{}
	queue.AssertPushedWithChain(r, reflect.TypeFor[sampleJob](), nil, nil)
	assertFails(t, r, "can not be empty", "AssertPushedWithoutChain")
}

func TestQueueFakeAssertPushedWithoutChain(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(otherJob{Name: "alone"}, nil, "")
	queue.Push(sampleJob{Name: "leading", chained: []any{otherJob{}}}, nil, "")

	r := &recorder{}
	queue.AssertPushedWithoutChain(r, reflect.TypeFor[otherJob](), nil)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertPushedWithoutChain(r, reflect.TypeFor[sampleJob](), nil)
	assertFails(t, r, "chained")
}

func TestQueueFakeClosuresBecomeCallQueuedClosure(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)

	r := &recorder{}
	queue.AssertClosureNotPushed(r, nil)
	assertPasses(t, r)

	queue.Push(func() {}, nil, "")

	r = &recorder{}
	queue.AssertClosurePushed(r, nil)
	assertPasses(t, r)

	r = &recorder{}
	queue.AssertClosureNotPushed(r, nil)
	assertFails(t, r, "CallQueuedClosure")
}

func TestQueueFakeExceptSendsToTheRealQueue(t *testing.T) {
	t.Parallel()

	spy := &spyQueue{}
	queue := NewQueueFake(spy).Except(reflect.TypeFor[otherJob]())

	queue.Push(sampleJob{}, nil, "")
	queue.Push(otherJob{}, nil, "")

	if len(spy.pushed) != 1 {
		t.Fatalf("the real queue got %d jobs, want 1", len(spy.pushed))
	}
	if _, ok := spy.pushed[0].(otherJob); !ok {
		t.Errorf("the real queue got %T, want fakes.otherJob", spy.pushed[0])
	}

	r := &recorder{}
	queue.AssertPushed(r, reflect.TypeFor[sampleJob](), nil)
	assertPasses(t, r)

	// The job that went to the real queue is not recorded, so it cannot be
	// asserted on: that is the whole point of Except.
	r = &recorder{}
	queue.AssertNotPushed(r, reflect.TypeFor[otherJob](), nil)
	assertPasses(t, r)
}

func TestQueueFakeOnlyFakesTheJobsItWasNamed(t *testing.T) {
	t.Parallel()

	spy := &spyQueue{}
	queue := NewQueueFake(spy, reflect.TypeFor[sampleJob]())

	queue.Push(sampleJob{}, nil, "")
	queue.Push(otherJob{}, nil, "")

	if len(spy.pushed) != 1 {
		t.Fatalf("the real queue got %d jobs, want 1", len(spy.pushed))
	}
	if !queue.HasPushed(reflect.TypeFor[sampleJob]()) {
		t.Error("the named job should have been recorded")
	}
	if queue.HasPushed(reflect.TypeFor[otherJob]()) {
		t.Error("the job that was not named should have gone to the real queue")
	}
}

func TestQueueFakeSendsAJobThroughItsOwnConnection(t *testing.T) {
	t.Parallel()

	spy := &spyQueue{}
	queue := NewQueueFake(spy).Except(reflect.TypeFor[connectedJob]())
	queue.Push(connectedJob{}, nil, "")

	if spy.connection != "redis" {
		t.Errorf("the job went through the %q connection, want redis", spy.connection)
	}
}

func TestQueueFakeSizeCountsOneQueue(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.PushOn("invoices", sampleJob{}, nil)
	queue.PushOn("invoices", otherJob{}, nil)
	queue.Push(sampleJob{}, nil, "")

	if got := queue.Size("invoices"); got != 2 {
		t.Errorf("Size(invoices) = %d, want 2", got)
	}
	// The empty name is PHP's null: the default queue, not "every queue".
	if got := queue.Size(""); got != 1 {
		t.Errorf("Size() = %d, want 1", got)
	}
	if got := queue.Size("nothing here"); got != 0 {
		t.Errorf("Size(nothing here) = %d, want 0", got)
	}
}

func TestQueueFakeRawPushes(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.PushRaw("{}", "invoices", map[string]any{"delay": 5})

	if got := len(queue.RawPushes()); got != 1 {
		t.Fatalf("RawPushes = %d, want 1", got)
	}
	matched := queue.PushedRaw(func(payload string, q string, options map[string]any) bool {
		return q == "invoices"
	})
	if len(matched) != 1 || matched[0].Payload != "{}" {
		t.Errorf("PushedRaw = %v, want the one raw push", matched)
	}
	// A raw push is not a job, and never answers a job assertion.
	r := &recorder{}
	queue.AssertNothingPushed(r)
	assertPasses(t, r)
}

func TestQueueFakeLaterDoesNotSleep(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	start := time.Now()
	queue.Later(time.Hour, sampleJob{}, nil, "")
	queue.LaterOn("invoices", time.Hour, otherJob{}, nil)

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Later took %s, want no wait at all", elapsed)
	}
	if got := len(queue.Pushed(reflect.TypeFor[sampleJob](), nil)); got != 1 {
		t.Errorf("a delayed job was recorded %d times, want 1", got)
	}
	if got := queue.Size("invoices"); got != 1 {
		t.Errorf("LaterOn put the job on %d queues named invoices, want 1", got)
	}
}

func TestQueueFakeBulkPushesEach(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Bulk([]any{sampleJob{}, otherJob{}}, nil, "invoices")

	r := &recorder{}
	queue.AssertCount(r, 2)
	assertPasses(t, r)
	// An empty bulk pushes nothing, rather than one empty job.
	queue.Bulk(nil, nil, "")
	r = &recorder{}
	queue.AssertCount(r, 2)
	assertPasses(t, r)
}

func TestQueueFakeSerializeAndRestore(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil).SerializeAndRestore(true)
	queue.Push(&sampleJob{Name: "a"}, nil, "")

	pushed := queue.Pushed(reflect.TypeFor[sampleJob](), nil)
	if len(pushed) != 1 {
		t.Fatalf("Pushed = %d, want 1", len(pushed))
	}
	// The recorded job is a copy that came back through the round trip, not
	// the value the caller still holds.
	restored, ok := pushed[0].(*sampleJob)
	if !ok {
		t.Fatalf("Pushed[0] = %T, want *fakes.sampleJob", pushed[0])
	}
	if restored.Name != "a" {
		t.Errorf("the restored job kept %q, want a", restored.Name)
	}
}

func TestQueueFakePushedJobsAndConnection(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	queue.Push(sampleJob{Name: "a"}, nil, "invoices")

	jobs := queue.PushedJobs()
	if got := len(jobs["fakes.sampleJob"]); got != 1 {
		t.Fatalf("PushedJobs[fakes.sampleJob] = %d, want 1", got)
	}
	if jobs["fakes.sampleJob"][0].Queue != "invoices" {
		t.Errorf("the record kept the %q queue, want invoices", jobs["fakes.sampleJob"][0].Queue)
	}
	if queue.Connection("anything") != Queue(queue) {
		t.Error("Connection should answer the fake itself, whatever the name")
	}
	if got := queue.GetConnectionName(); got != "" {
		t.Errorf("GetConnectionName = %q, want empty", got)
	}
	queue.SetConnectionName("redis")
	if got := queue.GetConnectionName(); got != "redis" {
		t.Errorf("GetConnectionName = %q, want redis", got)
	}
	if queue.Pop("") != nil {
		t.Error("Pop should answer nothing, as the PHP does")
	}
}

func TestQueueFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	queue := NewQueueFake(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queue.Push(sampleJob{Name: "a"}, nil, "invoices")
			queue.HasPushed(reflect.TypeFor[sampleJob]())
			queue.Size("invoices")
		}()
	}
	wg.Wait()

	r := &recorder{}
	queue.AssertCount(r, 50)
	assertPasses(t, r)
}
