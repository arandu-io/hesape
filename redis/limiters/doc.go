// Package limiters mirrors Illuminate\Redis\Limiters.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Limiters:
//
//	ConcurrencyLimiter.php
//	ConcurrencyLimiterBuilder.php
//	DurationLimiter.php
//	DurationLimiterBuilder.php
//
// Nothing is implemented here, and DurationLimiter is the reason to say so out
// loud rather than leave the directory looking unfinished.
//
// # DurationLimiter arrived as RedisStore.Increment
//
// A duration limiter is N attempts per window, and this collection has one of
// those: cache.RateLimiter counts against a cache.Store, and wiring
// redis.RedisStore in is what makes the count distributed.
//
//	limiter := cache.NewRateLimiter(redis.NewRedisStore(conn))
//
// There were two rate limiters before -- an in-memory one in the HTTP
// middleware, which counted per process, so N replicas allowed N times the
// limit on the one endpoint where that gap is worth exploiting, and a second
// one in the kv adapter. Writing a DurationLimiter here would be the third,
// with its own window arithmetic and its own answer to what happens when the
// server cannot be reached. That is precisely the second way RULE 9 refuses,
// and cache.RateLimiter exists because it was already paid for once.
//
// # ConcurrencyLimiter does not arrive
//
// It is a semaphore, and Laravel's is a Lua script. RULE 11 refuses Lua, which
// is what keeps Dragonfly, Redis, Valkey and KeyDB one product to this
// collection, so it would have to be a WATCH/MULTI/EXEC over a sorted set --
// the shape RedisStore.ReleaseLock already uses. Nothing in the collection asks
// for one: what wants to run alone runs under cache.Lock, and what wants a
// bounded number of workers sets that number on the worker.
//
// It arrives the day something needs it, and not before.
package limiters
