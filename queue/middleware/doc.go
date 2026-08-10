// Package middleware mirrors Illuminate\Queue\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Middleware:
//
//	FailOnException.php
//	RateLimited.php
//	RateLimitedWithRedis.php
//	Skip.php
//	SkipIfBatchCancelled.php
//	ThrottlesExceptions.php
//	ThrottlesExceptionsWithRedis.php
//	WithoutOverlapping.php
//
// Nothing is implemented here yet. docs/31-reorganizacao-hesape.md says what
// moves in, from where, and in which phase.
package middleware
