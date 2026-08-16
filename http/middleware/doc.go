// Package middleware mirrors Illuminate\Http\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Middleware:
//
//	CheckResponseForModifications.php    -> middleware.go
//	FrameGuard.php                       -> middleware.go
//	HandleCors.php                       -> middleware.go, state.go
//	SetCacheHeaders.php                  -> middleware.go
//	TrustHosts.php                       -> middleware.go, state.go
//	TrustProxies.php                     -> trustproxies.go
//	ValidatePathEncoding.php             -> middleware.go
//	ValidatePostSize.php                 -> middleware.go, limitbody.go
//
// A middleware here is a func(http.Handler) http.Handler, not a class with a
// handle method, because that is the shape net/http composes. The class
// statics -- what the PHP configures from bootstrap/app.go -- are package
// functions in state.go.
//
// # AddLinkHeadersForPreloadedAssets does not exist here
//
// The PHP emits a Link: rel=preload header naming the assets Vite said it
// preloaded -- Vite::preloadedAssets(), read out of a build manifest. There is
// no manifest here and no Vite: RULE 13 keeps Node out of the toolchain, and the
// assets are embedded with go:embed and served content-addressed, with the hash
// in the path.
//
// It was written as a middleware that took a limit and passed the request
// through untouched, with a doc comment saying it added Link headers. That is
// worse than the absence: nothing fails, the header never appears, and the
// reason is invisible to whoever wired it. Removed, and named here instead.
//
// If a preload hint is wanted later it is one header on a response whose asset
// URLs the renderer already knows, and it is not this shape. HTTP/2 server push,
// which the PHP's own doc comment mentions, is not the reason to build it:
// Chrome removed push in 2022.
//
// # Not mirrored, and why (ADR 0056)
//
//	SetCacheHeaders::using                     builds the router's middleware
//	AddLinkHeadersForPreloadedAssets::using    alias string, "Class:params",
//	                                           for a string-keyed middleware
//	                                           registry. A Go middleware is a
//	                                           function value and the
//	                                           parameters are its arguments:
//	                                           SetCacheHeaders(options) is
//	                                           what using(options) names.
//	TrustProxies::at                           sets the static the PHP
//	                                           constructor reads because the
//	                                           container builds the middleware
//	                                           with no arguments. ADR 0001
//	                                           rejected the container, so the
//	                                           proxies are the argument to
//	                                           TrustProxies.
//
// # There was a second TrustProxies, and it trusted everything
//
// middleware.go carried TrustProxiesHTTP alongside [TrustProxies], with no
// callers and no tests. It is deleted. Two ways to do one thing is RULE 9, and
// this pair was worse than that: the one that stays takes []netip.Prefix, and
// the one that went took []string and compared them against r.RemoteAddr --
// which carries the port, so a trusted "10.0.0.1" never matched "10.0.0.1:54321"
// and every proxy was untrusted. It also accepted "*", which trusts any peer at
// all: with it set, any client on the internet could send X-Forwarded-For and be
// recorded as whatever address it liked. And it assigned the whole header value
// to RemoteAddr, so a chained "a, b, c" became the remote address verbatim.
//
// A middleware that is wrong in the trusting direction is the kind nobody
// notices, because everything keeps working.
package middleware
