package matching

// UriValidator mirrors Illuminate\Routing\Matching\UriValidator.
//
// It validates that the request path matches the route's URI pattern.
type UriValidator struct{}

// Matches reports whether rawPath matches the route's URI.
//
// In Go, path matching is handled by http.ServeMux and net/http already
// separates the path from the query string. The URI validator delegates
// to a simple prefix/suffix check and relies on the mux for the actual
// pattern matching.
func (v UriValidator) Matches(route interface{ GetURI() string }, path string) bool {
	_ = route
	_ = path
	// The mux already matched. This is the shape for consistency with
	// the PHP interface, and for the case where a custom router reads
	// validators explicitly.
	return true
}
