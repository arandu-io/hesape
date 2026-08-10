package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Limit is how many attempts are allowed, over what period, against what key.
//
// It is a value and it is built by composition -- PerMinute(5).By(ip) -- so a
// route declares its limit once, as data, and the same value is what the
// middleware counts against and what the error message is written from.
type Limit struct {
	// Key identifies the caller being limited: an IP, a session id, an account.
	// It is not a tenant: rate limiting happens before authentication on the
	// routes where it matters most, and there is no Grant yet.
	Key string

	// MaxAttempts is how many are allowed within Decay.
	MaxAttempts int

	// Decay is the length of the window.
	Decay time.Duration
}

// PerMinute, PerHour and PerDay build the three limits anybody writes.
//
// They are three functions and not one with a unit argument, because the call
// site reads as the sentence somebody says out loud: "five per minute".
func PerMinute(max int) Limit { return Limit{MaxAttempts: max, Decay: time.Minute} }

// PerHour is PerMinute over an hour.
func PerHour(max int) Limit { return Limit{MaxAttempts: max, Decay: time.Hour} }

// PerDay is PerMinute over a day.
func PerDay(max int) Limit { return Limit{MaxAttempts: max, Decay: 24 * time.Hour} }

// By returns the same limit against a key.
func (l Limit) By(key string) Limit {
	l.Key = key
	return l
}

// RateLimiter counts attempts against limits.
//
// It replaces two implementations that disagreed: an in-memory one in the HTTP
// middleware, which counted per process -- so N replicas allowed N times the
// limit, on the one endpoint where that gap is worth exploiting -- and a second
// one in the kv adapter. There is one now, and which store it counts in is
// wiring.
//
// It does not decide what happens when the store is unreachable. It reports the
// error and the caller chooses: the HTTP throttle middleware fails open,
// because a rate limiter that is down must not become an outage, and a sign-in
// throttle may well choose the opposite. That decision was previously buried
// inside the limiter, where the middleware could not see it and could not
// change it.
type RateLimiter struct {
	store Store
}

// NewRateLimiter returns the limiter.
func NewRateLimiter(s Store) *RateLimiter { return &RateLimiter{store: s} }

// Result is what one Attempt did.
//
// It is a struct and not three return values because the three are read
// together: the throttle middleware writes all of them into response headers,
// and a signature that returned them loose would be four values with an error.
type Result struct {
	// OK says whether this attempt was within the limit. An attempt that was
	// not is still counted -- the flood is the thing being measured.
	OK bool

	// Attempts is how many have been counted in this window, including this
	// one.
	Attempts int

	// Remaining is how many are left, never below zero.
	Remaining int

	// RetryAfter is how long until the window rolls. It is zero when OK.
	RetryAfter time.Duration
}

// Attempt counts one attempt and says whether it fits.
//
// This is the call a throttle makes, and it counts before it answers on
// purpose. A limiter that answered first and counted afterwards would let
// everything that arrived in between through, and the endpoint where that
// matters -- sign-in -- is the one where the budget has to be taken before the
// password is checked, not after.
func (rl *RateLimiter) Attempt(ctx context.Context, l Limit) (Result, error) {
	attempts, err := rl.Hit(ctx, l)
	if err != nil {
		return Result{}, err
	}

	remaining := l.MaxAttempts - attempts
	if remaining < 0 {
		remaining = 0
	}
	if attempts > l.MaxAttempts {
		return Result{Attempts: attempts, Remaining: 0, RetryAfter: rl.AvailableIn(l)}, nil
	}
	return Result{OK: true, Attempts: attempts, Remaining: remaining}, nil
}

// Hit counts one attempt and returns how many there have been in this window.
//
// It is the primitive Attempt is built on, and it is exported because a caller
// that already knows it is going to refuse -- a sign-in that failed, an upload
// that was rejected -- wants to count without asking a question it has already
// answered.
func (rl *RateLimiter) Hit(ctx context.Context, l Limit) (int, error) {
	key, err := rl.key(l)
	if err != nil {
		return 0, err
	}
	n, err := rl.store.Increment(ctx, key, 1, l.Decay)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// Release gives one attempt back.
//
// It is what a success does to a failure counter: five wrong passwords lock the
// account for a minute, and the right one on the second try should not leave
// four attempts standing against somebody who is who they say they are. Use
// Clear to give all of them back.
//
// A counter that has already expired stays absent rather than being recreated
// at minus one, and a counter driven below zero is clamped back -- both are the
// same rule, which is that a window nobody has attempted anything in holds
// nothing.
func (rl *RateLimiter) Release(ctx context.Context, l Limit) error {
	key, err := rl.key(l)
	if err != nil {
		return err
	}

	switch _, err := rl.store.Get(ctx, key); {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	}

	n, err := rl.store.Increment(ctx, key, -1, l.Decay)
	if err != nil {
		return err
	}
	if n < 0 {
		// Somebody else released between the read and the decrement. Put the
		// counter back on the floor rather than leaving a negative one, which
		// would hand out free attempts for the rest of the window.
		if _, err := rl.store.Increment(ctx, key, -n, l.Decay); err != nil {
			return err
		}
	}
	return nil
}

// Clear forgets the attempts in the current window.
func (rl *RateLimiter) Clear(ctx context.Context, l Limit) error {
	key, err := rl.key(l)
	if err != nil {
		return err
	}
	return rl.store.Forget(ctx, key)
}

// AvailableIn is how long until the current window rolls.
//
// It is arithmetic and not a round trip, which is what the bucketed key buys:
// the window a counter belongs to is in its name, so when it ends is known
// without asking the store how long the key has left.
func (rl *RateLimiter) AvailableIn(l Limit) time.Duration {
	if l.Decay <= 0 {
		return 0
	}
	now := time.Now().UnixNano()
	return time.Duration(int64(l.Decay) - now%int64(l.Decay))
}

// key buckets the counter by window, so a new window is a new key and expiry is
// the only cleanup there is.
//
// The window is fixed rather than sliding: one increment, no script, no sorted
// set. A sliding window would be more precise and would need either Lua -- which
// RULE 11 refuses, because it is what makes Dragonfly a drop-in replacement --
// or a set per key. Precision is not what a rate limit is for; stopping a flood
// is, and a fixed window stops it.
//
// The known cost is the boundary: a caller can spend the whole budget at the end
// of one window and the whole budget at the start of the next. For sign-in
// throttling that is two bursts instead of one, which is not the attack anybody
// is worried about.
func (rl *RateLimiter) key(l Limit) (string, error) {
	if l.Key == "" {
		return "", errors.New("cache: a rate limit needs a key, and an empty one limits every caller together")
	}
	if l.Decay <= 0 {
		return "", fmt.Errorf("%w: the rate limit on %q has no window, and a limit with no window never resets", ErrNoTTL, l.Key)
	}
	if l.MaxAttempts <= 0 {
		return "", fmt.Errorf("cache: the rate limit on %q allows %d attempts, which is a closed door and not a limit", l.Key, l.MaxAttempts)
	}
	bucket := time.Now().UnixNano() / int64(l.Decay)
	return "ratelimit:" + l.Key + ":" + strconv.FormatInt(bucket, 10), nil
}
