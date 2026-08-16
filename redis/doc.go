// Package redis is the RESP adapter: the cache store, the distributed lock and
// the distributed session handler.
//
// # The name says Redis and the protocol says RESP
//
// The protocol is RESP, and RESP is one protocol and four products: Dragonfly,
// Redis, Valkey and KeyDB all answer it, and switching between them is a
// connection string. The package is called redis because that is the name the
// store, the lock and the session handler are looked for under. The recommended
// default is Dragonfly -- multi-threaded, and compatible enough that adopting it
// costs nothing.
//
// # Nothing here may depend on RedisJSON, RediSearch or advanced Lua
//
// That is the price of the compatibility above, and it is a restriction this
// package accepts everywhere. The moment something here needs a module or a
// script, Dragonfly stops being a drop-in replacement and four products become
// one -- which is the reason this adapter exists at all rather than being a
// dependency on a product.
//
// It is why the lock releases through WATCH/MULTI/EXEC instead of the canonical
// one-line script, why the session index is a sorted set rather than a JSON
// document, and why the rate limit is a fixed window rather than a sliding one.
// Each of those is a line longer and portable, and portable is the feature.
//
// # It is its own Go module
//
// This package carries its own go.mod. The driver is a third-party dependency,
// Go has no optional dependency, and the root module of hesape takes nothing
// beyond golang.org/x/crypto -- so without the split, every project would have
// downloaded a RESP driver in order to use the in-process cache.
//
// That is also why everything driver-dependent lives here rather than beside
// what it serves: the store, the lock and the session handler are together, one
// import away from the components they plug into.
//
// # What is here
//
//	RedisStore                  a cache.Store, a cache.Locking, a
//	                            cache.CurrentOwner and a cache.CanFlushLocks
//	CacheBasedSessionHandler    a session.Handler over RESP
//	Module                      the health check and the shutdown
//	connections.Connection      the connection the three of them share
//
// The lock is not a type in this package. cache.Lock is the lock in the whole
// collection, and what a backend supplies is the two atomic operations it needs
// -- so the RESP lock arrives as the Locking half of RedisStore, and the handle
// with Get, Block, Release, Owner and ForceRelease on it is the one every store
// shares:
//
//	store := redis.NewRedisStore(conn)
//	locks := cache.NewLocks(store)
//	err := locks.Lock("outbox-relay", 30*time.Second).Run(ctx, publish)
//
// The rate limiter is not a type here either. cache.RateLimiter counts against
// a cache.Store, so wiring this one in is what makes the count distributed:
//
//	limiter := cache.NewRateLimiter(redis.NewRedisStore(conn))
//
// Tags are not here: they are not in this collection, and hesape/cache says so.
package redis
