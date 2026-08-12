package matching

import "net/http"

// UriValidator mirrors Illuminate\Routing\Matching\UriValidator.
//
// PHP matches the request path against the compiled path regex Symfony built.
// There is no compiled route here -- the pattern is matched by the same code
// the table's match uses -- so the validator asks the route, with the method
// left out: the method is MethodValidator's question.
type UriValidator struct{}

// Matches is UriValidator::matches.
func (v UriValidator) Matches(route Route, req *http.Request) bool {
	if req == nil {
		return false
	}
	return route.Matches(req, false)
}
