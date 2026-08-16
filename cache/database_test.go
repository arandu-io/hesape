package cache_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/cachetest"
)

func newDatabaseStore(t *testing.T) (*cache.DatabaseStore, *fakeCacheDB) {
	t.Helper()

	db, state := newFakeCacheDB()
	t.Cleanup(func() { _ = db.Close() })

	store := cache.NewDatabaseStore(db, "cache", "", "cache_locks")
	t.Cleanup(func() {
		if left := state.unrecognised(); len(left) > 0 {
			t.Errorf("the store issued statements the fake database does not know: %q", left)
		}
	})
	return store, state
}

// TestDatabaseStoreContract is the point of cachetest again: the store in a
// table answers the same contract as the one in memory.
func TestDatabaseStoreContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Store {
		store, _ := newDatabaseStore(t)
		return store
	})
}

func TestDatabaseStoreForgetTakesTheFlexibleTimestampWithIt(t *testing.T) {
	ctx := context.Background()
	store, state := newDatabaseStore(t)

	if err := store.Put(ctx, "total", []byte("1"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, "flexible:created:total", []byte("2"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Forget(ctx, "total"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	if n := state.rows("cache"); n != 0 {
		t.Fatalf("%d rows left after Forget, want 0: the companion timestamp was left behind", n)
	}
}

func TestDatabaseStoreForgetIfExpiredKeepsALiveEntry(t *testing.T) {
	ctx := context.Background()
	store, state := newDatabaseStore(t)

	if err := store.Put(ctx, "k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.ForgetIfExpired(ctx, "k"); err != nil {
		t.Fatalf("ForgetIfExpired: %v", err)
	}
	if n := state.rows("cache"); n != 1 {
		t.Fatalf("%d rows after ForgetIfExpired on a live entry, want 1", n)
	}
}

// TestDatabaseStoreReadRemovesAnExpiredRow is the only cleanup this store has
// without a scheduled prune: a row nobody reads again is a row that stays, and
// one that is read is one that goes.
func TestDatabaseStoreReadRemovesAnExpiredRow(t *testing.T) {
	ctx := context.Background()
	store, state := newDatabaseStore(t)

	if err := store.Put(ctx, "k", []byte("v"), 40*time.Millisecond); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.Get(ctx, "k"); err != nil {
		t.Fatalf("Get before the ttl ran out: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	if _, err := store.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get after the ttl = %v, want cache.ErrNotFound", err)
	}
	if n := state.rows("cache"); n != 0 {
		t.Fatalf("%d rows left after reading an expired entry, want 0", n)
	}
}

func TestDatabaseStoreRefusesATtlThatIsNotOne(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if err := store.Put(ctx, "k", []byte("v"), -time.Hour); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("Put with a negative ttl = %v, want cache.ErrNoTTL", err)
	}
	if _, err := store.Touch(ctx, "k", 0); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("Touch with no ttl = %v, want cache.ErrNoTTL", err)
	}
}

// TestDatabaseStoreFlushOnlyRemovesThePrefix pins tenant isolation in SQL: one
// tenant's cache:clear must not be a DELETE FROM cache.
func TestDatabaseStoreFlushOnlyRemovesThePrefix(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if err := store.Put(ctx, "cache:acme:default:a", []byte("1"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, "cache:globex:default:a", []byte("2"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Flush(ctx, "cache:acme:"); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := store.Get(ctx, "cache:acme:default:a"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the flushed tenant's entry = %v, want cache.ErrNotFound", err)
	}
	if _, err := store.Get(ctx, "cache:globex:default:a"); err != nil {
		t.Fatalf("the other tenant's entry went with it: %v", err)
	}
}

// TestDatabaseStoreFlushEscapesTheLikeWildcards is the bug the escaping exists
// to stop: a tenant may contain an underscore, and LIKE reads one as "any
// character".
func TestDatabaseStoreFlushEscapesTheLikeWildcards(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if err := store.Put(ctx, "cache:a_b:default:k", []byte("1"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, "cache:axb:default:k", []byte("2"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Flush(ctx, "cache:a_b:"); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := store.Get(ctx, "cache:axb:default:k"); err != nil {
		t.Fatalf("the underscore matched another tenant and took its entry: %v", err)
	}
}

func TestDatabaseStoreMany(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if err := store.Put(ctx, "a", []byte("1"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Many(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Many: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Many returned %d entries for two keys, want 2: every key asked for comes back", len(got))
	}
	if string(got["a"]) != "1" {
		t.Fatalf("Many[a] = %q, want %q", got["a"], "1")
	}
	if got["b"] != nil {
		t.Fatalf("Many[b] = %q, want nil for a miss", got["b"])
	}
}

func TestDatabaseStoreLocksLiveInTheirOwnTable(t *testing.T) {
	ctx := context.Background()
	store, state := newDatabaseStore(t)

	if !store.HasSeparateLockStore() {
		t.Fatal("HasSeparateLockStore = false with two table names")
	}
	if err := store.Put(ctx, "kept", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ok, err := store.AcquireLock(ctx, "job", "owner-1", time.Minute); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}
	if n := state.rows("cache_locks"); n != 1 {
		t.Fatalf("%d rows in the lock table, want 1", n)
	}

	if err := store.FlushLocks(ctx); err != nil {
		t.Fatalf("FlushLocks: %v", err)
	}
	if n := state.rows("cache_locks"); n != 0 {
		t.Fatalf("%d locks left after FlushLocks, want 0", n)
	}
	if n := state.rows("cache"); n != 1 {
		t.Fatalf("FlushLocks emptied the cache table too: %d rows, want 1", n)
	}
}

func TestDatabaseStoreFlushLocksRefusesWithOneTable(t *testing.T) {
	db, _ := newFakeCacheDB()
	t.Cleanup(func() { _ = db.Close() })

	store := cache.NewDatabaseStore(db, "cache", "", "cache")
	if store.HasSeparateLockStore() {
		t.Fatal("HasSeparateLockStore = true with one table name")
	}
	if err := store.FlushLocks(context.Background()); !errors.Is(err, cache.ErrUnsupported) {
		t.Fatalf("FlushLocks = %v, want cache.ErrUnsupported", err)
	}
}

func TestDatabaseStoreLockIsTakenOnce(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if ok, err := store.AcquireLock(ctx, "job", "first", time.Minute); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}
	if ok, err := store.AcquireLock(ctx, "job", "second", time.Minute); err != nil || ok {
		t.Fatalf("a second holder took the lock: %v, %v", ok, err)
	}

	owner, err := store.CurrentOwner(ctx, "job")
	if err != nil {
		t.Fatalf("CurrentOwner: %v", err)
	}
	if owner != "first" {
		t.Fatalf("CurrentOwner = %q, want %q", owner, "first")
	}
}

func TestDatabaseStoreReleaseRefusesSomebodyElsesToken(t *testing.T) {
	ctx := context.Background()
	store, _ := newDatabaseStore(t)

	if ok, err := store.AcquireLock(ctx, "job", "mine", time.Minute); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}
	if err := store.ReleaseLock(ctx, "job", "theirs"); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	owner, err := store.CurrentOwner(ctx, "job")
	if err != nil {
		t.Fatalf("CurrentOwner: %v", err)
	}
	if owner != "mine" {
		t.Fatalf("CurrentOwner = %q, want %q: the wrong token released the lock", owner, "mine")
	}
}

func TestDatabaseStorePruneExpiredLocks(t *testing.T) {
	ctx := context.Background()
	store, state := newDatabaseStore(t)

	if ok, err := store.AcquireLock(ctx, "live", "owner", time.Hour); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}
	if err := store.PruneExpiredLocks(ctx); err != nil {
		t.Fatalf("PruneExpiredLocks: %v", err)
	}
	if n := state.rows("cache_locks"); n != 1 {
		t.Fatalf("PruneExpiredLocks took a live lock: %d rows, want 1", n)
	}
}

func TestDatabaseStorePrefix(t *testing.T) {
	ctx := context.Background()
	db, state := newFakeCacheDB()
	t.Cleanup(func() { _ = db.Close() })

	store := cache.NewDatabaseStore(db, "cache", "app:", "cache_locks")
	if store.GetPrefix() != "app:" {
		t.Fatalf("GetPrefix = %q, want %q", store.GetPrefix(), "app:")
	}
	if err := store.Put(ctx, "k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n := state.rows("cache"); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}

	store.SetPrefix("other:")
	if _, err := store.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get under another prefix = %v, want cache.ErrNotFound", err)
	}
}

func TestDatabaseStoreConnectionNameIsEmptyWhenNobodySaid(t *testing.T) {
	db, _ := newFakeCacheDB()
	t.Cleanup(func() { _ = db.Close() })

	store := cache.NewDatabaseStore(db, "cache", "", "cache_locks")
	if name := store.GetConnectionName(); name != "" {
		t.Fatalf("GetConnectionName = %q, want the empty string: a bare *sql.DB has no name", name)
	}

	store.SetLockConnection(namedConnection{DB: db, name: "locks"})
	if name := store.GetConnectionName(); name != "locks" {
		t.Fatalf("GetConnectionName = %q, want %q", name, "locks")
	}
}

// namedConnection is a connection that knows what it is called.
type namedConnection struct {
	*sql.DB
	name string
}

func (c namedConnection) GetName() string { return c.name }

var _ cache.NamedConnection = namedConnection{}
