package fakes

// PendingBatchFake is a batch of jobs waiting to be dispatched onto a
// [BusFake].
//
// The three settings are plain fields a test writes to directly, rather than
// setter methods, because a type cannot carry both a field Name and a method
// Name.
type PendingBatchFake struct {
	bus *BusFake

	// Jobs are the jobs the batch will dispatch.
	Jobs []any
	// Name is what the batch is called, and may be empty.
	Name string
	// Options are the batch's settings, keyed by name.
	Options map[string]any
}

// NewPendingBatchFake builds a pending batch over the given jobs, bound to the
// bus that will record it.
func NewPendingBatchFake(bus *BusFake, jobs []any) *PendingBatchFake {
	return &PendingBatchFake{bus: bus, Jobs: jobs, Options: map[string]any{}}
}

// Dispatch records the batch on the bus and returns the [BatchFake] made for
// it.
func (p *PendingBatchFake) Dispatch() *BatchFake {
	return p.bus.RecordPendingBatch(p)
}

// DispatchAfterResponse is [PendingBatchFake.Dispatch]: with no response to
// wait for, there is no later.
func (p *PendingBatchFake) DispatchAfterResponse() *BatchFake {
	return p.bus.RecordPendingBatch(p)
}
