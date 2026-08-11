package matching

// SchemeValidator mirrors Illuminate\Routing\Matching\SchemeValidator.
//
// It validates that the incoming request's scheme matches the route's
// constraint. The route may declare itself http-only or https-only.
type SchemeValidator struct{}

// Matches reports whether the request's scheme satisfies the route's
// scheme restrictions.
//
// A route with no restriction passes. An http-only route rejects https,
// and an https-only route rejects http.
func (v SchemeValidator) Matches(route SchemeRoute, isSecure bool) bool {
	if route.IsHTTPOnly() {
		return !isSecure
	}
	if route.IsHTTPSOnly() {
		return isSecure
	}
	return true
}

// SchemeRoute is the interface a route must implement for scheme validation.
type SchemeRoute interface {
	IsHTTPOnly() bool
	IsHTTPSOnly() bool
}
