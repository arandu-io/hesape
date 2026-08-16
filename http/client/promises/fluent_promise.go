package promises

import "sync"

// FluentPromise is a promise that hands itself back from every call, so
// that a caller can chain.
//
// It wraps any [Promise] and swaps the one it holds for whatever Then
// returns, so the caller always chains through this same wrapper even if
// the wrapped implementation's own Then would have handed back something
// else.
type FluentPromise struct {
	mu      sync.Mutex
	promise Promise
}

// NewFluentPromise wraps a promise so that it chains.
func NewFluentPromise(promise Promise) *FluentPromise {
	return &FluentPromise{promise: promise}
}

// Then registers callbacks on the wrapped promise, swaps in whatever it
// returns, and hands back f.
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

// Otherwise is Then with only the rejection callback.
func (f *FluentPromise) Otherwise(onRejected func(error) any) Promise {
	return f.Then(nil, onRejected)
}

// Resolve settles the wrapped promise with a value.
func (f *FluentPromise) Resolve(value any) error { return f.inner().Resolve(value) }

// Reject settles the wrapped promise with a reason.
func (f *FluentPromise) Reject(reason error) error { return f.inner().Reject(reason) }

// Cancel cancels the wrapped promise.
func (f *FluentPromise) Cancel() error { return f.inner().Cancel() }

// Wait waits on the wrapped promise.
func (f *FluentPromise) Wait(unwrap bool) (any, error) { return f.inner().Wait(unwrap) }

// GetState is the wrapped promise's current State.
func (f *FluentPromise) GetState() State { return f.inner().GetState() }

// GetUnderlyingPromise is the promise this one decorates.
func (f *FluentPromise) GetUnderlyingPromise() Promise { return f.inner() }

func (f *FluentPromise) inner() Promise {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.promise
}
