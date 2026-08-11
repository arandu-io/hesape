package fakes

// PendingBatchFake answers Illuminate\Support\Testing\Fakes\PendingBatchFake.
//
// It is what Bus::batch(jobs) hands back while the bus is faked: the batch is
// recorded when it is dispatched instead of being stored and worked, and it is
// the value a truth test given to AssertBatched is handed.
//
// The PHP extends PendingBatch and inherits the fluent setters -- name(),
// onQueue(), then(), catch() -- from it. Those belong to the bus package, and
// what the fake reads of them is the three properties below, which a test sets
// directly: a Go type cannot have both a field Name and a method Name, and the
// property is what BatchRepositoryFake::store reads.
type PendingBatchFake struct {
	bus *BusFake

	// Jobs answers PendingBatch::$jobs.
	Jobs []any
	// Name answers PendingBatch::$name.
	Name string
	// Options answers PendingBatch::$options.
	Options map[string]any
}

// NewPendingBatchFake answers PendingBatchFake::__construct.
func NewPendingBatchFake(bus *BusFake, jobs []any) *PendingBatchFake {
	return &PendingBatchFake{bus: bus, Jobs: jobs, Options: map[string]any{}}
}

// Dispatch answers PendingBatchFake::dispatch: the batch is recorded on the
// fake bus, and the batch the repository made for it is handed back.
func (p *PendingBatchFake) Dispatch() *BatchFake {
	return p.bus.RecordPendingBatch(p)
}

// DispatchAfterResponse answers PendingBatchFake::dispatchAfterResponse. It is
// Dispatch: with no response to wait for, there is no later.
func (p *PendingBatchFake) DispatchAfterResponse() *BatchFake {
	return p.bus.RecordPendingBatch(p)
}
