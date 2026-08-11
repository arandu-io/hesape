package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/redis/connections"
	goredis "github.com/redis/go-redis/v9"
)

// scanBatch is how many keys one SCAN pass asks for.
//
// A hint and not a promise -- the server may answer with more or fewer -- and a
// modest one on purpose: SCAN is the cooperative sweep, and asking for a
// hundred thousand keys at a time is how a "clear this tenant's cache" turns
// into a stall everybody else notices.
const scanBatch = 500

// RedisStore is the cache backend over RESP.
//
// It answers Illuminate\Cache\RedisStore, and Illuminate\Cache\RedisLock with
// it: a lock is two atomic operations, and cache.Lock is what turns them into a
// handle. The name stutters as redis.RedisStore, and it is the Illuminate name
// anyway (ADR 0044) -- the developer arriving from Laravel is looking for
// RedisStore.
//
// It is deliberately below the application's vocabulary: it moves bytes under
// keys that are already built, it has never heard of a Grant or a tenant, and
// it does not know what a namespace is. cache.Repository decides which key a
// value belongs under, in one place, where the tenant separator can be got
// right once instead of once per backend (RULE 14).
//
// What it adds to the key it is given is the application prefix from the
// connection, and nothing else.
//
// A RedisStore is safe for concurrent use.
type RedisStore struct {
	conn *connections.Connection
}

// NewRedisStore returns the store over a connection.
func NewRedisStore(c *connections.Connection) *RedisStore { return &RedisStore{conn: c} }

var (
	_ cache.Store         = (*RedisStore)(nil)
	_ cache.Locking       = (*RedisStore)(nil)
	_ cache.CurrentOwner  = (*RedisStore)(nil)
	_ cache.CanFlushLocks = (*RedisStore)(nil)
	_ cache.Taggable      = (*RedisStore)(nil)
)

// Connection returns the connection underneath.
//
// It answers RedisStore::connection(). It is the hatch for a caller that needs
// a command this type does not name, and everything reached through it skips
// the application prefix.
func (s *RedisStore) Connection() *connections.Connection { return s.conn }

// GetPrefix returns the prefix this store puts in front of every key.
//
// It answers RedisStore::getPrefix(). It is the application's prefix; the
// tenant and the namespace are already in the key by the time a Store sees it.
func (s *RedisStore) GetPrefix() string { return s.conn.Prefix() }

// Get returns the stored bytes, or cache.ErrNotFound when the key is absent or
// expired.
//
// cache.ErrNotFound specifically, and not the driver's redis.Nil: callers
// branch on it to mean "compute it", and a Store that returns something else
// makes swapping the backend change the behaviour of the application.
func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	raw, err := s.conn.Client().Get(ctx, s.conn.Key(key)).Bytes()
	if err != nil {
		return nil, translate(err)
	}
	return raw, nil
}

// Many returns the stored bytes for several keys in one round trip.
//
// It answers the many() of the RetrievesMultipleKeys trait, including the part
// that surprises people: every key asked for is in the result, and a key that
// is absent carries a nil value. So a caller can tell "I asked for six and got
// six" without holding the slice it asked with.
//
// One MGET rather than a pipeline of GETs, which is the whole reason the method
// exists.
func (s *RedisStore) Many(ctx context.Context, keys []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(keys))
	if len(keys) == 0 {
		return out, nil
	}

	full := make([]string, len(keys))
	for i, key := range keys {
		full[i] = s.conn.Key(key)
	}

	values, err := s.conn.Client().MGet(ctx, full...).Result()
	if err != nil {
		return nil, translate(err)
	}
	for i, key := range keys {
		if i >= len(values) {
			out[key] = nil
			continue
		}
		switch v := values[i].(type) {
		case string:
			out[key] = []byte(v)
		default:
			// A miss is nil in the answer MGET gives, and nil is what the
			// contract says a miss looks like here.
			out[key] = nil
		}
	}
	return out, nil
}

// Put stores value under key for ttl, replacing whatever was there.
func (s *RedisStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return cache.ErrNoTTL
	}
	return translate(s.conn.Client().Set(ctx, s.conn.Key(key), value, ttl).Err())
}

// PutMany stores several values under one ttl.
//
// It answers the putMany() of the RetrievesMultipleKeys trait. One round trip,
// and not atomic: MSET cannot carry an expiry, so this is a pipeline of SETs
// and a failure part-way through leaves the entries it already wrote. That is
// the right failure for a cache, and it is the same one the PHP has.
func (s *RedisStore) PutMany(ctx context.Context, values map[string][]byte, ttl time.Duration) error {
	if ttl <= 0 {
		return cache.ErrNoTTL
	}
	if len(values) == 0 {
		return nil
	}

	pipe := s.conn.Client().Pipeline()
	for key, value := range values {
		pipe.Set(ctx, s.conn.Key(key), value, ttl)
	}
	_, err := pipe.Exec(ctx)
	return translate(err)
}

// Add stores value only if the key is absent, and reports whether it did.
//
// It answers RedisStore::add(). Laravel needs a Lua script for it because it
// wants the ttl and the conditional write in one statement; SET NX EX is one
// statement and is plain RESP, which is what keeps this package portable across
// the four products (RULE 11).
func (s *RedisStore) Add(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, cache.ErrNoTTL
	}
	ok, err := s.conn.Client().SetNX(ctx, s.conn.Key(key), value, ttl).Result()
	if err != nil {
		return false, translate(err)
	}
	return ok, nil
}

// Forget removes a key. Removing what is not there is not an error: the caller
// wanted the key gone, and it is.
func (s *RedisStore) Forget(ctx context.Context, key string) error {
	return translate(s.conn.Client().Del(ctx, s.conn.Key(key)).Err())
}

// Increment adds delta to the counter under key and returns the new value.
//
// It answers RedisStore::increment(). An absent key starts at zero, so the
// first call returns delta.
//
// The expiry is set when the counter is created and an existing expiry is left
// alone -- that is what makes a fixed window fixed, and refreshing the deadline
// on every hit would keep a counter alive for as long as traffic continues,
// which is a window that never closes.
//
// It reads the ttl back in the same transaction rather than asking for EXPIRE
// NX, which is a Redis 7 option that KeyDB does not have: a counter that keeps
// its window on one product and loses it on another is exactly the divergence
// RULE 11 exists to prevent.
func (s *RedisStore) Increment(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, cache.ErrNoTTL
	}
	full := s.conn.Key(key)

	pipe := s.conn.Client().TxPipeline()
	counter := pipe.IncrBy(ctx, full, delta)
	deadline := pipe.TTL(ctx, full)
	if _, err := pipe.Exec(ctx); err != nil {
		if strings.Contains(err.Error(), "not an integer") {
			// Refusing rather than overwriting, because the alternative is a
			// counter that silently restarts from zero the first time somebody
			// caches a struct under the key a rate limiter is using -- and the
			// symptom of that is a limit that stops limiting.
			return 0, fmt.Errorf("redis: %q holds a value that is not a counter, and Increment would have to overwrite it to answer", key)
		}
		return 0, translate(err)
	}

	// -1 is "the key exists and has no deadline", which after an INCRBY means
	// this call created it. Anything else is a window already running, and it
	// is left where it is.
	//
	// PEXPIRE and not EXPIRE, because EXPIRE counts in whole seconds and a
	// window shorter than one is rounded UP to one -- a limit of five per
	// half-second would have let five through per second, and the contract
	// suite's own sub-second window would have outlived the test that waits it
	// out. Milliseconds cover every window anybody writes.
	if deadline.Val() == -1 {
		if err := s.conn.Client().PExpire(ctx, full, ttl).Err(); err != nil {
			return 0, translate(err)
		}
	}
	return counter.Val(), nil
}

// Decrement subtracts delta from the counter under key and returns the new
// value.
//
// It answers RedisStore::decrement(), and it is Increment with the sign turned
// round, which is what the PHP is.
func (s *RedisStore) Decrement(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return s.Increment(ctx, key, -delta, ttl)
}

// Forever stores a value with no expiry.
//
// It answers RedisStore::forever(), and it writes what Redis has: a key with no
// deadline. cache.ArrayStore writes a century instead, because a map has no
// such thing and a Store is promised a positive ttl everywhere else -- the two
// agree about every deadline anybody will live to see.
//
// Reach for it rarely. An entry that never expires is a second copy of the
// truth, and the day it diverges from the first nothing notices.
func (s *RedisStore) Forever(ctx context.Context, key string, value []byte) error {
	return translate(s.conn.Client().Set(ctx, s.conn.Key(key), value, 0).Err())
}

// Flush removes every key beginning with prefix.
//
// It takes a prefix and not nothing, because the only caller is
// cache.Repository.Flush and a repository owns one tenant's slice of one
// namespace. Laravel's RedisStore::flush() is FLUSHDB, and FLUSHDB here would
// empty every other tenant -- and every other application sharing the server --
// on the way past. In a SaaS that is an outage caused by a support request.
//
// SCAN and DEL, in batches: no KEYS, which blocks the server for as long as it
// takes to walk the whole space, and no Lua (RULE 11).
func (s *RedisStore) Flush(ctx context.Context, prefix string) error {
	if prefix == "" {
		// An empty prefix matches every key on the server. Nothing in the
		// collection asks for that, and the one caller that could reach it by
		// accident -- a Repository whose prefix came back empty -- would take
		// every tenant with it.
		return errors.New("redis: flushing with no prefix would empty every key on the server, including the ones this application does not own")
	}

	match := escapeGlob(s.conn.Key(prefix)) + "*"
	var cursor uint64
	for {
		keys, next, err := s.conn.Client().Scan(ctx, cursor, match, scanBatch).Result()
		if err != nil {
			return translate(err)
		}
		if len(keys) > 0 {
			if err := s.conn.Client().Del(ctx, keys...).Err(); err != nil {
				return translate(err)
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

// AcquireLock stores token under key for ttl if the key is free, and reports
// whether it took it.
//
// It answers RedisLock::acquire(). SET NX EX, which is one statement and needs
// no script.
func (s *RedisStore) AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, cache.ErrNoTTL
	}
	ok, err := s.conn.Client().SetNX(ctx, s.conn.Key(key), token, ttl).Result()
	if err != nil {
		return false, translate(err)
	}
	return ok, nil
}

// ReleaseLock removes key only if it still holds token.
//
// It answers RedisLock::release(). Releasing a lock that expired, or that
// somebody else now holds, is not an error and must not delete anything: that
// is the bug the token exists to prevent, and it is how a worker whose lock
// timed out takes the lock away from its successor.
//
// The compare-and-delete goes through WATCH/MULTI/EXEC rather than through a
// Lua script. The script is the canonical form and would be one line shorter;
// the transaction is plain RESP, and nothing in this package depends on Lua at
// all -- which is what keeps Dragonfly a drop-in replacement.
func (s *RedisStore) ReleaseLock(ctx context.Context, key, token string) error {
	full := s.conn.Key(key)

	err := s.conn.Client().Watch(ctx, func(tx *goredis.Tx) error {
		held, err := tx.Get(ctx, full).Result()
		if errors.Is(err, goredis.Nil) {
			// Already expired. Somebody else may hold it now, and deleting it
			// would be exactly the theft above.
			return nil
		}
		if err != nil {
			return err
		}
		if held != token {
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
			pipe.Del(ctx, full)
			return nil
		})
		return err
	}, full)

	if errors.Is(err, goredis.TxFailedErr) {
		// The value changed between the read and the delete, which means the
		// lock is not ours any more. Nothing to release.
		return nil
	}
	return translate(err)
}

// CurrentOwner returns the token holding the lock, or the empty string when it
// is free or has expired.
//
// It answers RedisLock::getCurrentOwner(). It is what cache.Lock.IsOwnedBy and
// cache.Lock.ForceRelease are built on.
func (s *RedisStore) CurrentOwner(ctx context.Context, key string) (string, error) {
	owner, err := s.conn.Client().Get(ctx, s.conn.Key(key)).Result()
	if errors.Is(err, goredis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", translate(err)
	}
	return owner, nil
}

// FlushLocks removes every lock this store holds, held or not.
//
// It answers Illuminate\Contracts\Cache\CanFlushLocks. It is safe to offer here
// for the reason cache.ArrayStore offers it: locks live in their own key space
// -- cache.Lock names them "lock:..." and entries are "cache:..." -- so
// emptying one cannot touch the other, and a Repository.Flush cannot release
// the lock the scheduler is holding.
//
// It is the recovery hatch, not a tool. A caller reaching for it routinely has
// two holders running at once and does not know it.
func (s *RedisStore) FlushLocks(ctx context.Context) error {
	return s.Flush(ctx, "lock:")
}

// HasSeparateLockStore reports whether locks live apart from entries.
//
// It answers RedisStore::hasSeparateLockStore(), and the answer is always yes
// here: see FlushLocks. Laravel's answer depends on whether a second connection
// was configured, which is a piece of wiring this collection does not have.
func (s *RedisStore) HasSeparateLockStore() bool { return true }

// translate maps a driver error into this collection's vocabulary.
//
// It is the one place that decides what redis.Nil means, so that a backend
// swap cannot change which branch an application takes.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, goredis.Nil):
		return cache.ErrNotFound
	default:
		return fmt.Errorf("redis: %w", err)
	}
}

// escapeGlob makes a key prefix safe to hand to SCAN MATCH.
//
// MATCH takes a glob, and a prefix is not one: a tenant or a namespace is
// limited to letters, digits, - and _ (auth.ValidTenant), but an application's
// own key is not, and Flush is reachable with any prefix at all. Without this,
// flushing the namespace "report[a-z]" would delete the entries of every
// namespace that pattern happens to name.
func escapeGlob(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '^', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
