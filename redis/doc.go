// Package redis is the RESP adapter: the cache store, the distributed lock and
// the distributed session handler.
//
// # The name says Redis and the protocol says RESP
//
// The package is called redis because the name is the Illuminate one (ADR
// 0044): Illuminate\Redis is where the connection lives, RedisStore is the
// cache backend and RedisLock is the lock. A Laravel developer looking for
// RedisStore finds it under the name they already hold, which is the whole
// point of the rule.
//
// The protocol is RESP, and RESP is one protocol and four products: Dragonfly,
// Redis, Valkey and KeyDB all answer it, and switching between them is a
// connection string. The recommended default is Dragonfly -- multi-threaded,
// and compatible enough that adopting it costs nothing.
//
// # Nothing here may depend on RedisJSON, RediSearch or advanced Lua
//
// That is the price of the compatibility above, and it is a restriction this
// package accepts everywhere. The moment something here needs a module or a
// script, Dragonfly stops being a drop-in replacement and four products become
// one -- which is the reason this adapter exists at all rather than being a
// dependency on a product (RULE 11).
//
// It is why the lock releases through WATCH/MULTI/EXEC instead of the canonical
// one-line script, why the session index is a sorted set rather than a JSON
// document, and why the rate limit is a fixed window rather than a sliding one.
// Each of those is a line longer and portable, and portable is the feature.
//
// # It is its own Go module
//
// ADR 0048 folded the four adapter repositories into hesape, and this one is
// the exception that keeps its own go.mod: the driver is a third-party
// dependency, Go has no optional dependency, and the CI of hesape refuses
// anything beyond golang.org/x/crypto in the root module. Every project would
// have downloaded go-redis to use the in-process cache.
//
// That is also why everything driver-dependent lives here rather than beside
// what it serves. In Laravel, RedisStore is in Illuminate\Cache, RedisLock in
// Illuminate\Cache and the Redis session handler in Illuminate\Session; here
// they are together, one import away from the components they plug into.
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
// -- so Illuminate\Cache\RedisLock arrives as the Locking half of RedisStore,
// and the handle with Get, Block, Release, Owner and ForceRelease on it is the
// one every store shares:
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
// # The files it answers to
//
// In the clone at laravel_illuminate:
//
//	redis/RedisManager.php            -> connections.Connect (ADR 0001: no manager)
//	redis/RedisServiceProvider.php    -> Module
//	cache/RedisStore.php              -> RedisStore
//	cache/RedisLock.php               -> RedisStore, the Locking half
//	session/CacheBasedSessionHandler.php -> CacheBasedSessionHandler
//
// RedisTagSet and RedisTaggedCache do not arrive: tags are not in this
// collection (see hesape/cache). LuaScripts does not arrive, and the paragraph
// above is why.
//
// # The two methods with no answer here
//
// Both are on RedisServiceProvider, and both are reason 2 of the porting rule
// -- a method that exists only to serve the container and the service provider,
// which ADR 0001 and ADR 0002 removed:
//
//   - register() binds 'redis' as a singleton over RedisManager and
//     'redis.connection' as its default connection, reading the client name out
//     of config('database.redis'). [Module] is what a kernel registers instead,
//     and the connection is [connections.Connect] over a URL (ADR 0035).
//   - provides() answers the two strings register() bound, which is the
//     deferred-provider bookkeeping those bindings need and nothing else.
package redis
