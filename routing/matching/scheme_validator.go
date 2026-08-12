package matching

import "net/http"

// SchemeValidator mirrors Illuminate\Routing\Matching\SchemeValidator.
//
// It validates that the incoming request's scheme satisfies the route's
// restriction. A route with no restriction passes; an http-only route rejects
// https, and a secure route rejects http.
type SchemeValidator struct{}

// Matches is SchemeValidator::matches.
func (v SchemeValidator) Matches(route Route, req *http.Request) bool {
	secure := req != nil && req.TLS != nil
	if route.HttpOnly() {
		return !secure
	}
	if route.Secure() {
		return secure
	}
	return true
}
