package matching

import "strings"

// MethodValidator mirrors Illuminate\Routing\Matching\MethodValidator.
//
// It validates that the incoming request's method is one of the route's
// accepted methods.
type MethodValidator struct{}

// Matches reports whether method is among the route's methods.
func (v MethodValidator) Matches(route interface{ GetMethods() []string }, method string) bool {
	for _, m := range route.GetMethods() {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return len(route.GetMethods()) == 0
}
