package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
)

func TestPerMinutesTakesTheWindowFirst(t *testing.T) {
	// The argument order Laravel gives perMinutes, and the one that catches
	// everybody out: the window comes first.
	l := cache.PerMinutes(5, 100)

	if l.Decay != 5*time.Minute {
		t.Fatalf("Decay = %s, want 5m", l.Decay)
	}
	if l.MaxAttempts != 100 {
		t.Fatalf("MaxAttempts = %d, want 100", l.MaxAttempts)
	}
}

func TestPerSecond(t *testing.T) {
	l := cache.PerSecond(3)
	if l.Decay != time.Second || l.MaxAttempts != 3 {
		t.Fatalf("PerSecond(3) = %+v, want three attempts a second", l)
	}
}

func TestNoneAllowsEverythingAnybodyWillEverAsk(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	l := cache.None().By("anyone")

	for range 100 {
		result, err := rl.Attempt(ctx, l)
		if err != nil {
			t.Fatalf("Attempt: %v", err)
		}
		if !result.OK {
			t.Fatal("None refused an attempt")
		}
	}
}

func TestFallbackKeySeparatesTwoLimitsOnOneKey(t *testing.T) {
	minute := cache.PerMinute(60).By("ip:1.2.3.4")
	hour := cache.PerHour(1000).By("ip:1.2.3.4")

	if minute.FallbackKey() == hour.FallbackKey() {
		t.Fatalf("two limits on one key share a fallback key: %q", minute.FallbackKey())
	}
	if got := minute.FallbackKey(); got != "ip:1.2.3.4:attempts:60:decay:60" {
		t.Fatalf("FallbackKey = %q", got)
	}
	if got := cache.PerMinute(60).FallbackKey(); got != "attempts:60:decay:60" {
		t.Fatalf("FallbackKey with no key = %q, want no leading colon", got)
	}
}

// TestLimiterSeparatesDuplicateKeys is the body of RateLimiter::limiter(): two
// limits that resolved to the same key would count in the same counter, and the
// longer window would be spent by the shorter one.
func TestLimiterSeparatesDuplicateKeys(t *testing.T) {
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	rl.For("api", func(context.Context) []cache.Limit {
		return []cache.Limit{
			cache.PerMinute(60).By("ip:1.2.3.4"),
			cache.PerHour(1000).By("ip:1.2.3.4"),
		}
	})

	limits := rl.Limiter("api")(context.Background())
	if len(limits) != 2 {
		t.Fatalf("the limiter returned %d limits, want 2", len(limits))
	}
	if limits[0].Key == limits[1].Key {
		t.Fatalf("both limits still count in %q", limits[0].Key)
	}
}

func TestLimiterLeavesDistinctKeysAlone(t *testing.T) {
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	rl.For("api", func(context.Context) []cache.Limit {
		return []cache.Limit{
			cache.PerMinute(60).By("ip:1.2.3.4"),
			cache.PerHour(1000).By("account:7"),
		}
	})

	limits := rl.Limiter("api")(context.Background())
	if limits[0].Key != "ip:1.2.3.4" || limits[1].Key != "account:7" {
		t.Fatalf("the limiter rewrote keys that did not collide: %q and %q", limits[0].Key, limits[1].Key)
	}
}

func TestLimiterOfAnUnregisteredNameIsNil(t *testing.T) {
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	if rl.Limiter("nobody") != nil {
		t.Fatal("Limiter of an unregistered name returned a resolver")
	}
}

func TestAttemptsAndRemaining(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	l := cache.PerMinute(3).By("ip:1.2.3.4")

	if n, err := rl.Attempts(ctx, l); err != nil || n != 0 {
		t.Fatalf("Attempts on a fresh window = %d, %v; want 0, nil", n, err)
	}
	if n, err := rl.Remaining(ctx, l); err != nil || n != 3 {
		t.Fatalf("Remaining = %d, %v; want 3, nil", n, err)
	}

	if _, err := rl.Hit(ctx, l); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	if n, err := rl.Attempts(ctx, l); err != nil || n != 1 {
		t.Fatalf("Attempts = %d, %v; want 1, nil", n, err)
	}
	if n, err := rl.RetriesLeft(ctx, l); err != nil || n != 2 {
		t.Fatalf("RetriesLeft = %d, %v; want 2, nil", n, err)
	}
}

func TestRemainingNeverGoesBelowZero(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	l := cache.PerMinute(2).By("ip:1.2.3.4")

	for range 5 {
		if _, err := rl.Hit(ctx, l); err != nil {
			t.Fatalf("Hit: %v", err)
		}
	}
	if n, err := rl.Remaining(ctx, l); err != nil || n != 0 {
		t.Fatalf("Remaining after five of two = %d, %v; want 0, nil", n, err)
	}
}

func TestTooManyAttempts(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	l := cache.PerMinute(2).By("ip:1.2.3.4")

	if over, err := rl.TooManyAttempts(ctx, l); err != nil || over {
		t.Fatalf("TooManyAttempts on a fresh window = %v, %v; want false, nil", over, err)
	}
	for range 2 {
		if _, err := rl.Hit(ctx, l); err != nil {
			t.Fatalf("Hit: %v", err)
		}
	}
	if over, err := rl.TooManyAttempts(ctx, l); err != nil || !over {
		t.Fatalf("TooManyAttempts after spending the budget = %v, %v; want true, nil", over, err)
	}
}

func TestResetAttempts(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())
	l := cache.PerMinute(2).By("ip:1.2.3.4")

	if _, err := rl.Hit(ctx, l); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	if err := rl.ResetAttempts(ctx, l); err != nil {
		t.Fatalf("ResetAttempts: %v", err)
	}
	if n, err := rl.Attempts(ctx, l); err != nil || n != 0 {
		t.Fatalf("Attempts after ResetAttempts = %d, %v; want 0, nil", n, err)
	}
}

// TestCleanRateLimiterKeyFolds is the body of
// RateLimiter::cleanRateLimiterKey(), which is two steps that look like
// escaping and are a fold.
func TestCleanRateLimiterKeyFolds(t *testing.T) {
	rl := cache.NewRateLimiter(cache.NewArrayStore())

	for _, c := range []struct{ in, want string }{
		{"plain", "plain"},
		{"jose@example.com", "jose@example.com"},
		{"José", "Jose"},
		{"münchen", "munchen"},
		{"a&b", "aab"},           // & is written &amp; and folds to a.
		{"a<b>c", "albgc"},       // < is &lt; and > is &gt;.
		{`say "hi"`, "say qhiq"}, // the double quote is &quot;.
		{"it's", "it&#039;s"},    // the apostrophe is numeric and does not fold.
		{"m²", "m&sup2;"},        // an entity with a digit does not fold either.
		{"日本", "日本"},             // no entity, left alone.
	} {
		if got := rl.CleanRateLimiterKey(c.in); got != c.want {
			t.Errorf("CleanRateLimiterKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCleanRateLimiterKeyMakesOneCounterOfTwoSpellings is what the fold is for.
func TestCleanRateLimiterKeyMakesOneCounterOfTwoSpellings(t *testing.T) {
	ctx := context.Background()
	rl := cache.NewRateLimiter(cache.NewArrayStore())

	if _, err := rl.Hit(ctx, cache.PerMinute(10).By("José")); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	n, err := rl.Attempts(ctx, cache.PerMinute(10).By("Jose"))
	if err != nil {
		t.Fatalf("Attempts: %v", err)
	}
	if n != 1 {
		t.Fatalf("Attempts for the unaccented spelling = %d, want 1: they are one caller", n)
	}
}

func TestAfterAndResponseAreCarried(t *testing.T) {
	l := cache.PerMinute(5).
		After(func(status int) bool { return status >= 400 }).
		Response(nil)

	if l.AfterCallback == nil {
		t.Fatal("After did not set the callback")
	}
	if !l.AfterCallback(500) || l.AfterCallback(200) {
		t.Fatal("the after callback was not the one that was set")
	}
	if l.MaxAttempts != 5 || l.Decay != time.Minute {
		t.Fatalf("After and Response changed the limit: %+v", l)
	}
}
