package matching

// HostValidator mirrors Illuminate\Routing\Matching\HostValidator.
//
// It validates that the incoming request's host matches the route's domain
// constraint. When the route has no domain, the check passes.
type HostValidator struct{}

// Matches reports whether the host of the request matches the route's domain.
//
// When the route carries no domain constraint, every host matches.
func (v HostValidator) Matches(route interface{ GetDomain() string }, host string) bool {
	domain := route.GetDomain()
	if domain == "" {
		return true
	}
	return domain == host
}
