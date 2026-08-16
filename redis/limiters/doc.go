// Package limiters holds the two RESP-native limiters.
//
// They answer different questions. DurationLimiter is "ten per minute" -- a
// window with a count. ConcurrencyLimiter is "three at a time" -- a semaphore
// with named slots. Both are reached through
// connections.Connection.Throttle and .Funnel.
//
// # Neither one uses a server-side script
//
// A script would make three of the four RESP products stop being drop-in
// replacements for each other, so the duration limiter is WATCH/MULTI/EXEC over
// a three-field hash, and the concurrency limiter is one `SET slot id NX EX n`
// per slot, released by the compare-and-delete that RedisStore already uses.
//
// Each of those is a few lines longer than the script it replaces, and
// portable. Portable is the feature.
//
// # This is not a third rate limiter
//
// cache.RateLimiter counts hits against a cache.Store and is what the HTTP
// middleware uses; it is the general one, and pointing it at redis.RedisStore
// is what makes it distributed. DurationLimiter is the RESP-native one, and it
// is here because a limiter that can also block and wait is a different
// contract from one that answers yes or no.
//
// If you are rate-limiting a route, reach for cache.RateLimiter. If you are
// pacing a worker against somebody else's API, reach for these.
package limiters
