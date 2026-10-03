package redis

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/session"
)

// driver is the value CACHE_STORE and SESSION_DRIVER take to ask for the store
// this package registers.
const driver = "redis"

// Importing the package is what links the RESP store into the binary: the
// registration below is the whole of what a blank import does, and cache.Open
// is what then builds the store from the endpoint the configuration describes.
func init() { cache.Register(connector{}) }

// connector is what this package registers with the cache.
type connector struct{}

// Driver is the value the configuration names this connector by.
func (connector) Driver() string { return driver }

// Open returns the store over the endpoint. It dials nothing -- see
// connections.Connect -- so a store that is down is reported by Ping and by the
// health check, not by a boot that refuses to start.
//
// An endpoint with no address is refused. connections.Connect reads an empty
// address as a server on this machine, which is right for a value written in
// code and wrong for one read from the environment: there it means nobody set
// it, and a process that quietly talked to localhost instead would share
// nothing with the replicas beside it.
func (connector) Open(e cache.Endpoint) (cache.SharedStore, error) {
	if e.Address == "" {
		return nil, errors.New("redis: the endpoint names no address, so there is no server to share the store on")
	}

	conn := connections.Connect(connections.Config{
		Address:  e.Address,
		Password: e.Password,
		Database: e.Database,
		Prefix:   e.Prefix,
		TLS:      e.TLS,
	})
	return &Shared{RedisStore: NewRedisStore(conn)}, nil
}

// Shared is the store cache.Open returns for the redis driver: the RESP store,
// with the calls that manage its connection and the sessions kept over it.
//
// It is a RedisStore and not a second store beside one, so the keys it writes
// are the keys a RedisStore over the same connection writes -- cache entries
// under the application prefix, locks under lock:, sessions under session: and
// session-index: -- and swapping one wiring for the other moves no data.
//
// Every method of RedisStore is promoted, which keeps the optional halves a
// caller asserts for -- cache.CurrentOwner, cache.CanFlushLocks -- answering
// through it.
type Shared struct {
	*RedisStore
}

var (
	_ cache.SharedStore   = (*Shared)(nil)
	_ cache.CurrentOwner  = (*Shared)(nil)
	_ cache.CanFlushLocks = (*Shared)(nil)
	_ session.Keeper      = (*Shared)(nil)
)

// Ping verifies that the server answers. It is the health check of a store
// cache.Open built, since opening it dialled nothing.
func (s *Shared) Ping(ctx context.Context) error { return s.Connection().Ping(ctx) }

// Close releases the connection the store, its locks and its sessions share.
func (s *Shared) Close() error { return s.Connection().Close() }

// Sessions returns the session handler over the store's connection, with each
// payload as the JSON it is stored as. session.Decode turns it into the handler
// for the application's payload type.
//
// It is the handler NewCacheBasedSessionHandler returns, so a session written
// through it is byte for byte the session a handler for the typed payload
// writes, and either reads what the other wrote.
func (s *Shared) Sessions() session.Handler[json.RawMessage] {
	return NewCacheBasedSessionHandler[json.RawMessage](s.Connection())
}
