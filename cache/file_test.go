package cache_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/cachetest"
)

// TestFileStoreContract is the reason cachetest exists: the store on disk
// answers the same contract as the one in memory, so an application wired to
// either behaves the same way.
func TestFileStoreContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Store {
		return cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)
	})
}

func TestFileStoreSurvivesANewStoreOverTheSameDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	written := cache.NewFileStore(cache.LocalFilesystem{}, dir, 0)
	if err := written.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The point of a file store over an array store: another process reads what
	// this one wrote.
	read := cache.NewFileStore(cache.LocalFilesystem{}, dir, 0)
	got, err := read.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get from a second store: %v", err)
	}
	if string(got) != "v" {
		t.Fatalf("Get = %q, want %q", got, "v")
	}
}

func TestFileStorePathShardsTheHash(t *testing.T) {
	dir := t.TempDir()
	s := cache.NewFileStore(cache.LocalFilesystem{}, dir, 0)

	path := s.Path("cache:acme:default:k")
	rest, ok := strings.CutPrefix(path, dir+string(filepath.Separator))
	if !ok {
		t.Fatalf("Path = %q, want it under %q", path, dir)
	}

	parts := strings.Split(rest, string(filepath.Separator))
	if len(parts) != 3 {
		t.Fatalf("Path = %q, want two directory levels and a file", rest)
	}
	if len(parts[0]) != 2 || len(parts[1]) != 2 {
		t.Fatalf("Path shards = %q and %q, want two characters each", parts[0], parts[1])
	}
	if !strings.HasPrefix(parts[2], parts[0]+parts[1]) {
		t.Fatalf("Path file %q does not begin with its shards %q%q", parts[2], parts[0], parts[1])
	}
}

// TestFileStoreFlushOnlyRemovesThePrefix is RULE 14 with a disk under it: a
// cache:clear for one customer must not touch another's entries, and the file
// store hashes the key into the path -- so the only way it can tell them apart
// is the key it wrote into the file.
func TestFileStoreFlushOnlyRemovesThePrefix(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	if err := s.Put(ctx, "cache:acme:default:a", []byte("1"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(ctx, "cache:globex:default:a", []byte("2"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Flush(ctx, "cache:acme:"); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := s.Get(ctx, "cache:acme:default:a"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the flushed tenant's entry = %v, want cache.ErrNotFound", err)
	}
	got, err := s.Get(ctx, "cache:globex:default:a")
	if err != nil {
		t.Fatalf("the other tenant's entry was taken with it: %v", err)
	}
	if string(got) != "2" {
		t.Fatalf("the other tenant's entry = %q, want %q", got, "2")
	}
}

func TestFileStoreEmptyPrefixFlushesEverything(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	for _, key := range []string{"a", "b", "c"} {
		if err := s.Put(ctx, key, []byte("v"), time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if err := s.Flush(ctx, ""); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	for _, key := range []string{"a", "b", "c"} {
		if _, err := s.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("%q after an empty-prefix flush = %v, want cache.ErrNotFound", key, err)
		}
	}
}

// TestFileStoreAddIsAtomic is the property Add exists for: N racers, one true.
func TestFileStoreAddIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	const racers = 32
	var wg sync.WaitGroup
	won := make(chan struct{}, racers)

	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			added, err := s.Add(ctx, "once", []byte("v"), time.Minute)
			if err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			if added {
				won <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(won)

	if n := len(won); n != 1 {
		t.Fatalf("%d of %d racers added the key, want exactly 1", n, racers)
	}
}

func TestFileStoreAddReplacesAnExpiredEntry(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	if _, err := s.Add(ctx, "k", []byte("old"), 30*time.Millisecond); err != nil {
		t.Fatalf("Add: %v", err)
	}
	time.Sleep(60 * time.Millisecond)

	added, err := s.Add(ctx, "k", []byte("new"), time.Minute)
	if err != nil {
		t.Fatalf("Add after expiry: %v", err)
	}
	if !added {
		t.Fatal("Add over an expired entry = false, want true: an expired key is a free key")
	}
}

func TestFileStoreIncrementKeepsTheOriginalDeadline(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	if _, err := s.Increment(ctx, "hits", 1, 60*time.Millisecond); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := s.Increment(ctx, "hits", 1, time.Hour); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// The second increment asked for an hour and must not have got it: the
	// window belongs to the counter, not to the increment, which is what makes a
	// fixed window fixed.
	if _, err := s.Get(ctx, "hits"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the counter outlived its window: Get = %v, want cache.ErrNotFound", err)
	}
}

func TestFileStoreFlushLocksRefusesWithoutASeparateDirectory(t *testing.T) {
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	if s.HasSeparateLockStore() {
		t.Fatal("HasSeparateLockStore = true with no lock directory set")
	}
	if err := s.FlushLocks(context.Background()); !errors.Is(err, cache.ErrUnsupported) {
		t.Fatalf("FlushLocks = %v, want cache.ErrUnsupported", err)
	}
}

func TestFileStoreFlushLocksLeavesTheEntriesAlone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := cache.NewFileStore(cache.LocalFilesystem{}, dir, 0).SetLockDirectory(filepath.Join(dir, "..", "locks"))

	if err := s.Put(ctx, "kept", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ok, err := s.AcquireLock(ctx, "job", "owner-1", time.Minute); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}

	if err := s.FlushLocks(ctx); err != nil {
		t.Fatalf("FlushLocks: %v", err)
	}

	if _, err := s.Get(ctx, "kept"); err != nil {
		t.Fatalf("FlushLocks took an entry with it: %v", err)
	}
	owner, err := s.CurrentOwner(ctx, "job")
	if err != nil {
		t.Fatalf("CurrentOwner: %v", err)
	}
	if owner != "" {
		t.Fatalf("CurrentOwner after FlushLocks = %q, want the lock gone", owner)
	}
}

func TestFileStoreReleaseLockRefusesSomebodyElsesToken(t *testing.T) {
	ctx := context.Background()
	s := cache.NewFileStore(cache.LocalFilesystem{}, t.TempDir(), 0)

	if ok, err := s.AcquireLock(ctx, "job", "mine", time.Minute); err != nil || !ok {
		t.Fatalf("AcquireLock = %v, %v; want true, nil", ok, err)
	}
	if err := s.ReleaseLock(ctx, "job", "theirs"); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	owner, err := s.CurrentOwner(ctx, "job")
	if err != nil {
		t.Fatalf("CurrentOwner: %v", err)
	}
	if owner != "mine" {
		t.Fatalf("CurrentOwner = %q, want %q: a release with the wrong token released somebody else's lock", owner, "mine")
	}
}

// TestFileStoreDropsAFileItCannotRead keeps a half-written or corrupted file
// from being an entry that can never be removed.
func TestFileStoreDropsAFileItCannotRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := cache.NewFileStore(cache.LocalFilesystem{}, dir, 0)

	if err := s.Put(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := s.Path("k")
	if err := os.WriteFile(path, []byte("junk"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := s.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get of a corrupted entry = %v, want cache.ErrNotFound", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the corrupted file is still there, and nothing will ever remove it")
	}
}
