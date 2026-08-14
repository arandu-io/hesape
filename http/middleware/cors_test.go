package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/http/middleware"
)

// The wildcard and credentials together are refused before anything serves.
//
// A browser rejects a response carrying both, and the workaround everybody
// reaches for -- echo the caller's Origin instead of "*" -- passes the browser
// check and lets every site read the responses of anybody signed in. A wrong
// CORS policy does not fail; it works, quietly, for the attacker.
func TestTheWildcardWithCredentialsIsRefusedAtBoot(t *testing.T) {
	cfg := middleware.CorsConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}

	if err := cfg.Valid(); err == nil {
		t.Fatal("a wildcard origin with credentials was accepted; that is every site reading authenticated responses")
	}

	cfg.AllowCredentials = false
	if err := cfg.Valid(); err != nil {
		t.Errorf("a wildcard without credentials is fine and was refused: %v", err)
	}

	cfg = middleware.CorsConfig{AllowedOrigins: []string{"https://app.example"}, AllowCredentials: true}
	if err := cfg.Valid(); err != nil {
		t.Errorf("a named origin with credentials is the normal case and was refused: %v", err)
	}
}

// Even if Valid is never called, the middleware does not send the pair.
func TestCredentialsAreNeverSentWithTheWildcard(t *testing.T) {
	mw := middleware.HandleCors([]string{"*"}, nil, nil, 0, true)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://attacker.test")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin is %q, want * -- echoing the caller is what slips past the browser check", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("credentials were allowed alongside a wildcard origin")
	}
}

// The named case still works, and still carries credentials.
func TestANamedOriginStillGetsCredentials(t *testing.T) {
	mw := middleware.HandleCors([]string{"https://app.example"}, nil, nil, 0, true)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin is %q, want the named origin", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("a named origin lost its credentials")
	}
}
