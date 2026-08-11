// Package ratelimiting mirrors Illuminate\Cache\RateLimiting.
//
// The files it answers to, in the clone at
// laravel_illuminate/cache/RateLimiting:
//
//	GlobalLimit.php
//	Limit.php
//	Unlimited.php
//
// Nothing is implemented here, and nothing will be: Limit landed in the parent
// package, next to the RateLimiter that counts against it, which is where
// docs/31-reorganizacao-hesape.md puts it. In PHP a sub-namespace is free; in
// Go a subpackage is a real boundary, and one struct on the far side of one is
// an import for no gain.
//
// Limit::perSecond, perMinute, perMinutes, perHour, perDay, by, after,
// response, fallbackKey and none are all there, under those names.
//
// GlobalLimit and Unlimited do not arrive as types. Both are one constructor's
// worth of difference from Limit -- a key that is always empty, a maximum that
// is always the largest integer -- and two more spellings of the same value is
// a second way to write it (RULE 9). What they are for is reachable: cache.None
// is Limit::none(), which is the only thing that ever returned an Unlimited.
package ratelimiting
