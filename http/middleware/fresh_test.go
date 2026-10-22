package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/http/middleware"
)

// replay sends the request again with the cookies it carried, as the next
// click of the same browser.
func replay(r *http.Request) *http.Request {
	next := httptest.NewRequest(r.Method, r.URL.Path, nil)
	for _, c := range r.Cookies() {
		next.AddCookie(c)
	}
	return next
}

// A role taken away is gone on the next request, not when the session ends:
// the session keeps who the person is, and the role guard reads what they are
// from the account on every request.
func TestDemotingARoleRemovesAccessOnTheNextRequest(t *testing.T) {
	sessions := newSessions()
	h := guarded(middleware.RequireRole(sessions, "admin"))
	admin := auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}
	first := signedIn(t, sessions, admin, http.MethodGet, "/admin/")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, first)
	if rec.Body.String() != "reached" {
		t.Fatalf("the admin was refused before any change: %d %q", rec.Code, rec.Body.String())
	}

	demoted := admin
	demoted.Roles = []string{"reader"}
	accountsOf(t, sessions).put(demoted)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, replay(first))
	if rec.Body.String() == "reached" {
		t.Fatal("the role guard admitted an account whose admin role was taken away, on the session opened while it had it")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("the demoted account got %d, want 403", rec.Code)
	}
}

// A role given is there on the next request too: the same read that takes
// access away hands it out, so nobody has to sign in again to receive it.
func TestARoleGrantedWhileSignedInIsHonouredOnTheNextRequest(t *testing.T) {
	sessions := newSessions()
	h := guarded(middleware.RequireRole(sessions, "admin"))
	reader := auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"reader"}}
	first := signedIn(t, sessions, reader, http.MethodGet, "/admin/")

	promoted := reader
	promoted.Roles = []string{"admin"}
	accountsOf(t, sessions).put(promoted)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, first)
	if rec.Body.String() != "reached" {
		t.Errorf("the promoted account got %d, want the page", rec.Code)
	}
}

// An account removed while signed in is signed out: the guard sends it to the
// sign-in screen, and the session stays gone if the account comes back.
func TestARemovedAccountIsSignedOutOnTheNextRequest(t *testing.T) {
	sessions := newSessions()
	h := guarded(middleware.RequireAuth(sessions))
	member := auth.Subject{ID: "u-1", Tenant: "acme"}
	first := signedIn(t, sessions, member, http.MethodGet, "/dashboard")

	accountsOf(t, sessions).remove("acme", "u-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, replay(first))
	if rec.Body.String() == "reached" || rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != middleware.SignInPath {
		t.Fatalf("a removed account got %d %q to %q, want 303 to the sign-in screen", rec.Code, rec.Body.String(), rec.Header().Get("Location"))
	}

	accountsOf(t, sessions).put(member)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, replay(first))
	if rec.Body.String() == "reached" {
		t.Error("the session of a removed account worked again once the account was back: it was not destroyed")
	}
}

// A resolver that cannot read the account fails the request. Neither the
// sign-in screen, which reads as a sign-out, nor the page, on the roles the
// session was opened with.
func TestAnUnreadableAccountFailsTheRequestRatherThanUsingTheSessionsCopy(t *testing.T) {
	for name, guard := range map[string]func(*testing.T) (http.Handler, *http.Request){
		"RequireAuth": func(t *testing.T) (http.Handler, *http.Request) {
			s := newSessions()
			r := signedIn(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}, http.MethodGet, "/x")
			accountsOf(t, s).fail(errors.New("connection refused"))
			return guarded(middleware.RequireAuth(s)), r
		},
		"RequireRole": func(t *testing.T) (http.Handler, *http.Request) {
			s := newSessions()
			r := signedIn(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}, http.MethodGet, "/x")
			accountsOf(t, s).fail(errors.New("connection refused"))
			return guarded(middleware.RequireRole(s, "admin")), r
		},
		"LoadSubject": func(t *testing.T) (http.Handler, *http.Request) {
			s := newSessions()
			r := signedIn(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}, http.MethodGet, "/x")
			accountsOf(t, s).fail(errors.New("connection refused"))
			return guarded(middleware.LoadSubject(s)), r
		},
		"RedirectIfAuthenticated": func(t *testing.T) (http.Handler, *http.Request) {
			s := newSessions()
			r := signedIn(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}, http.MethodGet, "/x")
			accountsOf(t, s).fail(errors.New("connection refused"))
			return guarded(middleware.RedirectIfAuthenticated(s, "/")), r
		},
		"RequireConfirmedPassword": func(t *testing.T) (http.Handler, *http.Request) {
			s := newSessions()
			r := confirmed(t, s, signedIn(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}, http.MethodGet, "/x"))
			accountsOf(t, s).fail(errors.New("connection refused"))
			return guarded(middleware.RequireConfirmedPassword(s)), r
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, r := guard(t)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Body.String() == "reached" {
				t.Fatal("the request went through on the session's copy of the subject while the account could not be read")
			}
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("got %d to %q, want 503", rec.Code, rec.Header().Get("Location"))
			}
			if strings.Contains(rec.Header().Get("Cache-Control"), "public") || rec.Header().Get("Cache-Control") == "" {
				t.Errorf("Cache-Control = %q, want an answer no shared cache keeps", rec.Header().Get("Cache-Control"))
			}
		})
	}
}
