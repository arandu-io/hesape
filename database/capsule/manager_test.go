package capsule

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// resetGlobals puts the package variables back the way the test found them.
//
// The capsule is a package variable on purpose -- that is what lets a script
// reach the database without carrying a handle -- and a test that leaves one set
// changes the answer of the next test in the file. NewManager sets both of these
// as a side effect, so every test that builds one has to undo it.
func resetGlobals(t *testing.T) {
	t.Helper()

	instanceMu.RLock()
	previous := instance
	instanceMu.RUnlock()
	hook := BootModelLayerUsing

	t.Cleanup(func() {
		instanceMu.Lock()
		instance = previous
		instanceMu.Unlock()
		BootModelLayerUsing = hook
	})
}

// recordingDispatcher is a database.Dispatcher that counts what it was given.
type recordingDispatcher struct{ events []any }

func (d *recordingDispatcher) Dispatch(event any) { d.events = append(d.events, event) }

func TestNewManagerFillsInTheDefaultsWhenTheConfigurationIsEmpty(t *testing.T) {
	resetGlobals(t)

	config := database.MapConfiguration{}
	NewManager(config)

	if got := config.Get("database.default"); got != "default" {
		t.Fatalf("database.default = %#v, want %q", got, "default")
	}
	connections, ok := config.Get("database.connections").(map[string]any)
	if !ok {
		t.Fatalf("database.connections = %#v, want an empty map", config.Get("database.connections"))
	}
	if len(connections) != 0 {
		t.Fatalf("database.connections = %#v, want it empty", connections)
	}
}

// TestNewManagerKeepsAConfigurationThatWasAlreadySet: the defaults fill a gap,
// and a default that overwrote the caller's connection name would send every
// statement to the wrong database with nothing to read that said so.
func TestNewManagerKeepsAConfigurationThatWasAlreadySet(t *testing.T) {
	resetGlobals(t)

	config := database.MapConfiguration{
		"database.default":     "reporting",
		"database.connections": map[string]any{"reporting": map[string]any{"driver": "sqlite"}},
	}
	NewManager(config)

	if got := config.Get("database.default"); got != "reporting" {
		t.Fatalf("database.default = %#v, and the caller had set it", got)
	}
	connections := config.Get("database.connections").(map[string]any)
	if _, kept := connections["reporting"]; !kept {
		t.Fatalf("the configured connections were replaced: %#v", connections)
	}
}

// TestNewManagerWithNoConfigurationTakesAFreshMap rather than writing through a
// nil one, which is a panic on the first Set.
func TestNewManagerWithNoConfigurationTakesAFreshMap(t *testing.T) {
	resetGlobals(t)

	m := NewManager(nil)

	config := m.GetConfiguration()
	if config == nil {
		t.Fatal("the manager holds no configuration")
	}
	if got := config.Get("database.default"); got != "default" {
		t.Fatalf("database.default = %#v", got)
	}
}

// TestNewManagerIsTheGlobalCapsuleAsSoonAsItIsBuilt, which is what makes the
// package-level functions usable without a second call.
func TestNewManagerIsTheGlobalCapsuleAsSoonAsItIsBuilt(t *testing.T) {
	resetGlobals(t)

	m := NewManager(database.MapConfiguration{})

	if Instance() != m {
		t.Fatal("the manager just built is not the one the package-level functions reach")
	}
}

// TestSetAsGlobalReplacesTheCapsule.
func TestSetAsGlobalReplacesTheCapsule(t *testing.T) {
	resetGlobals(t)

	first := NewManager(database.MapConfiguration{})
	second := NewManager(database.MapConfiguration{})
	if Instance() != second {
		t.Fatal("the second manager did not become the global one")
	}

	SetAsGlobal(first)
	if Instance() != first {
		t.Fatal("SetAsGlobal did not replace the capsule")
	}
}

// TestConnectionWithoutACapsuleSaysSoRatherThanPanicking.
//
// The nil instance is the state before NewManager has run, and dereferencing it
// would panic four frames further in, naming a line inside the manager. The
// error names the call that was missing.
func TestConnectionWithoutACapsuleSaysSoRatherThanPanicking(t *testing.T) {
	resetGlobals(t)

	instanceMu.Lock()
	instance = nil
	instanceMu.Unlock()

	conn, err := Connection("default")
	if !errors.Is(err, errNoCapsule) {
		t.Fatalf("Connection answered %v, want the no-capsule error", err)
	}
	if conn != nil {
		t.Fatal("Connection answered a connection beside the error")
	}
	if !strings.Contains(err.Error(), "capsule.NewManager") {
		t.Fatalf("the error is %q, and it has to name the call that was not made", err)
	}
}

// TestTableAndSchemaWithoutACapsuleReportTheSameThing: both reach the database
// through Connection, so neither may find its own way past a missing capsule.
func TestTableAndSchemaWithoutACapsuleReportTheSameThing(t *testing.T) {
	resetGlobals(t)

	instanceMu.Lock()
	instance = nil
	instanceMu.Unlock()

	if _, err := Table("users", "", "default"); !errors.Is(err, errNoCapsule) {
		t.Fatalf("Table answered %v, want the no-capsule error", err)
	}
	if _, err := Table("users", "u", "default"); !errors.Is(err, errNoCapsule) {
		t.Fatalf("Table with an alias answered %v, want the no-capsule error", err)
	}
	if _, err := Schema("default"); !errors.Is(err, errNoCapsule) {
		t.Fatalf("Schema answered %v, want the no-capsule error", err)
	}
}

// TestConnectionReportsAConnectionThatWasNeverConfigured rather than making one
// out of an empty configuration.
func TestConnectionReportsAConnectionThatWasNeverConfigured(t *testing.T) {
	resetGlobals(t)
	NewManager(database.MapConfiguration{})

	if _, err := Connection("reporting"); err == nil {
		t.Fatal("a connection nobody configured was answered")
	}
}

// TestAddConnectionRegistersUnderTheNameGiven, and under "default" when none is.
func TestAddConnectionRegistersUnderTheNameGiven(t *testing.T) {
	resetGlobals(t)

	config := database.MapConfiguration{}
	m := NewManager(config)

	m.AddConnection(map[string]any{"driver": "sqlite"}, "reporting")
	m.AddConnection(map[string]any{"driver": "pgsql"}, "")

	connections, ok := config.Get("database.connections").(map[string]any)
	if !ok {
		t.Fatalf("database.connections = %#v", config.Get("database.connections"))
	}
	if len(connections) != 2 {
		t.Fatalf("the configuration holds %#v, want both connections", connections)
	}
	if _, named := connections["reporting"]; !named {
		t.Fatalf("the named connection is missing: %#v", connections)
	}
	if _, fallback := connections["default"]; !fallback {
		t.Fatalf("the unnamed connection did not land under default: %#v", connections)
	}
}

// TestAddConnectionKeepsWhatWasAlreadyThere: a second call that replaced the map
// would silently drop the first connection, and the failure arrives later as a
// connection that was never configured.
func TestAddConnectionKeepsWhatWasAlreadyThere(t *testing.T) {
	resetGlobals(t)

	config := database.MapConfiguration{
		"database.connections": map[string]any{"legacy": map[string]any{"driver": "mysql"}},
	}
	m := NewManager(config)
	m.AddConnection(map[string]any{"driver": "sqlite"}, "reporting")

	connections := config.Get("database.connections").(map[string]any)
	if _, kept := connections["legacy"]; !kept {
		t.Fatalf("the configured connection was dropped: %#v", connections)
	}
}

// TestBootModelLayerDoesNothingWhenNoORMRegistered.
//
// A binary that never imported the ORM has a nil hook, and doing nothing is the
// correct answer to "boot the model layer I did not link". Calling through the
// nil would panic on a line the caller never wrote.
func TestBootModelLayerDoesNothingWhenNoORMRegistered(t *testing.T) {
	resetGlobals(t)

	BootModelLayerUsing = nil
	NewManager(database.MapConfiguration{}).BootModelLayer()
}

// TestBootModelLayerHandsTheORMTheManagerAndTheDispatcher.
func TestBootModelLayerHandsTheORMTheManagerAndTheDispatcher(t *testing.T) {
	resetGlobals(t)

	m := NewManager(database.MapConfiguration{})
	dispatcher := &recordingDispatcher{}
	m.SetEventDispatcher(dispatcher)

	var gotResolver database.ConnectionResolverInterface
	var gotEvents database.Dispatcher
	var calls int

	BootModelLayerUsing = func(resolver database.ConnectionResolverInterface, events database.Dispatcher) {
		calls++
		gotResolver, gotEvents = resolver, events
	}

	m.BootModelLayer()

	if calls != 1 {
		t.Fatalf("the hook ran %d times, want once", calls)
	}
	if gotResolver != database.ConnectionResolverInterface(m.GetDatabaseManager()) {
		t.Fatal("the hook was given a resolver that is not the capsule's own manager")
	}
	if gotEvents != database.Dispatcher(dispatcher) {
		t.Fatal("the hook was given a dispatcher that is not the one the capsule was set to")
	}
}

// TestSetEventDispatcherIsReadableBackOffTheCapsule.
//
// The capsule keeps its own copy so BootModelLayer can hand it on, which is the
// half asserted here. It also passes the dispatcher down to the manager, and
// that half is not asserted: DatabaseManager exposes no reader for it, and the
// only way to observe it is a connection carrying it -- which needs a linked
// driver this module does not have. Said rather than covered, because a test
// named for both halves and checking one is worse than a test named for one.
func TestSetEventDispatcherIsReadableBackOffTheCapsule(t *testing.T) {
	resetGlobals(t)

	m := NewManager(database.MapConfiguration{})
	if m.GetEventDispatcher() != nil {
		t.Fatal("a fresh capsule already carries a dispatcher")
	}

	dispatcher := &recordingDispatcher{}
	m.SetEventDispatcher(dispatcher)

	if m.GetEventDispatcher() != database.Dispatcher(dispatcher) {
		t.Fatal("the dispatcher did not come back off the capsule")
	}
}

// TestGetDatabaseManagerIsTheSameOneEveryTime: a capsule that built a new
// manager per call would hand out connections nobody else could see, and the
// pooling the manager exists for would be gone.
func TestGetDatabaseManagerIsTheSameOneEveryTime(t *testing.T) {
	resetGlobals(t)

	m := NewManager(database.MapConfiguration{})
	if m.GetDatabaseManager() != m.GetDatabaseManager() {
		t.Fatal("the capsule answered two different managers")
	}
}

// TestTheGlobalCapsuleSurvivesConcurrentUse.
//
// The mutex around the package variable is what a global costs in a language
// with goroutines, and this is the test that reads it under -race: a script
// setting the capsule while a goroutine reads it is the shape the lock is for.
func TestTheGlobalCapsuleSurvivesConcurrentUse(t *testing.T) {
	resetGlobals(t)

	first := NewManager(database.MapConfiguration{})
	second := NewManager(database.MapConfiguration{})

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				SetAsGlobal(first)
				return
			}
			SetAsGlobal(second)
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := Instance(); got != first && got != second {
				t.Error("Instance answered a capsule nobody set")
			}
		}()
	}
	wg.Wait()
}

// TestAddConnectionSurvivesConcurrentUse: the manager's own lock is what makes a
// script safe to write connections from more than one goroutine.
func TestAddConnectionSurvivesConcurrentUse(t *testing.T) {
	resetGlobals(t)

	config := database.MapConfiguration{}
	m := NewManager(config)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AddConnection(map[string]any{"driver": "sqlite"}, string(rune('a'+i)))
		}(i)
	}
	wg.Wait()

	connections := config.Get("database.connections").(map[string]any)
	if len(connections) != 8 {
		t.Fatalf("%d connections landed out of 8: %#v", len(connections), connections)
	}
}
