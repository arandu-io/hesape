package promises

import (
	"errors"
	"sync"
)

// State is the state a [Promise] can be in.
type State string

const (
	// StatePending is the state before a promise settles.
	StatePending State = "pending"
	// StateFulfilled is the state after Resolve settles a promise.
	StateFulfilled State = "fulfilled"
	// StateRejected is the state after Reject settles a promise.
	StateRejected State = "rejected"
)

// ErrCancelled is what a cancelled promise rejects with.
var ErrCancelled = errors.New("http/client/promises: promise cancelled")

// ErrAlreadyBuilt is what [LazyPromise.BuildPromise] returns when the
// promise was already built.
var ErrAlreadyBuilt = errors.New("http/client/promises: promise already built")

// ErrLazy is what LazyPromise returns from Resolve, Reject and Cancel: a
// promise nobody has built yet has nothing to settle.
var ErrLazy = errors.New("http/client/promises: cannot settle a lazy promise")

// Promise is a value that is not there yet, with Then, Otherwise, Resolve,
// Reject, Cancel, Wait and GetState to observe or settle it. It is a
// promise-style API over the goroutines and channels doing the actual work.
type Promise interface {
	// Then registers callbacks for fulfillment and rejection, and returns a
	// Promise for chaining.
	Then(onFulfilled func(any) any, onRejected func(error) any) Promise
	// Otherwise is Then with only the rejection callback.
	Otherwise(onRejected func(error) any) Promise
	// Resolve settles the promise with a value.
	Resolve(value any) error
	// Reject settles the promise with an error.
	Reject(reason error) error
	// Cancel rejects a pending promise; a settled one is unaffected.
	Cancel() error
	// Wait blocks until the promise settles and returns what it settled
	// with.
	Wait(unwrap bool) (any, error)
	// GetState is the promise's current State.
	GetState() State
}

// Deferred is the concrete [Promise] the two decorators in this package
// wrap: a value that is not there yet, and the callbacks waiting on it.
//
// It is settled from whichever goroutine is doing the work, and Wait
// blocks on a channel until it is.
type Deferred struct {
	mu sync.Mutex

	state  State
	value  any
	reason error
	done   chan struct{}

	// wait is what Wait runs when nothing has settled the promise yet: the
	// work whose result the promise stands for.
	wait func()
	// waitOnce keeps that work from running twice when two goroutines wait.
	waitOnce sync.Once

	onFulfilled []func(any) any
	onRejected  []func(error) any
}

// NewDeferred creates a pending promise.
//
// waitFn is the work the promise stands for, run by [Deferred.Wait] when
// nothing has settled the promise yet. It may be nil, in which case Wait blocks
// until another goroutine settles it.
func NewDeferred(waitFn func()) *Deferred {
	return &Deferred{
		state: StatePending,
		done:  make(chan struct{}),
		wait:  waitFn,
	}
}

// GetState is the promise's current State.
func (d *Deferred) GetState() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

// Resolve settles the promise with a value.
//
// Returns an error when the promise is already settled, because a settled
// promise is a race the caller wants told about rather than swallowed.
func (d *Deferred) Resolve(value any) error {
	return d.settle(StateFulfilled, value, nil)
}

// Reject settles the promise with a reason.
func (d *Deferred) Reject(reason error) error {
	return d.settle(StateRejected, nil, reason)
}

// Cancel rejects a pending promise with [ErrCancelled], and does nothing to
// one that has already settled.
func (d *Deferred) Cancel() error {
	d.mu.Lock()
	settled := d.state != StatePending
	d.mu.Unlock()
	if settled {
		return nil
	}
	return d.Reject(ErrCancelled)
}

func (d *Deferred) settle(state State, value any, reason error) error {
	d.mu.Lock()
	if d.state != StatePending {
		current := d.state
		d.mu.Unlock()
		return errors.New("http/client/promises: promise is already " + string(current))
	}
	d.state, d.value, d.reason = state, value, reason
	onFulfilled, onRejected := d.onFulfilled, d.onRejected
	d.onFulfilled, d.onRejected = nil, nil
	close(d.done)
	d.mu.Unlock()

	if state == StateFulfilled {
		for _, callback := range onFulfilled {
			callback(value)
		}
		return nil
	}
	for _, callback := range onRejected {
		callback(reason)
	}
	return nil
}

// Then registers callbacks for fulfillment and rejection.
//
// It hands back the same promise rather than a new one, because a Go
// callback that wants to chain has the value in hand and does not need a
// second object to carry it. Either callback may be nil.
func (d *Deferred) Then(onFulfilled func(any) any, onRejected func(error) any) Promise {
	d.mu.Lock()
	switch d.state {
	case StatePending:
		if onFulfilled != nil {
			d.onFulfilled = append(d.onFulfilled, onFulfilled)
		}
		if onRejected != nil {
			d.onRejected = append(d.onRejected, onRejected)
		}
		d.mu.Unlock()
		return d
	case StateFulfilled:
		value := d.value
		d.mu.Unlock()
		if onFulfilled != nil {
			onFulfilled(value)
		}
		return d
	default:
		reason := d.reason
		d.mu.Unlock()
		if onRejected != nil {
			onRejected(reason)
		}
		return d
	}
}

// Otherwise is Then with only the rejection half.
func (d *Deferred) Otherwise(onRejected func(error) any) Promise {
	return d.Then(nil, onRejected)
}

// Wait blocks until the promise settles, and hands back what it settled
// with.
//
// unwrap: false asks for the value without the error, so a rejected
// promise comes back with a nil error and the caller reads
// [Deferred.GetState] to learn it was rejected.
func (d *Deferred) Wait(unwrap bool) (any, error) {
	d.mu.Lock()
	waitFn := d.wait
	settled := d.state != StatePending
	d.mu.Unlock()

	if !settled && waitFn != nil {
		d.waitOnce.Do(waitFn)
	}
	<-d.done

	d.mu.Lock()
	defer d.mu.Unlock()
	if !unwrap {
		return d.value, nil
	}
	return d.value, d.reason
}
