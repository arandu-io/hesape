package middleware_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/routing/middleware"
)

// refused records what a middleware turned away, standing in for
// hesape/hhttp.Refuse.
type refusal struct {
	status  int
	message string
	called  bool
}

func (f *refusal) refuse(w http.ResponseWriter, r *http.Request, status int, message string) {
	f.called, f.status, f.message = true, status, message
	http.Error(w, message, status)
}

var reached = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestThrottleAllowsUpToTheLimitAndThenRefuses(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	h := middleware.Throttle(limiter, cache.PerMinute(2), middleware.KeyByIP, refused.refuse)(reached)

	for attempt := 1; attempt <= 2; attempt++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request())
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, want 200", attempt, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the third attempt = %d, want 429", rec.Code)
	}
	if !refused.called || refused.status != http.StatusTooManyRequests {
		t.Fatalf("the refusal did not go through the Refuse given to the middleware: %+v", refused)
	}
}

// TestTwoReplicasShareOneBudget is why the limit is counted in a store rather
// than in a map on the middleware. Counted per process, N replicas allow N times
// the limit, on exactly the endpoints somebody would bother to flood.
func TestTwoReplicasShareOneBudget(t *testing.T) {
	var refusedA, refusedB refusal
	backing := cache.NewArrayStore()

	replicaA := middleware.Throttle(cache.NewRateLimiter(backing),
		cache.PerMinute(2), middleware.KeyByIP, refusedA.refuse)(reached)
	replicaB := middleware.Throttle(cache.NewRateLimiter(backing),
		cache.PerMinute(2), middleware.KeyByIP, refusedB.refuse)(reached)

	// One attempt against each replica spends the budget of two.
	for _, h := range []http.Handler{replicaA, replicaB} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request())
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200: the budget is two", rec.Code)
		}
	}

	// The third attempt is over, whichever replica it arrives at.
	rec := httptest.NewRecorder()
	replicaA.ServeHTTP(rec, request())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429: the two replicas each kept their own count", rec.Code)
	}
}

// TestTheHeaderAndTheSentenceAgree: a Retry-After that disagrees with the
// sentence is worse than one that says nothing.
func TestTheHeaderAndTheSentenceAgree(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	h := middleware.Throttle(limiter, cache.PerMinute(1), middleware.KeyByIP, refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request())
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "1" {
		t.Errorf("X-RateLimit-Limit = %q, want 1", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request())

	retry := rec.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("the refusal carries no Retry-After")
	}
	if !strings.Contains(refused.message, retry) {
		t.Fatalf("the sentence %q does not carry the number in Retry-After (%s)", refused.message, retry)
	}
}

// TestTwoCallersHaveTwoBudgets, because a limiter that keyed everything the
// same would be a limit on the whole application.
func TestTwoCallersHaveTwoBudgets(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	h := middleware.Throttle(limiter, cache.PerMinute(1), middleware.KeyByIP, refused.refuse)(reached)

	first := request()
	first.RemoteAddr = "192.0.2.10:5000"
	second := request()
	second.RemoteAddr = "192.0.2.11:5000"

	for _, req := range []*http.Request{first, second} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200: the two addresses shared a budget", req.RemoteAddr, rec.Code)
		}
	}
}

// TestTheLimitsOwnKeyIsReplaced: a Limit built with a fixed key would be one
// budget shared by everybody, which is not a rate limit.
func TestTheLimitsOwnKeyIsReplaced(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	h := middleware.Throttle(limiter, cache.PerMinute(1).By("everybody"), middleware.KeyByIP, refused.refuse)(reached)

	first := request()
	first.RemoteAddr = "192.0.2.10:5000"
	second := request()
	second.RemoteAddr = "192.0.2.11:5000"

	for _, req := range []*http.Request{first, second} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200: the Limit's own key was not replaced", req.RemoteAddr, rec.Code)
		}
	}
}

// brokenStore is a store that cannot be reached.
type brokenStore struct{}

var errStoreDown = errors.New("the store is down")

func (brokenStore) Get(context.Context, string) ([]byte, error) { return nil, errStoreDown }
func (brokenStore) Put(context.Context, string, []byte, time.Duration) error {
	return errStoreDown
}
func (brokenStore) Add(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, errStoreDown
}
func (brokenStore) Increment(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errStoreDown
}
func (brokenStore) Forget(context.Context, string) error { return errStoreDown }
func (brokenStore) Flush(context.Context, string) error  { return errStoreDown }

// TestTheThrottleFailsOpen: a rate limiter is a guard against abuse, not a
// dependency of serving a page, and one that refuses everything while its store
// is down turns a cache outage into a total one.
func TestTheThrottleFailsOpen(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(brokenStore{})
	h := middleware.Throttle(limiter, cache.PerMinute(1), middleware.KeyByIP, refused.refuse)(reached)

	for attempt := 1; attempt <= 3; attempt++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request())
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, want 200: the throttle must fail open", attempt, rec.Code)
		}
	}
	if refused.called {
		t.Fatal("a request was refused because the store was unreachable")
	}
}

// TestFailingOpenIsLoudAboutIt is the other half of failing open. A limiter that
// stops limiting and says nothing has stopped existing exactly when the system
// is under stress, and nobody finds out until the bill or the breach.
func TestFailingOpenIsLoudAboutIt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	var refused refusal
	limiter := cache.NewRateLimiter(brokenStore{})
	h := middleware.Throttle(limiter, cache.PerMinute(1), middleware.KeyByIP, refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request().WithContext(log.Into(context.Background(), logger)))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	line := buf.String()
	if line == "" {
		t.Fatal("the throttle let the request through and logged nothing")
	}
	if !strings.Contains(line, "level=ERROR") {
		t.Fatalf("the request went unlimited and was not logged at ERROR: %s", line)
	}
	if !strings.Contains(line, "allowed through") || !strings.Contains(line, errStoreDown.Error()) {
		t.Fatalf("the log says neither what happened nor why: %s", line)
	}
}

// TestAThrottleThatCouldNeverLimitPanicsAtWiring.
//
// Each of these is a route declared as limited that would carry no limit at all:
// the counter rejects the limit, so every request errors, so every request is
// allowed through -- permanently, and with a line in the log blaming the store.
// None of it is recoverable at request time and all of it is knowable before the
// first request, so it stops the process instead.
func TestAThrottleThatCouldNeverLimitPanicsAtWiring(t *testing.T) {
	var refused refusal
	ok := cache.NewRateLimiter(cache.NewArrayStore())

	cases := []struct {
		name string
		wire func()
	}{
		{"no limiter", func() {
			middleware.Throttle(nil, cache.PerMinute(1), middleware.KeyByIP, refused.refuse)
		}},
		{"no key function", func() {
			middleware.Throttle(ok, cache.PerMinute(1), nil, refused.refuse)
		}},
		{"no Refuse", func() {
			middleware.Throttle(ok, cache.PerMinute(1), middleware.KeyByIP, nil)
		}},
		{"a budget of zero", func() {
			middleware.Throttle(ok, cache.PerMinute(0), middleware.KeyByIP, refused.refuse)
		}},
		{"a negative budget", func() {
			middleware.Throttle(ok, cache.PerMinute(-1), middleware.KeyByIP, refused.refuse)
		}},
		{"no window", func() {
			middleware.Throttle(ok, cache.Limit{MaxAttempts: 5}, middleware.KeyByIP, refused.refuse)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("wiring was accepted, and the route it guards would carry no limit at all")
				}
			}()
			c.wire()
		})
	}
}

// TestNoneIsWiredAndLimitsNothing, because the panic above must not catch the
// one limit that is allowed to allow everything.
func TestNoneIsWiredAndLimitsNothing(t *testing.T) {
	var refused refusal
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	h := middleware.Throttle(limiter, cache.None(), middleware.KeyByIP, refused.refuse)(reached)

	for attempt := 1; attempt <= 50; attempt++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request())
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, want 200: None limits nothing", attempt, rec.Code)
		}
	}
	if refused.called {
		t.Fatal("None refused a request")
	}
}

// TestKeyByIPMasksIPv6ToItsBlock: keyed on the full address, one machine with a
// routed /64 has eighteen quintillion budgets, which is no limit at all.
func TestKeyByIPMasksIPv6ToItsBlock(t *testing.T) {
	first := request()
	first.RemoteAddr = "[2001:db8::1]:5000"
	second := request()
	second.RemoteAddr = "[2001:db8::dead:beef]:5000"

	if middleware.KeyByIP(first) != middleware.KeyByIP(second) {
		t.Fatalf("two addresses in one /64 got two keys: %s and %s",
			middleware.KeyByIP(first), middleware.KeyByIP(second))
	}

	other := request()
	other.RemoteAddr = "[2001:db8:0:1::1]:5000"
	if middleware.KeyByIP(first) == middleware.KeyByIP(other) {
		t.Fatal("two different /64s got one key: every subscriber would share a budget")
	}
}

// TestKeyByIPIgnoresWhatTheClientSends: X-Forwarded-For is a header anybody can
// write, and reading it is a way to reset someone else's counter.
func TestKeyByIPIgnoresWhatTheClientSends(t *testing.T) {
	req := request()
	req.RemoteAddr = "192.0.2.10:5000"
	before := middleware.KeyByIP(req)

	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	if middleware.KeyByIP(req) != before {
		t.Fatal("the key changed with a header the client controls")
	}
}

// TestKeyBySessionFallsBackToTheAddress for an anonymous request, which is
// every request that reaches a sign-in form.
func TestKeyBySessionFallsBackToTheAddress(t *testing.T) {
	signedIn := middleware.KeyBySession(func(*http.Request) string { return "sess-1" })
	anonymous := middleware.KeyBySession(func(*http.Request) string { return "" })

	req := request()
	req.RemoteAddr = "192.0.2.10:5000"

	if got := signedIn(req); got != "session:sess-1" {
		t.Errorf("key = %q, want session:sess-1", got)
	}
	if got := anonymous(req); got != middleware.KeyByIP(req) {
		t.Errorf("key = %q, want the address key %q", got, middleware.KeyByIP(req))
	}
}

func request() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.RemoteAddr = "192.0.2.1:5000"
	return req
}
