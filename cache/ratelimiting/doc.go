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
// GlobalLimit and Unlimited do not arrive at all. A limit that allows
// everything is a route with no limit on it, and a second way to write that is
// a second way (RULE 9).
package ratelimiting
