package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
)

func TestOnlyOneHolderAtATime(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	first := locks.Lock("relay", time.Minute)
	if err := first.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !first.Held() {
		t.Fatal("Held after Acquire = false, want true")
	}

	second := locks.Lock("relay", time.Minute)
	if err := second.Acquire(ctx); !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("the second Acquire = %v, want cache.ErrLocked", err)
	}
	if second.Held() {
		t.Fatal("Held after a refused Acquire = true, want false")
	}

	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := second.Acquire(ctx); err != nil {
		t.Fatalf("Acquire after the holder released: %v", err)
	}
}

// TestReleaseCannotStealTheLockBack is the property the token exists for: a
// holder that already gave the lock up must not be able to release the lock
// somebody else now owns.
func TestReleaseCannotStealTheLockBack(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	first := locks.Lock("relay", time.Minute)
	if err := first.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second := locks.Lock("relay", time.Minute)
	if err := second.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// The stale holder releasing again must do nothing at all.
	if err := first.Release(ctx); err != nil {
		t.Fatalf("the stale Release: %v", err)
	}

	third := locks.Lock("relay", time.Minute)
	if err := third.Acquire(ctx); !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("the lock was freed by a holder that no longer had it: err = %v", err)
	}
}

func TestReleaseOfALockNeverTakenIsNotAnError(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	if err := locks.Lock("relay", time.Minute).Release(context.Background()); err != nil {
		t.Fatalf("Release without Acquire: %v", err)
	}
}

func TestRunHoldsTheLockAndGivesItBack(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	ran := false
	err := locks.Lock("nightly", time.Minute).Run(ctx, func(context.Context) error {
		ran = true

		// While fn runs, nobody else gets in. This is the whole reason the
		// scheduler has a lock: with N replicas, a task scheduled every minute
		// runs N times a minute without one.
		if err := locks.Lock("nightly", time.Minute).Acquire(ctx); !errors.Is(err, cache.ErrLocked) {
			t.Errorf("a second holder got in during Run: err = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran {
		t.Fatal("Run did not run fn")
	}

	if err := locks.Lock("nightly", time.Minute).Acquire(ctx); err != nil {
		t.Fatalf("Acquire after Run: %v", err)
	}
}

func TestRunGivesTheLockBackWhenTheWorkFails(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	want := errors.New("the task failed")
	if err := locks.Lock("nightly", time.Minute).Run(ctx, func(context.Context) error {
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("Run = %v, want the task's error", err)
	}

	if err := locks.Lock("nightly", time.Minute).Acquire(ctx); err != nil {
		t.Fatalf("the lock was not released after the task failed: %v", err)
	}
}

// TestRunReleasesEvenOnACancelledContext: the release runs on a context that is
// not cancelled with the caller's, so an abandoned request gives the lock back
// instead of leaving it to expire.
func TestRunReleasesEvenOnACancelledContext(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())

	ctx, cancel := context.WithCancel(context.Background())
	err := locks.Lock("nightly", time.Minute).Run(ctx, func(context.Context) error {
		cancel()
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := locks.Lock("nightly", time.Minute).Acquire(context.Background()); err != nil {
		t.Fatalf("the lock was not released on a cancelled context: %v", err)
	}
}

func TestRunDoesNotRunTheWorkWhenTheLockIsHeld(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	held := locks.Lock("nightly", time.Minute)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	ran := false
	err := locks.Lock("nightly", time.Minute).Run(ctx, func(context.Context) error {
		ran = true
		return nil
	})
	if !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("Run against a held lock = %v, want cache.ErrLocked", err)
	}
	if ran {
		t.Fatal("Run ran the work while another holder had the lock")
	}
}

func TestALockNeedsATTLAndAName(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	if err := locks.Lock("relay", 0).Acquire(ctx); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("Acquire with no ttl = %v, want cache.ErrNoTTL", err)
	}
	if err := locks.Lock("", time.Minute).Acquire(ctx); err == nil {
		t.Fatal("Acquire with no name = nil, want an error")
	}
}

func TestAnExpiredLockIsFree(t *testing.T) {
	ctx := context.Background()
	locks := cache.NewLocks(cache.NewArrayStore())

	if err := locks.Lock("relay", 50*time.Millisecond).Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := locks.Lock("relay", time.Minute).Acquire(ctx); err != nil {
		t.Fatalf("Acquire after the ttl ran out: %v", err)
	}
}
