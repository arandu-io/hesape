// Package cachetest is the contract suite every cache store passes.
//
// A Store is an interface with six methods, and the difference between two
// implementations of it is meant to be invisible from the call site: the same
// application, wired to the in-process ArrayStore in a test and to the RESP
// store in production, must behave the same way. Nothing about the Go type
// system enforces that. This package does.
//
// It is here and not in the store's own repository because there is one
// contract, not one per backend: a suite copied into each adapter drifts, and
// the copy that drifts is the one that stops noticing that a miss returns the
// backend's own not-found error instead of cache.ErrNotFound.
//
// An adapter runs it in one test:
//
//	func TestStoreContract(t *testing.T) {
//		cachetest.Run(t, func(t *testing.T) cache.Store {
//			return newStoreAgainstAServerStartedForThisTest(t)
//		})
//	}
//
// The factory takes the *testing.T so an adapter can skip when its server is
// not available, and is called once per subtest so no subtest can be affected
// by what another one left behind.
//
// The expiry subtests pin support's clock and travel it past the ttl, so a
// test that runs the suite must not run in parallel with another that reads
// support.Now. A store whose entries expire on a server's clock is handed in
// wrapped in a [ServerClock], which waits for real.
//
// It is named for net/http/httptest: a package whose name ends in "test" is a
// package that imports testing, and every consumer of this one is a test.
package cachetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/support"
)

// expiry is the ttl the expiry subtests wait out.
//
// A store that reads the time through support.Now -- the array, file and
// database stores -- is waited out by travelling with the fake clock, so the
// value costs nothing. It is still short because a store whose entries expire
// on a server's clock waits it out for real, through [ServerClock].
const expiry = 80 * time.Millisecond

// ServerClock is implemented by a store under test whose entries expire on a
// clock support.Now does not move -- a server's, which no test can travel.
//
// The suite asks every store it is handed for it. One that has it is waited
// out by Elapse; every other one is waited out by pinning support's clock for
// the subtest and travelling it past the ttl, which is instant and does not
// depend on how loaded the machine is. The store itself never implements it:
// the adapter's test wraps the store it returns in a type that does, so the
// production interface carries no testing seam.
type ServerClock interface {
	// Elapse returns once d has passed on the store's clock.
	Elapse(d time.Duration)
}

// clockFor answers how a subtest moves the store's time forward. For a store
// that reads support.Now it freezes that clock for the subtest, so every ttl
// is computed from a known instant, and puts back what was pinned before when
// the subtest ends.
func clockFor(t *testing.T, s any) func(time.Duration) {
	t.Helper()
	if c, ok := s.(ServerClock); ok {
		return c.Elapse
	}
	previous := support.GetTestNow()
	support.FreezeTime()
	t.Cleanup(func() { support.SetTestNow(previous) })
	return func(d time.Duration) { support.Travel(d) }
}

// Run checks a Store against the contract Repository depends on.
func Run(t *testing.T, newStore func(*testing.T) cache.Store) {
	t.Helper()

	t.Run("GetOfAnAbsentKeyIsErrNotFound", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Get(context.Background(), "absent"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("Get on an absent key = %v, want cache.ErrNotFound", err)
		}
	})

	t.Run("PutThenGet", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		if err := s.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(got, []byte("v")) {
			t.Fatalf("Get = %q, want %q", got, "v")
		}
	})

	t.Run("PutReplaces", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		mustPut(t, s, "k", "first")
		mustPut(t, s, "k", "second")

		got, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(got, []byte("second")) {
			t.Fatalf("Get = %q, want the second value", got)
		}
	})

	t.Run("AValueDoesNotOutliveItsTTL", func(t *testing.T) {
		s := newStore(t)
		elapse := clockFor(t, s)
		ctx := context.Background()

		if err := s.Put(ctx, "k", []byte("v"), expiry); err != nil {
			t.Fatalf("Put: %v", err)
		}
		elapse(2 * expiry)

		if _, err := s.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("Get after the ttl = %v, want cache.ErrNotFound", err)
		}
	})

	t.Run("AddOnlyWritesWhenTheKeyIsFree", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		ok, err := s.Add(ctx, "k", []byte("first"), time.Minute)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if !ok {
			t.Fatal("Add on a free key = false, want true")
		}

		ok, err = s.Add(ctx, "k", []byte("second"), time.Minute)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if ok {
			t.Fatal("Add on a taken key = true, want false")
		}

		got, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(got, []byte("first")) {
			t.Fatalf("Get = %q, want the first value: a losing Add must not write", got)
		}
	})

	t.Run("AddSucceedsOnceTheKeyHasExpired", func(t *testing.T) {
		s := newStore(t)
		elapse := clockFor(t, s)
		ctx := context.Background()

		if _, err := s.Add(ctx, "k", []byte("first"), expiry); err != nil {
			t.Fatalf("Add: %v", err)
		}
		elapse(2 * expiry)

		ok, err := s.Add(ctx, "k", []byte("second"), time.Minute)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if !ok {
			t.Fatal("Add on an expired key = false, want true")
		}
	})

	t.Run("ForgetRemoves", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		mustPut(t, s, "k", "v")
		if err := s.Forget(ctx, "k"); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		if _, err := s.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("Get after Forget = %v, want cache.ErrNotFound", err)
		}
	})

	t.Run("ForgetOfAnAbsentKeyIsNotAnError", func(t *testing.T) {
		s := newStore(t)
		if err := s.Forget(context.Background(), "absent"); err != nil {
			t.Fatalf("Forget on an absent key: %v", err)
		}
	})

	t.Run("IncrementCountsFromZero", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		n, err := s.Increment(ctx, "n", 1, time.Minute)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if n != 1 {
			t.Fatalf("first Increment = %d, want 1", n)
		}

		n, err = s.Increment(ctx, "n", 2, time.Minute)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if n != 3 {
			t.Fatalf("Increment = %d, want 3", n)
		}

		n, err = s.Increment(ctx, "n", -1, time.Minute)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if n != 2 {
			t.Fatalf("Increment by -1 = %d, want 2", n)
		}
	})

	// The property the fixed-window rate limiter rests on. A store that
	// refreshed the deadline on every increment would keep a counter alive for
	// as long as traffic continued, and the window would never close.
	t.Run("IncrementDoesNotRefreshTheExpiry", func(t *testing.T) {
		s := newStore(t)
		elapse := clockFor(t, s)
		ctx := context.Background()

		if _, err := s.Increment(ctx, "n", 1, expiry); err != nil {
			t.Fatalf("Increment: %v", err)
		}
		elapse(expiry / 2)
		if _, err := s.Increment(ctx, "n", 1, expiry); err != nil {
			t.Fatalf("Increment: %v", err)
		}
		// A quarter past the first deadline and a quarter before the one a
		// refreshing store would have moved it to: at exactly one ttl, or
		// at one and a half, both answers are the same and the check proves
		// nothing.
		elapse(expiry * 3 / 4)

		n, err := s.Increment(ctx, "n", 1, expiry)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if n != 1 {
			t.Fatalf("Increment after the window = %d, want 1: the counter kept the deadline it was created with", n)
		}
	})

	t.Run("IncrementReadsWhatItWrote", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		if _, err := s.Increment(ctx, "n", 7, time.Minute); err != nil {
			t.Fatalf("Increment: %v", err)
		}
		got, err := s.Get(ctx, "n")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(got, []byte("7")) {
			t.Fatalf("Get of a counter = %q, want %q: a counter is decimal text, which is what json makes of an integer too", got, "7")
		}
	})

	t.Run("FlushRemovesOnlyThePrefix", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		mustPut(t, s, "one:a", "1")
		mustPut(t, s, "one:b", "2")
		mustPut(t, s, "two:a", "3")

		if err := s.Flush(ctx, "one:"); err != nil {
			t.Fatalf("Flush: %v", err)
		}

		for _, key := range []string{"one:a", "one:b"} {
			if _, err := s.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
				t.Fatalf("Get(%q) after Flush = %v, want cache.ErrNotFound", key, err)
			}
		}
		if _, err := s.Get(ctx, "two:a"); err != nil {
			t.Fatalf("Get outside the flushed prefix: %v", err)
		}
	})

	// A store that handed back the slice it holds would let a caller rewrite
	// what is cached by appending to what it read -- which is a bug that shows
	// up in a completely different module, an hour later.
	t.Run("GetDoesNotHandBackTheStoredBytes", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		mustPut(t, s, "k", "value")

		got, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		for i := range got {
			got[i] = 'x'
		}

		again, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(again, []byte("value")) {
			t.Fatalf("Get after the caller overwrote what it was given = %q, want %q", again, "value")
		}
	})
}

// RunLocking checks the lock half of a store: the two calls cache.Lock makes.
func RunLocking(t *testing.T, newStore func(*testing.T) cache.Locking) {
	t.Helper()

	t.Run("AcquireTakesAFreeLock", func(t *testing.T) {
		s := newStore(t)
		ok, err := s.AcquireLock(context.Background(), "lock:job", "token-a", time.Minute)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if !ok {
			t.Fatal("AcquireLock on a free lock = false, want true")
		}
	})

	t.Run("AcquireRefusesAHeldLock", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		if _, err := s.AcquireLock(ctx, "lock:job", "token-a", time.Minute); err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		ok, err := s.AcquireLock(ctx, "lock:job", "token-b", time.Minute)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if ok {
			t.Fatal("AcquireLock on a held lock = true, want false")
		}
	})

	t.Run("ReleaseFreesTheLock", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		if _, err := s.AcquireLock(ctx, "lock:job", "token-a", time.Minute); err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if err := s.ReleaseLock(ctx, "lock:job", "token-a"); err != nil {
			t.Fatalf("ReleaseLock: %v", err)
		}
		ok, err := s.AcquireLock(ctx, "lock:job", "token-b", time.Minute)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if !ok {
			t.Fatal("AcquireLock after the holder released = false, want true")
		}
	})

	// The property the token exists for: a holder whose lock expired must not
	// be able to release the lock its successor now holds.
	t.Run("ReleaseWithTheWrongTokenDoesNothing", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()

		if _, err := s.AcquireLock(ctx, "lock:job", "token-a", time.Minute); err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if err := s.ReleaseLock(ctx, "lock:job", "token-b"); err != nil {
			t.Fatalf("ReleaseLock with somebody else's token: %v", err)
		}
		ok, err := s.AcquireLock(ctx, "lock:job", "token-c", time.Minute)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if ok {
			t.Fatal("the lock was released by a token that did not hold it")
		}
	})

	t.Run("ReleaseOfALockNobodyHoldsIsNotAnError", func(t *testing.T) {
		s := newStore(t)
		if err := s.ReleaseLock(context.Background(), "lock:absent", "token-a"); err != nil {
			t.Fatalf("ReleaseLock on an absent lock: %v", err)
		}
	})

	// The deadlock protection: a process that dies holding the lock releases it
	// when the ttl expires, and there is no other way out.
	t.Run("ALockDoesNotOutliveItsTTL", func(t *testing.T) {
		s := newStore(t)
		elapse := clockFor(t, s)
		ctx := context.Background()

		if _, err := s.AcquireLock(ctx, "lock:job", "token-a", expiry); err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		elapse(2 * expiry)

		ok, err := s.AcquireLock(ctx, "lock:job", "token-b", time.Minute)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if !ok {
			t.Fatal("AcquireLock after the ttl = false, want true")
		}
	})
}

func mustPut(t *testing.T, s cache.Store, key, value string) {
	t.Helper()
	if err := s.Put(context.Background(), key, []byte(value), time.Minute); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}
