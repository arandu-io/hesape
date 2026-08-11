package capsule

import (
	"context"
	"sync"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/query"
)

// instance answers Illuminate\Support\Traits\CapsuleManagerTrait::$instance,
// which Manager sets in setupContainer and the static methods read.
//
// It is a package variable for the same reason it is static there: the point of
// a capsule is that a script can reach the database without carrying a handle
// through every function. A mutex is what static costs in a language that has
// goroutines.
var (
	instanceMu sync.RWMutex
	instance   *Manager
)

// Manager answers Illuminate\Database\Capsule\Manager: the database, usable
// from a script that has no application around it.
//
// It exists for exactly the case Laravel built it for -- a standalone script, a
// migration tool, a test harness -- and it is not how an Arandu application
// reaches its data. An application holds a Repository, which holds a Grant. The
// static methods below hold neither, which is why the doc on every one of them
// says so and why nothing in a request path should be calling them.
type Manager struct {
	mu sync.RWMutex

	// config is what the PHP keeps in $this->container['config'].
	config database.Configuration

	// manager is Manager::$manager.
	manager *database.DatabaseManager

	// events is what the PHP keeps in the container under 'events'.
	events database.Dispatcher
}

// NewManager answers Manager::__construct.
//
// The PHP's argument is a container; there is none (ADR 0001), so what it
// actually needed -- somewhere to keep the configuration -- is the argument
// instead. Nil takes a fresh map, which is what `new Container` was doing.
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

// setupDefaultConfiguration answers the protected
// Manager::setupDefaultConfiguration.
//
// The PHP sets database.fetch as well, which selects between a stdClass and an
// array per row. A row here is a query.Record and there is one shape, so there
// is nothing to choose.
func (m *Manager) setupDefaultConfiguration() {
	if m.config.Get("database.default") == nil {
		m.config.Set("database.default", "default")
	}
	if m.config.Get("database.connections") == nil {
		m.config.Set("database.connections", map[string]any{})
	}
}

// setupManager answers the protected Manager::setupManager.
func (m *Manager) setupManager() {
	m.manager = database.NewDatabaseManager(m.config, database.NewConnectionFactory())
}

// SetAsGlobal answers CapsuleManagerTrait::setAsGlobal: make this the capsule
// the static methods reach.
//
// NewManager calls it, which the PHP's constructor also effectively does
// through setupContainer -- Laravel's Manager is unusable statically until
// something sets the instance, and every example sets it immediately.
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

// Connection answers Manager::connection, the static one.
//
// The PHP throws nothing and lets a null instance fail four frames later; this
// answers the error, because "call to a member function on null" names nothing
// a person can act on.
func Connection(name string) (database.ConnectionInterface, error) {
	m := Instance()
	if m == nil {
		return nil, errNoCapsule
	}
	return m.GetConnection(name)
}

// Table answers Manager::table, the static one: a query builder against a
// table on a named connection.
//
// It takes a context, which the PHP's does not, for the reason Connection.Table
// gives: a builder that cannot be cancelled holds a server connection for as
// long as the server likes.
func Table(ctx context.Context, table any, as, connection string) (*query.Builder, error) {
	conn, err := Connection(connection)
	if err != nil {
		return nil, err
	}
	if as == "" {
		return conn.Table(ctx, table), nil
	}
	return conn.Table(ctx, table, as), nil
}

// Schema answers Manager::schema, the static one.
//
// It answers any because the schema builder lives in database/schema, which
// nothing here imports: a capsule needs to hand one over and never to call it.
// A nil answer means no schema builder was registered, which is what a binary
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

// GetConnection answers Manager::getConnection.
func (m *Manager) GetConnection(name string) (database.ConnectionInterface, error) {
	m.mu.RLock()
	manager := m.manager
	m.mu.RUnlock()
	return manager.Connection(name)
}

// AddConnection answers Manager::addConnection.
//
// name empty is the PHP's 'default' default.
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
// The PHP calls Eloquent::setConnectionResolver and Eloquent::setEventDispatcher
// directly. Doing that here would make the capsule import the ORM, and the
// capsule is the piece a script uses precisely because it wants the small half.
// So the ORM registers instead, from its own init, and a binary that never
// imported it has a BootEloquent that does nothing -- which is the correct
// answer to "boot the ORM I did not link".
var BootEloquentUsing func(resolver database.ConnectionResolverInterface, events database.Dispatcher)

// BootEloquent answers Manager::bootEloquent.
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

// GetDatabaseManager answers Manager::getDatabaseManager.
func (m *Manager) GetDatabaseManager() *database.DatabaseManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.manager
}

// GetEventDispatcher answers Manager::getEventDispatcher.
func (m *Manager) GetEventDispatcher() database.Dispatcher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.events
}

// SetEventDispatcher answers Manager::setEventDispatcher: every connection the
// capsule makes from here on carries it.
func (m *Manager) SetEventDispatcher(dispatcher database.Dispatcher) {
	m.mu.Lock()
	m.events = dispatcher
	manager := m.manager
	m.mu.Unlock()

	manager.SetEventDispatcher(dispatcher)
}

// SetTransactionManager gives the capsule's connections a transactions manager,
// which is what makes AfterCommit work.
//
// The PHP reads it out of the container under 'db.transactions'; there is no
// container, so it is set.
func (m *Manager) SetTransactionManager(manager *database.DatabaseTransactionsManager) {
	m.mu.RLock()
	dbManager := m.manager
	m.mu.RUnlock()

	dbManager.SetTransactionManager(manager)
}

// GetConfiguration answers what the PHP reads as $this->container['config'].
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
