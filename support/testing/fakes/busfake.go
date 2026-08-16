package fakes

import (
	"reflect"
	"sync"
)

// QueueingDispatcher is what a [BusFake] needs from the dispatcher it stands
// in for.
//
// A fake only reaches for it when [BusFake.Except] named a job that must be
// dispatched for real, or when a caller asks it about a handler; a fake built
// with a nil dispatcher and no Except never touches it.
type QueueingDispatcher interface {
	// Dispatch sends the command wherever it belongs, queue included.
	Dispatch(command any) any
	// DispatchSync runs the command in the current process on purpose.
	DispatchSync(command any, handler any) any
	// DispatchNow runs the command in the current process.
	DispatchNow(command any, handler any) any
	// DispatchToQueue puts the command on the queue.
	DispatchToQueue(command any) any
	// PipeThrough sets the pipes every command passes through.
	PipeThrough(pipes []any)
	// HasCommandHandler reports whether a handler is registered for the
	// command.
	HasCommandHandler(command any) bool
	// GetCommandHandler returns the handler registered for the command.
	GetCommandHandler(command any) any
	// Map registers handlers by name, keyed by the command they handle.
	Map(commands map[string]string)
}

// ChainedBatch is what a chain assertion asks of a chained job: the batch it
// stands for, so a truth test can be run against it.
type ChainedBatch interface {
	// ToPendingBatch returns the batch the chained job stands for.
	ToPendingBatch() *PendingBatchFake
}

// ChainedBatchTruthTest is a truth test about a batch sitting inside a chain,
// which [BusFake.ChainedBatch] makes and [BusFake.AssertChained] recognizes
// among the links of a chain.
type ChainedBatchTruthTest struct {
	callback func(batch *PendingBatchFake) bool
}

// NewChainedBatchTruthTest wraps a callback as a truth test about a chained
// batch.
func NewChainedBatchTruthTest(callback func(batch *PendingBatchFake) bool) *ChainedBatchTruthTest {
	return &ChainedBatchTruthTest{callback: callback}
}

// Invoke runs the truth test against the batch. A nil test accepts every
// batch.
func (c *ChainedBatchTruthTest) Invoke(batch *PendingBatchFake) bool {
	if c == nil || c.callback == nil {
		return true
	}
	return c.callback(batch)
}

// BusFake is the command bus a
// test installs so that no job is handled, and every dispatch, chain and batch
// can be asserted on afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while the
// lock is held.
type BusFake struct {
	mu sync.Mutex
	// dispatcher is the real dispatcher the fake stands in for.
	dispatcher            QueueingDispatcher
	jobsToFake            []any
	jobsToDispatch        []any
	batchRepository       *BatchRepositoryFake
	commands              []any
	commandsSync          []any
	commandsAfterResponse []any
	batches               []*PendingBatchFake
	serializeAndRestore   bool
}

// NewBusFake builds a bus that records the named job types and forwards the
// rest.
//
// A nil dispatcher is the ordinary case.
func NewBusFake(dispatcher QueueingDispatcher, jobsToFake ...any) *BusFake {
	return &BusFake{
		dispatcher:      dispatcher,
		jobsToFake:      jobsToFake,
		batchRepository: NewBatchRepositoryFake(),
	}
}

func (f *BusFake) isFake() {}

// OriginalDispatcher returns the dispatcher the fake
// stands in for.
func (f *BusFake) OriginalDispatcher() QueueingDispatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dispatcher
}

// Except names the jobs that should reach the real
// dispatcher instead of being recorded.
func (f *BusFake) Except(jobs ...any) *BusFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobsToDispatch = append(f.jobsToDispatch, jobs...)
	return f
}

// AssertDispatched fails the test unless a job of the given type was
// dispatched, in any of the three ways -- to the queue, synchronously, or
// after the response -- and the callback accepted it.
//
// The callback slot accepts nil (any job of the type), an int (an exact
// count), or a func(job any) bool. Any other form fails the test naming the
// ones that are accepted.
func (f *BusFake) AssertDispatched(t TestingT, command any, callback any) {
	t.Helper()

	if times, ok := callback.(int); ok {
		f.AssertDispatchedTimes(t, command, times)
		return
	}

	test, ok := jobTest(t, "AssertDispatched", callback)
	if !ok {
		return
	}

	if len(f.matching(f.snapshotCommands(), command, test)) > 0 ||
		len(f.matching(f.snapshotAfterResponse(), command, test)) > 0 ||
		len(f.matching(f.snapshotSync(), command, test)) > 0 {
		return
	}

	t.Errorf(
		"AssertDispatched: the expected [%s] job was not dispatched.%s",
		tokenName(command), f.everything(),
	)
}

// AssertDispatchedOnce fails the test unless the job was dispatched exactly
// once.
func (f *BusFake) AssertDispatchedOnce(t TestingT, command any) {
	t.Helper()
	f.AssertDispatchedTimes(t, command, 1)
}

// AssertDispatchedTimes fails the test unless the job was dispatched exactly
// that many times, counting the three ways of dispatching together.
func (f *BusFake) AssertDispatchedTimes(t TestingT, command any, times int) {
	t.Helper()

	count := len(f.matching(f.snapshotCommands(), command, nil)) +
		len(f.matching(f.snapshotAfterResponse(), command, nil)) +
		len(f.matching(f.snapshotSync(), command, nil))
	if count == times {
		return
	}

	t.Errorf(
		"AssertDispatchedTimes: the expected [%s] job was dispatched %d %s instead of %d %s.%s",
		tokenName(command), count, plural("time", count), times, plural("time", times), f.everything(),
	)
}

// AssertNotDispatched fails the test when the job was dispatched in any of the
// three ways.
func (f *BusFake) AssertNotDispatched(t TestingT, command any, callback any) {
	t.Helper()

	test, ok := jobTest(t, "AssertNotDispatched", callback)
	if !ok {
		return
	}

	found := append(append(
		f.matching(f.snapshotCommands(), command, test),
		f.matching(f.snapshotSync(), command, test)...),
		f.matching(f.snapshotAfterResponse(), command, test)...)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotDispatched: the unexpected [%s] job was dispatched %d %s:%s",
		tokenName(command), len(found), plural("time", len(found)), listOf(classNames(found)),
	)
}

// AssertNothingDispatched fails the test unless nothing was dispatched to the
// queue. It reads only that ledger: a job dispatched synchronously or after
// the response is not one of them.
func (f *BusFake) AssertNothingDispatched(t TestingT) {
	t.Helper()

	commands := f.snapshotCommands()
	if len(commands) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingDispatched: the following %s dispatched unexpectedly:%s",
		plural("job was", len(commands)), listOf(classCounts(commands, "dispatched")),
	)
}

// AssertDispatchedSync fails the test unless the job was dispatched in the
// current process on purpose. The callback slot takes the same forms as
// [BusFake.AssertDispatched].
func (f *BusFake) AssertDispatchedSync(t TestingT, command any, callback any) {
	t.Helper()

	if times, ok := callback.(int); ok {
		f.AssertDispatchedSyncTimes(t, command, times)
		return
	}

	test, ok := jobTest(t, "AssertDispatchedSync", callback)
	if !ok {
		return
	}

	if len(f.matching(f.snapshotSync(), command, test)) > 0 {
		return
	}

	t.Errorf(
		"AssertDispatchedSync: the expected [%s] job was not dispatched synchronously.%s",
		tokenName(command), f.everything(),
	)
}

// AssertDispatchedSyncTimes fails the test unless the job was dispatched
// synchronously exactly that many times.
func (f *BusFake) AssertDispatchedSyncTimes(t TestingT, command any, times int) {
	t.Helper()

	sync := f.snapshotSync()
	count := len(f.matching(sync, command, nil))
	if count == times {
		return
	}

	t.Errorf(
		"AssertDispatchedSyncTimes: the expected [%s] job was synchronously dispatched %d %s instead of %d %s. %s dispatched synchronously:%s",
		tokenName(command), count, plural("time", count), times, plural("time", times),
		countedWere(len(sync), "job"), listOf(classNames(sync)),
	)
}

// AssertNotDispatchedSync fails the test when the job was dispatched
// synchronously and the callback accepted it.
func (f *BusFake) AssertNotDispatchedSync(t TestingT, command any, callback any) {
	t.Helper()

	test, ok := jobTest(t, "AssertNotDispatchedSync", callback)
	if !ok {
		return
	}

	found := f.matching(f.snapshotSync(), command, test)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotDispatchedSync: the unexpected [%s] job was dispatched synchronously %d %s:%s",
		tokenName(command), len(found), plural("time", len(found)), listOf(classNames(found)),
	)
}

// AssertDispatchedAfterResponse fails the test unless the job was dispatched
// after the response was sent.
func (f *BusFake) AssertDispatchedAfterResponse(t TestingT, command any, callback any) {
	t.Helper()

	if times, ok := callback.(int); ok {
		f.AssertDispatchedAfterResponseTimes(t, command, times)
		return
	}

	test, ok := jobTest(t, "AssertDispatchedAfterResponse", callback)
	if !ok {
		return
	}

	if len(f.matching(f.snapshotAfterResponse(), command, test)) > 0 {
		return
	}

	t.Errorf(
		"AssertDispatchedAfterResponse: the expected [%s] job was not dispatched after sending the response.%s",
		tokenName(command), f.everything(),
	)
}

// AssertDispatchedAfterResponseTimes fails the test unless the job was
// dispatched after the response exactly that many times.
func (f *BusFake) AssertDispatchedAfterResponseTimes(t TestingT, command any, times int) {
	t.Helper()

	after := f.snapshotAfterResponse()
	count := len(f.matching(after, command, nil))
	if count == times {
		return
	}

	t.Errorf(
		"AssertDispatchedAfterResponseTimes: the expected [%s] job was dispatched after the response %d %s instead of %d %s. %s dispatched after the response:%s",
		tokenName(command), count, plural("time", count), times, plural("time", times),
		countedWere(len(after), "job"), listOf(classNames(after)),
	)
}

// AssertNotDispatchedAfterResponse fails the test when the job was dispatched
// after the response and the callback accepted it.
func (f *BusFake) AssertNotDispatchedAfterResponse(t TestingT, command any, callback any) {
	t.Helper()

	test, ok := jobTest(t, "AssertNotDispatchedAfterResponse", callback)
	if !ok {
		return
	}

	found := f.matching(f.snapshotAfterResponse(), command, test)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotDispatchedAfterResponse: the unexpected [%s] job was dispatched after sending the response %d %s:%s",
		tokenName(command), len(found), plural("time", len(found)), listOf(classNames(found)),
	)
}

// AssertChained fails the test unless the first element of the chain was
// dispatched with the rest of it chained behind.
//
// Each element may be a class token, a value, a func(job any) bool truth test,
// or a ChainedBatchTruthTest. An empty chain asserts that the first job went
// out with nothing behind it, which is what AssertDispatchedWithoutChain says
// in one word.
//
// Put a class token first when the class matters.
func (f *BusFake) AssertChained(t TestingT, expectedChain []any) {
	t.Helper()

	if len(expectedChain) == 0 {
		t.Errorf("AssertChained: the expected chain can not be empty. Its first element is the job that was dispatched.")
		return
	}

	command := expectedChain[0]
	rest := expectedChain[1:]

	token := command
	var test func(any) bool
	switch tk := command.(type) {
	case reflect.Type, string:
		// The token names the class, and nothing more is asked of the job.
	case func(job any) bool:
		// A parameter type cannot be read at run time, so a truth test in the
		// first slot stands on its own: it is asked about every job
		// dispatched, whatever its class, and the one it accepts is the one
		// that led the chain.
		token, test = nil, tk
	case *ChainedBatchTruthTest:
		token = nil
		test = func(job any) bool {
			batch, ok := job.(ChainedBatch)
			return ok && tk.Invoke(batch.ToPendingBatch())
		}
	default:
		// A value stands for itself: the job dispatched has to equal it.
		want := command
		test = func(job any) bool { return reflect.DeepEqual(restore(job), restore(want)) }
	}

	f.assertChainOf(t, "AssertChained", token, rest, test)
}

// AssertDispatchedWithoutChain fails the test unless
// the job was dispatched, and nothing is chained behind it.
func (f *BusFake) AssertDispatchedWithoutChain(t TestingT, command any, callback any) {
	t.Helper()

	test, ok := jobTest(t, "AssertDispatchedWithoutChain", callback)
	if !ok {
		return
	}

	f.assertChainOf(t, "AssertDispatchedWithoutChain", command, nil, test)
}

// assertChainOf is the body AssertChained and AssertDispatchedWithoutChain
// share. A nil command matches any class, which is how a truth test with no
// class beside it is asked about every job dispatched.
func (f *BusFake) assertChainOf(t TestingT, name string, command any, expectedChain []any, test func(any) bool) {
	t.Helper()

	label := "any"
	if command != nil {
		label = tokenName(command)
	}

	var found []any
	for _, job := range f.snapshotCommands() {
		if command != nil && !sameClass(job, command) {
			continue
		}
		if !callFn(test, job) {
			continue
		}
		found = append(found, job)
	}

	if len(found) == 0 {
		t.Errorf(
			"%s: the expected [%s] job was not dispatched.%s",
			name, label, f.everything(),
		)
		return
	}

	actual := make([]string, 0, len(found))
	for _, job := range found {
		chain := chainOf(job)
		if matchesChain(chain, expectedChain) {
			return
		}
		actual = append(actual, "["+className(job)+"] chained "+describeChain(chain))
	}

	t.Errorf(
		"%s: the expected chain %s was not dispatched behind [%s]. %s dispatched:%s",
		name, describeChain(expectedChain), label,
		countedWere(len(found), "job"), listOf(actual),
	)
}

// AssertNothingChained is [BusFake.AssertNothingDispatched]: a chain is
// recorded on the same ledger.
func (f *BusFake) AssertNothingChained(t TestingT) {
	t.Helper()
	f.AssertNothingDispatched(t)
}

// ChainedBatch builds a truth test about a batch sitting inside a chain, to be
// put where a chained job would go in [BusFake.AssertChained].
func (f *BusFake) ChainedBatch(callback func(batch *PendingBatchFake) bool) *ChainedBatchTruthTest {
	return NewChainedBatchTruthTest(callback)
}

// AssertBatched fails the test unless a batch the truth test accepted was
// dispatched.
func (f *BusFake) AssertBatched(t TestingT, callback func(batch *PendingBatchFake) bool) {
	t.Helper()

	if len(f.Batched(callback)) > 0 {
		return
	}

	batches := f.DispatchedBatches()
	t.Errorf(
		"AssertBatched: the expected batch was not dispatched. %s dispatched:%s",
		countedWere(len(batches), "batch"), listOf(describeBatches(batches)),
	)
}

// AssertBatchCount fails the test unless exactly that many batches were
// dispatched.
func (f *BusFake) AssertBatchCount(t TestingT, count int) {
	t.Helper()

	batches := f.DispatchedBatches()
	if len(batches) == count {
		return
	}

	t.Errorf(
		"AssertBatchCount: the number of batches dispatched was %d instead of %d:%s",
		len(batches), count, listOf(describeBatches(batches)),
	)
}

// AssertNothingBatched fails the test unless no batch was dispatched at all.
func (f *BusFake) AssertNothingBatched(t TestingT) {
	t.Helper()

	batches := f.DispatchedBatches()
	if len(batches) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingBatched: the following batched %s dispatched unexpectedly:%s",
		plural("job was", len(batches)), listOf(describeBatches(batches)),
	)
}

// AssertNothingPlaced fails the test unless nothing was dispatched, chained or
// batched.
func (f *BusFake) AssertNothingPlaced(t TestingT) {
	t.Helper()
	f.AssertNothingDispatched(t)
	f.AssertNothingBatched(t)
}

// Dispatched returns the jobs of the given type that the
// truth test accepted, in the order they were dispatched to the queue.
func (f *BusFake) Dispatched(command any, callback any) []any {
	test, ok := jobTest(nil, "Dispatched", callback)
	if !ok {
		return nil
	}
	return f.matching(f.snapshotCommands(), command, test)
}

// DispatchedSync returns the jobs of the given type dispatched synchronously
// that the truth test accepted, in order.
func (f *BusFake) DispatchedSync(command any, callback any) []any {
	test, ok := jobTest(nil, "DispatchedSync", callback)
	if !ok {
		return nil
	}
	return f.matching(f.snapshotSync(), command, test)
}

// DispatchedAfterResponse returns the jobs of the given type dispatched after
// the response that the truth test accepted, in order.
func (f *BusFake) DispatchedAfterResponse(command any, callback any) []any {
	test, ok := jobTest(nil, "DispatchedAfterResponse", callback)
	if !ok {
		return nil
	}
	return f.matching(f.snapshotAfterResponse(), command, test)
}

// Batched returns the batches the truth test accepted, in the order they were
// dispatched.
func (f *BusFake) Batched(callback func(batch *PendingBatchFake) bool) []*PendingBatchFake {
	batches := f.DispatchedBatches()
	if len(batches) == 0 {
		return nil
	}

	var found []*PendingBatchFake
	for _, batch := range batches {
		if callback != nil && !callback(batch) {
			continue
		}
		found = append(found, batch)
	}
	return found
}

// HasDispatched reports whether a job of the type was dispatched to the queue
// at all.
func (f *BusFake) HasDispatched(command any) bool {
	return len(f.matching(f.snapshotCommands(), command, nil)) > 0
}

// HasDispatchedSync reports whether a job of the type was dispatched
// synchronously at all.
func (f *BusFake) HasDispatchedSync(command any) bool {
	return len(f.matching(f.snapshotSync(), command, nil)) > 0
}

// HasDispatchedAfterResponse reports whether a job of the type was dispatched
// after the response at all.
func (f *BusFake) HasDispatchedAfterResponse(command any) bool {
	return len(f.matching(f.snapshotAfterResponse(), command, nil)) > 0
}

// Dispatch records the job, or hands it to the
// real dispatcher when Except named it.
func (f *BusFake) Dispatch(command any) any {
	if !f.shouldFakeJob(command) {
		if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
			return dispatcher.Dispatch(command)
		}
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, f.representationLocked(command))
	return nil
}

// DispatchSync records the job on the synchronous ledger, or hands it to the
// real dispatcher when the fake was not told to intercept it.
func (f *BusFake) DispatchSync(command any, handler any) any {
	if !f.shouldFakeJob(command) {
		if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
			return dispatcher.DispatchSync(command, handler)
		}
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.commandsSync = append(f.commandsSync, f.representationLocked(command))
	return nil
}

// DispatchNow files the job on the same ledger as [BusFake.DispatchSync].
func (f *BusFake) DispatchNow(command any, handler any) any {
	if !f.shouldFakeJob(command) {
		if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
			return dispatcher.DispatchNow(command, handler)
		}
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, f.representationLocked(command))
	return nil
}

// DispatchToQueue records the job on the queue ledger, or hands it to the real
// dispatcher when the fake was not told to intercept it.
func (f *BusFake) DispatchToQueue(command any) any {
	if !f.shouldFakeJob(command) {
		if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
			return dispatcher.DispatchToQueue(command)
		}
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, f.representationLocked(command))
	return nil
}

// DispatchAfterResponse records the job on the after-response ledger.
func (f *BusFake) DispatchAfterResponse(command any) any {
	if !f.shouldFakeJob(command) {
		if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
			return dispatcher.Dispatch(command)
		}
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.commandsAfterResponse = append(f.commandsAfterResponse, f.representationLocked(command))
	return nil
}

// Chain begins a chain of jobs that records instead of going out when it is
// dispatched.
func (f *BusFake) Chain(jobs []any) *PendingChainFake {
	if len(jobs) == 0 {
		return NewPendingChainFake(f, nil, nil)
	}
	return NewPendingChainFake(f, jobs[0], append([]any(nil), jobs[1:]...))
}

// FindBatch returns the batch with that id from the fake repository, or nil
// when there is none.
func (f *BusFake) FindBatch(batchID string) *BatchFake {
	f.mu.Lock()
	repository := f.batchRepository
	f.mu.Unlock()
	return repository.Find(batchID)
}

// Batch begins a batch of jobs that records instead of being stored when it is
// dispatched.
func (f *BusFake) Batch(jobs []any) *PendingBatchFake {
	return NewPendingBatchFake(f, append([]any(nil), jobs...))
}

// DispatchFakeBatch dispatches an empty batch under the given name, so a test
// that needs a batch to hand to something has one.
func (f *BusFake) DispatchFakeBatch(name string) *BatchFake {
	batch := f.Batch(nil)
	batch.Name = name
	return batch.Dispatch()
}

// RecordPendingBatch remembers the batch for [BusFake.AssertBatched] and
// stores it in the fake repository, which is what makes the [BatchFake] the
// caller gets.
func (f *BusFake) RecordPendingBatch(batch *PendingBatchFake) *BatchFake {
	f.mu.Lock()
	f.batches = append(f.batches, batch)
	repository := f.batchRepository
	f.mu.Unlock()

	return repository.Store(batch)
}

// shouldFakeJob reports whether the job is recorded rather than forwarded.
//
// A fake built without a list of jobs to fake fakes everything. A token may be
// a class, or a func(job any) bool, which is the Closure form.
func (f *BusFake) shouldFakeJob(command any) bool {
	f.mu.Lock()
	toFake := append([]any(nil), f.jobsToFake...)
	toDispatch := append([]any(nil), f.jobsToDispatch...)
	f.mu.Unlock()

	if matchesJobToken(toDispatch, command) {
		return false
	}
	if len(toFake) == 0 {
		return true
	}
	return matchesJobToken(toFake, command)
}

func matchesJobToken(tokens []any, command any) bool {
	for _, token := range tokens {
		if test, ok := token.(func(job any) bool); ok {
			if test != nil && test(command) {
				return true
			}
			continue
		}
		if sameClass(command, token) {
			return true
		}
	}
	return false
}

// SerializeAndRestore says whether a job is
// put through the round trip the queue would put it through before it is
// recorded.
func (f *BusFake) SerializeAndRestore(serializeAndRestore bool) *BusFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serializeAndRestore = serializeAndRestore
	return f
}

// representationLocked returns the form a job is recorded in, put through the
// queue's round trip when [BusFake.SerializeAndRestore] asked for that.
//
// The caller holds the lock.
func (f *BusFake) representationLocked(command any) any {
	if f.serializeAndRestore {
		return restore(command)
	}
	return command
}

// PipeThrough forwards to the real dispatcher and returns the fake. A fake
// with no dispatcher drops the call.
func (f *BusFake) PipeThrough(pipes []any) *BusFake {
	if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
		dispatcher.PipeThrough(pipes)
	}
	return f
}

// HasCommandHandler forwards to the real dispatcher, and returns false when
// there is none.
func (f *BusFake) HasCommandHandler(command any) bool {
	dispatcher := f.OriginalDispatcher()
	if dispatcher == nil {
		return false
	}
	return dispatcher.HasCommandHandler(command)
}

// GetCommandHandler forwards to the real dispatcher, and returns nil when
// there is none.
func (f *BusFake) GetCommandHandler(command any) any {
	dispatcher := f.OriginalDispatcher()
	if dispatcher == nil {
		return nil
	}
	return dispatcher.GetCommandHandler(command)
}

// Map forwards to the real dispatcher and returns the fake: handler names
// keyed by the command class
// name.
func (f *BusFake) Map(commands map[string]string) *BusFake {
	if dispatcher := f.OriginalDispatcher(); dispatcher != nil {
		dispatcher.Map(commands)
	}
	return f
}

// DispatchedBatches returns a copy of every batch dispatched, in order.
func (f *BusFake) DispatchedBatches() []*PendingBatchFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*PendingBatchFake(nil), f.batches...)
}

func (f *BusFake) snapshotCommands() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.commands...)
}

func (f *BusFake) snapshotSync() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.commandsSync...)
}

func (f *BusFake) snapshotAfterResponse() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.commandsAfterResponse...)
}

// matching returns the jobs filed under that exact class and accepted by the
// truth test.
func (f *BusFake) matching(records []any, command any, test func(any) bool) []any {
	var found []any
	for _, job := range records {
		if !sameClass(job, command) {
			continue
		}
		if !callFn(test, job) {
			continue
		}
		found = append(found, job)
	}
	return found
}

// everything renders what was dispatched, in all three ways, for a failure
// message. Naming only the job that was missing would leave the reader to
// guess whether it went out synchronously, went out after the response, or
// never went out at all.
func (f *BusFake) everything() string {
	commands := f.snapshotCommands()
	sync := f.snapshotSync()
	after := f.snapshotAfterResponse()

	if len(commands)+len(sync)+len(after) == 0 {
		return " Nothing was dispatched."
	}

	lines := make([]string, 0, len(commands)+len(sync)+len(after))
	for _, name := range classNames(commands) {
		lines = append(lines, name+" dispatched to the queue")
	}
	for _, name := range classNames(sync) {
		lines = append(lines, name+" dispatched synchronously")
	}
	for _, name := range classNames(after) {
		lines = append(lines, name+" dispatched after the response")
	}
	return " These were dispatched:" + listOf(lines)
}

// jobTest normalizes the truth test forms the bus assertions accept. It reports
// the failure itself when handed a form it does not know, and answers false so
// the caller stops.
func jobTest(t TestingT, name string, callback any) (func(any) bool, bool) {
	switch cb := callback.(type) {
	case nil:
		return nil, true
	case func(job any) bool:
		if cb == nil {
			return nil, true
		}
		return cb, true
	default:
		if t != nil {
			t.Helper()
			t.Errorf("%s: the callback must be nil, a func(job any) bool or an int; got %T.", name, callback)
		}
		return nil, false
	}
}

// describeBatches renders the batches a failure message ends with: the name of
// each and the classes in it, because a batch has no class of its own to print.
func describeBatches(batches []*PendingBatchFake) []string {
	lines := make([]string, 0, len(batches))
	for _, batch := range batches {
		line := "["
		if batch.Name != "" {
			line += batch.Name
		} else {
			line += "unnamed batch"
		}
		line += "] of " + countedAs(len(batch.Jobs), "job")
		if names := classNames(batch.Jobs); len(names) > 0 {
			line += ": " + joinNames(names)
		}
		lines = append(lines, line)
	}
	return lines
}
