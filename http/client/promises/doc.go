// Package promises provides the two promise-like types the HTTP client
// returns from an asynchronous request: [FluentPromise] and [LazyPromise].
//
// [Deferred] is what both are built around: a value that is not there yet,
// settled from whichever goroutine is doing the work, with Wait blocking on
// a channel until it is.
package promises
