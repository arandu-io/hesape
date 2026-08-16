package limiters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ConcurrencyLimiter is a semaphore: MaxLocks named slots, each held by one
// caller for at most ReleaseAfter.
//
// "Three exports at a time, and the fourth waits", spelled as a limiter.
//
// # No server-side script
//
// Dragonfly, Redis, Valkey and KeyDB stay interchangeable only while nothing
// needs a script, so acquisition is a `SET slot id NX EX n` per slot -- atomic
// on its own, and cheap because the first free slot ends the loop -- and
// release is the WATCH/MULTI/EXEC that RedisStore already uses to release a
// lock.
type ConcurrencyLimiter struct {
	redis        Connection
	name         string
	maxLocks     int64
	releaseAfter time.Duration
}

// NewConcurrencyLimiter builds the limiter.
//
// releaseAfter is how long a slot stays held if the holder never gives it back
// -- a crashed worker must not take a slot out of circulation for good.
func NewConcurrencyLimiter(conn Connection, name string, maxLocks int64, releaseAfter time.Duration) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{redis: conn, name: name, maxLocks: maxLocks, releaseAfter: releaseAfter}
}

// Block waits up to timeout for a slot, runs callback, and gives the slot back.
//
// The slot is released whether callback succeeded or failed: a slot held by a
// returning error is a slot held until releaseAfter expires.
//
// sleepFor is how long to wait between attempts; zero takes the 250ms default.
// A nil callback acquires the slot, releases it and returns.
func (l *ConcurrencyLimiter) Block(ctx context.Context, timeout time.Duration, callback func(context.Context) error, sleepFor time.Duration) error {
	if sleepFor <= 0 {
		sleepFor = 250 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)

	id, err := randomID()
	if err != nil {
		return err
	}

	var slot string
	for {
		slot, err = l.acquire(ctx, id)
		if err != nil {
			return err
		}
		if slot != "" {
			break
		}
		if !time.Now().Before(deadline) {
			return ErrLimiterTimeout
		}
		if !sleep(ctx, sleepFor) {
			return ctx.Err()
		}
	}

	// The release runs on the way out of every path, including a panic in the
	// callback: a panicking handler that keeps its slot narrows the funnel by
	// one for releaseAfter, and nothing says why.
	defer func() { _ = l.release(context.WithoutCancel(ctx), slot, id) }()

	if callback == nil {
		return nil
	}
	return callback(ctx)
}

// acquire attempts to take a slot, and returns the key of the one it took or
// the empty string when every slot was busy.
func (l *ConcurrencyLimiter) acquire(ctx context.Context, id string) (string, error) {
	for i := int64(1); i <= l.maxLocks; i++ {
		slot := l.redis.Key(l.name + strconv.FormatInt(i, 10))

		ok, err := l.redis.Client().SetNX(ctx, slot, id, l.releaseAfter).Result()
		if err != nil {
			return "", err
		}
		if ok {
			return slot, nil
		}
	}
	return "", nil
}

// release gives the slot back, and only if this caller still holds it.
//
// The check is not decoration: without it, a caller whose slot expired under it
// deletes the slot the next caller is holding, and the funnel silently lets one
// extra through for every slow call.
func (l *ConcurrencyLimiter) release(ctx context.Context, slot, id string) error {
	return l.redis.Client().Watch(ctx, func(tx *goredis.Tx) error {
		held, err := tx.Get(ctx, slot).Result()
		if err == goredis.Nil {
			return nil
		}
		if err != nil {
			return err
		}
		if held != id {
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
			p.Del(ctx, slot)
			return nil
		})
		if err == goredis.TxFailedErr {
			// Somebody else changed the slot between the read and the write,
			// which means it is no longer ours to delete.
			return nil
		}
		return err
	}, slot)
}

// randomID is Str::random(20) -- the token that says which caller holds a slot.
func randomID() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ConcurrencyLimiterBuilder configures a ConcurrencyLimiter.
//
// It is what Connection.Funnel returns:
//
//	conn.Funnel("export").Limit(3).Then(ctx, run, nil)
//
// The fields are unexported and the setters are the API, so there is one way to
// configure the limiter rather than two.
type ConcurrencyLimiterBuilder struct {
	connection   Connection
	name         string
	maxLocks     int64
	releaseAfter time.Duration
	timeout      time.Duration
	sleepFor     time.Duration
}

// NewConcurrencyLimiterBuilder builds the builder: sixty seconds of holding,
// three seconds of waiting, 250 milliseconds between attempts.
func NewConcurrencyLimiterBuilder(conn Connection, name string) *ConcurrencyLimiterBuilder {
	return &ConcurrencyLimiterBuilder{
		connection:   conn,
		name:         name,
		releaseAfter: 60 * time.Second,
		timeout:      3 * time.Second,
		sleepFor:     250 * time.Millisecond,
	}
}

// Limit sets the maximum number of locks that can be held at the same time.
func (b *ConcurrencyLimiterBuilder) Limit(maxLocks int64) *ConcurrencyLimiterBuilder {
	b.maxLocks = maxLocks
	return b
}

// ReleaseAfter sets how long until the lock is released automatically.
func (b *ConcurrencyLimiterBuilder) ReleaseAfter(d time.Duration) *ConcurrencyLimiterBuilder {
	b.releaseAfter = d
	return b
}

// Block sets the amount of time to block until a lock is available.
func (b *ConcurrencyLimiterBuilder) Block(timeout time.Duration) *ConcurrencyLimiterBuilder {
	b.timeout = timeout
	return b
}

// Sleep sets how long to wait between lock acquisition attempts.
func (b *ConcurrencyLimiterBuilder) Sleep(d time.Duration) *ConcurrencyLimiterBuilder {
	b.sleepFor = d
	return b
}

// Then executes callback if a lock is obtained, and failure if the wait ran
// out.
//
// A nil failure returns ErrLimiterTimeout to the caller.
func (b *ConcurrencyLimiterBuilder) Then(ctx context.Context, callback func(context.Context) error, failure func(error) error) error {
	err := NewConcurrencyLimiter(b.connection, b.name, b.maxLocks, b.releaseAfter).
		Block(ctx, b.timeout, callback, b.sleepFor)

	if err == ErrLimiterTimeout && failure != nil {
		return failure(err)
	}
	return err
}
