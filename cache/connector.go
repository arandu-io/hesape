package cache

import (
	"context"
	"crypto/tls"

	"github.com/arandu-io/hesape/internal/linking"
)

// SharedStore is a store every replica of a deployment sees: a [Store] and a
// [Locking] over one connection to a server, plus the two calls that manage the
// connection itself.
//
// Locking is part of the interface rather than an optional half, because what a
// shared store is for is state more than one process reads -- a session, the
// lock a scheduled task takes so it runs once -- and a store every replica can
// read that cannot hold a lock is a store a Singleton task runs on all of.
type SharedStore interface {
	Store
	Locking

	// Ping verifies that the server answers. Opening never dials, so this is
	// the call a health check makes.
	Ping(ctx context.Context) error

	// Close releases the connection.
	Close() error
}

// Endpoint is where a shared store lives and how to reach it.
//
// It is the parsed form of what the environment says about the server, and a
// typed struct rather than a connection string for the reason [Config] is one:
// a field nobody can misspell is a field nobody sets by accident.
type Endpoint struct {
	// Address is host:port.
	Address string

	// Password is optional, and comes from configuration -- never from a
	// literal.
	Password string

	// Database is the numbered database on the server, zero for almost every
	// deployment.
	Database int

	// Prefix namespaces every key of this application, so two applications can
	// share one server. It is the application's prefix and nothing else: the
	// tenant is a key segment [Repository] builds from the Grant.
	Prefix string

	// TLS encrypts the connection, and nil leaves it in the clear. It is
	// crypto/tls's own type because a private authority and a client
	// certificate are exactly what tls.Config already names.
	TLS *tls.Config
}

// Connector opens the shared store for one driver.
//
// A connector lives in its own module, because the client it carries is a
// third-party dependency and Go has no optional one. Importing that module for
// its side effect is what links the driver into the binary:
//
//	import _ "github.com/arandu-io/hesape/redis"
//
// The module's init() calls [Register], and [Open] resolves the rest from the
// driver name the configuration holds. Nothing else in this package changes,
// and a binary that imports no connector carries no client.
type Connector interface {
	// Driver is the value CACHE_STORE takes to ask for this connector.
	Driver() string

	// Open returns the store over the endpoint. It dials nothing: a connection
	// opened at boot would make the application refuse to start because the
	// cache is down, which is the opposite of what a cache is for. Ping is
	// what reaches the server.
	Open(e Endpoint) (SharedStore, error)
}

// connectors holds what the imported modules registered.
var connectors = linking.NewRegistry[Connector]("cache")

// modules is the package that carries the connector for each driver name the
// collection provides, which is what the error for a missing one tells people
// to import.
var modules = map[string]string{
	"redis": "github.com/arandu-io/hesape/redis",
}

// Register records that a connector is linked into the binary.
//
// Connectors call it from init(); it is not for application code. Registering a
// driver name twice panics rather than picking one: each package initialises
// once, so a second registration means two imported modules claim the same
// value of CACHE_STORE, and finding out at boot beats finding out from a store
// that behaves differently.
func Register(c Connector) { connectors.Register(c.Driver(), c) }

// Registered reports the driver names this binary links, sorted.
func Registered() []string { return connectors.Names() }

// Linked reports whether a connector for driver is linked into the binary, and
// when none is, returns the error that names the module and the import that
// add it. setting is the environment variable that asked for the driver --
// CACHE_STORE, or SESSION_DRIVER for a session kept in the same store -- so the
// sentence points at the line the person actually wrote.
//
// It is the check a boot makes before anything is opened, so a missing import
// stops the process with the fix on screen instead of a nil store failing
// inside the first request.
func Linked(setting, driver string) error {
	if _, linked, found := connectors.Lookup(driver); !found {
		return linking.NotLinked(setting, driver, modules[driver], linked)
	}
	return nil
}

// Open returns the shared store the connector registered for driver builds
// over e. It dials nothing; see [Connector].
//
// A driver no imported module registered is the error [Linked] gives for
// CACHE_STORE. A caller resolving a different setting asks Linked first, so its
// error names that setting instead.
func Open(driver string, e Endpoint) (SharedStore, error) {
	c, linked, found := connectors.Lookup(driver)
	if !found {
		return nil, linking.NotLinked("CACHE_STORE", driver, modules[driver], linked)
	}
	return c.Open(e)
}
