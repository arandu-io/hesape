package redis

import (
	"fmt"
	"maps"
	"sync"

	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/redis/connectors"
)

// DefaultConnection is the name a caller gets when it asks for none. It is
// Laravel's 'default', spelled once.
const DefaultConnection = "default"

// RedisManager answers Illuminate\Redis\RedisManager.
//
// It holds the configured connections by name, opens them on first use and
// hands out the same one afterwards -- which is the point of it: a connection
// pool per name, not per caller.
//
// # What it is not
//
// Laravel's manager resolves the event dispatcher out of the container and
// parses connection configuration out of an array of arrays. Neither happens
// here (ADR 0001, ADR 0002): the application builds connections.Config values
// in bootstrap/app.go and passes them in, and the dispatcher arrives through
// SetEventDispatcher. What is left is what anyone actually calls -- turn a
// name into a connection, and turn command events on.
//
// It is safe for concurrent use, which Laravel's is not and does not need to
// be: one manager serves every request in the process.
type RedisManager struct {
	mu sync.RWMutex

	driver         string
	config         map[string]connections.Config
	connections    map[string]*connections.Connection
	events         bool
	dispatcher     connections.Dispatcher
	customCreators map[string]func(connections.Config) (*connections.Connection, error)
}

// NewRedisManager builds the manager from the configured connections.
//
// driver is the client name -- "phpredis" in a Laravel configuration file --
// and it selects a custom creator registered with Extend. There is one built-in
// client, so an unknown driver with no creator behind it is the built-in one;
// see the package documentation for why there is no second.
func NewRedisManager(driver string, config map[string]connections.Config) *RedisManager {
	return &RedisManager{
		driver:         driver,
		config:         maps.Clone(config),
		connections:    map[string]*connections.Connection{},
		customCreators: map[string]func(connections.Config) (*connections.Connection, error){},
	}
}

// Connection returns the connection with the given name, opening it the first
// time it is asked for.
//
// It answers RedisManager::connection(). An empty name is "default", which is
// the `$name ?: 'default'` of the PHP.
func (m *RedisManager) Connection(name string) (*connections.Connection, error) {
	if name == "" {
		name = DefaultConnection
	}

	m.mu.RLock()
	existing, ok := m.connections[name]
	m.mu.RUnlock()
	if ok {
		return existing, nil
	}

	conn, err := m.Resolve(name)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Somebody may have opened it while this one was resolving. Keeping the
	// first is what makes Connection return the same value to every caller,
	// which is the property the whole type exists for.
	if existing, ok := m.connections[name]; ok {
		_ = conn.Close()
		return existing, nil
	}
	m.connections[name] = conn
	return conn, nil
}

// Resolve opens the given connection by name, without remembering it.
//
// It answers RedisManager::resolve(), and it is the half of Connection that
// does the work: a caller that wants a second, separate connection to the same
// server -- a subscriber, which blocks its socket for as long as it listens --
// calls this one.
//
// A name nobody configured is an error, which is the PHP's
// InvalidArgumentException.
func (m *RedisManager) Resolve(name string) (*connections.Connection, error) {
	if name == "" {
		name = DefaultConnection
	}

	m.mu.RLock()
	config, ok := m.config[name]
	creator := m.customCreators[m.driver]
	events, dispatcher := m.events, m.dispatcher
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("redis: connection [%s] not configured", name)
	}

	var (
		conn *connections.Connection
		err  error
	)
	if creator != nil {
		conn, err = creator(config)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, fmt.Errorf("redis: the custom creator for driver [%s] returned no connection", m.driver)
		}
	} else if len(config.Addresses) > 0 {
		conn = connectors.Connector{}.ConnectToCluster([]connections.Config{config})
	} else {
		conn = connectors.Connector{}.Connect(config)
	}

	// This is RedisManager::configure: name the connection, and give it the
	// dispatcher when events are on.
	conn.SetName(name)
	if events && dispatcher != nil {
		conn.SetEventDispatcher(dispatcher)
	}
	return conn, nil
}

// Connections returns all of the created connections, by name.
//
// It answers RedisManager::connections(). It is a copy: a caller iterating the
// live map while another request opens a connection is a data race, and the map
// is small.
func (m *RedisManager) Connections() map[string]*connections.Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Clone(m.connections)
}

// EnableEvents turns on the firing of Redis command events.
//
// It answers RedisManager::enableEvents(). It applies to the connections
// already open as well as the ones still to be resolved, because a caller who
// turns events on to debug a slow page does not want to restart the process
// first.
func (m *RedisManager) EnableEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = true
	if m.dispatcher == nil {
		return
	}
	for _, conn := range m.connections {
		conn.SetEventDispatcher(m.dispatcher)
	}
}

// DisableEvents turns off the firing of Redis command events.
//
// It answers RedisManager::disableEvents(), and takes the dispatcher off every
// open connection -- which is what makes the per-command event cost nothing
// once it is off, rather than cost a dispatch with no listeners.
func (m *RedisManager) DisableEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = false
	for _, conn := range m.connections {
		conn.UnsetEventDispatcher()
	}
}

// SetEventDispatcher gives the manager the dispatcher command events go to.
//
// Laravel resolves it from the container inside configure(); there is no
// container here (ADR 0001), so the application hands it over. Without it
// EnableEvents turns on something with nowhere to go, and says so by doing
// nothing.
func (m *RedisManager) SetEventDispatcher(dispatcher connections.Dispatcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatcher = dispatcher
	if !m.events {
		return
	}
	for _, conn := range m.connections {
		conn.SetEventDispatcher(dispatcher)
	}
}

// SetDriver sets the default driver, which is the name Extend registered a
// custom creator under.
//
// It answers RedisManager::setDriver().
func (m *RedisManager) SetDriver(driver string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.driver = driver
}

// Purge disconnects the given connection and forgets it, so the next
// Connection opens a fresh one.
//
// It answers RedisManager::purge(). The PHP only drops its reference and lets
// the object be collected; Go has no finalizer to close the socket, so this
// closes it -- a purge that leaked the pool would be a purge that runs out of
// file descriptors under a reconnect loop.
func (m *RedisManager) Purge(name string) error {
	if name == "" {
		name = DefaultConnection
	}

	m.mu.Lock()
	conn, ok := m.connections[name]
	delete(m.connections, name)
	m.mu.Unlock()

	if !ok {
		return nil
	}
	return conn.Close()
}

// Extend registers a custom connection creator for a driver name.
//
// It answers RedisManager::extend(). It is how an application opens its
// connection its own way -- a TLS configuration this package does not name, a
// socket path, a proxy -- without a second constructor existing for everybody
// else (RULE 9).
func (m *RedisManager) Extend(driver string, callback func(connections.Config) (*connections.Connection, error)) *RedisManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customCreators[driver] = callback
	return m
}

// Close disconnects every open connection.
//
// Laravel has no equivalent, because PHP tears the process down after each
// request. A long-lived process has to give the sockets back on shutdown, and
// the module lifecycle calls this.
func (m *RedisManager) Close() error {
	m.mu.Lock()
	open := m.connections
	m.connections = map[string]*connections.Connection{}
	m.mu.Unlock()

	var first error
	for _, conn := range open {
		if err := conn.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
