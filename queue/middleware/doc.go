// Package middleware wraps the handling of a job.
//
// It mirrors Illuminate\Queue\Middleware. A middleware sits between the worker
// and the handler and can decide the work should not happen now -- another
// worker holds the lock, the budget for this minute is spent, the batch was
// cancelled -- by putting the job back on its queue instead of running it.
//
// The shape is Laravel's handle($job, $next), with the context Go carries
// everywhere added: [Middleware.Handle] gets the job and the rest of the chain,
// and what it does with the job is release it, delete it, fail it, or hand it
// on.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Middleware:
//
//	FailOnException.php          -> FailOnException
//	RateLimited.php              -> RateLimited
//	RateLimitedWithRedis.php     -> RateLimited, over a Redis-backed cache.Store
//	Skip.php                     -> SkipWhen, SkipUnless
//	SkipIfBatchCancelled.php     -> SkipIfBatchCancelled
//	ThrottlesExceptions.php      -> ThrottlesExceptions
//	ThrottlesExceptionsWithRedis.php -> ThrottlesExceptions, same
//	WithoutOverlapping.php       -> WithoutOverlapping
//
// The two WithRedis variants have no separate type here, and that is RULE 9
// rather than an omission: in Laravel they exist because the Redis limiter is a
// different class with a Lua script behind it. Here the limiter is
// cache.RateLimiter and which store it counts in is wiring, so the Redis
// version of RateLimited is RateLimited with a Redis-backed cache.Store.
//
// # Every key is scoped by tenant
//
// The lock a job takes and the counter a job spends are named after the tenant
// the job belongs to (RULE 14). Laravel has no equivalent because it has no
// tenant, and without it one customer's slow import would rate limit every
// other customer's.
package middleware
