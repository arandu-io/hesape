package cache_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/events"
)

func newManager() *cache.CacheManager {
	return cache.NewCacheManager(cache.Config{
		Default: "memory",
		Stores: map[string]cache.StoreConfig{
			"memory": {Driver: "array"},
			"quiet":  {Driver: "null"},
		},
	})
}

func TestManagerKeepsTheStoreItBuilt(t *testing.T) {
	m := newManager()

	first, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	second, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first != second {
		t.Fatal("two calls for one store name built two stores")
	}
	if first.GetStore() != second.GetStore() {
		t.Fatal("two calls for one store name built two backends")
	}
}

func TestManagerStoreWithNoNameIsTheDefault(t *testing.T) {
	m := newManager()

	byName, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	byDefault, err := m.Store("")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if byName != byDefault {
		t.Fatal("the default store is not the one the configuration names")
	}
}

func TestManagerDriverIsStore(t *testing.T) {
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	driver, err := m.Driver("memory")
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if store != driver {
		t.Fatal("Driver and Store answered with different repositories")
	}
}

func TestManagerNamesTheRepositoryAfterTheStore(t *testing.T) {
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if store.GetName() != "memory" {
		t.Fatalf("GetName = %q, want %q: the name is what every event reports", store.GetName(), "memory")
	}
}

func TestManagerRefusesAStoreThatIsNotConfigured(t *testing.T) {
	m := newManager()

	_, err := m.Store("nowhere")
	if err == nil {
		t.Fatal("Store of an undefined name = nil, want an error")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("the error does not name the store: %v", err)
	}
}

func TestManagerNullIsAlwaysAvailable(t *testing.T) {
	m := cache.NewCacheManager(cache.Config{})

	store, err := m.Store("null")
	if err != nil {
		t.Fatalf("Store(\"null\"): %v", err)
	}
	if _, ok := store.GetStore().(*cache.NullStore); !ok {
		t.Fatalf("the null store is a %T", store.GetStore())
	}
	if m.GetDefaultDriver() != "null" {
		t.Fatalf("GetDefaultDriver with nothing configured = %q, want %q", m.GetDefaultDriver(), "null")
	}
}

func TestManagerForgetDriverRebuildsIt(t *testing.T) {
	m := newManager()

	first, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	m.ForgetDriver("memory")

	second, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first == second {
		t.Fatal("ForgetDriver kept the store it was told to forget")
	}
}

func TestManagerForgetDriverWithNoNamesForgetsTheDefault(t *testing.T) {
	m := newManager()

	first, err := m.Store("")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	m.ForgetDriver()

	second, err := m.Store("")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first == second {
		t.Fatal("ForgetDriver with no names did not forget the default")
	}
}

func TestManagerPurgeIsForgetDriverForOne(t *testing.T) {
	m := newManager()

	first, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	m.Purge("memory")

	second, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first == second {
		t.Fatal("Purge kept the store it was told to drop")
	}
}

func TestManagerSetDefaultDriver(t *testing.T) {
	m := newManager()
	m.SetDefaultDriver("quiet")

	if m.GetDefaultDriver() != "quiet" {
		t.Fatalf("GetDefaultDriver = %q, want %q", m.GetDefaultDriver(), "quiet")
	}
	store, err := m.Store("")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, ok := store.GetStore().(*cache.NullStore); !ok {
		t.Fatalf("the default store is a %T, want the null store", store.GetStore())
	}
}

// TestManagerExtendIsHowTheRespStoreArrives is the whole reason Extend is
// exported: the driver that needs a connection lives outside this package.
func TestManagerExtendIsHowTheRespStoreArrives(t *testing.T) {
	m := cache.NewCacheManager(cache.Config{
		Default: "resp",
		Stores:  map[string]cache.StoreConfig{"resp": {Driver: "resp"}},
	})

	built := 0
	m.Extend("resp", func(manager *cache.CacheManager, config cache.StoreConfig) (*cache.Repository, error) {
		built++
		return manager.Repository(cache.NewArrayStore(), config), nil
	})

	store, err := m.Store("resp")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if built != 1 {
		t.Fatalf("the creator ran %d times, want 1", built)
	}
	if store.GetName() != "resp" {
		t.Fatalf("GetName = %q, want %q", store.GetName(), "resp")
	}
}

func TestManagerRefusesADriverItDoesNotKnow(t *testing.T) {
	m := cache.NewCacheManager(cache.Config{
		Stores: map[string]cache.StoreConfig{"odd": {Driver: "carrier-pigeon"}},
	})

	_, err := m.Store("odd")
	if err == nil {
		t.Fatal("an unknown driver built a store")
	}
	if !strings.Contains(err.Error(), "Extend") {
		t.Fatalf("the error does not say how to register one: %v", err)
	}
}

func TestManagerMemoIsShared(t *testing.T) {
	m := newManager()

	first, err := m.Memo("memory")
	if err != nil {
		t.Fatalf("Memo: %v", err)
	}
	second, err := m.Memo("memory")
	if err != nil {
		t.Fatalf("Memo: %v", err)
	}
	if first != second {
		t.Fatal("two calls for one memo store built two maps, which is two caches")
	}
	if _, ok := first.GetStore().(*cache.MemoizedStore); !ok {
		t.Fatalf("Memo built a %T", first.GetStore())
	}
}

func TestManagerMemoDoesNotFireEvents(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	m := newManager().SetEventDispatcher(rec)

	memo, err := m.Memo("memory")
	if err != nil {
		t.Fatalf("Memo: %v", err)
	}
	if err := memo.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n := len(rec.all()); n != 0 {
		t.Fatalf("the memo store fired %d events, want 0: the store underneath already fired them", n)
	}
}

func TestManagerBuildsAFileStore(t *testing.T) {
	dir := t.TempDir()
	m := cache.NewCacheManager(cache.Config{
		Default: "disk",
		Stores:  map[string]cache.StoreConfig{"disk": {Driver: "file", Path: dir}},
	})

	store, err := m.Store("disk")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	file, ok := store.GetStore().(*cache.FileStore)
	if !ok {
		t.Fatalf("the file store is a %T", store.GetStore())
	}
	if file.GetDirectory() != dir {
		t.Fatalf("GetDirectory = %q, want %q", file.GetDirectory(), dir)
	}
}

func TestManagerRefusesAFileStoreWithNoPath(t *testing.T) {
	m := cache.NewCacheManager(cache.Config{
		Stores: map[string]cache.StoreConfig{"disk": {Driver: "file"}},
	})
	if _, err := m.Store("disk"); err == nil {
		t.Fatal("a file store with no directory was built anyway")
	}
}

func TestManagerBuildsAnOndemandStore(t *testing.T) {
	m := newManager()

	store, err := m.Build(cache.StoreConfig{Driver: "array"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if store.GetName() != "ondemand" {
		t.Fatalf("GetName = %q, want %q", store.GetName(), "ondemand")
	}
}

func TestManagerRefreshEventDispatcherReachesAStoreAlreadyBuilt(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	// Built during boot, before the bus exists.
	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if store.GetEventDispatcher() != nil {
		t.Fatal("a store built before the bus already has one")
	}

	rec := &recorder{}
	m.SetEventDispatcher(rec).RefreshEventDispatcher()

	refreshed, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := refreshed.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n := count[*events.KeyWritten](rec); n != 1 {
		t.Fatalf("%d KeyWritten events after RefreshEventDispatcher, want 1", n)
	}
}

func TestManagerBuildsAFailoverStore(t *testing.T) {
	ctx := context.Background()
	m := cache.NewCacheManager(cache.Config{
		Default: "resilient",
		Stores: map[string]cache.StoreConfig{
			"memory":    {Driver: "array"},
			"quiet":     {Driver: "null"},
			"resilient": {Driver: "failover", Stores: []string{"memory", "quiet"}},
		},
	})

	store, err := m.Store("resilient")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, ok := store.GetStore().(*cache.FailoverStore); !ok {
		t.Fatalf("the failover store is a %T", store.GetStore())
	}
	if err := store.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The first store took it, so the value is readable through the set.
	got, err := cache.Get[string](ctx, store, grantFor("acme"), "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "v" {
		t.Fatalf("Get = %q, want %q", got, "v")
	}
}

func TestManagerFlushOnOneTenantLeavesTheOtherAlone(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	acme, globex := grantFor("acme"), grantFor("globex")

	if err := store.Put(ctx, acme, "k", "acme", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, globex, "k", "globex", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Flush(ctx, acme); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := cache.Get[string](ctx, store, acme, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the flushed tenant's entry = %v, want cache.ErrNotFound", err)
	}
	got, err := cache.Get[string](ctx, store, globex, "k")
	if err != nil {
		t.Fatalf("the other tenant's entry went with it: %v", err)
	}
	if got != "globex" {
		t.Fatalf("the other tenant's entry = %q, want %q", got, "globex")
	}
}
