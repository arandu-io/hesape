package fakes

import "time"

// PendingChainFake answers
// Illuminate\Support\Testing\Fakes\PendingChainFake.
//
// It is what Bus::chain(jobs) hands back while the bus is faked: the same chain
// the real PendingChain builds, whose Dispatch writes the rest of the chain
// onto the first job and hands that job to the fake bus, so that
// AssertChained has a chain to read.
//
// The connection, queue and delay are the ones the real PendingChain carries;
// a first job that answers Chainable is told about all three, and one that does
// not is dispatched as it stands -- which is what an unset property is in PHP.
type PendingChainFake struct {
	bus *BusFake

	// Job answers PendingChain::$job, the first job of the chain.
	Job any
	// Chain answers PendingChain::$chain, the jobs behind the first.
	Chain []any
	// Connection answers PendingChain::$connection.
	Connection string
	// Queue answers PendingChain::$queue.
	Queue string
	// Delay answers PendingChain::$delay.
	Delay time.Duration
}

// NewPendingChainFake answers PendingChainFake::__construct.
func NewPendingChainFake(bus *BusFake, job any, chain []any) *PendingChainFake {
	return &PendingChainFake{bus: bus, Job: job, Chain: chain}
}

// Dispatch answers PendingChainFake::dispatch.
//
// A func() first job becomes a CallQueuedClosure, which is what the PHP does
// with a Closure, and the chain, connection, queue and delay are written onto
// the job before it goes to the bus, exactly as they are there.
func (p *PendingChainFake) Dispatch() any {
	job := p.Job
	if closure, ok := job.(func()); ok {
		job = NewCallQueuedClosure(closure)
	}
	if job == nil {
		return nil
	}

	if chainable, ok := job.(Chainable); ok {
		chainable.AllOnConnection(p.Connection)
		chainable.AllOnQueue(p.Queue)
		chainable.Chain(p.Chain)
		chainable.Delay(p.Delay)
	}

	return p.bus.Dispatch(job)
}
