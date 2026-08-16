// Package ratelimiting is reserved, and holds no code.
//
// The rate limiter and the Limit it counts against live in the parent package,
// next to each other: a subpackage is a real boundary, and one struct on the
// far side of one is an import for no gain. Use cache.Limit, cache.PerMinute
// and cache.None.
package ratelimiting
