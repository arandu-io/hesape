// Package middleware is Arandu's Http\Middleware.
//
// It holds the middleware that is about the request itself rather than about
// anything the application knows: [SecurityHeaders], [TrustProxies] and
// [LimitBodySize]. Authentication is in hesape/auth/middleware, the CSRF check
// and the observability root are in hesape/foundation, the panic net is in
// hesape/exception, and throttling is in hesape/routing/middleware -- each of
// them next to the thing it needs, which is why they are not all here.
//
// Every one of these returns an httpx.Middleware, which is an alias of
// pipeline.Middleware[http.Handler] and therefore of the standard
// func(http.Handler) http.Handler. Compose them with pipeline.Chain.
//
// CORS is not here yet. Its options struct is a decision -- which origins, which
// methods, credentials or not, how long a preflight is cached -- and a decision
// is not made while moving files.
package middleware
