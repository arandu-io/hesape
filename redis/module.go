package redis

import (
	"context"

	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/routing"
)

// Module reports the connection on the health check and closes it on shutdown.
//
// It is what registers the component with the lifecycle. It owns no tables and
// registers no routes.
//
// It exists so that "the key-value store is down" shows up on
// /_arandu/health next to the database, rather than as a class of request
// failures somebody has to correlate by hand.
type Module struct {
	conn *connections.Connection
}

// NewModule returns the module.
func NewModule(c *connections.Connection) *Module { return &Module{conn: c} }

var (
	_ foundation.Module   = (*Module)(nil)
	_ foundation.Health   = (*Module)(nil)
	_ foundation.Closable = (*Module)(nil)
)

// Name is the module identifier.
func (*Module) Name() string { return "redis" }

// Routes registers nothing.
func (*Module) Routes(*routing.Router) {}

// Health pings the server.
func (m *Module) Health(ctx context.Context) error { return m.conn.Ping(ctx) }

// Close releases the pool on shutdown.
func (m *Module) Close(context.Context) error { return m.conn.Close() }
