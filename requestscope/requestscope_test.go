package requestscope_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/arandu-io/hesape/requestscope"
)

type rowKey struct{ kind, id string }

type row struct{ name string }

func counted(calls *atomic.Int32, value *row) func() (*row, error) {
	return func() (*row, error) {
		calls.Add(1)
		return value, nil
	}
}

// TestARememberedReadRunsOnce: a page asks the same question at every gate, and
// the point of the scope is that the second ask is not a second query.
func TestARememberedReadRunsOnce(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls atomic.Int32
	want := &row{name: "acme"}

	first, err := requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&calls, want))
	if err != nil {
		t.Fatal(err)
	}
	second, err := requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&calls, &row{name: "other"}))
	if err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 1 {
		t.Fatalf("load ran %d times, want 1", calls.Load())
	}
	if first != want || second != want {
		t.Fatal("every caller of the request must get the same value, shared and not copied")
	}
}

func TestKeysThatDifferInAnyInputDoNotShareAnAnswer(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls atomic.Int32

	for _, key := range []rowKey{{"org", "1"}, {"org", "2"}, {"seller", "1"}} {
		if _, err := requestscope.Remember(ctx, key, counted(&calls, &row{name: key.kind + key.id})); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("load ran %d times for three different questions, want 3", calls.Load())
	}
}

func TestAKeyOfAnotherTypeIsAnotherQuestion(t *testing.T) {
	type a struct{ id string }
	type b struct{ id string }
	ctx := requestscope.With(context.Background())

	one, _ := requestscope.Remember(ctx, a{"1"}, func() (string, error) { return "from a", nil })
	two, _ := requestscope.Remember(ctx, b{"1"}, func() (string, error) { return "from b", nil })
	if one != "from a" || two != "from b" {
		t.Fatalf("got %q and %q: keys with equal fields but different types must not collide", one, two)
	}
}

func TestAnErrorIsNeverRemembered(t *testing.T) {
	ctx := requestscope.With(context.Background())
	boom := errors.New("boom")
	var calls int

	load := func() (*row, error) {
		calls++
		if calls == 1 {
			return nil, boom
		}
		return &row{name: "ok"}, nil
	}
	if _, err := requestscope.Remember(ctx, rowKey{"org", "1"}, load); !errors.Is(err, boom) {
		t.Fatalf("first call error = %v, want boom", err)
	}
	got, err := requestscope.Remember(ctx, rowKey{"org", "1"}, load)
	if err != nil || got.name != "ok" {
		t.Fatalf("second call = %v, %v: a failed read must be tried again", got, err)
	}
}

// TestARowNotFoundIsRemembered: a nil row with no error is an answer, and
// asking again must not repeat the query that found nothing.
func TestARowNotFoundIsRemembered(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls atomic.Int32

	for range 3 {
		got, err := requestscope.Remember(ctx, rowKey{"org", "missing"}, func() (*row, error) {
			calls.Add(1)
			return nil, nil
		})
		if err != nil || got != nil {
			t.Fatalf("got %v, %v, want a remembered nil", got, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("load ran %d times, want 1", calls.Load())
	}
}

func TestANilInterfaceIsRemembered(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls int
	for range 2 {
		got, err := requestscope.Remember(ctx, rowKey{"x", "y"}, func() (error, error) {
			calls++
			return nil, nil
		})
		if err != nil || got != nil {
			t.Fatal("expected nil, nil")
		}
	}
	if calls != 1 {
		t.Fatalf("load ran %d times, want 1", calls)
	}
}

// TestAWriteClearsWhatWasRemembered: a write that changes a row calls Forget so
// the rest of the request reads what the write left.
func TestAWriteClearsWhatWasRemembered(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls atomic.Int32

	read := func() *row {
		got, err := requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&calls, &row{name: "v"}))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := read()
	read()
	requestscope.Forget(ctx)
	after := read()

	if calls.Load() != 2 {
		t.Fatalf("load ran %d times, want 2: once before the write and once after", calls.Load())
	}
	if before == after {
		t.Fatal("the read after Forget must load a new value")
	}
}

func TestForgetKeyDropsOnlyThatKey(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var orgCalls, sellerCalls atomic.Int32

	read := func() {
		requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&orgCalls, &row{}))
		requestscope.Remember(ctx, rowKey{"seller", "1"}, counted(&sellerCalls, &row{}))
	}
	read()
	requestscope.ForgetKey(ctx, rowKey{"org", "1"})
	read()

	if orgCalls.Load() != 2 || sellerCalls.Load() != 1 {
		t.Fatalf("org loaded %d times and seller %d, want 2 and 1", orgCalls.Load(), sellerCalls.Load())
	}
}

func TestForgetOnAContextWithoutAScopeIsHarmless(t *testing.T) {
	requestscope.Forget(context.Background())
	requestscope.ForgetKey(context.Background(), rowKey{"a", "b"})
}

// TestWithoutAScopeEveryReadLoads: a job, a listener, a command and a write have
// no scope, and they read the database as they always did.
func TestWithoutAScopeEveryReadLoads(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32

	for range 3 {
		if _, err := requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&calls, &row{})); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("load ran %d times, want 3", calls.Load())
	}
	if requestscope.Active(ctx) {
		t.Fatal("a bare context must not report a scope")
	}
}

func TestWithKeepsTheScopeAContextAlreadyHas(t *testing.T) {
	outer := requestscope.With(context.Background())
	if requestscope.With(outer) != outer {
		t.Fatal("With must return a context that already carries a scope as it is")
	}
}

func TestPeekAndStore(t *testing.T) {
	ctx := requestscope.With(context.Background())
	key := rowKey{"span", "1"}

	if _, ok := requestscope.Peek[rowKey, *row](ctx, key); ok {
		t.Fatal("Peek found something in an empty scope")
	}
	want := &row{name: "span"}
	requestscope.Store(ctx, key, want)
	got, ok := requestscope.Peek[rowKey, *row](ctx, key)
	if !ok || got != want {
		t.Fatalf("Peek = %v, %v, want the stored value", got, ok)
	}
	if _, ok := requestscope.Peek[rowKey, string](ctx, key); ok {
		t.Fatal("Peek must not answer a key as a type it was not stored with")
	}

	var calls atomic.Int32
	if r, _ := requestscope.Remember(ctx, key, counted(&calls, &row{})); r != want || calls.Load() != 0 {
		t.Fatal("Remember must answer from what Store left")
	}

	requestscope.Store(context.Background(), key, want) // no scope: nothing happens
	if _, ok := requestscope.Peek[rowKey, *row](context.Background(), key); ok {
		t.Fatal("Peek on a bare context must find nothing")
	}
}

func TestAKeyAskedAsAnotherTypeLoadsWithoutDisturbingTheFirst(t *testing.T) {
	ctx := requestscope.With(context.Background())
	key := rowKey{"k", "1"}

	requestscope.Remember(ctx, key, func() (string, error) { return "text", nil })
	n, err := requestscope.Remember(ctx, key, func() (int, error) { return 7, nil })
	if err != nil || n != 7 {
		t.Fatalf("got %d, %v, want the load's own value", n, err)
	}
	s, _ := requestscope.Remember(ctx, key, func() (string, error) { return "again", nil })
	if s != "text" {
		t.Fatalf("got %q, want the first remembered value", s)
	}
}

// TestConcurrentCallersShareOneLoad is meant for -race: handlers of one request
// run in goroutines (a page that fetches its fragments in parallel), and they
// all ask the same question.
func TestConcurrentCallersShareOneLoad(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var calls atomic.Int32
	var wg sync.WaitGroup

	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			requestscope.Remember(ctx, rowKey{"org", "1"}, counted(&calls, &row{}))
			requestscope.Peek[rowKey, *row](ctx, rowKey{"org", "1"})
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("load ran %d times across 32 callers, want 1", calls.Load())
	}
}

func TestForgetWhileLoadingIsSafe(t *testing.T) {
	ctx := requestscope.With(context.Background())
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%4 == 0 {
				requestscope.Forget(ctx)
				requestscope.ForgetKey(ctx, rowKey{"org", "1"})
				return
			}
			requestscope.Remember(ctx, rowKey{"org", "1"}, func() (*row, error) { return &row{}, nil })
		}()
	}
	wg.Wait()
}

// scopeSeen is a handler that records whether its request carries a scope.
func scopeSeen(seen *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = requestscope.Active(r.Context())
	})
}

func TestSafeMethodsGetAScopeAndUnsafeMethodsDoNot(t *testing.T) {
	cases := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodPost:    false,
		http.MethodPut:     false,
		http.MethodPatch:   false,
		http.MethodDelete:  false,
	}
	for method, want := range cases {
		var seen bool
		requestscope.Middleware(scopeSeen(&seen)).
			ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/", nil))
		if seen != want {
			t.Errorf("%s: scope present = %v, want %v", method, seen, want)
		}
	}
}

// TestNothingIsSharedAcrossRequests: a membership or a permission revoked is
// seen by the very next request, because the next request has its own scope.
func TestNothingIsSharedAcrossRequests(t *testing.T) {
	var calls atomic.Int32
	handler := requestscope.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 2 {
			requestscope.Remember(r.Context(), rowKey{"membership", "1"}, counted(&calls, &row{}))
		}
	}))

	for range 3 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	if calls.Load() != 3 {
		t.Fatalf("load ran %d times over three requests of two reads each, want 3 (one per request)", calls.Load())
	}
}

func TestAnUnsafeRequestReadsEveryTime(t *testing.T) {
	var calls atomic.Int32
	handler := requestscope.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 2 {
			requestscope.Remember(r.Context(), rowKey{"membership", "1"}, counted(&calls, &row{}))
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	if calls.Load() != 2 {
		t.Fatalf("load ran %d times on a POST, want 2: a write gets no memory", calls.Load())
	}
}

func TestAScopeAlreadyOnTheRequestIsKept(t *testing.T) {
	outer := requestscope.With(context.Background())
	var inner context.Context
	h := requestscope.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner = r.Context() }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(outer))

	requestscope.Store(outer, rowKey{"a", "b"}, "x")
	if got, ok := requestscope.Peek[rowKey, string](inner, rowKey{"a", "b"}); !ok || got != "x" {
		t.Fatal("the middleware replaced a scope the request already had")
	}
}
