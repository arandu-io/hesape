package fakes

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

// Queue is what a [QueueFake] needs from the queue it stands in for.
//
// A fake only reaches for it when [QueueFake.Except] named a job that must go
// to the real queue; a fake built with a nil queue and no Except never touches
// it.
type Queue interface {
	// Connection returns the queue for the named connection, which is what a
	// job carrying its own connection is pushed through.
	Connection(name string) Queue
	// Push puts the job on the named queue.
	Push(job any, data any, queue string)
	// PushRaw puts an already-encoded payload on the named queue.
	PushRaw(payload string, queue string, options map[string]any)
	// Later puts the job on the named queue after the delay.
	Later(delay time.Duration, job any, data any, queue string)
	// Bulk puts several jobs on the named queue at once.
	Bulk(jobs []any, data any, queue string)
	// Size returns how many jobs the named queue holds.
	Size(queue string) int
	// Pop takes the next job off the named queue.
	Pop(queue string) any
}

// Queueable is what a [QueueFake] reads off a job it is about to hand to the
// real queue.
type Queueable interface {
	// Connection returns the connection the job asked to run on.
	Connection() string
}

// Chained is satisfied by a job carrying a chain of jobs behind it, and is what
// [QueueFake.AssertPushedWithChain] and the chain assertions of [BusFake] read.
//
// It returns the jobs themselves rather than an encoded form of them.
type Chained interface {
	// Chained returns the jobs queued behind this one.
	Chained() []any
}

// Chainable is satisfied by a job a [PendingChainFake] can configure before it
// goes out: the first job of a chain.
type Chainable interface {
	Chained
	// Chain sets the jobs queued behind this one.
	Chain(jobs []any)
	// AllOnConnection puts the whole chain on the named connection.
	AllOnConnection(connection string)
	// AllOnQueue puts the whole chain on the named queue.
	AllOnQueue(queue string)
	// Delay holds the chain back for the given duration.
	Delay(delay time.Duration)
}

// QueuedListener is what [QueueFake.ListenersPushed] reads off a pushed job to
// recognize a queued event listener.
type QueuedListener interface {
	// ListenerClass names the listener that was queued.
	ListenerClass() string
	// ListenerData returns the arguments it was queued with.
	ListenerData() []any
}

// CallQueuedClosure is what a closure becomes when it is pushed onto the
// queue, and the type [QueueFake.AssertClosurePushed] looks for.
type CallQueuedClosure struct {
	// Closure is the func that was queued.
	Closure func()
}

// NewCallQueuedClosure wraps a closure so it can be pushed onto the queue.
func NewCallQueuedClosure(closure func()) *CallQueuedClosure {
	return &CallQueuedClosure{Closure: closure}
}

// PushedJob is one recorded push: the job, the queue it was named onto, and the
// data pushed beside it.
type PushedJob struct {
	// Job is what was pushed.
	Job any
	// Queue is the queue it was named onto, and may be empty.
	Queue string
	// Data is whatever was pushed beside it.
	Data any
}

// RawPush is one recorded raw push: the payload, the queue it was named onto,
// and the options it carried.
type RawPush struct {
	// Payload is the already-encoded job.
	Payload string
	// Queue is the queue it was named onto, and may be empty.
	Queue string
	// Options are the settings it carried.
	Options map[string]any
}

// QueueFake is the queue a test installs so that nothing is worked, and every
// push can be asserted on.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while
// the lock is held, so a callback that asks the fake another question does not
// deadlock.
type QueueFake struct {
	mu sync.Mutex
	// queue is the real queue the fake stands in for.
	// It is unexported here because Queue() would collide with the queue
	// name a record carries; OriginalQueue reads it.
	queue               Queue
	jobsToFake          []any
	jobsToBeQueued      []any
	jobs                []PushedJob
	rawPushes           []RawPush
	serializeAndRestore bool
	connectionName      string
}

// NewQueueFake builds a queue that records the named job types and forwards
// the rest.
//
// A nil queue is the ordinary case: it is only reached for by a job that
// Except sent to the real thing.
func NewQueueFake(queue Queue, jobsToFake ...any) *QueueFake {
	return &QueueFake{queue: queue, jobsToFake: jobsToFake}
}

func (f *QueueFake) isFake() {}

// OriginalQueue returns the queue the fake stands in for, which may be nil.
func (f *QueueFake) OriginalQueue() Queue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queue
}

// Except names the jobs that should reach the real queue
// instead of being recorded.
//
// The new ones go in front of the ones already there. The slice is copied
// rather than appended to, because a caller who spread their own slice into
// the call would find it written through otherwise.
func (f *QueueFake) Except(jobs ...any) *QueueFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobsToBeQueued = append(append([]any(nil), jobs...), f.jobsToBeQueued...)
	return f
}

// AssertPushed fails the test unless a job of the
// given type was pushed and the truth test accepted it.
//
// Go has no overloading, so the slot is typed any and a form it does not know
// is reported as a failure naming the forms it does.
func (f *QueueFake) AssertPushed(t TestingT, job any, callback any) {
	t.Helper()

	if times, ok := callback.(int); ok {
		f.AssertPushedTimes(t, job, times)
		return
	}

	test, ok := pushTest(t, "AssertPushed", callback)
	if !ok {
		return
	}

	if len(f.pushedRecords(job, test)) > 0 {
		return
	}

	pushed := f.snapshot()
	t.Errorf(
		"AssertPushed: the expected [%s] job was not pushed. %s were pushed:%s",
		tokenName(job), countedAs(len(pushed), "job"), listOf(describePushed(pushed)),
	)
}

// AssertPushedTimes fails the test unless
// exactly that many jobs of the type were pushed.
//
// It is also a method of its own.
func (f *QueueFake) AssertPushedTimes(t TestingT, job any, times int) {
	t.Helper()

	found := f.pushedRecords(job, nil)
	if len(found) == times {
		return
	}

	pushed := f.snapshot()
	t.Errorf(
		"AssertPushedTimes: the expected [%s] job was pushed %d %s instead of %d %s. %s were pushed:%s",
		tokenName(job), len(found), plural("time", len(found)), times, plural("time", times),
		countedAs(len(pushed), "job"), listOf(describePushed(pushed)),
	)
}

// AssertPushedOn fails the test unless the job was pushed, and
// onto that queue.
func (f *QueueFake) AssertPushedOn(t TestingT, queue string, job any, callback any) {
	t.Helper()

	test, ok := pushTest(t, "AssertPushedOn", callback)
	if !ok {
		return
	}

	onQueue := func(record PushedJob) bool {
		return record.Queue == queue && callFn(test, record)
	}
	if len(f.pushedRecords(job, onQueue)) > 0 {
		return
	}

	elsewhere := f.pushedRecords(job, nil)
	if len(elsewhere) > 0 {
		t.Errorf(
			"AssertPushedOn: the expected [%s] job was not pushed on the [%s] queue. It was pushed %d %s:%s",
			tokenName(job), queue, len(elsewhere), plural("time", len(elsewhere)), listOf(describePushed(elsewhere)),
		)
		return
	}

	pushed := f.snapshot()
	t.Errorf(
		"AssertPushedOn: the expected [%s] job was not pushed at all, on the [%s] queue or anywhere else. %s were pushed:%s",
		tokenName(job), queue, countedAs(len(pushed), "job"), listOf(describePushed(pushed)),
	)
}

// AssertPushedWithChain fails the test unless the job was
// pushed, and the chain behind it is the one expected.
//
// An element of the expected chain may be a class token, a value, or a
// func(job any) bool truth test, and each element is judged on its own.
func (f *QueueFake) AssertPushedWithChain(t TestingT, job any, expectedChain []any, callback any) {
	t.Helper()

	test, ok := pushTest(t, "AssertPushedWithChain", callback)
	if !ok {
		return
	}

	if len(expectedChain) == 0 {
		t.Errorf("AssertPushedWithChain: the expected chain can not be empty. Use AssertPushedWithoutChain to assert that there is no chain.")
		return
	}

	f.assertPushedWithChain(t, "AssertPushedWithChain", job, expectedChain, test)
}

// AssertPushedWithoutChain fails the test unless the job
// was pushed, and nothing is chained behind it.
func (f *QueueFake) AssertPushedWithoutChain(t TestingT, job any, callback any) {
	t.Helper()

	test, ok := pushTest(t, "AssertPushedWithoutChain", callback)
	if !ok {
		return
	}

	f.assertPushedWithChain(t, "AssertPushedWithoutChain", job, nil, test)
}

func (f *QueueFake) assertPushedWithChain(t TestingT, name string, job any, expectedChain []any, test func(PushedJob) bool) {
	t.Helper()

	found := f.pushedRecords(job, test)
	if len(found) == 0 {
		pushed := f.snapshot()
		t.Errorf(
			"%s: the expected [%s] job was not pushed. %s were pushed:%s",
			name, tokenName(job), countedAs(len(pushed), "job"), listOf(describePushed(pushed)),
		)
		return
	}

	actual := make([]string, 0, len(found))
	for _, record := range found {
		chain := chainOf(record.Job)
		if matchesChain(chain, expectedChain) {
			return
		}
		actual = append(actual, "["+className(record.Job)+"] chained "+describeChain(chain))
	}

	t.Errorf(
		"%s: the expected chain %s was not pushed behind [%s]. %s pushed:%s",
		name, describeChain(expectedChain), tokenName(job),
		countedAs(len(found), "job"), listOf(actual),
	)
}

// AssertClosurePushed fails the test unless a closure was
// pushed onto the queue.
func (f *QueueFake) AssertClosurePushed(t TestingT, callback any) {
	t.Helper()
	f.AssertPushed(t, reflect.TypeFor[CallQueuedClosure](), callback)
}

// AssertClosureNotPushed fails the test when a closure was pushed onto the
// queue.
func (f *QueueFake) AssertClosureNotPushed(t TestingT, callback any) {
	t.Helper()
	f.AssertNotPushed(t, reflect.TypeFor[CallQueuedClosure](), callback)
}

// AssertNotPushed fails the test when a job of the given type was pushed and
// the callback accepted it.
func (f *QueueFake) AssertNotPushed(t TestingT, job any, callback any) {
	t.Helper()

	test, ok := pushTest(t, "AssertNotPushed", callback)
	if !ok {
		return
	}

	found := f.pushedRecords(job, test)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotPushed: the unexpected [%s] job was pushed %d %s:%s",
		tokenName(job), len(found), plural("time", len(found)), listOf(describePushed(found)),
	)
}

// AssertCount fails the test unless the total number of jobs pushed,
// of every type.
func (f *QueueFake) AssertCount(t TestingT, expectedCount int) {
	t.Helper()

	pushed := f.snapshot()
	if len(pushed) == expectedCount {
		return
	}

	t.Errorf(
		"AssertCount: expected %d %s to be pushed, but found %d instead:%s",
		expectedCount, plural("job", expectedCount), len(pushed), listOf(describePushed(pushed)),
	)
}

// AssertNothingPushed fails the test unless nothing was pushed at all.
func (f *QueueFake) AssertNothingPushed(t TestingT) {
	t.Helper()

	pushed := f.snapshot()
	if len(pushed) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingPushed: the following %s were pushed unexpectedly:%s",
		plural("job", len(pushed)), listOf(describePushed(pushed)),
	)
}

// Pushed returns the jobs of the given type that the truth
// test accepted, in the order they were pushed. The callback slot takes the
// same forms as AssertPushed, minus the count.
//
// A form the slot does not know yields no jobs rather than a failure, because
// this one has no test to report to; the assertions that call it do report.
func (f *QueueFake) Pushed(job any, callback any) []any {
	test, ok := pushTest(nil, "Pushed", callback)
	if !ok {
		return nil
	}
	records := f.pushedRecords(job, test)
	jobs := make([]any, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, record.Job)
	}
	return jobs
}

// PushedRaw returns the raw payloads the truth test
// accepted. A nil truth test accepts all of them.
func (f *QueueFake) PushedRaw(callback func(payload string, queue string, options map[string]any) bool) []RawPush {
	f.mu.Lock()
	raw := append([]RawPush(nil), f.rawPushes...)
	f.mu.Unlock()

	found := make([]RawPush, 0, len(raw))
	for _, push := range raw {
		if callback != nil && !callback(push.Payload, push.Queue, push.Options) {
			continue
		}
		found = append(found, push)
	}
	return found
}

// ListenersPushed returns the queued listeners
// pushed for the given listener class.
func (f *QueueFake) ListenersPushed(listenerClass string, callback func(event any, listener QueuedListener, queue string, data any) bool) []QueuedListener {
	pushed := f.snapshot()

	found := make([]QueuedListener, 0, len(pushed))
	for _, record := range pushed {
		listener, ok := record.Job.(QueuedListener)
		if !ok || listener.ListenerClass() != listenerClass {
			continue
		}
		if callback != nil {
			var event any
			if data := listener.ListenerData(); len(data) > 0 {
				event = data[0]
			}
			if !callback(event, listener, record.Queue, record.Data) {
				continue
			}
		}
		found = append(found, listener)
	}
	return found
}

// HasPushed reports whether a job of the type was pushed
// at all, truth test aside.
func (f *QueueFake) HasPushed(job any) bool {
	return len(f.pushedRecords(job, nil)) > 0
}

// Connection returns the fake itself: it stands in for every
// connection, so it answers itself whatever the name.
func (f *QueueFake) Connection(name string) Queue {
	return f
}

// Size returns how many jobs were pushed onto that queue.
func (f *QueueFake) Size(queue string) int {
	count := 0
	for _, record := range f.snapshot() {
		if record.Queue == queue {
			count++
		}
	}
	return count
}

// Push records the job, or hands it to the real
// queue when Except named it.
//
// A func() pushed here becomes a CallQueuedClosure.
func (f *QueueFake) Push(job any, data any, queue string) {
	if closure, ok := job.(func()); ok {
		job = NewCallQueuedClosure(closure)
	}

	if !f.ShouldFakeJob(job) {
		f.pushToRealQueue(job, data, queue)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	recorded := job
	if f.serializeAndRestore {
		recorded = restore(job)
	}
	f.jobs = append(f.jobs, PushedJob{Job: recorded, Queue: queue, Data: data})
}

// pushToRealQueue hands a job to the real queue: a job carrying
// its own connection goes through that connection, and every other job goes
// through the queue as it was handed over.
func (f *QueueFake) pushToRealQueue(job any, data any, queue string) {
	f.mu.Lock()
	real := f.queue
	f.mu.Unlock()

	if real == nil {
		return
	}
	if queueable, ok := job.(Queueable); ok && queueable.Connection() != "" {
		if connection := real.Connection(queueable.Connection()); connection != nil {
			connection.Push(job, data, queue)
			return
		}
	}
	real.Push(job, data, queue)
}

// ShouldFakeJob reports whether the job is recorded
// or pushed for real.
//
// A fake built without a list of jobs to fake fakes everything.
func (f *QueueFake) ShouldFakeJob(job any) bool {
	f.mu.Lock()
	toFake := append([]any(nil), f.jobsToFake...)
	toBeQueued := append([]any(nil), f.jobsToBeQueued...)
	f.mu.Unlock()

	for _, token := range toBeQueued {
		if instanceOf(job, token) {
			return false
		}
	}
	if len(toFake) == 0 {
		return true
	}
	for _, token := range toFake {
		if instanceOf(job, token) {
			return true
		}
	}
	return false
}

// PushRaw records the payload, and never sends it.
func (f *QueueFake) PushRaw(payload string, queue string, options map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rawPushes = append(f.rawPushes, RawPush{Payload: payload, Queue: queue, Options: options})
}

// Later records the job as [QueueFake.Push] does. The delay is accepted and
// dropped rather than slept.
func (f *QueueFake) Later(delay time.Duration, job any, data any, queue string) {
	f.Push(job, data, queue)
}

// PushOn records the job as pushed onto the named queue.
func (f *QueueFake) PushOn(queue string, job any, data any) {
	f.Push(job, data, queue)
}

// LaterOn records the job as pushed onto the named queue, dropping the delay.
func (f *QueueFake) LaterOn(queue string, delay time.Duration, job any, data any) {
	f.Push(job, data, queue)
}

// Pop returns nil: a fake queue
// is never worked, so there is no job to hand back.
func (f *QueueFake) Pop(queue string) any {
	return nil
}

// Bulk records each job in turn.
func (f *QueueFake) Bulk(jobs []any, data any, queue string) {
	for _, job := range jobs {
		f.Push(job, data, queue)
	}
}

// PushedJobs returns every record, keyed by the class of the job.
func (f *QueueFake) PushedJobs() map[string][]PushedJob {
	pushed := f.snapshot()
	jobs := make(map[string][]PushedJob, len(pushed))
	for _, record := range pushed {
		name := className(record.Job)
		jobs[name] = append(jobs[name], record)
	}
	return jobs
}

// RawPushes returns a copy of every raw payload recorded, in order.
func (f *QueueFake) RawPushes() []RawPush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RawPush(nil), f.rawPushes...)
}

// SerializeAndRestore says whether a job is
// put through the round trip the queue would put it through before it is
// recorded.
func (f *QueueFake) SerializeAndRestore(serializeAndRestore bool) *QueueFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serializeAndRestore = serializeAndRestore
	return f
}

// GetConnectionName returns the connection name the fake reports, which is
// empty until [QueueFake.SetConnectionName] sets one.
func (f *QueueFake) GetConnectionName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectionName
}

// SetConnectionName sets the connection name the fake reports, and returns the
// fake.
func (f *QueueFake) SetConnectionName(name string) *QueueFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectionName = name
	return f
}

// snapshot copies the ledger under the lock, so a truth test runs outside it.
func (f *QueueFake) snapshot() []PushedJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PushedJob(nil), f.jobs...)
}

// pushedRecords returns the
// records filed under the exact class, then the truth test.
func (f *QueueFake) pushedRecords(job any, test func(PushedJob) bool) []PushedJob {
	var found []PushedJob
	for _, record := range f.snapshot() {
		if !sameClass(record.Job, job) {
			continue
		}
		if !callFn(test, record) {
			continue
		}
		found = append(found, record)
	}
	return found
}

// pushTest normalizes the truth test forms QueueFake accepts into the one the
// filter uses. It reports the failure itself when handed a form it does not
// know, and answers false so the caller stops; a nil test is accepted from
// every form.
func pushTest(t TestingT, name string, callback any) (func(PushedJob) bool, bool) {
	switch cb := callback.(type) {
	case nil:
		return nil, true
	case func(job any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record PushedJob) bool { return cb(record.Job) }, true
	case func(job any, queue string) bool:
		if cb == nil {
			return nil, true
		}
		return func(record PushedJob) bool { return cb(record.Job, record.Queue) }, true
	case func(job any, queue string, data any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record PushedJob) bool { return cb(record.Job, record.Queue, record.Data) }, true
	default:
		if t != nil {
			t.Helper()
			t.Errorf("%s: the callback must be nil, a func(job any) bool, a func(job any, queue string) bool or a func(job any, queue string, data any) bool; got %T.", name, callback)
		}
		return nil, false
	}
}

// chainOf returns the chain behind a job that satisfies [Chained], and nil for
// a job that does not.
func chainOf(job any) []any {
	chained, ok := job.(Chained)
	if !ok {
		return nil
	}
	return chained.Chained()
}

// matchesChain compares a chain element by element:
// a class token or a name matches by class, a truth test is asked, and anything
// else is compared by value.
func matchesChain(chain []any, expected []any) bool {
	if len(chain) != len(expected) {
		return false
	}
	for i, want := range expected {
		got := chain[i]
		switch tk := want.(type) {
		case reflect.Type, string:
			if !sameClass(got, tk) {
				return false
			}
		case func(job any) bool:
			if tk == nil || !tk(got) {
				return false
			}
		case *ChainedBatchTruthTest:
			batch, ok := got.(ChainedBatch)
			if !ok || !tk.Invoke(batch.ToPendingBatch()) {
				return false
			}
		default:
			if !reflect.DeepEqual(restore(got), restore(want)) {
				return false
			}
		}
	}
	return true
}

// describeChain renders a chain for a failure message.
func describeChain(chain []any) string {
	if len(chain) == 0 {
		return "[]"
	}
	names := make([]string, 0, len(chain))
	for _, job := range chain {
		names = append(names, tokenName(job))
	}
	return "[" + strings.Join(names, " -> ") + "]"
}

// describePushed renders the records a failure message ends with.
func describePushed(records []PushedJob) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		line := "[" + className(record.Job) + "]"
		if record.Queue != "" {
			line += " on the [" + record.Queue + "] queue"
		} else {
			line += " on the default queue"
		}
		if chain := chainOf(record.Job); len(chain) > 0 {
			line += " chained " + describeChain(chain)
		}
		lines = append(lines, line)
	}
	return lines
}
