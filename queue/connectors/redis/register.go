package redis

import (
	"errors"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/queue"
)

// Importing the package is what links the RESP queue into the binary: the
// registration below is the whole of what a blank import does, and queue.Open
// is what then builds the queue from the endpoint the configuration describes.
func init() { queue.Register(connector{}) }

// driver is the value QUEUE_CONNECTION takes to ask for the queue this package
// registers.
const driver = "redis"

// connector is what this package registers with the queue.
type connector struct{}

// Driver is the value QUEUE_CONNECTION takes to ask for this connector.
func (connector) Driver() string { return driver }

// Open returns the queue over the endpoint. It dials nothing: the client
// connects on the first command, so a binary that runs a migration never opens
// a socket to the queue.
//
// An endpoint with no address is refused. New reads an empty address as a
// server on this machine, which is right for a value written in code and wrong
// for one read from the environment: there it means nobody set it, and a
// worker that quietly popped from localhost would never see a job the web
// replicas pushed.
func (connector) Open(e cache.Endpoint) (queue.Queue, error) {
	if e.Address == "" {
		return nil, errors.New("queue/redis: the endpoint names no address, so there is no server to queue on")
	}
	return New(Options{
		Address:  e.Address,
		Password: e.Password,
		Database: e.Database,
		Prefix:   e.Prefix,
		TLS:      e.TLS,
	}), nil
}
