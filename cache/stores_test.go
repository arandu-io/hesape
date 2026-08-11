package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/events"
)

// recorder is a Dispatcher that keeps what it was told, so a test can assert on
// the events a cache fired rather than on the calls it made.
type recorder struct {
	mu     sync.Mutex
	events []any
}

func (r *recorder) Dispatch(e any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) all() []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]any(nil), r.events...)
}

// count is how many events of type T were fired.
func count[T any](r *recorder) int {
	n := 0
	for _, e := range r.all() {
		if _, ok := e.(T); ok {
			n++
		}
	}
	return n
}

// broken is a Store that refuses everything, which is what a cache that is down
// looks like from the call site.
type broken struct{ err error }

func (b broken) Get(context.Context, string) ([]byte, error) { return nil, b.err }
func (b broken) Put(context.Context, string, []byte, time.Duration) error {
	return b.err
}

func (b broken) Add(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, b.err
}
func (b broken) Forget(context.Context, string) error { return b.err }
func (b broken) Increment(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, b.err
}
func (b broken) Flush(context.Context, string) error { return b.err }

func TestNullStoreReadsAreAlwaysAMiss(t *testing.T) {
	ctx := context.Background()
	s := cache.NewNullStore()

	if err := s.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put on the null store = %v, want no error: caching is off, not broken", err)
	}
	if _, err := s.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get = %v, want cache.ErrNotFound", err)
	}
	if added, err := s.Add(ctx, "k", []byte("v"), time.Minute); err != nil || added {
		t.Fatalf("Add = %v, %v; want false, nil", added, err)
	}
	if touched, err := s.Touch(ctx, "k", time.Minute); err != nil || touched {
		t.Fatalf("Touch = %v, %v; want false, nil", touched, err)
	}
}

// TestNullStoreRepositoryStillAnswers is why Put returns no error: a repository
// over the null store has to be usable, or turning the cache off takes the
// application down with it.
func TestNullStoreRepositoryStillAnswers(t *testing.T) {
	ctx := context.Background()
	r := cache.New(cache.NewNullStore())
	g := grantFor("acme")

	computed := 0
	for range 3 {
		got, err := cache.Remember(ctx, r, g, "total", time.Minute, func(context.Context) (int, error) {
			computed++
			return 7, nil
		})
		if err != nil {
			t.Fatalf("Remember: %v", err)
		}
		if got != 7 {
			t.Fatalf("Remember = %d, want 7", got)
		}
	}
	if computed != 3 {
		t.Fatalf("the callback ran %d times, want 3: the null store must not remember anything", computed)
	}
}

func TestMemoizedStoreAsksTheStoreUnderneathOnce(t *testing.T) {
	ctx := context.Background()
	counted := &countingStore{Store: cache.NewArrayStore()}
	s := cache.NewMemoizedStore("array", counted)

	if err := counted.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for range 4 {
		got, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "v" {
			t.Fatalf("Get = %q, want %q", got, "v")
		}
	}
	if counted.gets != 1 {
		t.Fatalf("the store underneath was read %d times, want 1", counted.gets)
	}
}

// TestMemoizedStoreRemembersAMissToo is the half people forget: a miss asked for
// four times is four round trips, and the misses are the expensive ones.
func TestMemoizedStoreRemembersAMissToo(t *testing.T) {
	ctx := context.Background()
	counted := &countingStore{Store: cache.NewArrayStore()}
	s := cache.NewMemoizedStore("array", counted)

	for range 4 {
		if _, err := s.Get(ctx, "absent"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("Get = %v, want cache.ErrNotFound", err)
		}
	}
	if counted.gets != 1 {
		t.Fatalf("the store underneath was read %d times for a miss, want 1", counted.gets)
	}
}

func TestMemoizedStoreForgetsWhatItWrote(t *testing.T) {
	ctx := context.Background()
	s := cache.NewMemoizedStore("array", cache.NewArrayStore())

	if err := s.Put(ctx, "k", []byte("first"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Get(ctx, "k"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := s.Put(ctx, "k", []byte("second"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("Get after a write = %q, want %q: a writer must see what it wrote", got, "second")
	}
}

func TestMemoizedStoreDoesNotRememberAFailure(t *testing.T) {
	ctx := context.Background()
	failing := &flakyStore{Store: cache.NewArrayStore(), fail: true}
	s := cache.NewMemoizedStore("array", failing)

	if _, err := s.Get(ctx, "k"); err == nil {
		t.Fatal("Get = nil, want the store's error")
	}

	failing.fail = false
	if err := failing.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get after the store came back: %v", err)
	}
	if string(got) != "v" {
		t.Fatalf("Get = %q, want %q: one bad moment was remembered as a miss", got, "v")
	}
}

func TestFailoverStoreFallsThroughToTheStoreThatAnswers(t *testing.T) {
	ctx := context.Background()
	second := cache.NewArrayStore()
	rec := &recorder{}
	s := cache.NewFailoverStore(rec, []string{"first", "second"},
		broken{err: errors.New("connection refused")}, second)

	if err := s.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := second.Get(ctx, "k"); err != nil {
		t.Fatalf("the value did not reach the second store: %v", err)
	}

	if n := count[*events.CacheFailedOver](rec); n != 1 {
		t.Fatalf("%d CacheFailedOver events, want 1", n)
	}
}

// TestFailoverStoreFiresOnceWhileAStoreStaysDown is what makes the event worth
// listening to: a cache that has been down for an hour is one event, not one per
// request.
func TestFailoverStoreFiresOnceWhileAStoreStaysDown(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	s := cache.NewFailoverStore(rec, []string{"first", "second"},
		broken{err: errors.New("connection refused")}, cache.NewArrayStore())

	for range 5 {
		if err := s.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if n := count[*events.CacheFailedOver](rec); n != 1 {
		t.Fatalf("%d CacheFailedOver events over five writes, want 1", n)
	}
}

// TestFailoverStoreTreatsAMissAsAnAnswer keeps every miss from costing a round
// trip to every store -- and from being answered by an older copy.
func TestFailoverStoreTreatsAMissAsAnAnswer(t *testing.T) {
	ctx := context.Background()
	first := cache.NewArrayStore()
	second := cache.NewArrayStore()
	if err := second.Put(ctx, "k", []byte("stale"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	s := cache.NewFailoverStore(nil, []string{"first", "second"}, first, second)
	if _, err := s.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get = %v, want cache.ErrNotFound: the second store's older copy answered", err)
	}
}

func TestFailoverStoreWithNoStoresSaysSo(t *testing.T) {
	s := cache.NewFailoverStore(nil, nil)
	if _, err := s.Get(context.Background(), "k"); !errors.Is(err, cache.ErrAllStoresFailed) {
		t.Fatalf("Get = %v, want cache.ErrAllStoresFailed", err)
	}
}

func TestFailoverStoreReportsTheLastStoresOwnError(t *testing.T) {
	refused := errors.New("connection refused")
	s := cache.NewFailoverStore(nil, []string{"only"}, broken{err: refused})

	if _, err := s.Get(context.Background(), "k"); !errors.Is(err, refused) {
		t.Fatalf("Get = %v, want the store's own error: \"the cache is down\" is less use than why", err)
	}
}

// countingStore counts the reads that reached it.
type countingStore struct {
	cache.Store
	gets int
}

func (s *countingStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.gets++
	return s.Store.Get(ctx, key)
}

// flakyStore refuses reads while fail is set.
type flakyStore struct {
	cache.Store
	fail bool
}

func (s *flakyStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.fail {
		return nil, errors.New("connection refused")
	}
	return s.Store.Get(ctx, key)
}
