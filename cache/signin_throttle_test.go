package cache_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/support"
)

const (
	acme  = "acme"
	globe = "globex"
	ana   = "ana@example.com"
	bia   = "bia@example.com"
	home  = "ip:198.51.100.7"
	away  = "ip:203.0.113.9"
)

// small is a policy a test can spend in a few calls: three wrong passwords per
// identity from one address, five per address, in a minute.
func small() auth.ThrottlePolicy {
	return auth.ThrottlePolicy{MaxFailures: 3, MaxFailuresPerClient: 5, Window: time.Minute}
}

func lockout() auth.ThrottlePolicy { return auth.LockoutPolicy() }

func identityWide() auth.ThrottlePolicy {
	p := auth.LockoutPolicy()
	p.IdentityWide = true
	return p
}

func throttleOver(t *testing.T, store cache.ThrottleStore, policy auth.ThrottlePolicy) *cache.SignInThrottle {
	t.Helper()
	th, err := cache.NewSignInThrottle(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func attempts(th auth.SignInThrottle, n int, tenant, identity, client string) (granted int) {
	for i := 0; i < n; i++ {
		if _, ok := th.Attempt(context.Background(), tenant, identity, client); ok {
			granted++
		}
	}
	return granted
}

func allowed(th auth.SignInThrottle, tenant, identity, client string) bool {
	_, ok := th.Attempt(context.Background(), tenant, identity, client)
	return ok
}

// The rules run against every store that can hold a counter and a lock, so a
// store is not trusted to behave like the array one because it implements the
// same interface.
func forEachStore(t *testing.T, run func(t *testing.T, newStore func() cache.ThrottleStore)) {
	t.Run("array", func(t *testing.T) {
		run(t, func() cache.ThrottleStore { return cache.NewArrayStore() })
	})
	t.Run("database", func(t *testing.T) {
		run(t, func() cache.ThrottleStore {
			store, _ := newDatabaseStore(t)
			return store
		})
	})
}

func TestSignInThrottleIsAnAuthSignInThrottle(t *testing.T) {
	var _ auth.SignInThrottle = (*cache.SignInThrottle)(nil)
}

func TestNewSignInThrottleRefusesWhatCannotCount(t *testing.T) {
	if _, err := cache.NewSignInThrottle(nil, small()); err == nil {
		t.Error("a throttle with no store must be refused")
	}
	for _, policy := range []auth.ThrottlePolicy{
		{},
		{MaxFailures: 1, MaxFailuresPerClient: 1},
		{MaxFailures: 0, MaxFailuresPerClient: 1, Window: time.Minute},
		{MaxFailures: 1, MaxFailuresPerClient: 0, Window: time.Minute},
		{MaxFailures: 1, MaxFailuresPerClient: 1, Window: time.Minute, Lockout: -time.Second},
	} {
		if _, err := cache.NewSignInThrottle(cache.NewArrayStore(), policy); err == nil {
			t.Errorf("the incomplete policy %+v must be refused", policy)
		}
	}
}

func TestSignInThrottleBudgetOfOneIdentityFromOneAddress(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())

		if got := attempts(th, 3, acme, ana, home); got != 3 {
			t.Fatalf("the budget is three and %d got through", got)
		}
		retry, ok := th.Attempt(context.Background(), acme, ana, home)
		if ok || retry != time.Minute {
			t.Fatalf("the fourth attempt: ok=%v retry=%v, want a refusal for the whole window", ok, retry)
		}
		// Another address is another budget, and so is another identity.
		if !allowed(th, acme, ana, away) {
			t.Error("a second address shares the first one's budget without IdentityWide")
		}
		if !allowed(th, acme, bia, home) {
			t.Error("a second identity shares the first one's budget")
		}
	})
}

func TestSignInThrottleWindowOpensAgain(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())

		attempts(th, 3, acme, ana, home)
		support.Travel(59 * time.Second)
		retry, ok := th.Attempt(context.Background(), acme, ana, home)
		if ok || retry != time.Second {
			t.Fatalf("a second before the window ends: ok=%v retry=%v", ok, retry)
		}
		support.Travel(time.Second)
		if got := attempts(th, 3, acme, ana, home); got != 3 {
			t.Fatalf("the window ended and %d of three got through", got)
		}
	})
}

func TestSignInThrottleAttemptsInsideTheWindowAddUp(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())

		attempts(th, 2, acme, ana, home)
		support.Travel(30 * time.Second)
		if !allowed(th, acme, ana, home) {
			t.Fatal("the third attempt of the window was refused")
		}
		if allowed(th, acme, ana, home) {
			t.Fatal("a budget spent across two halves of the window was not spent")
		}
	})
}

func TestSignInThrottleLockoutRunsFromTheAttemptThatSpentTheBudget(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		policy := lockout()
		th := throttleOver(t, newStore(), policy)

		if got := attempts(th, policy.MaxFailures-1, acme, ana, home); got != policy.MaxFailures-1 {
			t.Fatalf("%d of %d got through before the budget was spent", got, policy.MaxFailures-1)
		}
		// The last one arrives late in the window: the lockout still lasts its
		// whole length from here, not what was left of the window.
		support.Travel(25 * time.Minute)
		if !allowed(th, acme, ana, home) {
			t.Fatal("the tenth attempt was refused")
		}
		retry, ok := th.Attempt(context.Background(), acme, ana, home)
		if ok || retry != policy.Lockout {
			t.Fatalf("the eleventh attempt: ok=%v retry=%v, want a refusal for %v", ok, retry, policy.Lockout)
		}
		support.Travel(29 * time.Minute)
		if allowed(th, acme, ana, home) {
			t.Fatal("the lockout ended before its length")
		}
		support.Travel(time.Minute)
		if !allowed(th, acme, ana, home) {
			t.Fatal("the lockout never ended")
		}
	})
}

func TestSignInThrottleRefusedAttemptCostsNothingAndDoesNotExtendTheLockout(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), identityWide())

		attempts(th, 10, acme, ana, home)
		support.Travel(20 * time.Minute)
		if got := attempts(th, 200, acme, ana, away); got != 0 {
			t.Fatalf("a locked identity let %d attempts through", got)
		}
		retry, _ := th.Attempt(context.Background(), acme, ana, away)
		if retry != 10*time.Minute {
			t.Fatalf("200 refused attempts moved the end of the lockout: %v left, want 10m", retry)
		}
		support.Travel(10 * time.Minute)
		if !allowed(th, acme, ana, away) {
			t.Fatal("the lockout was extended by the attempts it refused")
		}
		// Nor did the refusals spend the address's own budget.
		if !allowed(th, acme, bia, away) {
			t.Fatal("refused attempts were charged to the address")
		}
	})
}

func TestSignInThrottleIdentityWideSharesOneBudgetAcrossEveryAddress(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), identityWide())

		for i := 0; i < 10; i++ {
			if !allowed(th, acme, ana, fmt.Sprintf("ip:198.51.100.%d", i)) {
				t.Fatalf("attempt %d was refused before the budget was spent", i+1)
			}
		}
		retry, ok := th.Attempt(context.Background(), acme, ana, "ip:203.0.113.200")
		if ok || retry != 30*time.Minute {
			t.Fatalf("an eleventh address: ok=%v retry=%v, want a refusal for 30m", ok, retry)
		}
		if !allowed(th, acme, bia, home) {
			t.Fatal("one locked identity locked another")
		}
	})
}

func TestSignInThrottleWithoutIdentityWideTenAddressesLockNothing(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), lockout())

		for i := 0; i < 20; i++ {
			if !allowed(th, acme, ana, fmt.Sprintf("ip:198.51.100.%d", i)) {
				t.Fatalf("attempt %d from an address of its own was refused", i+1)
			}
		}
	})
}

func TestSignInThrottleOneAddressCannotWalkAListOfIdentities(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())

		for i := 0; i < 5; i++ {
			if !allowed(th, acme, fmt.Sprintf("user%d@example.com", i), home) {
				t.Fatalf("identity %d was refused before the address spent its budget", i)
			}
		}
		retry, ok := th.Attempt(context.Background(), acme, "fresh@example.com", home)
		if ok || retry != time.Minute {
			t.Fatalf("a sixth identity from the same address: ok=%v retry=%v", ok, retry)
		}
		if !allowed(th, acme, "fresh@example.com", away) {
			t.Fatal("the budget of one address reached another")
		}
	})
}

func TestSignInThrottleRefundGivesOneUnitBack(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())
		ctx := context.Background()

		attempts(th, 3, acme, ana, home)
		if allowed(th, acme, ana, home) {
			t.Fatal("the budget should be spent")
		}
		th.Refund(ctx, acme, ana, home)
		if !allowed(th, acme, ana, home) {
			t.Fatal("a refund gave nothing back")
		}
		if allowed(th, acme, ana, home) {
			t.Fatal("a refund gave back more than one unit")
		}
	})
}

func TestSignInThrottleRefundNeverClearsAndNeverGoesBelowNothing(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())
		ctx := context.Background()

		// A hundred refunds of attempts that were never made buy nothing.
		for i := 0; i < 100; i++ {
			th.Refund(ctx, acme, ana, home)
		}
		if got := attempts(th, 10, acme, ana, home); got != 3 {
			t.Fatalf("refunds with nothing to refund bought %d attempts, want 3", got)
		}
		// A hundred abandoned attempts, each refunded, are worth what they cost.
		th = throttleOver(t, newStore(), small())
		attempts(th, 3, acme, bia, home)
		for i := 0; i < 100; i++ {
			th.Refund(ctx, acme, bia, home)
			th.Attempt(ctx, acme, bia, home)
		}
		if allowed(th, acme, bia, home) {
			t.Fatal("an attempt that refunds itself is the way out of the lockout")
		}
	})
}

func TestSignInThrottleClearForgetsTheIdentityAndGivesTheAddressOneBack(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())
		ctx := context.Background()

		attempts(th, 2, acme, ana, home)
		th.Attempt(ctx, acme, ana, home)
		th.Clear(ctx, acme, ana, home)
		if got := attempts(th, 5, acme, ana, home); got != 3 {
			t.Fatalf("after a successful sign-in the identity should have its whole budget, got %d", got)
		}

		// The address was given back exactly one unit, not its whole count: it
		// had spent three of five, got one back, and so has three left.
		th = throttleOver(t, newStore(), small())
		attempts(th, 3, acme, ana, home)
		th.Clear(ctx, acme, ana, home)
		granted := 0
		for i := 0; i < 10; i++ {
			if allowed(th, acme, fmt.Sprintf("other%d@example.com", i), home) {
				granted++
			}
		}
		if granted != 3 {
			t.Fatalf("the address had %d attempts left after a Clear, want 3", granted)
		}
	})
}

func TestSignInThrottleClearUnderIdentityWideForgetsEveryAddress(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), identityWide())

		for i := 0; i < 9; i++ {
			attempts(th, 1, acme, ana, fmt.Sprintf("ip:198.51.100.%d", i))
		}
		th.Attempt(context.Background(), acme, ana, home)
		th.Clear(context.Background(), acme, ana, home)
		if got := attempts(th, 20, acme, ana, away); got != 10 {
			t.Fatalf("the identity should start over with 10, got %d", got)
		}
	})
}

func TestSignInThrottleTenantsDoNotShareAnything(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		store := newStore()
		th := throttleOver(t, store, small())

		attempts(th, 3, acme, ana, home)
		if allowed(th, acme, ana, home) {
			t.Fatal("the budget in acme should be spent")
		}
		if got := attempts(th, 3, globe, ana, home); got != 3 {
			t.Fatalf("one tenant's budget reached another's: %d of three", got)
		}
		// The address's budget is per tenant too.
		th = throttleOver(t, newStore(), small())
		attempts(th, 5, acme, "a@example.com", home)
		if !allowed(th, globe, "b@example.com", home) {
			t.Fatal("one tenant spent the address budget of another")
		}
	})
}

func TestSignInThrottleOneIdentitySpelledInTwoCasesIsOneBudget(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		frozenClock(t)
		th := throttleOver(t, newStore(), small())

		attempts(th, 2, acme, "Ana@Example.com", home)
		attempts(th, 1, acme, "  ana@example.com ", home)
		if allowed(th, acme, "ANA@EXAMPLE.COM", home) {
			t.Fatal("three spellings of one account got four tries")
		}
	})
}

func TestSignInThrottleKeepsNoAddressInClear(t *testing.T) {
	frozenClock(t)
	store := cache.NewArrayStore()
	th := throttleOver(t, store, identityWide())
	th.Attempt(context.Background(), acme, ana, home)
	th.Attempt(context.Background(), acme, bia, away)

	entries := store.All()
	if len(entries) == 0 {
		t.Fatal("nothing was stored")
	}
	for key, entry := range entries {
		if !strings.HasPrefix(key, "arandu:signin-throttle:") {
			t.Errorf("%q is not under the fixed prefix", key)
		}
		for _, secret := range []string{"ana", "bia", "example.com", "198.51.100", "203.0.113", acme} {
			if strings.Contains(key, secret) || strings.Contains(string(entry.Value), secret) {
				t.Errorf("%q or its value carries %q in clear", key, secret)
			}
		}
	}
}

func TestSignInThrottleNothingOutlivesItsWindow(t *testing.T) {
	frozenClock(t)
	store := cache.NewArrayStore()
	th := throttleOver(t, store, small())
	attempts(th, 3, acme, ana, home)
	if len(store.All()) == 0 {
		t.Fatal("nothing was stored")
	}
	support.Travel(time.Minute)
	if left := store.All(); len(left) != 0 {
		t.Fatalf("the store still holds %d counters after the window", len(left))
	}
}

// TestSignInThrottleLetsExactlyTheBudgetThroughUnderABurst is the proof of the
// one indivisible step: two hundred simultaneous guesses at one identity from
// two hundred addresses, a budget of ten.
func TestSignInThrottleLetsExactlyTheBudgetThroughUnderABurst(t *testing.T) {
	forEachStore(t, func(t *testing.T, newStore func() cache.ThrottleStore) {
		policy := identityWide()
		policy.MaxFailuresPerClient = 1000
		th := throttleOver(t, newStore(), policy).LockWait(30 * time.Second)

		var through atomic.Int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if allowed(th, acme, ana, fmt.Sprintf("ip:10.0.%d.%d", i/250, i%250)) {
					through.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if got := through.Load(); got != 10 {
			t.Fatalf("a budget of 10 let %d of 200 simultaneous attempts through", got)
		}
	})
}

func TestSignInThrottleBurstFromOneAddressIsBoundedByItsBudgetToo(t *testing.T) {
	policy := auth.ThrottlePolicy{MaxFailures: 1000, MaxFailuresPerClient: 7, Window: time.Minute}
	th := throttleOver(t, cache.NewArrayStore(), policy).LockWait(30 * time.Second)

	var through atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if allowed(th, acme, fmt.Sprintf("user%d@example.com", i), home) {
				through.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := through.Load(); got != 7 {
		t.Fatalf("an address budget of 7 let %d of 200 through", got)
	}
}

// busyStore is a store whose locks are always held by somebody else.
type busyStore struct{ *cache.ArrayStore }

func (busyStore) AcquireLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func TestSignInThrottleRefusesAndCountsNothingWhenTheLocksCannotBeTaken(t *testing.T) {
	frozenClock(t)
	arr := cache.NewArrayStore()
	th := throttleOver(t, busyStore{arr}, small()).LockWait(40 * time.Millisecond)

	started := time.Now()
	retry, ok := th.Attempt(context.Background(), acme, ana, home)
	if ok {
		t.Fatal("an attempt that could not take its locks was let through")
	}
	if retry <= 0 || retry > 5*time.Second {
		t.Fatalf("the retry-after of a busy refusal should be small and positive, got %v", retry)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("waiting for the locks was not bounded: %v", time.Since(started))
	}
	if left := arr.All(); len(left) != 0 {
		t.Fatalf("a refused attempt wrote %d counters", len(left))
	}
}

func TestSignInThrottleLeavesNoLockHeld(t *testing.T) {
	frozenClock(t)
	arr := cache.NewArrayStore()
	th := throttleOver(t, arr, small())
	// Both locks are free again after every call, or the next one would wait for
	// their expiry.
	for i := 0; i < 20; i++ {
		th.Attempt(context.Background(), acme, ana, home)
	}
	held := cache.NewLocks(arr)
	// Taking a lock on every counter key proves none is left held: the only way
	// to know the keys is to read them off the store.
	for key := range arr.All() {
		lock := held.Lock(key, time.Second)
		if err := lock.Acquire(context.Background()); err != nil {
			t.Fatalf("the lock of %q was left held: %v", key, err)
		}
		_ = lock.Release(context.Background())
	}
}

// signInStore fails reads while Down is set.
type signInStore struct {
	*cache.ArrayStore
	down atomic.Bool
}

var errSignInStoreDown = errors.New("connection refused")

func (s *signInStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.down.Load() {
		return nil, errSignInStoreDown
	}
	return s.ArrayStore.Get(ctx, key)
}

func (s *signInStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s.down.Load() {
		return errSignInStoreDown
	}
	return s.ArrayStore.Put(ctx, key, value, ttl)
}

func TestSignInThrottleCountsInProcessWhenTheStoreFails(t *testing.T) {
	frozenClock(t)
	store := &signInStore{ArrayStore: cache.NewArrayStore()}
	store.down.Store(true)
	th := throttleOver(t, store, small())

	logger, records := log.Capture()
	ctx := log.Into(context.Background(), logger)

	granted := 0
	for i := 0; i < 10; i++ {
		if _, ok := th.Attempt(ctx, acme, ana, home); ok {
			granted++
		}
	}
	if granted != 3 {
		t.Fatalf("with the store down the budget of three let %d through: failing open, or not counting", granted)
	}
	if records.Len() != 1 {
		t.Fatalf("an outage should be one warning, got %d lines", records.Len())
	}
	line := records.All()[0]
	if line.Level.String() != "WARN" || !strings.Contains(fmt.Sprint(line.Attrs["error"]), "connection refused") {
		t.Fatalf("the warning should be at WARN and carry the error: %+v", line)
	}

	// Refund and Clear apply to what was counted in process.
	th.Refund(ctx, acme, ana, home)
	if !allowed(th, acme, ana, home) {
		t.Error("a refund during the outage gave nothing back")
	}
	th.Clear(ctx, acme, ana, home)
	if got := attempts(th, 10, acme, ana, home); got != 3 {
		t.Errorf("a clear during the outage left the identity with %d attempts, want 3", got)
	}

	// The store comes back and is used again.
	store.down.Store(false)
	if got := attempts(th, 10, acme, bia, home); got != 3 {
		t.Errorf("after the store recovered the budget of three let %d through", got)
	}
	if len(store.All()) == 0 {
		t.Error("the store was not asked again after it recovered")
	}
}

func TestSignInThrottleCancelledContextCostsNothing(t *testing.T) {
	frozenClock(t)
	arr := cache.NewArrayStore()
	th := throttleOver(t, arr, small())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, ok := th.Attempt(ctx, acme, ana, home); ok {
		t.Fatal("an attempt with a cancelled context was let through")
	}
	if left := arr.All(); len(left) != 0 {
		t.Fatalf("a cancelled attempt wrote %d counters", len(left))
	}
}

func TestSignInThrottleRefundStillWorksAfterTheRequestWasCancelled(t *testing.T) {
	frozenClock(t)
	th := throttleOver(t, cache.NewArrayStore(), small())
	attempts(th, 3, acme, ana, home)

	// Cancellation is the reason an application calls Refund.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	th.Refund(ctx, acme, ana, home)

	if !allowed(th, acme, ana, home) {
		t.Fatal("a refund called after the request was cancelled gave nothing back")
	}
}

func TestSignInThrottleAContextThatEndsWhileWaitingStopsTheWait(t *testing.T) {
	th := throttleOver(t, busyStore{cache.NewArrayStore()}, small()).LockWait(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, ok := th.Attempt(ctx, acme, ana, home); ok {
		t.Fatal("an attempt that never got its locks was let through")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("the wait ignored the context: %v", time.Since(started))
	}
}

func TestSignInThrottleSurvivesARestart(t *testing.T) {
	frozenClock(t)
	store := cache.NewArrayStore()

	attempts(throttleOver(t, store, small()), 3, acme, ana, home)
	// A new process over the same store: nothing is in the throttle itself.
	if allowed(throttleOver(t, store, small()), acme, ana, home) {
		t.Fatal("a second throttle over the same store did not see what the first counted")
	}
}
