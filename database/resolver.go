package database

import (
	"fmt"
	"sync"
)

// ConnectionResolverInterface answers
// Illuminate\Database\ConnectionResolverInterface.
//
// Connection answers with an error where the PHP would let an undefined index
// through: `$this->connections[$name]` on a name nobody registered is a PHP
// notice and a null, and the null then fails four frames away with a message
// about a method call on null.
type ConnectionResolverInterface interface {
	// Connection answers ConnectionResolverInterface::connection. An empty
	// name is the PHP's null: the default connection.
	Connection(name string) (ConnectionInterface, error)

	// GetDefaultConnection answers
	// ConnectionResolverInterface::getDefaultConnection.
	GetDefaultConnection() string

	// SetDefaultConnection answers
	// ConnectionResolverInterface::setDefaultConnection.
	SetDefaultConnection(name string)
}

// ConnectionResolver answers Illuminate\Database\ConnectionResolver: a map of
// name to connection, and the name of the default one.
//
// It is the resolver for something that already has its connections -- a test,
// a worker built by hand, the capsule. DatabaseManager is the resolver that
// makes them on demand from configuration.
type ConnectionResolver struct {
	mu sync.RWMutex

	// connections is ConnectionResolver::$connections.
	connections map[string]ConnectionInterface

	// defaultConnection is ConnectionResolver::$default.
	defaultConnection string
}

// NewConnectionResolver answers ConnectionResolver::__construct.
func NewConnectionResolver(connections map[string]ConnectionInterface) *ConnectionResolver {
	r := &ConnectionResolver{connections: map[string]ConnectionInterface{}}
	for name, connection := range connections {
		r.AddConnection(name, connection)
	}
	return r
}

// Connection answers ConnectionResolver::connection.
func (r *ConnectionResolver) Connection(name string) (ConnectionInterface, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		name = r.defaultConnection
	}

	connection, found := r.connections[name]
	if !found {
		return nil, fmt.Errorf("Database connection [%s] not configured.", name)
	}
	return connection, nil
}

// AddConnection answers ConnectionResolver::addConnection.
func (r *ConnectionResolver) AddConnection(name string, connection ConnectionInterface) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connections == nil {
		r.connections = map[string]ConnectionInterface{}
	}
	r.connections[name] = connection
}

// HasConnection answers ConnectionResolver::hasConnection.
func (r *ConnectionResolver) HasConnection(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, found := r.connections[name]
	return found
}

// GetDefaultConnection answers
// ConnectionResolver::getDefaultConnection.
func (r *ConnectionResolver) GetDefaultConnection() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultConnection
}

// SetDefaultConnection answers
// ConnectionResolver::setDefaultConnection.
func (r *ConnectionResolver) SetDefaultConnection(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultConnection = name
}

// ConnectionResolver satisfies the interface it answers to.
var _ ConnectionResolverInterface = (*ConnectionResolver)(nil)
