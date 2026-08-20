package connections

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Config is what it takes to open a connection.
//
// It is a typed struct rather than a map of strings, because a map is a set of
// keys nobody can list and a typo in one of them is a default nobody chose.
type Config struct {
	// Address is host:port. It is the single-node case, and it is the one
	// almost everything wants: Dragonfly exists precisely so that a single node
	// covers what a Redis cluster used to.
	Address string

	// Addresses is the cluster, when the deployment shards.
	//
	// It is a field rather than a second connection type because that is all
	// the difference amounts to here: the driver routes by slot when it is
	// given more than one address, and every command in this package is written
	// the same either way.
	//
	// When it is set, Address is ignored. Nothing in this adapter uses a
	// multi-key command across slots, which is what makes that safe -- see the
	// note on Lua and modules in the package documentation.
	Addresses []string

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
	// prefix chosen at boot could not carry it.
	Prefix string

	// DialTimeout bounds the connect. Unbounded, a key-value store that is down
	// turns into requests that hang rather than requests that fail.
	DialTimeout time.Duration

	// ReadTimeout bounds each command.
	ReadTimeout time.Duration

	// TLS encrypts the connection. Nil leaves it in the clear, which is the
	// default because a client that demanded encryption would refuse every
	// server that does not offer it. Turning it on is the operator's decision,
	// and on any network the process does not own it is the only correct one:
	// without it the password, the session ids and every cached value cross the
	// wire as plain text, and so does the length the server declares for each
	// reply.
	//
	// It is crypto/tls's own type rather than a handful of narrower fields,
	// because the narrower fields do not reach the case that needs them. A
	// managed endpoint presents a certificate a public authority signed, and for
	// that a zero &tls.Config{} is the whole configuration -- an empty
	// ServerName becomes the host half of the address being dialled, which is
	// the name the certificate has to carry. A self-hosted server does neither:
	// it presents a certificate signed by an authority of its own and, by
	// default, demands one from the client in return. A private root and a
	// client certificate are what tls.Config already says, and a second
	// vocabulary for them here would say less of it.
	//
	// One configuration covers every entry in Addresses, for the same reason:
	// leaving ServerName empty lets each node be verified under its own host.
	// Naming one there names it for all of them.
	//
	// The value is used as given, not copied, so it must not be mutated after
	// Connect returns.
	TLS *tls.Config
}

// Connection is one open connection.
//
// It wraps the driver rather than exposing it as the thing callers hold, for
// the same reason database.DB wraps *sql.DB: what goes through the wrapper
// carries the application prefix, and what bypasses it does not. Client is the
// hatch for the commands this type does not name.
type Connection struct {
	// PacksPhpRedisValues is embedded so that Pack and the questions about
	// serialization are reachable on a connection. Every answer is a constant
	// -- see the type.
	PacksPhpRedisValues

	client goredis.UniversalClient
	prefix string

	// mu guards the two fields the manager writes after construction. One
	// Connection is shared by every goroutine serving a request, and
	// `go test -race` says so.
	mu     sync.RWMutex
	name   string
	events Dispatcher
}

// Dispatcher is the slice of an event dispatcher this connection needs.
//
// It is declared here rather than imported so that the adapter's own module
// does not pull the whole event system in to fire one event; hesape/events
// satisfies it as written.
type Dispatcher interface {
	// Dispatch fires the event. The name is the event name and the payload is
	// what listeners receive.
	Dispatch(event any, payload ...any) []any
	// Listen registers a listener for one or more event names.
	Listen(events any, listener ...any)
}

// Connect opens the connection. It does not talk to the server: use Ping for
// that, which is what the module health check does.
func Connect(cfg Config) *Connection {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 3 * time.Second
	}

	addrs := cfg.Addresses
	if len(addrs) == 0 {
		if cfg.Address == "" {
			cfg.Address = "127.0.0.1:6379"
		}
		addrs = []string{cfg.Address}
	}

	return &Connection{
		// NewUniversalClient returns the single-node client for one address and
		// the cluster client for several, which is the whole of what the
		// single-node and cluster cases differ by.
		client: goredis.NewUniversalClient(&goredis.UniversalOptions{
			Addrs:        addrs,
			Password:     cfg.Password,
			DB:           cfg.Database,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.ReadTimeout,
			TLSConfig:    cfg.TLS,
		}),
		prefix: strings.TrimSuffix(cfg.Prefix, ":"),
	}
}

// Client returns the driver client.
//
// It answers Connection::client(). Everything reached through it bypasses Key,
// so a command written here carries no application prefix unless it is built
// with one.
//
// The type is the driver's UniversalClient interface because a clustered
// connection is a different concrete type and the same set of commands; see
// Config.Addresses.
func (c *Connection) Client() goredis.UniversalClient { return c.client }

// Prefix is what Key puts in front of every key, without its separator.
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
func (c *Connection) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}

// Close releases the pool. It answers Connection::disconnect().
func (c *Connection) Close() error { return c.client.Close() }
