package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/http/middleware"
)

// TestHandleCorsWithRefusesTheUnsafePolicy: the policy Valid refuses never
// becomes a middleware, so the refusal is heard where the application is wired.
func TestHandleCorsWithRefusesTheUnsafePolicy(t *testing.T) {
	mw, err := middleware.HandleCorsWith(middleware.CorsConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
	if err == nil || mw != nil {
		t.Fatalf("HandleCorsWith = (%v, %v), want no middleware and the refusal", mw != nil, err)
	}
}

// TestHandleCorsWithAnswersAPreflightFromThePolicy: the methods, the headers
// and the lifetime come from the config, the lifetime in whole seconds.
func TestHandleCorsWithAnswersAPreflightFromThePolicy(t *testing.T) {
	mw, err := middleware.HandleCorsWith(middleware.CorsConfig{
		AllowedOrigins:   []string{"https://app.example"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		MaxAge:           2 * time.Hour,
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatalf("HandleCorsWith: %v", err)
	}
	r := httptest.NewRequest(http.MethodOptions, "/api", nil)
	r.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(w, r)

	h := w.Header()
	if w.Code != http.StatusNoContent ||
		h.Get("Access-Control-Allow-Origin") != "https://app.example" ||
		h.Get("Access-Control-Allow-Credentials") != "true" ||
		h.Get("Access-Control-Allow-Methods") != "GET, POST" ||
		h.Get("Access-Control-Allow-Headers") != "Content-Type" ||
		h.Get("Access-Control-Max-Age") != "7200" {
		t.Fatalf("preflight answered %d with %v", w.Code, h)
	}
}

// TestHandleCorsWithExposesTheHeadersItNames: without the expose header a
// script cannot read a header the response plainly carries.
func TestHandleCorsWithExposesTheHeadersItNames(t *testing.T) {
	mw, err := middleware.HandleCorsWith(middleware.CorsConfig{
		AllowedOrigins: []string{"https://app.example"},
		ExposedHeaders: []string{"X-Request-Id", "Link"},
	})
	if err != nil {
		t.Fatalf("HandleCorsWith: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "X-Request-Id, Link" {
		t.Fatalf("Access-Control-Expose-Headers = %q", got)
	}

	other := httptest.NewRequest(http.MethodGet, "/api", nil)
	other.Header.Set("Origin", "https://elsewhere.example")
	w = httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(w, other)
	if len(w.Header()) != 0 {
		t.Fatalf("an origin outside the list got CORS headers: %v", w.Header())
	}
}
