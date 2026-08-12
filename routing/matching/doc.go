// Package matching holds the four checks that decide whether a route answers
// a request: its host, its method, its scheme and its path.
//
// It mirrors Illuminate\Routing\Matching. The files it answers to, in the
// clone at laravel_illuminate/Routing/Matching:
//
//	HostValidator.php
//	MethodValidator.php
//	SchemeValidator.php
//	UriValidator.php
//	ValidatorInterface.php
//
// Route.GetValidators returns the four in the order PHP lists them, and
// hesape/routing uses them for the questions the mux does not answer: the mux
// has already picked a route by method and path, and a middleware, a URL
// generator or a custom dispatcher still has to know whether a route answers a
// host and a scheme.
//
// PHP asks the compiled route -- Symfony's path and host regexes -- and there
// is no compiled route here. UriValidator asks the route to match its own
// pattern instead, and HostValidator compares the declared domain to the
// request host.
package matching
