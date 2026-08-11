// Package limiters mirrors Illuminate\Redis\Limiters.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Limiters:
//
//	ConcurrencyLimiter.php        -> ConcurrencyLimiter
//	ConcurrencyLimiterBuilder.php -> ConcurrencyLimiterBuilder
//	DurationLimiter.php           -> DurationLimiter
//	DurationLimiterBuilder.php    -> DurationLimiterBuilder
//
// Two limiters, and they answer different questions. DurationLimiter is "ten
// per minute" -- a window with a count. ConcurrencyLimiter is "three at a
// time" -- a semaphore with named slots. Laravel reaches them through
// Redis::throttle() and Redis::funnel(), and so does this package, through
// connections.Connection.Throttle and .Funnel.
//
// # Neither one uses Lua
//
// Laravel implements both as EVAL scripts. RULE 11 refuses Lua here, because it
// is what keeps Dragonfly, Redis, Valkey and KeyDB one product to this
// collection: the day something needs a script, three of the four stop being
// drop-in. So the duration limiter is WATCH/MULTI/EXEC over the same three-field
// hash, and the concurrency limiter is one `SET slot id NX EX n` per slot,
// released by the compare-and-delete that RedisStore already uses.
//
// Each of those is a few lines longer than the script it replaces, and portable.
// Portable is the feature.
//
// # This is not a third rate limiter
//
// cache.RateLimiter counts hits against a cache.Store and is what the HTTP
// middleware uses; it is the general one, and pointing it at redis.RedisStore
// is what makes it distributed. DurationLimiter is the RESP-native one, and it
// is here because Redis::throttle() is a name a Laravel developer already
// holds and because a limiter that can also block and wait is a different
// contract from one that answers yes or no.
//
// If you are rate-limiting a route, reach for cache.RateLimiter. If you are
// pacing a worker against somebody else's API, reach for these.
package limiters
