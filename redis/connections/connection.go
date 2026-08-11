package connections

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Config is what it takes to open a connection.
//
// It answers the array a connector is handed in Laravel -- host, port, password,
// database, prefix -- as a typed struct, because a map of strings is a set of
// keys nobody can list and a typo in one of them is a default nobody chose.
type Config struct {
	// Address is host:port. It is one address, not a cluster: RESP sharding
	// belongs to the deployment, and Dragonfly exists precisely so that a single
	// node covers what a Redis cluster used to.
	Address string

	// Password is optional, and comes from configuration -- never from a
	// literal.
	Password string

	// Database is the numbered database. Zero is right for almost everything;
	// separating environments belongs to separate instances, not to db 1.
	Database int

	// Prefix namespaces every key of this application, so two applications can
	// share one server without one flushing the other's entries.
	//
	// It is the application's prefix and nothing else. The tenant is NOT here:
	// it is a key segment that cache.Repository builds from the Grant, and a
	// prefix chosen at boot could not carry it (RULE 14).
	Prefix string

	// DialTimeout bounds the connect. Unbounded, a key-value store that is down
	// turns into requests that hang rather than requests that fail.
	DialTimeout time.Duration

	// ReadTimeout bounds each command.
	ReadTimeout time.Duration
}

// Connection is one open connection.
//
// It wraps the driver rather than exposing it as the thing callers hold, for
// the same reason database.DB wraps *sql.DB: what goes through the wrapper
// carries the application prefix, and what bypasses it does not. Client is the
// hatch for the commands this type does not name.
type Connection struct {
	client *goredis.Client
	prefix string
}

// Connect opens the connection. It does not talk to the server: use Ping for
// that, which is what the module health check does.
func Connect(cfg Config) *Connection {
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:6379"
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 3 * time.Second
	}

	return &Connection{
		client: goredis.NewClient(&goredis.Options{
			Addr:         cfg.Address,
			Password:     cfg.Password,
			DB:           cfg.Database,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.ReadTimeout,
		}),
		prefix: strings.TrimSuffix(cfg.Prefix, ":"),
	}
}

// Client returns the driver client.
//
// It answers Connection::client(). Everything reached through it bypasses Key,
// so a command written here carries no application prefix unless it is built
// with one.
func (c *Connection) Client() *goredis.Client { return c.client }

// Prefix is what Key puts in front of every key, without its separator.
//
// It answers the getPrefix() every store and connection in Laravel has.
func (c *Connection) Prefix() string { return c.prefix }

// Key puts the application prefix in front of a key that is already built.
//
// The key it is given is whole: cache.Repository has already put the tenant and
// the namespace in it. This adds only what separates one application from
// another on a shared server, and an empty prefix adds nothing rather than a
// leading colon.
func (c *Connection) Key(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + ":" + key
}

// Ping verifies the connection. It feeds the module health check.
//
// It answers Connection::ping().
func (c *Connection) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}

// Close releases the pool. It answers Connection::disconnect().
func (c *Connection) Close() error { return c.client.Close() }
