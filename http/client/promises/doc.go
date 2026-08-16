// Package promises mirrors Illuminate\Http\Client\Promises.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Client/Promises:
//
//	FluentPromise.php -> fluent_promise.go
//	LazyPromise.php   -> lazy_promise.go
//
// Both decorate a Guzzle promise in the PHP. There is no Guzzle here, so
// [Deferred] stands in: a value that is not there yet, settled from whichever
// goroutine is doing the work, with Wait blocking on a channel until it is.
//
// # Not mirrored, and why (ADR 0056)
//
//	FluentPromise::getGuzzlePromise    is GetUnderlyingPromise. The PHP name
//	                                   says which library the decorated object
//	                                   came from, and this package has never
//	                                   heard of Guzzle.
package promises
