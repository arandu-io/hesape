package limiters

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// DurationLimiter is N acquisitions per window.
//
// The first Acquire opens a window of Decay, the next MaxLocks-1 are let
// through, and everything after that is refused until the window rolls over --
// "ten per minute", spelled as a limiter.
//
// # No server-side script
//
// It is WATCH/MULTI/EXEC over a hash with three fields -- start, end, count --
// because Dragonfly, Redis, Valkey and KeyDB stay one product to this
// collection only while nothing needs a script. The optimistic transaction
// retries when another acquirer got there first, which is what a script's
// atomicity would have bought.
type DurationLimiter struct {
	redis    Connection
	name     string
	maxLocks int64
	decay    time.Duration

	// DecaysAt is the Unix time in seconds at which the current window ends.
	//
	// It is exported because a caller prints it: the Retry-After header of a
	// throttled response is this minus now.
	DecaysAt int64

	// Remaining is the number of slots left in the current window.
	Remaining int64
}

// NewDurationLimiter builds the limiter.
//
// decay is the window, and it is a Duration rather than a count of seconds: a
// limiter configured with 60 that meant minutes is a bug nobody sees for a
// week.
func NewDurationLimiter(conn Connection, name string, maxLocks int64, decay time.Duration) *DurationLimiter {
	return &DurationLimiter{redis: conn, name: name, maxLocks: maxLocks, decay: decay}
}

// Acquire attempts to acquire the lock, and reports whether it got it.
//
// It updates DecaysAt and Remaining on every call, hit or miss -- a refused
// caller still needs to know when to come back.
func (l *DurationLimiter) Acquire(ctx context.Context) (bool, error) {
	key := l.redis.Key(l.name)
	seconds := int64(l.decay / time.Second)
	if seconds < 1 {
		seconds = 1
	}

	acquired := false
	err := l.transact(ctx, key, func(tx *goredis.Tx, start, end, count int64) error {
		now := time.Now().Unix()

		// No window, or the window has passed: open a new one holding this
		// acquisition.
		if end == 0 || now < start || now > end {
			_, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.HSet(ctx, key, "start", now, "end", now+seconds, "count", 1)
				p.Expire(ctx, key, 2*time.Duration(seconds)*time.Second)
				return nil
			})
			if err != nil {
				return err
			}
			acquired = true
			l.DecaysAt = now + seconds
			l.Remaining = l.maxLocks - 1
			return nil
		}

		// Inside the window: take a slot and find out whether it was one of the
		// ones on offer. The count is incremented either way, which is what
		// makes the refusal cheap and the arithmetic honest.
		var incr *goredis.IntCmd
		_, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
			incr = p.HIncrBy(ctx, key, "count", 1)
			return nil
		})
		if err != nil {
			return err
		}
		count = incr.Val()
		acquired = count <= l.maxLocks
		l.DecaysAt = end
		l.Remaining = max(0, l.maxLocks-count)
		return nil
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

// TooManyAttempts reports whether the key has been "accessed" too many times,
// without taking a slot.
//
// It updates DecaysAt and Remaining like Acquire does, including when no window
// is open: an answer that is right while the two fields are stale is an answer
// a caller cannot build a Retry-After from.
func (l *DurationLimiter) TooManyAttempts(ctx context.Context) (bool, error) {
	key := l.redis.Key(l.name)
	seconds := int64(l.decay / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	now := time.Now().Unix()

	fields, err := l.redis.Client().HMGet(ctx, key, "start", "end", "count").Result()
	if err != nil {
		return false, err
	}
	start, end, count := toInt(fields[0]), toInt(fields[1]), toInt(fields[2])

	if end == 0 || now < start || now > end {
		l.DecaysAt = now + seconds
		l.Remaining = l.maxLocks
		return false, nil
	}

	l.DecaysAt = end
	l.Remaining = max(0, l.maxLocks-count)
	return l.Remaining <= 0, nil
}

// Clear clears the limiter, so the next Acquire opens a fresh window.
func (l *DurationLimiter) Clear(ctx context.Context) error {
	return l.redis.Client().Del(ctx, l.redis.Key(l.name)).Err()
}

// Block waits up to timeout for a slot and then runs callback.
//
// A nil callback makes it a wait-until-allowed. sleepFor is how long to wait
// between attempts; zero takes the 750ms default.
//
// It returns ErrLimiterTimeout when the wait runs out, and the context's error
// when the caller went away -- a request that was cancelled must stop queueing
// for a slot it will never use.
func (l *DurationLimiter) Block(ctx context.Context, timeout time.Duration, callback func(context.Context) error, sleepFor time.Duration) error {
	if sleepFor <= 0 {
		sleepFor = 750 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)

	for {
		ok, err := l.Acquire(ctx)
		if err != nil {
			return err
		}
		if ok {
			break
		}
		if !time.Now().Before(deadline) {
			return ErrLimiterTimeout
		}
		if !sleep(ctx, sleepFor) {
			return ctx.Err()
		}
	}

	if callback == nil {
		return nil
	}
	return callback(ctx)
}

// transact runs fn inside an optimistic transaction on key, retrying while
// another acquirer wins the race.
//
// It is the atomicity a server-side script would have given, spelled out: WATCH
// the hash, read it, queue the writes, EXEC, and start over if EXEC was
// refused.
func (l *DurationLimiter) transact(ctx context.Context, key string, fn func(tx *goredis.Tx, start, end, count int64) error) error {
	// Ten is enough for any real contention and small enough that a pathological
	// case fails loudly instead of spinning.
	const attempts = 10

	for range attempts {
		err := l.redis.Client().Watch(ctx, func(tx *goredis.Tx) error {
			fields, err := tx.HMGet(ctx, key, "start", "end", "count").Result()
			if err != nil {
				return err
			}
			return fn(tx, toInt(fields[0]), toInt(fields[1]), toInt(fields[2]))
		}, key)

		if err == nil {
			return nil
		}
		if err != goredis.TxFailedErr {
			return err
		}
	}
	return goredis.TxFailedErr
}

// toInt reads one field of the limiter hash. A missing field is zero, which is
// the "no window" case every caller checks for.
func toInt(v any) int64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// DurationLimiterBuilder configures a DurationLimiter.
//
// It is what Connection.Throttle returns, and it exists so the call reads as a
// sentence:
//
//	conn.Throttle("reports").Allow(10).Every(time.Minute).Then(ctx, build, nil)
//
// The fields are unexported and the setters are the API, for the reason
// ConcurrencyLimiterBuilder says.
type DurationLimiterBuilder struct {
	connection Connection
	name       string
	maxLocks   int64
	decay      time.Duration
	timeout    time.Duration
	sleepFor   time.Duration
}

// NewDurationLimiterBuilder builds the builder: three seconds of waiting, 750
// milliseconds between attempts.
func NewDurationLimiterBuilder(conn Connection, name string) *DurationLimiterBuilder {
	return &DurationLimiterBuilder{
		connection: conn,
		name:       name,
		timeout:    3 * time.Second,
		sleepFor:   750 * time.Millisecond,
	}
}

// Allow sets the maximum number of locks that can be obtained per time window.
func (b *DurationLimiterBuilder) Allow(maxLocks int64) *DurationLimiterBuilder {
	b.maxLocks = maxLocks
	return b
}

// Every sets the amount of time the lock window is maintained.
func (b *DurationLimiterBuilder) Every(decay time.Duration) *DurationLimiterBuilder {
	b.decay = decay
	return b
}

// Block sets the amount of time to block until a lock is available.
func (b *DurationLimiterBuilder) Block(timeout time.Duration) *DurationLimiterBuilder {
	b.timeout = timeout
	return b
}

// Sleep sets how long to wait between lock acquisition attempts.
func (b *DurationLimiterBuilder) Sleep(d time.Duration) *DurationLimiterBuilder {
	b.sleepFor = d
	return b
}

// Then executes callback if a lock is obtained, and failure if the wait ran
// out.
//
// A nil failure returns ErrLimiterTimeout to the caller.
func (b *DurationLimiterBuilder) Then(ctx context.Context, callback func(context.Context) error, failure func(error) error) error {
	err := NewDurationLimiter(b.connection, b.name, b.maxLocks, b.decay).
		Block(ctx, b.timeout, callback, b.sleepFor)

	if err == ErrLimiterTimeout && failure != nil {
		return failure(err)
	}
	return err
}
