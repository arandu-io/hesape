package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
)

func TestAttemptCountsUpToTheLimitAndThenRefuses(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerMinute(3).By("203.0.113.1")

	for i := 1; i <= 3; i++ {
		got, err := rl.Attempt(ctx, limit)
		if err != nil {
			t.Fatalf("Attempt %d: %v", i, err)
		}
		if !got.OK {
			t.Fatalf("attempt %d was refused, want allowed", i)
		}
		if got.Attempts != i {
			t.Fatalf("attempt %d counted %d", i, got.Attempts)
		}
		if want := 3 - i; got.Remaining != want {
			t.Fatalf("attempt %d left %d, want %d", i, got.Remaining, want)
		}
	}

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.OK {
		t.Fatal("the fourth attempt was allowed under a limit of three")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
	if got.RetryAfter <= 0 || got.RetryAfter > time.Minute {
		t.Fatalf("RetryAfter = %v, want something inside the window", got.RetryAfter)
	}
}

// TestLimitsDoNotShareKeys: one caller running out of budget must not lock
// everybody else out.
func TestLimitsDoNotShareKeys(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())

	for range 2 {
		if _, err := rl.Attempt(ctx, cache.PerMinute(1).By("one")); err != nil {
			t.Fatalf("Attempt: %v", err)
		}
	}

	got, err := rl.Attempt(ctx, cache.PerMinute(1).By("two"))
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if !got.OK {
		t.Fatal("a second caller was refused on somebody else's budget")
	}
}

// TestHitCountsWithoutAsking is the sign-in shape: the budget is taken before
// the password is checked, and a failure counts without a second question.
func TestHitCountsWithoutAsking(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerMinute(5).By("ana@example.com")

	for i := 1; i <= 3; i++ {
		n, err := rl.Hit(ctx, limit)
		if err != nil {
			t.Fatalf("Hit: %v", err)
		}
		if n != i {
			t.Fatalf("Hit %d = %d", i, n)
		}
	}

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Attempts != 4 {
		t.Fatalf("Attempt counted %d, want 4: Hit and Attempt count in the same place", got.Attempts)
	}
}

func TestReleaseGivesOneAttemptBack(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerMinute(3).By("ana@example.com")

	if _, err := rl.Hit(ctx, limit); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	if _, err := rl.Hit(ctx, limit); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	if err := rl.Release(ctx, limit); err != nil {
		t.Fatalf("Release: %v", err)
	}

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Attempts != 2 {
		t.Fatalf("Attempts after two hits and a release = %d, want 2", got.Attempts)
	}
}

func TestReleaseNeverGoesBelowZero(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerMinute(3).By("ana@example.com")

	// Nothing has been counted, so there is nothing to give back. A counter
	// driven to minus one would hand out a free attempt for the rest of the
	// window.
	if err := rl.Release(ctx, limit); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := rl.Hit(ctx, limit); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	if err := rl.Release(ctx, limit); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := rl.Release(ctx, limit); err != nil {
		t.Fatalf("Release: %v", err)
	}

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1: releasing more than was counted must not create budget", got.Attempts)
	}
}

func TestClearForgetsTheWindow(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerMinute(2).By("ana@example.com")

	for range 3 {
		if _, err := rl.Attempt(ctx, limit); err != nil {
			t.Fatalf("Attempt: %v", err)
		}
	}
	if err := rl.Clear(ctx, limit); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if !got.OK || got.Attempts != 1 {
		t.Fatalf("after Clear: OK = %v, Attempts = %d, want true and 1", got.OK, got.Attempts)
	}
}

// TestTheWindowRolls is the fixed window: the counter belongs to a window, and
// the next window is a new counter rather than a decay of the old one.
func TestTheWindowRolls(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.Limit{Key: "203.0.113.1", MaxAttempts: 1, Decay: 100 * time.Millisecond}

	if _, err := rl.Attempt(ctx, limit); err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	refused, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if refused.OK {
		t.Fatal("the second attempt was allowed under a limit of one")
	}

	time.Sleep(refused.RetryAfter + 20*time.Millisecond)

	got, err := rl.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if !got.OK {
		t.Fatal("the window did not roll: the caller is still refused after RetryAfter")
	}
}

func TestAvailableInStaysInsideTheWindow(t *testing.T) {
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	limit := cache.PerHour(10).By("203.0.113.1")

	got := rl.AvailableIn(limit)
	if got <= 0 || got > time.Hour {
		t.Fatalf("AvailableIn = %v, want something inside the hour", got)
	}
}

func TestTheThreeWindows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit cache.Limit
		want  time.Duration
	}{
		{"PerMinute", cache.PerMinute(1), time.Minute},
		{"PerHour", cache.PerHour(1), time.Hour},
		{"PerDay", cache.PerDay(1), 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.limit.Decay != tc.want {
				t.Fatalf("Decay = %v, want %v", tc.limit.Decay, tc.want)
			}
			if tc.limit.MaxAttempts != 1 {
				t.Fatalf("MaxAttempts = %d, want 1", tc.limit.MaxAttempts)
			}
		})
	}
}

func TestByDoesNotMutateTheLimitItCameFrom(t *testing.T) {
	base := cache.PerMinute(5)
	one := base.By("one")
	two := base.By("two")

	if base.Key != "" {
		t.Fatalf("By wrote back into the limit it was called on: Key = %q", base.Key)
	}
	if one.Key != "one" || two.Key != "two" {
		t.Fatalf("By = %q and %q, want one and two", one.Key, two.Key)
	}
}

func TestALimitThatCannotBeCountedIsRefused(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())

	if _, err := rl.Attempt(ctx, cache.PerMinute(5)); err == nil {
		t.Fatal("a limit with no key = nil, want an error: an empty key limits every caller together")
	}
	if _, err := rl.Attempt(ctx, cache.Limit{Key: "k", MaxAttempts: 5}); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("a limit with no window = %v, want cache.ErrNoTTL", err)
	}
	if _, err := rl.Attempt(ctx, cache.Limit{Key: "k", Decay: time.Minute}); err == nil {
		t.Fatal("a limit of zero attempts = nil, want an error: that is a closed door, not a limit")
	}
}

// TestTheLimiterReportsAStoreThatIsDown rather than deciding what to do about
// it. Failing open is the throttle middleware's call, and it was previously
// made inside the limiter where nothing could see it.
func TestTheLimiterReportsAStoreThatIsDown(t *testing.T) {
	rl := cache.NewRateLimiter(brokenStore{})

	if _, err := rl.Attempt(context.Background(), cache.PerMinute(5).By("k")); !errors.Is(err, errDown) {
		t.Fatalf("Attempt against a store that is down = %v, want the store's error", err)
	}
}
