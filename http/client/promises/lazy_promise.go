package promises

import "sync"

// LazyPromise is Illuminate\Http\Client\Promises\LazyPromise: a promise whose
// work has not started, and does not start until somebody waits on it.
//
// Every then and otherwise registered before that is queued, and replayed onto
// the real promise the moment it is built. It is what a pool uses to describe
// a request it has not sent yet.
type LazyPromise struct {
	mu sync.Mutex

	builder func() Promise
	promise Promise
	pending []func(Promise)
}

// NewLazyPromise creates a promise that builds itself from the given builder
// the first time it is waited on.
func NewLazyPromise(builder func() Promise) *LazyPromise {
	return &LazyPromise{builder: builder}
}

// PromiseNeedsBuilt is LazyPromise::promiseNeedsBuilt.
func (l *LazyPromise) PromiseNeedsBuilt() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.promise == nil
}

// BuildPromise is LazyPromise::buildPromise: run the builder, replay everything
// that was queued onto what it returned, and hand it back.
//
// The PHP throws when the promise is already built; this returns
// [ErrAlreadyBuilt], because building twice would run the request twice.
func (l *LazyPromise) BuildPromise() (Promise, error) {
	l.mu.Lock()
	if l.promise != nil {
		l.mu.Unlock()
		return nil, ErrAlreadyBuilt
	}
	builder := l.builder
	l.mu.Unlock()

	built := builder()

	l.mu.Lock()
	l.promise = built
	pending := l.pending
	l.pending = nil
	l.mu.Unlock()

	for _, queued := range pending {
		queued(built)
	}
	return built, nil
}

// Then is LazyPromise::then: queued until the promise is built, forwarded once
// it is.
func (l *LazyPromise) Then(onFulfilled func(any) any, onRejected func(error) any) Promise {
	l.mu.Lock()
	if l.promise == nil {
		l.pending = append(l.pending, func(promise Promise) {
			promise.Then(onFulfilled, onRejected)
		})
		l.mu.Unlock()
		return l
	}
	promise := l.promise
	l.mu.Unlock()
	return promise.Then(onFulfilled, onRejected)
}

// Otherwise is LazyPromise::otherwise.
func (l *LazyPromise) Otherwise(onRejected func(error) any) Promise {
	return l.Then(nil, onRejected)
}

// GetState is LazyPromise::getState: pending until the promise is built.
func (l *LazyPromise) GetState() State {
	l.mu.Lock()
	promise := l.promise
	l.mu.Unlock()
	if promise == nil {
		return StatePending
	}
	return promise.GetState()
}

// Resolve is LazyPromise::resolve: always [ErrLazy], as the PHP always throws.
func (l *LazyPromise) Resolve(value any) error { return ErrLazy }

// Reject is LazyPromise::reject: always [ErrLazy], as the PHP always throws.
func (l *LazyPromise) Reject(reason error) error { return ErrLazy }

// Cancel is LazyPromise::cancel: always [ErrLazy], as the PHP always throws.
func (l *LazyPromise) Cancel() error { return ErrLazy }

// Wait is LazyPromise::wait: build the promise if it has not been built, then
// wait on it.
func (l *LazyPromise) Wait(unwrap bool) (any, error) {
	if l.PromiseNeedsBuilt() {
		if _, err := l.BuildPromise(); err != nil && err != ErrAlreadyBuilt {
			return nil, err
		}
	}
	l.mu.Lock()
	promise := l.promise
	l.mu.Unlock()
	return promise.Wait(unwrap)
}
