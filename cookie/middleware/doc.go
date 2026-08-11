// Package middleware mirrors Illuminate\Cookie\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/cookie/Middleware:
//
//	AddQueuedCookiesToResponse.php
//	EncryptCookies.php
//
// Both are identical in the clone and in
// reference_laravel/framework/src/Illuminate/Cookie/Middleware.
//
// Each is a struct with a Handle method, mirroring the PHP class with its
// handle method. Handle takes the next handler and returns one, rather than
// taking the request and a closure: in PHP the response comes back as a value
// and can still be changed, while here it is written as the handler runs, so
// both middlewares wrap the http.ResponseWriter and do their work at the last
// moment before the header goes out.
//
// The order is Laravel's global order, [EncryptCookies] outside
// [AddQueuedCookiesToResponse], so that a queued cookie is encrypted too:
//
//	r.Get("/", h, encrypt.Handle, queue.Handle)
package middleware
