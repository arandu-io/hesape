package requestscope

import (
	"context"
	"net/http"
	"sync"
)

// Scope is the memory of one request. It is built by [Middleware] or [With] and
// reached through the context; a program never holds one directly.
type Scope struct {
	mu      sync.Mutex
	entries map[any]*entry
}

// entry is one remembered question. Its own mutex is held while the load runs,
// so concurrent callers of one key wait for the first load instead of repeating
// it, while callers of other keys are not held up.
type entry struct {
	mu     sync.Mutex
	loaded bool
	value  any
}

// box holds a value with its static type, so that a remembered nil interface
// is still a remembered value and a key read back as another type is a miss.
type box[T any] struct{ value T }

type contextKey struct{}

func newScope() *Scope {
	return &Scope{entries: map[any]*entry{}}
}

func from(ctx context.Context) (*Scope, bool) {
	scope, ok := ctx.Value(contextKey{}).(*Scope)
	return scope, ok
}

// With returns a context carrying an empty scope, or ctx itself when it already
// has one. [Middleware] is the way a request gets its scope; With is for a test
// and for code that runs a unit of work shaped like a request.
func With(ctx context.Context) context.Context {
	if _, ok := from(ctx); ok {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, newScope())
}

// Active reports whether ctx carries a scope.
func Active(ctx context.Context) bool {
	_, ok := from(ctx)
	return ok
}

// Middleware gives a safe request (GET, HEAD, OPTIONS) a scope that lives and
// dies with it. Any other method gets none, so a request that writes reads the
// database at every call, and a write that follows a read cannot be answered
// from what the read saw.
//
// It decides nothing about who may do what. Authentication and authorization
// run after it, as they would without it.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			r = r.WithContext(With(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

// Remember runs load once per key in the request's scope and answers its value
// to every later call of the same request. An error is never kept: the next
// call loads again. Without a scope on ctx it is load itself and keeps nothing.
//
// Concurrent callers of one key wait for the first load rather than running it
// again. load must not call Remember with its own key.
//
// The value is shared by every caller of the request, not copied; see the
// package documentation for what that asks of a caller.
func Remember[K comparable, T any](ctx context.Context, key K, load func() (T, error)) (T, error) {
	scope, ok := from(ctx)
	if !ok {
		return load()
	}
	scope.mu.Lock()
	e := scope.entries[key]
	if e == nil {
		e = &entry{}
		scope.entries[key] = e
	}
	scope.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loaded {
		if held, ok := e.value.(box[T]); ok {
			return held.value, nil
		}
		// The key already names a value of another type: answer without
		// disturbing it.
		return load()
	}
	value, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	e.value, e.loaded = box[T]{value}, true
	return value, nil
}

// Peek answers what the request remembered under key, without loading. The
// second result is false when nothing is remembered, when ctx has no scope and
// when the key holds a value of another type.
func Peek[K comparable, T any](ctx context.Context, key K) (T, bool) {
	var zero T
	scope, ok := from(ctx)
	if !ok {
		return zero, false
	}
	scope.mu.Lock()
	e := scope.entries[key]
	scope.mu.Unlock()
	if e == nil {
		return zero, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		return zero, false
	}
	held, ok := e.value.(box[T])
	return held.value, ok
}

// Store remembers value under key for the rest of the request, replacing what
// was there. It does nothing without a scope on ctx.
func Store[K comparable, T any](ctx context.Context, key K, value T) {
	scope, ok := from(ctx)
	if !ok {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	scope.entries[key] = &entry{loaded: true, value: box[T]{value}}
}

// Forget drops everything the request remembered. A write that changes what the
// scope holds calls it, so the rest of the same request reads what the write
// left. Without a scope it does nothing.
func Forget(ctx context.Context) {
	scope, ok := from(ctx)
	if !ok {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	clear(scope.entries)
}

// ForgetKey drops what the request remembered under one key. A load already
// running for that key finishes, answers its own callers and keeps nothing.
func ForgetKey[K comparable](ctx context.Context, key K) {
	scope, ok := from(ctx)
	if !ok {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	delete(scope.entries, key)
}
