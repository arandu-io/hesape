package promises

import "sync"

// FluentPromise is Illuminate\Http\Client\Promises\FluentPromise: a promise
// that hands itself back from every call, so that a caller can chain.
//
// The PHP decorates a Guzzle promise and swaps the one it holds for whatever
// then() returned, because Guzzle's then() makes a new promise every time.
// [Deferred.Then] hands back the same promise, so there is nothing to swap --
// but the decorator stays, because the type is what a Laravel caller names.
type FluentPromise struct {
	mu      sync.Mutex
	promise Promise
}

// NewFluentPromise wraps a promise so that it chains.
func NewFluentPromise(promise Promise) *FluentPromise {
	return &FluentPromise{promise: promise}
}

// Then is FluentPromise::then.
func (f *FluentPromise) Then(onFulfilled func(any) any, onRejected func(error) any) Promise {
	f.mu.Lock()
	inner := f.promise
	f.mu.Unlock()

	result := inner.Then(onFulfilled, onRejected)

	f.mu.Lock()
	f.promise = result
	f.mu.Unlock()
	return f
}

// Otherwise is FluentPromise::otherwise.
func (f *FluentPromise) Otherwise(onRejected func(error) any) Promise {
	return f.Then(nil, onRejected)
}

// Resolve is FluentPromise::resolve.
func (f *FluentPromise) Resolve(value any) error { return f.inner().Resolve(value) }

// Reject is FluentPromise::reject.
func (f *FluentPromise) Reject(reason error) error { return f.inner().Reject(reason) }

// Cancel is FluentPromise::cancel.
func (f *FluentPromise) Cancel() error { return f.inner().Cancel() }

// Wait is FluentPromise::wait.
func (f *FluentPromise) Wait(unwrap bool) (any, error) { return f.inner().Wait(unwrap) }

// GetState is FluentPromise::getState.
func (f *FluentPromise) GetState() State { return f.inner().GetState() }

// GetUnderlyingPromise is FluentPromise::getGuzzlePromise: the promise this one
// decorates.
//
// It is not GetGuzzlePromise because there is no Guzzle here; the name in the
// PHP says which library the decorated object came from, and saying Guzzle in
// a package that has never heard of it would name a thing that is not there.
func (f *FluentPromise) GetUnderlyingPromise() Promise { return f.inner() }

func (f *FluentPromise) inner() Promise {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.promise
}
