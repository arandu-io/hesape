package fakes

import "time"

// PendingChainFake is a chain of jobs waiting to be dispatched onto a
// [BusFake].
//
// A first job that satisfies [Chainable] is told the connection, the queue,
// the rest of the chain and the delay; one that does not is dispatched as it
// stands.
type PendingChainFake struct {
	bus *BusFake

	// Job is the first job of the chain.
	Job any
	// Chain is the jobs queued behind the first.
	Chain []any
	// Connection is the connection the chain runs on, and may be empty.
	Connection string
	// Queue is the queue the chain runs on, and may be empty.
	Queue string
	// Delay is how long the chain waits before running.
	Delay time.Duration
}

// NewPendingChainFake builds a pending chain over a first job and the jobs
// behind it, bound to the bus that will dispatch it.
func NewPendingChainFake(bus *BusFake, job any, chain []any) *PendingChainFake {
	return &PendingChainFake{bus: bus, Job: job, Chain: chain}
}

// Dispatch dispatches the chain on the bus and returns what the bus returned.
// A nil first job dispatches nothing.
//
// A func() first job is wrapped in a [CallQueuedClosure] first.
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
