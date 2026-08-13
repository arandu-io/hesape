// Package middleware mirrors Illuminate\Http\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Middleware:
//
//	AddLinkHeadersForPreloadedAssets.php -> middleware.go
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
// # Not mirrored, and why (ADR 0044)
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
