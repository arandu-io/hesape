package promises

import "sync"

// LazyPromise is a promise whose work has not started, and does not start
// until somebody waits on it.
//
// Every Then and Otherwise registered before that is queued, and replayed
// onto the real promise the moment it is built. It is what a pool uses to
// describe a request it has not sent yet.
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

// PromiseNeedsBuilt reports whether the promise has not been built yet.
func (l *LazyPromise) PromiseNeedsBuilt() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.promise == nil
}

// BuildPromise runs the builder, replays everything that was queued onto
// what it returned, and hands it back.
//
// Returns [ErrAlreadyBuilt] when the promise is already built, because
// building twice would run the request twice.
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

// Then is queued until the promise is built, forwarded once it is.
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

// Otherwise is Then with only the rejection callback.
func (l *LazyPromise) Otherwise(onRejected func(error) any) Promise {
	return l.Then(nil, onRejected)
}

// GetState is [StatePending] until the promise is built, and the built
// promise's state after.
func (l *LazyPromise) GetState() State {
	l.mu.Lock()
	promise := l.promise
	l.mu.Unlock()
	if promise == nil {
		return StatePending
	}
	return promise.GetState()
}

// Resolve always returns [ErrLazy]: a lazy promise has nothing of its own
// to settle.
func (l *LazyPromise) Resolve(value any) error { return ErrLazy }

// Reject always returns [ErrLazy]: a lazy promise has nothing of its own
// to settle.
func (l *LazyPromise) Reject(reason error) error { return ErrLazy }

// Cancel always returns [ErrLazy]: a lazy promise has nothing of its own
// to settle.
func (l *LazyPromise) Cancel() error { return ErrLazy }

// Wait builds the promise if it has not been built, then waits on it.
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
