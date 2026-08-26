package capsule

import (
	"sync"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/query"
)

// instance is the capsule the package-level functions reach.
//
// It is a package variable because the point of a capsule is that a script can
// reach the database without carrying a handle through every function. The mutex
// is what that costs in a language with goroutines.
var (
	instanceMu sync.RWMutex
	instance   *Manager
)

// Manager is the database, usable from a script that has no application around
// it.
//
// It exists for a standalone script, a migration tool or a test harness, and it
// is not how an Arandu application reaches its data. An application holds a
// Repository, which holds a Grant. The package-level functions below hold
// neither, which is why the doc on every one of them says so and why nothing in
// a request path should be calling them.
type Manager struct {
	mu sync.RWMutex

	// config is where the capsule reads and writes the default connection
	// name and the configured connections.
	config database.Configuration

	// manager is the DatabaseManager the capsule wraps.
	manager *database.DatabaseManager

	// events is the dispatcher put on every connection the capsule makes.
	events database.Dispatcher
}

// NewManager builds a Manager over the configuration it is given. Nil takes a
// fresh map.
func NewManager(config database.Configuration) *Manager {
	if config == nil {
		config = database.MapConfiguration{}
	}

	m := &Manager{config: config}
	m.setupDefaultConfiguration()
	m.setupManager()

	SetAsGlobal(m)

	return m
}

// setupDefaultConfiguration fills in a default connection name and an empty
// connections map when neither is set.
//
// A row here is always a query.Record, so there is no separate fetch-mode
// setting to default: there is only one shape to choose.
func (m *Manager) setupDefaultConfiguration() {
	if m.config.Get("database.default") == nil {
		m.config.Set("database.default", "default")
	}
	if m.config.Get("database.connections") == nil {
		m.config.Set("database.connections", map[string]any{})
	}
}

// setupManager builds the DatabaseManager the capsule wraps.
func (m *Manager) setupManager() {
	m.manager = database.NewDatabaseManager(m.config, database.NewConnectionFactory())
}

// SetAsGlobal makes this the capsule the package-level functions reach.
//
// NewManager calls it, so a manager is usable that way as soon as it is built.
func SetAsGlobal(m *Manager) {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	instance = m
}

// Instance answers the capsule the static methods use. It is nil before
// NewManager has run, and every static method below says so rather than
// dereferencing it.
func Instance() *Manager {
	instanceMu.RLock()
	defer instanceMu.RUnlock()
	return instance
}

// Connection returns the named connection from the global capsule.
//
// It fails with errNoCapsule rather than letting a nil instance panic four
// frames later, because a nil-pointer panic names nothing a person can act
// on.
func Connection(name string) (database.ConnectionInterface, error) {
	m := Instance()
	if m == nil {
		return nil, errNoCapsule
	}
	return m.GetConnection(name)
}

// Table returns a query builder against a table on a named connection, from
// the global capsule.
//
// It takes a context for the reason Connection.Table gives: a builder that
// cannot be cancelled holds a server connection for as long as the server
// likes.
func Table(table any, as, connection string) (*query.Builder, error) {
	conn, err := Connection(connection)
	if err != nil {
		return nil, err
	}
	if as == "" {
		return conn.Table(table), nil
	}
	return conn.Table(table, as), nil
}

// Schema returns the schema builder for a named connection, from the global
// capsule.
//
// It returns any because the schema builder lives in database/schema, which
// nothing here imports: a capsule needs to hand one over and never to call
// it. Nil means no schema builder was registered, which is what a binary
// that never imported the schema package has.
func Schema(connection string) (any, error) {
	conn, err := Connection(connection)
	if err != nil {
		return nil, err
	}
	concrete, ok := conn.(*database.Connection)
	if !ok {
		return nil, errNoSchemaBuilder
	}
	return concrete.GetSchemaBuilder(), nil
}

// GetConnection returns the named connection from this manager's
// DatabaseManager.
func (m *Manager) GetConnection(name string) (database.ConnectionInterface, error) {
	m.mu.RLock()
	manager := m.manager
	m.mu.RUnlock()
	return manager.Connection(name)
}

// AddConnection registers a connection configuration under name, or under
// "default" when name is empty.
func (m *Manager) AddConnection(config map[string]any, name string) {
	if name == "" {
		name = "default"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	connections, _ := m.config.Get("database.connections").(map[string]any)
	if connections == nil {
		connections = map[string]any{}
	}
	connections[name] = config
	m.config.Set("database.connections", connections)
}

// BootEloquentUsing is where an ORM registers the wiring BootEloquent does.
//
// Calling into an ORM's connection resolver and event dispatcher setters
// directly here would make the capsule import the ORM, and the capsule is
// the piece a script uses precisely because it wants the small half. So the
// ORM registers instead, from its own init, and a binary that never
// imported it has a BootEloquent that does nothing -- which is the correct
// response to "boot the ORM I did not link".
var BootEloquentUsing func(resolver database.ConnectionResolverInterface, events database.Dispatcher)

// BootEloquent wires an ORM's connection resolver and event dispatcher to
// the capsule's manager, if BootEloquentUsing was set.
func (m *Manager) BootEloquent() {
	if BootEloquentUsing == nil {
		return
	}

	m.mu.RLock()
	manager := m.manager
	events := m.events
	m.mu.RUnlock()

	BootEloquentUsing(manager, events)
}

// GetDatabaseManager returns the DatabaseManager the capsule wraps.
func (m *Manager) GetDatabaseManager() *database.DatabaseManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.manager
}

// GetEventDispatcher returns the dispatcher put on every connection the
// capsule makes.
func (m *Manager) GetEventDispatcher() database.Dispatcher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.events
}

// SetEventDispatcher replaces the dispatcher put on every connection the
// capsule makes; every connection made from here on carries it.
func (m *Manager) SetEventDispatcher(dispatcher database.Dispatcher) {
	m.mu.Lock()
	m.events = dispatcher
	manager := m.manager
	m.mu.Unlock()

	manager.SetEventDispatcher(dispatcher)
}

// SetTransactionManager gives the capsule's connections a transactions
// manager, which is what makes AfterCommit work.
//
// There is no container to read one from, so it is set directly.
func (m *Manager) SetTransactionManager(manager *database.DatabaseTransactionsManager) {
	m.mu.RLock()
	dbManager := m.manager
	m.mu.RUnlock()

	dbManager.SetTransactionManager(manager)
}

// GetConfiguration returns the configuration the capsule reads and writes
// connection settings through.
func (m *Manager) GetConfiguration() database.Configuration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

type capsuleError string

func (e capsuleError) Error() string { return string(e) }

const (
	errNoCapsule = capsuleError("capsule: no manager has been created yet -- call capsule.NewManager first")

	errNoSchemaBuilder = capsuleError("capsule: this connection cannot answer a schema builder")
)
