package matching

import "net/http"

// Route is what a validator needs to read off a route.
//
// It is an interface and not the route type because hesape/routing imports
// this package to answer Route.GetValidators, and naming the route type here
// would be a cycle. hesape/routing.Route satisfies it.
type Route interface {
	// GetDomain is Route::getDomain.
	GetDomain() string
	// Methods is Route::methods.
	Methods() []string
	// HttpOnly is Route::httpOnly.
	HttpOnly() bool
	// Secure is Route::secure.
	Secure() bool
	// URI is Route::uri.
	URI() string
	// Matches reports whether the path matches the route pattern. It stands
	// in for Symfony's compiled path regex, which this package does not
	// carry: the pattern is matched by the same code the mux match uses.
	Matches(req *http.Request, includeMethod bool) bool
}

// ValidatorInterface is one question asked of a route and a request: does this
// route answer it? This package has the four implementations that matter --
// [UriValidator], [MethodValidator], [SchemeValidator] and [HostValidator] --
// and a route answers a request only when every one of them says so.
//
// hesape/routing.Route.GetValidators returns the four in the order PHP lists
// them, which is the set a caller ranges over. Nothing here dispatches a
// request: the mux has already picked a route by method and path, and these
// exist for the callers that still have to ask -- a middleware, a URL
// generator, a custom dispatcher.
//
// Mirrors Illuminate\Routing\Matching\ValidatorInterface.
type ValidatorInterface interface {
	// Matches is ValidatorInterface::matches.
	Matches(route Route, req *http.Request) bool
}
