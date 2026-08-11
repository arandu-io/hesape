package database

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Configuration is where DatabaseManager reads its connections.
//
// It answers the $app['config'] the PHP indexes with 'database.default' and
// 'database.connections' -- the same two keys, so a configuration file written
// for Laravel is read the same way. It is an interface rather than
// hesape/config directly because the manager has to be constructible in a test
// with three lines and no file.
type Configuration interface {
	// Get answers Repository::get.
	Get(key string) any

	// Set answers Repository::set.
	Set(key string, value any)
}

// MapConfiguration is a Configuration backed by a map, for a test or a worker
// wired by hand.
type MapConfiguration map[string]any

// Get answers Configuration.Get.
func (m MapConfiguration) Get(key string) any { return m[key] }

// Set answers Configuration.Set.
func (m MapConfiguration) Set(key string, value any) { m[key] = value }

// DatabaseManager answers Illuminate\Database\DatabaseManager: the resolver
// that makes connections on demand from configuration, keeps them, and hands
// the same one back next time.
//
// The PHP's __call forwards every unknown method to the default connection, so
// DB::table(...) reads as though the manager were a connection. Go has no
// __call and this has no forwarding: Connection(name) answers the connection,
// and the call goes on that. It is one more word and one fewer indirection, and
// it is the only shape a compiler can check.
type DatabaseManager struct {
	mu sync.RWMutex

	// config is DatabaseManager::$app['config'].
	config Configuration

	// factory is DatabaseManager::$factory.
	factory *ConnectionFactory

	// connections is DatabaseManager::$connections.
	connections map[string]*Connection

	// dynamicConnectionConfigurations is
	// DatabaseManager::$dynamicConnectionConfigurations, what Build leaves
	// behind so a reconnect can rebuild an on-demand connection.
	dynamicConnectionConfigurations map[string]map[string]any

	// extensions is DatabaseManager::$extensions.
	extensions map[string]func(config map[string]any, name string) (*Connection, error)

	// reconnector is DatabaseManager::$reconnector.
	reconnector func(*Connection) error

	// events is the dispatcher the manager puts on every connection it makes,
	// which the PHP takes from $this->app['events'].
	events Dispatcher

	// transactions is the manager the PHP takes from
	// $this->app['db.transactions'].
	transactions *DatabaseTransactionsManager
}

// NewDatabaseManager answers DatabaseManager::__construct.
//
// The PHP's first argument is the application, which it uses for exactly three
// things: the configuration, the event dispatcher and the transactions manager.
// Those three are the arguments here, because a manager that took the whole
// application would be a manager nothing but the application could build.
func NewDatabaseManager(config Configuration, factory *ConnectionFactory) *DatabaseManager {
	if factory == nil {
		factory = NewConnectionFactory()
	}

	m := &DatabaseManager{
		config:                          config,
		factory:                         factory,
		connections:                     map[string]*Connection{},
		dynamicConnectionConfigurations: map[string]map[string]any{},
		extensions:                      map[string]func(map[string]any, string) (*Connection, error){},
	}

	m.reconnector = func(connection *Connection) error {
		fresh, err := m.Reconnect(connection.GetNameWithReadWriteType())
		if err != nil {
			return err
		}
		connection.SetPDO(fresh.GetRawPDO())
		return nil
	}

	return m
}

// Connection answers DatabaseManager::connection.
//
// The name may carry a read/write suffix -- "primary::read" -- which is what
// makes a replica addressable by name.
func (m *DatabaseManager) Connection(name string) (ConnectionInterface, error) {
	return m.connection(name)
}

// connection is Connection with the concrete type, which everything inside this
// file wants and the interface method cannot answer.
func (m *DatabaseManager) connection(name string) (*Connection, error) {
	if name == "" {
		name = m.GetDefaultConnection()
	}

	m.mu.RLock()
	existing, found := m.connections[name]
	m.mu.RUnlock()
	if found {
		return existing, nil
	}

	database, typ := parseConnectionName(name)

	made, err := m.makeConnection(database)
	if err != nil {
		return nil, err
	}
	made = m.configure(made, typ)

	m.mu.Lock()
	// Another goroutine may have made the same connection while this one was
	// opening a socket. The PHP cannot meet this and has no guard; here the
	// first one to arrive wins, and the second's pool is closed rather than
	// leaked.
	if raced, taken := m.connections[name]; taken {
		m.mu.Unlock()
		if pool := made.GetRawPDO(); pool != nil {
			_ = pool.Close()
		}
		return raced, nil
	}
	m.connections[name] = made
	m.mu.Unlock()

	m.dispatchConnectionEstablishedEvent(made)

	return made, nil
}

// Build answers DatabaseManager::build: a connection from a configuration
// nobody wrote in a file.
func (m *DatabaseManager) Build(config map[string]any) (*Connection, error) {
	name, _ := config["name"].(string)
	if name == "" {
		name = CalculateDynamicConnectionName(config)
		config["name"] = name
	}

	m.mu.Lock()
	m.dynamicConnectionConfigurations[name] = config
	m.mu.Unlock()

	return m.ConnectUsing(name, config, true)
}

// CalculateDynamicConnectionName answers
// DatabaseManager::calculateDynamicConnectionName.
//
// The PHP concatenates key and value for every entry and hashes the result;
// map order is not stable in either language, so the keys are sorted first --
// which the PHP gets away with because its arrays keep insertion order, and
// which Go must do explicitly or the same configuration would get two names.
func CalculateDynamicConnectionName(config map[string]any) string {
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		switch value := config[key].(type) {
		case string:
			b.WriteString(value)
		case int:
			fmt.Fprintf(&b, "%d", value)
		}
	}

	return fmt.Sprintf("dynamic_%x", md5.Sum([]byte(b.String())))
}

// ConnectUsing answers DatabaseManager::connectUsing.
func (m *DatabaseManager) ConnectUsing(name string, config map[string]any, force bool) (*Connection, error) {
	if force {
		m.Purge(name)
	}

	m.mu.RLock()
	_, taken := m.connections[name]
	m.mu.RUnlock()
	if taken {
		return nil, fmt.Errorf("Cannot establish connection [%s] because another connection with that name already exists.", name)
	}

	made, err := m.factory.Make(config, name)
	if err != nil {
		return nil, err
	}
	made = m.configure(made, "")

	m.mu.Lock()
	m.connections[name] = made
	m.mu.Unlock()

	m.dispatchConnectionEstablishedEvent(made)

	return made, nil
}

// parseConnectionName answers the protected
// DatabaseManager::parseConnectionName.
func parseConnectionName(name string) (string, string) {
	for _, suffix := range []string{"::read", "::write"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), strings.TrimPrefix(suffix, "::")
		}
	}
	return name, ""
}

// makeConnection answers the protected DatabaseManager::makeConnection.
func (m *DatabaseManager) makeConnection(name string) (*Connection, error) {
	config, err := m.configuration(name)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	byName := m.extensions[name]
	driver, _ := config["driver"].(string)
	byDriver := m.extensions[driver]
	m.mu.RUnlock()

	if byName != nil {
		return byName(config, name)
	}
	if byDriver != nil {
		return byDriver(config, name)
	}
	return m.factory.Make(config, name)
}

// configuration answers the protected DatabaseManager::configuration.
func (m *DatabaseManager) configuration(name string) (map[string]any, error) {
	m.mu.RLock()
	dynamic := m.dynamicConnectionConfigurations[name]
	m.mu.RUnlock()

	if dynamic != nil {
		return dynamic, nil
	}

	connections, _ := m.config.Get("database.connections").(map[string]any)
	config, found := connections[name].(map[string]any)
	if !found {
		return nil, fmt.Errorf("Database connection [%s] not configured.", name)
	}
	return config, nil
}

// configure answers the protected DatabaseManager::configure.
func (m *DatabaseManager) configure(connection *Connection, typ string) *Connection {
	connection = m.setPDOForType(connection, typ).SetReadWriteType(typ)

	m.mu.RLock()
	events := m.events
	transactions := m.transactions
	reconnector := m.reconnector
	m.mu.RUnlock()

	if events != nil {
		connection.SetEventDispatcher(events)
	}
	if transactions != nil {
		connection.SetTransactionManager(transactions)
	}
	connection.SetReconnector(reconnector)

	return connection
}

// setPDOForType answers the protected DatabaseManager::setPdoForType.
func (m *DatabaseManager) setPDOForType(connection *Connection, typ string) *Connection {
	switch typ {
	case "read":
		if pool, err := connection.GetReadPDO(); err == nil {
			connection.SetPDO(pool)
		}
	case "write":
		if pool, err := connection.GetPDO(); err == nil {
			connection.SetReadPDO(pool)
		}
	}
	return connection
}

// dispatchConnectionEstablishedEvent answers the protected method of the same
// name.
func (m *DatabaseManager) dispatchConnectionEstablishedEvent(connection *Connection) {
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()

	if events == nil {
		return
	}
	events.Dispatch(newConnectionEstablished(connection))
}

// Purge answers DatabaseManager::purge.
func (m *DatabaseManager) Purge(name string) {
	if name == "" {
		name = m.GetDefaultConnection()
	}
	m.Disconnect(name)

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.connections, name)
}

// Disconnect answers DatabaseManager::disconnect.
func (m *DatabaseManager) Disconnect(name string) {
	if name == "" {
		name = m.GetDefaultConnection()
	}

	m.mu.RLock()
	connection, found := m.connections[name]
	m.mu.RUnlock()

	if found {
		connection.Disconnect()
	}
}

// Reconnect answers DatabaseManager::reconnect.
func (m *DatabaseManager) Reconnect(name string) (*Connection, error) {
	if name == "" {
		name = m.GetDefaultConnection()
	}
	m.Disconnect(name)

	m.mu.RLock()
	_, found := m.connections[name]
	m.mu.RUnlock()

	if !found {
		return m.connection(name)
	}

	refreshed, err := m.refreshPDOConnections(name)
	if err != nil {
		return nil, err
	}
	m.dispatchConnectionEstablishedEvent(refreshed)
	return refreshed, nil
}

// UsingConnection answers DatabaseManager::usingConnection.
func (m *DatabaseManager) UsingConnection(name string, callback func() error) error {
	previous := m.GetDefaultConnection()

	m.SetDefaultConnection(name)
	defer m.SetDefaultConnection(previous)

	return callback()
}

// refreshPDOConnections answers the protected
// DatabaseManager::refreshPdoConnections.
func (m *DatabaseManager) refreshPDOConnections(name string) (*Connection, error) {
	database, typ := parseConnectionName(name)

	made, err := m.makeConnection(database)
	if err != nil {
		return nil, err
	}
	fresh := m.configure(made, typ)

	m.mu.RLock()
	existing := m.connections[name]
	m.mu.RUnlock()

	if existing == nil {
		return fresh, nil
	}
	return existing.SetPDO(fresh.GetRawPDO()).SetReadPDO(fresh.GetRawReadPDO()), nil
}

// GetDefaultConnection answers
// DatabaseManager::getDefaultConnection.
func (m *DatabaseManager) GetDefaultConnection() string {
	name, _ := m.config.Get("database.default").(string)
	return name
}

// SetDefaultConnection answers
// DatabaseManager::setDefaultConnection.
func (m *DatabaseManager) SetDefaultConnection(name string) {
	m.config.Set("database.default", name)
}

// SupportedDrivers answers DatabaseManager::supportedDrivers.
func (m *DatabaseManager) SupportedDrivers() []string { return SupportedDrivers() }

// AvailableDrivers answers DatabaseManager::availableDrivers.
func (m *DatabaseManager) AvailableDrivers() []string { return AvailableDrivers() }

// Extend answers DatabaseManager::extend.
func (m *DatabaseManager) Extend(name string, resolver func(config map[string]any, name string) (*Connection, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[name] = resolver
}

// ForgetExtension answers DatabaseManager::forgetExtension.
func (m *DatabaseManager) ForgetExtension(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.extensions, name)
}

// GetConnections answers DatabaseManager::getConnections.
func (m *DatabaseManager) GetConnections() map[string]*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]*Connection, len(m.connections))
	for name, connection := range m.connections {
		out[name] = connection
	}
	return out
}

// ConnectionNames answers the sorted names of the open connections.
//
// The PHP reads getConnections() and relies on its arrays keeping insertion
// order; Go's maps do not, so a console table built from GetConnections would
// print its rows in a different order on every run.
func (m *DatabaseManager) ConnectionNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.connections))
	for name := range m.connections {
		names = append(names, name)
	}
	return sortedNames(names)
}

// SetReconnector answers DatabaseManager::setReconnector.
func (m *DatabaseManager) SetReconnector(reconnector func(*Connection) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconnector = reconnector
}

// SetApplication answers DatabaseManager::setApplication, narrowed to the
// configuration it is only ever asked for.
func (m *DatabaseManager) SetApplication(config Configuration) *DatabaseManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config
	return m
}

// SetEventDispatcher puts a dispatcher on every connection the manager makes.
//
// The PHP reads $this->app['events'] inside configure(); there is no container
// to read from (ADR 0001), so it is set here once.
func (m *DatabaseManager) SetEventDispatcher(events Dispatcher) *DatabaseManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = events
	return m
}

// SetTransactionManager puts a transactions manager on every connection the
// manager makes, which the PHP takes from $this->app['db.transactions'].
func (m *DatabaseManager) SetTransactionManager(manager *DatabaseTransactionsManager) *DatabaseManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transactions = manager
	return m
}

// DatabaseManager satisfies the resolver interface, which is what lets a
// Migrator take either it or a hand-built ConnectionResolver.
var _ ConnectionResolverInterface = (*DatabaseManager)(nil)
