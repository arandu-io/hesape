package cache

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/internal/linking"
)

// The tests are in the package because the registry is package state, and
// replacing it for each test is what keeps them independent of each other and
// of whatever a connector module would register on import.

// fakeConnector opens an in-process store and remembers the endpoint it was
// handed, which is everything the registry is responsible for.
type fakeConnector struct {
	driver string
	opened *Endpoint
}

func (c fakeConnector) Driver() string { return c.driver }

func (c fakeConnector) Open(e Endpoint) (SharedStore, error) {
	if c.opened != nil {
		*c.opened = e
	}
	return fakeShared{NewArrayStore()}, nil
}

// fakeShared is the array store with the two connection calls a server-backed
// store adds.
type fakeShared struct{ *ArrayStore }

func (fakeShared) Ping(context.Context) error { return nil }
func (fakeShared) Close() error               { return nil }

func freshRegistry(t *testing.T) {
	t.Helper()
	saved := connectors
	connectors = linking.NewRegistry[Connector]("cache")
	t.Cleanup(func() { connectors = saved })
}

func TestOpenUsesTheRegisteredConnector(t *testing.T) {
	freshRegistry(t)
	var opened Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	store, err := Open("redis", Endpoint{Address: "cache.internal:6379", Prefix: "app"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if store == nil {
		t.Fatal("Open returned no store and no error")
	}
	if opened.Address != "cache.internal:6379" || opened.Prefix != "app" {
		t.Errorf("the connector was handed %+v, want the endpoint Open received", opened)
	}
}

// TestLinkedIsNilForALinkedDriver: the check a boot makes must not stop a
// binary that has what it asked for.
func TestLinkedIsNilForALinkedDriver(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "redis"})

	if err := Linked("CACHE_STORE", "redis"); err != nil {
		t.Errorf("Linked = %v, want nil for a linked driver", err)
	}
}

// TestLinkedNamesTheMissingImport is the reason the registry exists: a binary
// built without the connector says which module to add and where the import
// goes, instead of failing inside the first request.
func TestLinkedNamesTheMissingImport(t *testing.T) {
	freshRegistry(t)

	err := Linked("CACHE_STORE", "redis")
	if err == nil {
		t.Fatal("Linked accepted a driver nothing registered")
	}

	want := "CACHE_STORE asks for redis and no connector for it is linked into this binary (linked: none).\n" +
		"Add it:\n\n" +
		"    go get github.com/arandu-io/hesape/redis\n\n" +
		"and blank-import it in bootstrap/app.go, next to the other connectors:\n\n" +
		"    _ \"github.com/arandu-io/hesape/redis\""
	if err.Error() != want {
		t.Errorf("message =\n%s\n\nwant\n%s", err, want)
	}
}

// TestLinkedNamesTheSettingThatAsked: a session kept in the shared store is
// asked for by SESSION_DRIVER, and a message naming CACHE_STORE would send
// somebody to a line they did not write.
func TestLinkedNamesTheSettingThatAsked(t *testing.T) {
	freshRegistry(t)

	err := Linked("SESSION_DRIVER", "redis")
	if err == nil || !strings.HasPrefix(err.Error(), "SESSION_DRIVER asks for redis ") {
		t.Errorf("Linked = %v, want the sentence to open with the setting that asked", err)
	}
}

// TestLinkedListsWhatIsLinked: the gap between what was asked for and what is
// there is the diagnosis.
func TestLinkedListsWhatIsLinked(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "valkey"})
	Register(fakeConnector{driver: "dragonfly"})

	err := Linked("CACHE_STORE", "redis")
	if err == nil || !strings.Contains(err.Error(), "(linked: dragonfly, valkey).") {
		t.Errorf("Linked = %v, want it to list what is linked, sorted", err)
	}
}

// TestOpenOnAnUnlinkedDriverIsTheLinkedError: two calls answering the same
// question must give the same answer, word for word.
func TestOpenOnAnUnlinkedDriverIsTheLinkedError(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "valkey"})

	store, err := Open("redis", Endpoint{Address: "127.0.0.1:6379"})
	if err == nil {
		t.Fatal("Open succeeded for a driver nothing registered")
	}
	if store != nil {
		t.Errorf("Open returned a store (%v) beside its error", store)
	}
	if want := Linked("CACHE_STORE", "redis"); err.Error() != want.Error() {
		t.Errorf("Open error =\n%s\n\nLinked error =\n%s", err, want)
	}
}

// TestADriverNoModuleProvidesNamesNoCommand: telling somebody to `go get` a
// module that does not exist stacks a second error on the first.
func TestADriverNoModuleProvidesNamesNoCommand(t *testing.T) {
	freshRegistry(t)

	err := Linked("CACHE_STORE", "memcached")
	if err == nil {
		t.Fatal("Linked accepted a driver nothing registered")
	}
	if strings.Contains(err.Error(), "go get") {
		t.Errorf("the message names a command for a module that does not exist:\n%s", err)
	}
}

// TestTwoConnectorsForOneDriverPanic: two imports claiming one value of
// CACHE_STORE is an import nobody meant to add.
func TestTwoConnectorsForOneDriverPanic(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "redis"})

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a second connector for the same driver was accepted")
		}
		if message, _ := recovered.(string); !strings.Contains(message, "remove one of the imports") {
			t.Errorf("the panic does not say what to do: %v", recovered)
		}
	}()
	Register(fakeConnector{driver: "redis"})
}

func TestRegisteredIsSorted(t *testing.T) {
	freshRegistry(t)
	for _, driver := range []string{"valkey", "dragonfly", "redis"} {
		Register(fakeConnector{driver: driver})
	}

	if got := strings.Join(Registered(), ","); got != "dragonfly,redis,valkey" {
		t.Errorf("Registered = %s, want dragonfly,redis,valkey", got)
	}
}

// TestTheConnectorsOpenErrorReachesTheCaller: a connector that refuses an
// endpoint is the one that knows why.
func TestTheConnectorsOpenErrorReachesTheCaller(t *testing.T) {
	freshRegistry(t)
	Register(refusingConnector{})

	if _, err := Open("refusing", Endpoint{}); !errors.Is(err, errRefused) {
		t.Errorf("Open = %v, want the connector's own error", err)
	}
}

var errRefused = errors.New("refused")

type refusingConnector struct{}

func (refusingConnector) Driver() string                     { return "refusing" }
func (refusingConnector) Open(Endpoint) (SharedStore, error) { return nil, errRefused }
