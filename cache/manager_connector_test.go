package cache

import (
	"strings"
	"testing"
)

// TestTheManagerBuildsAConnectorStoreFromItsEndpoint: a store whose driver a
// linked module provides is built from its configuration alone -- the server
// in StoreConfig.Endpoint, the keys under the store's prefix -- so no
// application writes an Extend for it.
func TestTheManagerBuildsAConnectorStoreFromItsEndpoint(t *testing.T) {
	freshRegistry(t)
	var opened Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	m := NewCacheManager(Config{
		Default: "redis",
		Prefix:  "shop",
		Stores: map[string]StoreConfig{
			"redis": {Driver: "redis", Endpoint: Endpoint{Address: "cache.internal:6379", Password: "pw", Database: 5}},
		},
	})

	store, err := m.Store("")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if opened.Address != "cache.internal:6379" || opened.Password != "pw" || opened.Database != 5 {
		t.Fatalf("the connector was handed %+v, want the store's endpoint", opened)
	}
	if opened.Prefix != "shop" {
		t.Fatalf("the connector was handed the prefix %q, want the store's", opened.Prefix)
	}
	if _, ok := store.GetStore().(SharedStore); !ok {
		t.Fatalf("the store is %T, want the shared store the connector opened", store.GetStore())
	}

	shared, err := m.Shared("redis")
	if err != nil || shared != store.GetStore() {
		t.Fatalf("Shared = %v, %v; want the same store Store built", shared, err)
	}
}

// TestAConnectorStoreKeepsTheEndpointsOwnPrefix: a prefix written on the
// endpoint is the one the server namespace uses.
func TestAConnectorStoreKeepsTheEndpointsOwnPrefix(t *testing.T) {
	freshRegistry(t)
	var opened Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	m := NewCacheManager(Config{Prefix: "shop", Stores: map[string]StoreConfig{
		"redis": {Driver: "redis", Endpoint: Endpoint{Address: "a:1", Prefix: "elsewhere"}},
	}})
	if _, err := m.Store("redis"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if opened.Prefix != "elsewhere" {
		t.Fatalf("prefix = %q, want the endpoint's own", opened.Prefix)
	}
}

// TestAConnectorStoreWithNoAddressIsRefused: a server store with nowhere to
// connect is a configuration mistake named at the store, not a client failing
// at the first request.
func TestAConnectorStoreWithNoAddressIsRefused(t *testing.T) {
	freshRegistry(t)
	var opened Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	m := NewCacheManager(Config{Stores: map[string]StoreConfig{"redis": {Driver: "redis"}}})
	if _, err := m.Store("redis"); err == nil || !strings.Contains(err.Error(), "names no endpoint") {
		t.Fatalf("Store with no address answered %v", err)
	}
	if opened != (Endpoint{}) {
		t.Fatalf("the connector was opened with %+v for a store that had no address", opened)
	}
}

// TestAnUnlinkedConnectorNamesTheImport: naming redis in a binary that did not
// import hesape/redis says which import to add.
func TestAnUnlinkedConnectorNamesTheImport(t *testing.T) {
	freshRegistry(t)

	m := NewCacheManager(Config{Stores: map[string]StoreConfig{"redis": {Driver: "redis", Endpoint: Endpoint{Address: "a:1"}}}})
	_, err := m.Store("redis")
	if err == nil || !strings.Contains(err.Error(), "github.com/arandu-io/hesape/redis") {
		t.Fatalf("an unlinked redis store answered %v, want the import named", err)
	}
}

// TestExtendStillWinsOverAConnector: a driver the application registered by
// hand is the one it meant.
func TestExtendStillWinsOverAConnector(t *testing.T) {
	freshRegistry(t)
	var opened Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	m := NewCacheManager(Config{Stores: map[string]StoreConfig{"redis": {Driver: "redis", Endpoint: Endpoint{Address: "a:1"}}}})
	m.Extend("redis", func(m *CacheManager, config StoreConfig) (*Repository, error) {
		return m.Repository(NewArrayStore(), config), nil
	})
	if _, err := m.Store("redis"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if opened != (Endpoint{}) {
		t.Fatal("the connector was opened although Extend registered the driver")
	}
}

// TestSharedRefusesAStoreOnlyThisProcessSees: an array store would satisfy the
// type and hold a lock no other replica sees.
func TestSharedRefusesAStoreOnlyThisProcessSees(t *testing.T) {
	m := NewCacheManager(Config{Stores: map[string]StoreConfig{"memory": {Driver: "array"}}})
	if _, err := m.Shared("memory"); err == nil {
		t.Fatal("Shared answered an array store")
	}
}
