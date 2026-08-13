package promises

import (
	"errors"
	"sync"
)

// State is the three states of GuzzleHttp\Promise\PromiseInterface.
type State string

const (
	// StatePending is PromiseInterface::PENDING.
	StatePending State = "pending"
	// StateFulfilled is PromiseInterface::FULFILLED.
	StateFulfilled State = "fulfilled"
	// StateRejected is PromiseInterface::REJECTED.
	StateRejected State = "rejected"
)

// ErrCancelled is what a cancelled promise rejects with.
var ErrCancelled = errors.New("http/client/promises: promise cancelled")

// ErrAlreadyBuilt is LazyPromise::buildPromise's RuntimeException.
var ErrAlreadyBuilt = errors.New("http/client/promises: promise already built")

// ErrLazy is the LogicException LazyPromise throws from resolve, reject and
// cancel: a promise nobody has built yet has nothing to settle.
var ErrLazy = errors.New("http/client/promises: cannot settle a lazy promise")

// Promise is the slice of GuzzleHttp\Promise\PromiseInterface that
// Illuminate\Http\Client uses.
//
// The Guzzle interface exists because PHP has no way to wait on work that has
// not finished; Go has goroutines and channels, and this is the shape that
// wraps them for a caller who came from Laravel and expects then, otherwise
// and wait.
type Promise interface {
	// Then is PromiseInterface::then.
	Then(onFulfilled func(any) any, onRejected func(error) any) Promise
	// Otherwise is PromiseInterface::otherwise.
	Otherwise(onRejected func(error) any) Promise
	// Resolve is PromiseInterface::resolve.
	Resolve(value any) error
	// Reject is PromiseInterface::reject.
	Reject(reason error) error
	// Cancel is PromiseInterface::cancel.
	Cancel() error
	// Wait is PromiseInterface::wait.
	Wait(unwrap bool) (any, error)
	// GetState is PromiseInterface::getState.
	GetState() State
}

// Deferred is the GuzzleHttp\Promise\Promise the two decorators in this package
// wrap: a value that is not there yet, and the callbacks waiting on it.
//
// Guzzle needs a task queue because PHP cannot wait on anything; a Deferred is
// settled from whichever goroutine is doing the work, and Wait blocks on a
// channel until it is.
type Deferred struct {
	mu sync.Mutex

	state  State
	value  any
	reason error
	done   chan struct{}

	// wait is what Wait runs when nothing has settled the promise yet. It is
	// the Guzzle wait function: the work whose result the promise stands for.
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

// GetState is PromiseInterface::getState.
func (d *Deferred) GetState() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

// Resolve is PromiseInterface::resolve: settle the promise with a value.
//
// The PHP throws when the promise is already settled with a different value;
// this returns that as an error, because a settled promise is a race the caller
// wants told about rather than swallowed.
func (d *Deferred) Resolve(value any) error {
	return d.settle(StateFulfilled, value, nil)
}

// Reject is PromiseInterface::reject: settle the promise with a reason.
func (d *Deferred) Reject(reason error) error {
	return d.settle(StateRejected, nil, reason)
}

// Cancel is PromiseInterface::cancel: reject a pending promise with
// [ErrCancelled], and do nothing to one that has already settled.
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

// Then is PromiseInterface::then.
//
// Guzzle hands back a new promise for the callback's return value; this hands
// back the same promise, because a Go callback that wants to chain has the
// value in hand and does not need a second object to carry it. Either callback
// may be nil, as in the PHP.
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

// Otherwise is PromiseInterface::otherwise: Then with only the rejection half.
func (d *Deferred) Otherwise(onRejected func(error) any) Promise {
	return d.Then(nil, onRejected)
}

// Wait is PromiseInterface::wait: block until the promise settles, and hand
// back what it settled with.
//
// unwrap is the PHP's $unwrap: false asks for the state rather than the value,
// so a rejected promise comes back with a nil error and the caller reads
// [Deferred.GetState].
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
