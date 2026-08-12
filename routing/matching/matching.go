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

// ValidatorInterface mirrors Illuminate\Routing\Matching\ValidatorInterface.
type ValidatorInterface interface {
	// Matches is ValidatorInterface::matches.
	Matches(route Route, req *http.Request) bool
}
