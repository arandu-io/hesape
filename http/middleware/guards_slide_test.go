package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/session"
)

func slidingSessions() *hhttp.SessionStore {
	s := hhttp.NewSessionStore(appKey, time.Second, false, hhttp.NewMemoryBackend())
	table := &accounts{rows: map[string]auth.Subject{}}
	s.ResolveSubjects(table.resolve)
	tables.Store(s, table)
	return s
}

func cookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	return nil
}

// The guards are the place the session is used, so they are the place it slides:
// a person who keeps loading pages behind RequireAuth is not signed out when the
// ttl counted from sign-in runs out. Each guard that loads the session is run,
// because each was a separate Load call that had to be changed.
func TestTheGuardsSlideTheSession(t *testing.T) {
	guards := map[string]func(*hhttp.SessionStore) func(http.Handler) http.Handler{
		"RequireAuth": middleware.RequireAuth,
		"LoadSubject": middleware.LoadSubject,
		"RequireConfirmedPassword": func(s *hhttp.SessionStore) func(http.Handler) http.Handler {
			return slideThrough(middleware.RequireConfirmedPassword(s))
		},
		"RequireRole": func(s *hhttp.SessionStore) func(http.Handler) http.Handler {
			return middleware.RequireRole(s, "member")
		},
	}
	for name, guard := range guards {
		t.Run(name, func(t *testing.T) {
			sessions := slidingSessions()
			sub := auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"member"}}
			first := signedIn(t, sessions, sub, http.MethodGet, "/dashboard")
			h := guarded(guard(sessions))

			// Inside the first tenth of the second: nothing to renew.
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, first)
			if c := cookieOf(rec); c != nil {
				t.Fatalf("the session was renewed on its first request: %+v", c)
			}

			// Past it: the cookie comes back with the full Max-Age.
			time.Sleep(150 * time.Millisecond)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, first)
			c := cookieOf(rec)
			if c == nil {
				t.Fatal("a request after a tenth of the lifetime did not renew the cookie")
			}
			if c.MaxAge != 1 {
				t.Errorf("Max-Age = %d, want the store's 1 second", c.MaxAge)
			}
		})
	}
}

// slideThrough lets a guard that would send the request to the confirmation
// screen be exercised: it is only the load that matters here.
func slideThrough(guard func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return guard
}

// Used every 0.6 seconds, a one-second session stays open through the guard;
// left alone for 1.2 seconds it is gone.
func TestAPersonUsingTheConsoleIsNotSignedOutAndAnIdleOneIs(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps for a few seconds")
	}
	sessions := slidingSessions()
	h := guarded(middleware.RequireAuth(sessions))
	r := signedIn(t, sessions, auth.Subject{ID: "u-1", Tenant: "acme"}, http.MethodGet, "/dashboard")

	for i := 0; i < 4; i++ {
		time.Sleep(600 * time.Millisecond)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Body.String() != "reached" {
			t.Fatalf("request %d, 0.6s after the last, was refused (%d): the session did not slide", i+1, rec.Code)
		}
	}

	time.Sleep(1200 * time.Millisecond)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Body.String() == "reached" {
		t.Fatal("a session idle past its ttl still opened the page")
	}
}
