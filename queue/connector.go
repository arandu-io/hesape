package queue

import (
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/internal/linking"
)

// Connector opens the queue for one driver whose client is a third-party
// dependency.
//
// A connector lives in its own module, because Go has no optional dependency
// and a client in this module would sit in the go.sum of every project that
// queues over its own database. Importing that module for its side effect is
// what links the driver into the binary:
//
//	import _ "github.com/arandu-io/hesape/queue/connectors/redis"
//
// The module's init() calls [Register], and [Open] resolves the rest from the
// driver name the configuration holds. The drivers that need nothing installed
// are in this package beside the contract, and register nothing.
//
// The endpoint is the cache's, because the server a queue of this kind talks to
// is the one a shared cache store does, described by the same settings.
type Connector interface {
	// Driver is the value QUEUE_CONNECTION takes to ask for this connector.
	Driver() string

	// Open returns the queue over the endpoint. It dials nothing: a binary
	// that runs a migration must not open a socket to the queue on the way
	// past, and the first push or pop is what reaches the server.
	Open(e cache.Endpoint) (Queue, error)
}

// connectors holds what the imported modules registered.
var connectors = linking.NewRegistry[Connector]("queue")

// modules is the package that carries the connector for each driver name the
// collection provides, which is what the error for a missing one tells people
// to import.
var modules = map[string]string{
	"redis": "github.com/arandu-io/hesape/queue/connectors/redis",
}

// Register records that a connector is linked into the binary.
//
// Connectors call it from init(); it is not for application code. Registering a
// driver name twice panics rather than picking one: each package initialises
// once, so a second registration means two imported modules claim the same
// value of QUEUE_CONNECTION, and finding out at boot beats finding out from a
// queue that loses jobs differently.
func Register(c Connector) { connectors.Register(c.Driver(), c) }

// Linked reports whether a connector for driver is linked into the binary, and
// when none is, returns the error that names the module and the import that
// add it. setting is the environment variable that asked for the driver, which
// the sentence opens with.
//
// It is the check a boot makes before anything is opened, so a missing import
// stops the process with the fix on screen instead of at the first push.
func Linked(setting, driver string) error {
	if _, linked, found := connectors.Lookup(driver); !found {
		return linking.NotLinked(setting, driver, modules[driver], linked)
	}
	return nil
}

// Open returns the queue the connector registered for driver builds over e.
// It dials nothing; see [Connector].
//
// A driver no imported module registered is the error [Linked] gives for
// QUEUE_CONNECTION.
func Open(driver string, e cache.Endpoint) (Queue, error) {
	c, linked, found := connectors.Lookup(driver)
	if !found {
		return nil, linking.NotLinked("QUEUE_CONNECTION", driver, modules[driver], linked)
	}
	return c.Open(e)
}
