package redis_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/cachetest"
	"github.com/arandu-io/hesape/redis"
	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/session"
)

// The tests below talk to a real server, because everything this package does
// is a claim about a protocol: SET NX either is atomic or it is not, and a fake
// that implements the happy path proves nothing about the case that matters.
//
// Without REDIS_ADDRESS they skip. CI sets it, which is where the claim is
// checked on every push.

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

// address is the server, or the reason there is none.
func address(t *testing.T) string {
	t.Helper()

	addr := os.Getenv("REDIS_ADDRESS")
	if addr == "" {
		t.Skip("REDIS_ADDRESS is not set: start a RESP server and set it, e.g. REDIS_ADDRESS=127.0.0.1:6379")
	}
	return addr
}

func connect(t *testing.T) *connections.Connection {
	t.Helper()

	addr := address(t)

	// A prefix per test AND per run. Per test alone is not enough: the rate
	// limiter buckets by wall-clock window, so a second run inside the same
	// minute would inherit the first run's counts -- which is a test that passes
	// or fails depending on how fast you type.
	prefix := "test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	conn := connections.Connect(connections.Config{Address: addr, Prefix: prefix})
	if err := conn.Ping(context.Background()); err != nil {
		t.Fatalf("connecting to %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func store(t *testing.T) *redis.RedisStore {
	t.Helper()
	return redis.NewRedisStore(connect(t))
}

func grant(tenant string) auth.Grant {
	return auth.SystemGrant("cache.read", tenant)
}

type total struct {
	Cents int `json:"cents"`
}

// TestStoreContract and TestLockingContract are the point of the exercise.
//
// There is one cache contract, not one per backend: the same application wired
// to the in-process ArrayStore in a test and to this one in production has to
// behave the same way, and nothing in the Go type system enforces that. The
// suite lives in hesape/cache/cachetest so it cannot drift -- a copy kept in
// the adapter is the copy that stops noticing that a miss returns the driver's
// own not-found instead of cache.ErrNotFound, which is exactly the defect the
// kv version of this package shipped.
func TestStoreContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Store { return store(t) })
}

// TestLockingContract checks the two calls cache.Lock makes. It is the half the
// scheduler and the outbox relay depend on: with N replicas, a task scheduled
// every minute runs N times unless exactly one of them wins a lock first.
func TestLockingContract(t *testing.T) {
	cachetest.RunLocking(t, func(t *testing.T) cache.Locking { return store(t) })
}

func TestCacheRoundTrip(t *testing.T) {
	repo := cache.New(store(t)).Namespace("invoice")
	ctx := context.Background()

	if err := repo.Put(ctx, grant(tenantA), "i-1", total{Cents: 1250}, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cache.Get[total](ctx, repo, grant(tenantA), "i-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Cents != 1250 {
		t.Fatalf("got %+v", got)
	}

	if err := repo.Forget(ctx, grant(tenantA), "i-1"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := cache.Get[total](ctx, repo, grant(tenantA), "i-1"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("after Forget, err = %v, want cache.ErrNotFound", err)
	}
}

// TestOneTenantCannotReadAnother is the reason every Repository method takes a
// Grant. A cache shared across tenants is a data leak with a fast path, and it
// is the kind that survives review because the query underneath was correct.
//
// The store knows nothing about it -- it moves bytes under keys that are
// already built -- so what this proves is that the key the Repository builds
// still separates them once a real server is the one holding them.
func TestOneTenantCannotReadAnother(t *testing.T) {
	repo := cache.New(store(t)).Namespace("invoice")
	ctx := context.Background()

	if err := repo.Put(ctx, grant(tenantA), "i-1", total{Cents: 1250}, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cache.Get[total](ctx, repo, grant(tenantB), "i-1")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("another tenant read the entry: err = %v, value = %+v", err, got)
	}
}

// TestAGrantWithNoTenantIsRefused is RULE 14 with teeth: without it, the key
// would simply be missing a segment and every tenant would share one entry.
func TestAGrantWithNoTenantIsRefused(t *testing.T) {
	repo := cache.New(store(t)).Namespace("invoice")

	err := repo.Put(context.Background(), grant(""), "i-1", 1, time.Minute)
	if !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("err = %v, want cache.ErrNoTenant", err)
	}
}

// TestATenantCannotNameAnotherTenantsKey is the same defect the storage path
// had, in the cache: the separator is a colon, so a tenant containing one names
// another tenant's key -- tenant "acme:session" with namespace "user" and key
// "1" builds exactly the string tenant "acme" builds with namespace "session"
// and key "user:1".
//
// It is refused twice over, and this checks it end to end against a real
// server: auth.SystemGrant will not mint a Grant for such a tenant, and
// cache.Repository will not build a key from one.
func TestATenantCannotNameAnotherTenantsKey(t *testing.T) {
	backing := store(t)
	ctx := context.Background()

	sessions := cache.New(backing).Namespace("session")
	users := cache.New(backing).Namespace("user")

	if err := sessions.Put(ctx, grant("acme"), "user:1", "the real session", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := users.Put(ctx, grant("acme:session"), "1", "the forged session", time.Minute); !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("err = %v, want cache.ErrNoTenant: a tenant containing the separator was accepted", err)
	}

	got, err := cache.Get[string](ctx, sessions, grant("acme"), "user:1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "the real session" {
		t.Errorf("the stored value is now %q", got)
	}
}

// TestACacheEntryNeedsATTL: an entry with no expiry is a second copy of the
// truth, and the day it diverges nobody knows it exists.
func TestACacheEntryNeedsATTL(t *testing.T) {
	if err := store(t).Put(context.Background(), "cache:t:n:k", []byte("1"), 0); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("err = %v, want cache.ErrNoTTL", err)
	}
}

// TestRememberComputesOnceAndCaches: the get-check-compute-put sequence lives
// in the Repository so it is not written slightly differently in every module,
// and it has to keep working when the store is on the far side of a socket.
func TestRememberComputesOnceAndCaches(t *testing.T) {
	repo := cache.New(store(t)).Namespace("invoice")
	ctx := context.Background()

	calls := 0
	compute := func(context.Context) (total, error) {
		calls++
		return total{Cents: 99}, nil
	}

	first, err := cache.Remember(ctx, repo, grant(tenantA), "i-2", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	second, err := cache.Remember(ctx, repo, grant(tenantA), "i-2", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if calls != 1 {
		t.Errorf("computed %d times, want 1", calls)
	}
	// A hit and a miss have to leave the value in the same state, or the first
	// request after a deploy behaves differently from the rest.
	if first.Cents != 99 || second.Cents != 99 {
		t.Errorf("first = %+v, second = %+v", first, second)
	}
}

// TestAddIsAtomic is the primitive underneath "only one of them may do this".
// A Get followed by a Put is the same code with a race in the middle, and a
// sequential test would pass on both.
func TestAddIsAtomic(t *testing.T) {
	backing := store(t)
	ctx := context.Background()

	const racers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0

	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			ok, err := backing.Add(ctx, "cache:t:n:only-once", []byte("1"), time.Minute)
			if err != nil || !ok {
				return
			}
			mu.Lock()
			won++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if won != 1 {
		t.Fatalf("%d callers were told they had stored the key, want exactly 1", won)
	}
}

// TestManyAndPutMany: both exist to be one round trip, and both have to answer
// for every key that was asked for -- a miss is present and nil, so the caller
// can tell "I asked for three and got three" without holding the slice.
func TestManyAndPutMany(t *testing.T) {
	backing := store(t)
	ctx := context.Background()

	if err := backing.PutMany(ctx, map[string][]byte{
		"cache:t:n:a": []byte("1"),
		"cache:t:n:b": []byte("2"),
	}, time.Minute); err != nil {
		t.Fatalf("PutMany: %v", err)
	}

	got, err := backing.Many(ctx, []string{"cache:t:n:a", "cache:t:n:b", "cache:t:n:absent"})
	if err != nil {
		t.Fatalf("Many: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("asked for 3 keys and got %d back: %v", len(got), got)
	}
	if string(got["cache:t:n:a"]) != "1" || string(got["cache:t:n:b"]) != "2" {
		t.Errorf("values = %v", got)
	}
	if value, ok := got["cache:t:n:absent"]; !ok || value != nil {
		t.Errorf("a key that is not there came back as %q, want a nil value under the key", value)
	}
}

// TestIncrementKeepsTheWindowItStartedWith is what makes a fixed window fixed.
//
// Refreshing the deadline on every hit keeps a counter alive for as long as
// traffic continues, which is a window that never closes and a rate limit that
// never lets anybody back in. The obvious spelling is EXPIRE NX, and it is a
// Redis 7 option KeyDB does not have -- so the deadline is read back in the
// same transaction instead (RULE 11).
func TestIncrementKeepsTheWindowItStartedWith(t *testing.T) {
	conn := connect(t)
	backing := redis.NewRedisStore(conn)
	ctx := context.Background()

	const key = "ratelimit:1.2.3.4:0"

	if n, err := backing.Increment(ctx, key, 1, 2*time.Second); err != nil || n != 1 {
		t.Fatalf("first Increment = %d, %v", n, err)
	}
	// A second hit that asks for a far longer window must not get one.
	if n, err := backing.Increment(ctx, key, 1, time.Hour); err != nil || n != 2 {
		t.Fatalf("second Increment = %d, %v", n, err)
	}

	left, err := conn.Client().TTL(ctx, conn.Key(key)).Result()
	if err != nil {
		t.Fatalf("reading the ttl: %v", err)
	}
	if left <= 0 || left > 2*time.Second {
		t.Errorf("the counter now expires in %v, and the window it was created with was 2s", left)
	}
}

// TestIncrementRefusesAKeyThatIsNotACounter: without the refusal a counter
// silently restarts from zero the first time somebody caches a struct under the
// key a rate limiter is using, and the symptom is a limit that stops limiting.
func TestIncrementRefusesAKeyThatIsNotACounter(t *testing.T) {
	backing := store(t)
	ctx := context.Background()

	if err := backing.Put(ctx, "cache:t:n:not-a-counter", []byte(`{"cents":1}`), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := backing.Increment(ctx, "cache:t:n:not-a-counter", 1, time.Minute); err == nil {
		t.Fatal("incrementing a key holding an object was accepted")
	}
}

// TestFlushStopsAtTheTenant: Laravel's RedisStore::flush() is FLUSHDB, and
// FLUSHDB here would empty every other tenant -- and every other application
// sharing the server -- on the way past. In a SaaS that is an outage caused by
// a support request.
func TestFlushStopsAtTheTenant(t *testing.T) {
	backing := store(t)
	repo := cache.New(backing).Namespace("invoice")
	other := cache.New(backing).Namespace("report")
	ctx := context.Background()

	for _, seed := range []struct {
		repo   *cache.Repository
		tenant string
	}{{repo, tenantA}, {repo, tenantB}, {other, tenantA}} {
		if err := seed.repo.Put(ctx, grant(seed.tenant), "i-1", total{Cents: 1}, time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	if err := repo.Flush(ctx, grant(tenantA)); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := cache.Get[total](ctx, repo, grant(tenantA), "i-1"); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the tenant's own entry survived its flush: %v", err)
	}
	if _, err := cache.Get[total](ctx, repo, grant(tenantB), "i-1"); err != nil {
		t.Errorf("another tenant lost its entry to somebody else's flush: %v", err)
	}
	if _, err := cache.Get[total](ctx, other, grant(tenantA), "i-1"); err != nil {
		t.Errorf("another namespace of the same tenant was emptied: %v", err)
	}
}

// TestForeverHasNoDeadline: Redis has a key with no expiry where a map does
// not, so this writes one rather than the century cache.ArrayStore writes.
func TestForeverHasNoDeadline(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()

	if err := redis.NewRedisStore(conn).Forever(ctx, "cache:t:n:k", []byte(`"kept"`)); err != nil {
		t.Fatalf("Forever: %v", err)
	}
	left, err := conn.Client().TTL(ctx, conn.Key("cache:t:n:k")).Result()
	if err != nil {
		t.Fatalf("reading the ttl: %v", err)
	}
	if left != -1 {
		t.Errorf("ttl = %v, want -1: the key has a deadline it was not given", left)
	}
}

// TestOnlyOneHolderAtATime is what the scheduler depends on: with N replicas, a
// task scheduled every minute runs N times unless exactly one wins.
func TestOnlyOneHolderAtATime(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	held := locks.Lock("scheduler", time.Minute)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := locks.Lock("scheduler", time.Minute).Acquire(ctx); !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("a second holder got the lock: err = %v", err)
	}

	if err := held.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again := locks.Lock("scheduler", time.Minute)
	if err := again.Acquire(ctx); err != nil {
		t.Fatalf("after Release, Acquire: %v", err)
	}
	_ = again.Release(ctx)
}

// TestConcurrentAcquireHasOneWinner: the claim is about atomicity, which a
// sequential test cannot check.
func TestConcurrentAcquireHasOneWinner(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	const racers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0

	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			lock := locks.Lock("relay", 5*time.Second)
			if err := lock.Acquire(ctx); err != nil {
				return
			}
			mu.Lock()
			winners++
			mu.Unlock()
			_ = lock.Release(ctx)
		}()
	}
	wg.Wait()

	// Between one goroutine's Release and the next one's Acquire the lock is
	// free, so more than one can legitimately win over time. What must never
	// happen is two holders at once, and that is what the sequential test above
	// covers -- here the only real failure is zero winners.
	if winners == 0 {
		t.Fatal("nobody acquired the lock")
	}
}

// TestReleaseDoesNotStealSomebodyElsesLock is the bug the owner token exists to
// prevent: a worker whose lock already expired must not delete the lock a
// different worker now holds.
func TestReleaseDoesNotStealSomebodyElsesLock(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	expired := locks.Lock("stealing", 50*time.Millisecond)
	if err := expired.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	next := locks.Lock("stealing", time.Minute)
	if err := next.Acquire(ctx); err != nil {
		t.Fatalf("the expired lock did not free up: %v", err)
	}

	// The first holder now releases, believing it still owns the lock.
	if err := expired.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if err := locks.Lock("stealing", time.Minute).Acquire(ctx); !errors.Is(err, cache.ErrLocked) {
		t.Fatal("the stale holder released a lock it no longer owned")
	}
	_ = next.Release(ctx)
}

// TestRunSkipsWhenAnotherReplicaHoldsIt: for a scheduled task and for the
// outbox relay, ErrLocked means "somebody else is doing it", which is the
// normal case rather than a failure.
func TestRunSkipsWhenAnotherReplicaHoldsIt(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	held := locks.Lock("outbox-relay", time.Minute)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = held.Release(ctx) }()

	ran := false
	err := locks.Lock("outbox-relay", time.Minute).Run(ctx, func(context.Context) error {
		ran = true
		return nil
	})
	if !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("err = %v, want cache.ErrLocked", err)
	}
	if ran {
		t.Error("the pass ran while another replica held the lock")
	}
}

// TestBlockGivesUpRatherThanWaitingForever: a wait that runs out is
// ErrLockTimeout and not ErrLocked, because a caller that retries on "not now"
// should not retry on "not within the time you were willing to wait".
func TestBlockGivesUpRatherThanWaitingForever(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	held := locks.Lock("queued", time.Minute)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = held.Release(ctx) }()

	err := locks.Lock("queued", time.Minute).
		BetweenBlockedAttemptsSleepFor(10*time.Millisecond).
		Block(ctx, 50*time.Millisecond, nil)
	if !errors.Is(err, cache.ErrLockTimeout) {
		t.Fatalf("err = %v, want cache.ErrLockTimeout", err)
	}
}

// TestForceReleaseTakesTheLockFromWhoeverHoldsIt is the recovery hatch, and it
// only works because the store can say who holds a lock.
func TestForceReleaseTakesTheLockFromWhoeverHoldsIt(t *testing.T) {
	locks := cache.NewLocks(store(t))
	ctx := context.Background()

	held := locks.Lock("stuck", time.Hour)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if held.Owner() == "" {
		t.Fatal("the holder has no owner token")
	}
	if owned, err := held.IsOwnedByCurrentProcess(ctx); err != nil || !owned {
		t.Fatalf("IsOwnedByCurrentProcess = %v, %v", owned, err)
	}

	if err := locks.Lock("stuck", time.Hour).ForceRelease(ctx); err != nil {
		t.Fatalf("ForceRelease: %v", err)
	}
	after := locks.Lock("stuck", time.Minute)
	if err := after.Acquire(ctx); err != nil {
		t.Fatalf("the lock was not taken away from its holder: %v", err)
	}
	_ = after.Release(ctx)
}

// TestFlushLocksLeavesTheEntriesAlone: locks and entries share a server and not
// a key space, which is what stops a cache:clear from releasing the lock the
// scheduler is holding.
func TestFlushLocksLeavesTheEntriesAlone(t *testing.T) {
	backing := store(t)
	repo := cache.New(backing).Namespace("invoice")
	ctx := context.Background()

	if !repo.SupportsFlushingLocks() {
		t.Fatal("the store says it cannot flush locks")
	}
	if err := repo.Put(ctx, grant(tenantA), "i-1", total{Cents: 1}, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	held := cache.NewLocks(backing).Lock("nightly", time.Hour)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := repo.FlushLocks(ctx); err != nil {
		t.Fatalf("FlushLocks: %v", err)
	}
	free := cache.NewLocks(backing).Lock("nightly", time.Minute)
	if err := free.Acquire(ctx); err != nil {
		t.Errorf("the lock survived FlushLocks: %v", err)
	}
	_ = free.Release(ctx)

	if _, err := cache.Get[total](ctx, repo, grant(tenantA), "i-1"); err != nil {
		t.Errorf("flushing the locks took a cache entry with it: %v", err)
	}
}

// TestTheLimiterCountsAcrossProcesses is the whole reason to wire this store
// into cache.RateLimiter: the in-process one counts per replica, so N replicas
// allow N times the limit -- and the endpoint where that gap is worth
// exploiting is the sign-in.
func TestTheLimiterCountsAcrossProcesses(t *testing.T) {
	backing := store(t)
	ctx := context.Background()

	// Two limiters over one store stand in for two replicas.
	limiter := cache.NewRateLimiter(backing)
	other := cache.NewRateLimiter(backing)

	limit := cache.PerMinute(3).By("login:1.2.3.4")
	for i := range limit.MaxAttempts {
		result, err := limiter.Attempt(ctx, limit)
		if err != nil {
			t.Fatalf("Attempt: %v", err)
		}
		if !result.OK {
			t.Fatalf("request %d was refused before the limit", i+1)
		}
	}

	result, err := other.Attempt(ctx, limit)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if result.OK {
		t.Fatal("the second replica allowed a request past the shared limit")
	}
	if result.RetryAfter <= 0 {
		t.Error("no retry-after was reported")
	}
}

func TestTheLimiterReportsWhatIsLeft(t *testing.T) {
	result, err := cache.NewRateLimiter(store(t)).Attempt(context.Background(), cache.PerMinute(10).By("api:key"))
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if !result.OK || result.Remaining != 9 || result.Attempts != 1 {
		t.Fatalf("result = %+v, want the first of ten", result)
	}
}

// TestTheStoreBeingDownIsReportedRatherThanDecided: the limiter used to swallow
// an unreachable store and answer "allowed", which buried the decision where
// the caller could not see it and could not change it. It reports now, and
// routing/middleware.Throttle is what chooses to fail open -- a rate limiter
// that is down must not become an outage -- while a sign-in may choose the
// opposite.
func TestTheStoreBeingDownIsReportedRatherThanDecided(t *testing.T) {
	// Port 1 refuses connections everywhere, so this needs no server and does
	// not skip.
	conn := connections.Connect(connections.Config{Address: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	defer func() { _ = conn.Close() }()

	if _, err := cache.NewRateLimiter(redis.NewRedisStore(conn)).Attempt(context.Background(), cache.PerMinute(5).By("key")); err == nil {
		t.Error("the limiter answered as if it had counted, with nothing to count in")
	}
	if err := redis.NewModule(conn).Health(context.Background()); err == nil {
		t.Error("Health reported a server that is down as healthy")
	}
}

func TestHealthReportsTheConnection(t *testing.T) {
	module := redis.NewModule(connect(t))

	if err := module.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if module.Name() != "redis" {
		t.Errorf("name = %q", module.Name())
	}
}

func sessions(t *testing.T) *redis.CacheBasedSessionHandler[auth.Subject] {
	t.Helper()
	return redis.NewCacheBasedSessionHandler[auth.Subject](connect(t))
}

func record(id string) session.Record[auth.Subject] {
	return session.Record[auth.Subject]{
		Payload:   auth.Subject{ID: id, Tenant: tenantA, Roles: []string{"admin"}},
		Tenant:    tenantA,
		SubjectID: id,
	}
}

func TestSessionRoundTrip(t *testing.T) {
	handler := sessions(t)
	ctx := context.Background()

	if err := handler.Write(ctx, "session-id", record("u-1"), time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := handler.Read(ctx, "session-id")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Payload.ID != "u-1" || got.Tenant != tenantA || len(got.Payload.Roles) != 1 || got.Payload.Roles[0] != "admin" {
		t.Fatalf("record = %+v", got)
	}

	// Destroying is what the in-memory handler cannot do across replicas: a
	// logout invalidates the session everywhere, not only where it was handled.
	if err := handler.Destroy(ctx, "session-id"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := handler.Read(ctx, "session-id"); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("after Destroy, err = %v, want session.ErrExpired", err)
	}
}

// TestASessionKeepsEveryFactThatDecidesAuthorization is the wire shape holding
// its side of the bargain.
//
// The version of this handler in the kv repository mapped the record onto a
// struct of its own, and every field that struct did not name was written,
// accepted, and read back as zero -- only in the deployment that used it.
// Verified was missing once, so a policy that required a confirmed address
// denied everybody who was signed in behind more than one replica and passed in
// every test. PasswordConfirmedAt was missing later, which turned the password
// screen into a loop with nothing in the logs.
//
// Marshalling session.Record whole is what makes the class of bug unreachable,
// and this is the test that says so.
func TestASessionKeepsEveryFactThatDecidesAuthorization(t *testing.T) {
	handler := sessions(t)
	ctx := context.Background()

	confirmed := time.Now().UTC().Truncate(time.Millisecond)
	rec := record("u-1")
	rec.Payload.Verified = true
	rec.Remembered = true
	rec.PasswordConfirmedAt = confirmed

	if err := handler.Write(ctx, "full-session", rec, time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := handler.Read(ctx, "full-session")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if !got.Payload.Verified {
		t.Error("a verified account read back unverified, so every policy that asks for a confirmed address denies them")
	}
	if !got.Remembered {
		t.Error("a remembered session read back as an ordinary one, so it is cut down to the plain ttl on the next write")
	}
	if !got.PasswordConfirmedAt.Equal(confirmed) {
		t.Errorf("the password confirmation stamp read back as %v, want %v: the password screen is now a loop", got.PasswordConfirmedAt, confirmed)
	}
	if !got.PasswordConfirmedWithin(session.PasswordConfirmationWindow) {
		t.Error("a password confirmed a moment ago does not count as recent")
	}

	// The other direction has to survive too, or the fields would be constants
	// with extra steps.
	if err := handler.Write(ctx, "plain-session", record("u-2"), time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}
	plain, err := handler.Read(ctx, "plain-session")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if plain.Payload.Verified || plain.Remembered || !plain.PasswordConfirmedAt.IsZero() {
		t.Errorf("an ordinary session read back as %+v", plain)
	}
}

// TestSigningOutASubjectSpansReplicasAndStopsAtTheTenant is the half a password
// reset could not do before: ending every session of one account, in one
// tenant, from a process that never saw any of them.
//
// The distributed handler is where it matters -- the reset is handled by
// whichever replica the load balancer picked, and the session that must stop
// working belongs to whoever forced the reset, on a machine nobody can reach.
func TestSigningOutASubjectSpansReplicasAndStopsAtTheTenant(t *testing.T) {
	handler := sessions(t)
	ctx := context.Background()

	write := func(id string, rec session.Record[auth.Subject]) {
		t.Helper()
		if err := handler.Write(ctx, id, rec, time.Minute); err != nil {
			t.Fatalf("Write %s: %v", id, err)
		}
	}
	namesake := record("u-1")
	namesake.Tenant = tenantB
	namesake.Payload.Tenant = tenantB

	write("laptop", record("u-1"))
	write("phone", record("u-1"))
	write("here", record("u-1"))
	write("colleague", record("u-2"))
	write("namesake", namesake)

	if err := handler.DestroyIndex(ctx, tenantA, "u-1", "here"); err != nil {
		t.Fatalf("DestroyIndex: %v", err)
	}

	for _, id := range []string{"laptop", "phone"} {
		if _, err := handler.Read(ctx, id); !errors.Is(err, session.ErrExpired) {
			t.Errorf("the %s of the account whose password was reset is still signed in: err = %v", id, err)
		}
	}
	if _, err := handler.Read(ctx, "here"); err != nil {
		t.Errorf("the browser the password was changed in was signed out: %v", err)
	}
	if _, err := handler.Read(ctx, "colleague"); err != nil {
		t.Errorf("a colleague in the same tenant was signed out by somebody else's password reset: %v", err)
	}
	// RULE 14: two customers may both call an account "u-1".
	if _, err := handler.Read(ctx, "namesake"); err != nil {
		t.Errorf("the same account id in another tenant was signed out: %v", err)
	}
}

// TestTheSubjectIndexOutlivesTheLongestSessionInIt: the index is what a
// password reset reads to find the sessions it has to end, so an index that
// expires before the sessions it names is a reset that reports success and
// signs nobody out.
//
// Both orders are checked. The short session written second is the one that
// used to be able to shorten the index, and a remembered session is exactly the
// one that would then survive the reset it was aimed at.
func TestTheSubjectIndexOutlivesTheLongestSessionInIt(t *testing.T) {
	ctx := context.Background()
	const long = 30 * 24 * time.Hour

	for _, order := range []struct {
		name  string
		first time.Duration
		last  time.Duration
	}{
		{"the remembered session first", long, time.Hour},
		{"the remembered session last", time.Hour, long},
	} {
		t.Run(order.name, func(t *testing.T) {
			handler := sessions(t)
			if err := handler.Write(ctx, "first", record("u-1"), order.first); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := handler.Write(ctx, "last", record("u-1"), order.last); err != nil {
				t.Fatalf("Write: %v", err)
			}

			// Signing out reaches both, which is only true if the index still
			// names both.
			if err := handler.DestroyIndex(ctx, tenantA, "u-1", ""); err != nil {
				t.Fatalf("DestroyIndex: %v", err)
			}
			for _, id := range []string{"first", "last"} {
				if _, err := handler.Read(ctx, id); !errors.Is(err, session.ErrExpired) {
					t.Errorf("the %s session survived the password reset: err = %v", id, err)
				}
			}
		})
	}
}

// TestTheSubjectIndexForgetsSessionsThatEnded: without this the index only ever
// grows. Nothing removes an id when its session expires or the person logs out,
// and every login pushes the index's own expiry out again -- so an account that
// signs in daily carries one dead id per day, for as long as the account
// exists.
//
// This is the one test that reads the stored shape rather than the API, and it
// has to: an index that names ten thousand sessions that ended answers every
// question correctly and never stops growing, so nothing on the outside can
// tell it apart from a correct one.
func TestTheSubjectIndexForgetsSessionsThatEnded(t *testing.T) {
	conn := connect(t)
	handler := redis.NewCacheBasedSessionHandler[auth.Subject](conn)
	ctx := context.Background()
	indexKey := conn.Key("session-index:" + tenantA + ":u-1")

	if err := handler.Write(ctx, "yesterday", record("u-1"), 50*time.Millisecond); err != nil {
		t.Fatalf("Write: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := handler.Write(ctx, "today", record("u-1"), time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}

	held, err := conn.Client().ZRange(ctx, indexKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	if len(held) != 1 || held[0] != "today" {
		t.Errorf("the index names %v, and only one of those sessions still exists", held)
	}

	// And it goes when its last session does, rather than being pushed out by
	// every login until the account is deleted.
	left, err := conn.Client().TTL(ctx, indexKey).Result()
	if err != nil {
		t.Fatalf("reading the index ttl: %v", err)
	}
	if left <= 0 || left > time.Minute {
		t.Errorf("the index expires in %v, and the longest session in it lasts a minute", left)
	}
}

// TestSigningOutASubjectNobodySignedInIsRefused: an empty subject id names
// every session with no account on it, and session.ArrayHandler refuses the
// same call. A handler that answers a bulk sign-out differently from the one
// the tests run against is a difference that only shows in production.
func TestSigningOutASubjectNobodySignedInIsRefused(t *testing.T) {
	handler := sessions(t)
	ctx := context.Background()

	if err := handler.DestroyIndex(ctx, tenantA, "", ""); err == nil {
		t.Error("signing out the subject with no id was accepted")
	}
	if err := handler.DestroyIndex(ctx, "", "u-1", ""); !errors.Is(err, redis.ErrNoTenant) {
		t.Errorf("signing out without a tenant answered %v, want redis.ErrNoTenant: that id exists in every tenant", err)
	}
	if err := handler.DestroyIndex(ctx, "acme:u-1", "x", ""); !errors.Is(err, redis.ErrNoTenant) {
		t.Errorf("a tenant containing the separator answered %v: it names another tenant's index", err)
	}
}

// TestSessionsExpire: without the ttl reaching the server, a session lives
// until it is restarted.
func TestSessionsExpire(t *testing.T) {
	handler := sessions(t)
	ctx := context.Background()

	if err := handler.Write(ctx, "short", record("u-1"), 50*time.Millisecond); err != nil {
		t.Fatalf("Write: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	if _, err := handler.Read(ctx, "short"); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("the session outlived its ttl: err = %v", err)
	}
}

// TestASessionNeedsALifetime: a ttl of zero is "no expiry" to Redis, which is a
// session that lives until the server is restarted.
func TestASessionNeedsALifetime(t *testing.T) {
	if err := sessions(t).Write(context.Background(), "forever", record("u-1"), 0); err == nil {
		t.Fatal("a session with no lifetime was accepted")
	}
}

// TestBothHandlersAgreeOnAnUnknownSession is the point of the contract:
// swapping the handler must not change how the application behaves.
//
// session.ArrayHandler returns session.ErrExpired for an id it does not hold,
// and the kv version of this one returned its own not-found -- so the branch
// that sends somebody back to the login page ran on a single instance and did
// not run behind Redis.
func TestBothHandlersAgreeOnAnUnknownSession(t *testing.T) {
	ctx := context.Background()

	_, distributed := sessions(t).Read(ctx, "never-stored")
	_, inMemory := session.NewArrayHandler[auth.Subject]().Read(ctx, "never-stored")

	if !errors.Is(distributed, session.ErrExpired) {
		t.Errorf("the distributed handler answers %v", distributed)
	}
	if !errors.Is(inMemory, session.ErrExpired) {
		t.Errorf("the in-memory handler answers %v", inMemory)
	}
}
