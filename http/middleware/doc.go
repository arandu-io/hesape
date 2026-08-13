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
package middleware
