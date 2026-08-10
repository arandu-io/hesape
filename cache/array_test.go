package cache_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/cachetest"
)

// TestArrayStoreContract is the whole point of cachetest: the store that ships
// with the collection passes the same suite the RESP adapter has to pass, and
// neither of them gets its own definition of what a cache does.
func TestArrayStoreContract(t *testing.T) {
	cachetest.Run(t, func(*testing.T) cache.Store {
		return cache.NewArrayStore()
	})
}

func TestArrayStoreLockingContract(t *testing.T) {
	cachetest.RunLocking(t, func(*testing.T) cache.Locking {
		return cache.NewArrayStore()
	})
}

// TestArrayStoreIncrementRefusesANonCounter documents the refusal: overwriting
// would leave a rate limiter counting from zero the first time somebody cached
// a struct under its key, and a limit that restarts is a limit that stopped
// limiting.
func TestArrayStoreIncrementRefusesANonCounter(t *testing.T) {
	s := cache.NewArrayStore()
	ctx := context.Background()

	if err := s.Put(ctx, "k", []byte(`{"a":1}`), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Increment(ctx, "k", 1, time.Minute); err == nil {
		t.Fatal("Increment over a value that is not a counter = nil, want an error")
	}
}

func TestArrayStoreRefusesAnEntryWithNoTTL(t *testing.T) {
	s := cache.NewArrayStore()
	ctx := context.Background()

	if err := s.Put(ctx, "k", []byte("v"), 0); err != cache.ErrNoTTL {
		t.Fatalf("Put with no ttl = %v, want cache.ErrNoTTL", err)
	}
	if _, err := s.Add(ctx, "k", []byte("v"), 0); err != cache.ErrNoTTL {
		t.Fatalf("Add with no ttl = %v, want cache.ErrNoTTL", err)
	}
	if _, err := s.Increment(ctx, "n", 1, 0); err != cache.ErrNoTTL {
		t.Fatalf("Increment with no ttl = %v, want cache.ErrNoTTL", err)
	}
	if _, err := s.AcquireLock(ctx, "lock:x", "t", 0); err != cache.ErrNoTTL {
		t.Fatalf("AcquireLock with no ttl = %v, want cache.ErrNoTTL", err)
	}
}

// TestArrayStoreIsConcurrent is what -race is for: the store is reached from
// every request of a process, and a map without a mutex is a crash under load
// and nothing at all under test.
func TestArrayStoreIsConcurrent(t *testing.T) {
	s := cache.NewArrayStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "k" + strconv.Itoa(i)
			for range 50 {
				if err := s.Put(ctx, key, []byte("v"), time.Minute); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				if _, err := s.Get(ctx, key); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if _, err := s.Increment(ctx, "shared", 1, time.Minute); err != nil {
					t.Errorf("Increment: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	n, err := s.Increment(ctx, "shared", 0, time.Minute)
	if err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if n != 400 {
		t.Fatalf("the shared counter = %d, want 400", n)
	}
}

// TestArrayStoreFlushLeavesLocksAlone is why entries and locks are two maps: a
// cache:clear for one tenant must not release the lock the scheduler is holding
// while it runs.
func TestArrayStoreFlushLeavesLocksAlone(t *testing.T) {
	s := cache.NewArrayStore()
	ctx := context.Background()

	if _, err := s.AcquireLock(ctx, "lock:report", "token-a", time.Minute); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if err := s.Flush(ctx, ""); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	ok, err := s.AcquireLock(ctx, "lock:report", "token-b", time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if ok {
		t.Fatal("a flush of every entry released a held lock")
	}
}
