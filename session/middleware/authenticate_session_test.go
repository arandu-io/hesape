package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/session/middleware"
)

// fakeGuard is the smallest thing that is a Guard: one password hash and a
// record of whether it was told to sign out.
type fakeGuard struct {
	hash      string
	signedIn  bool
	loggedOut int
}

func (g *fakeGuard) GetDefaultDriver() string { return "web" }

func (g *fakeGuard) GetAuthPassword(*http.Request) (string, bool) {
	return g.hash, g.signedIn
}

func (g *fakeGuard) LogoutCurrentDevice(*http.Request) error {
	g.loggedOut++
	return nil
}

func withSession(t *testing.T, handler http.Handler, store *session.Store) *httptest.ResponseRecorder {
	t.Helper()
	r := pageRequest(http.MethodGet, "/account")
	r = r.WithContext(middleware.WithSession(r.Context(), store))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func liveStore(t *testing.T) *session.Store {
	t.Helper()
	s := session.NewStore("arandu_session", session.NewNullSessionHandler(), "")
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// TestTheFirstRequestRecordsTheHashAndLetsThePersonThrough.
func TestTheFirstRequestRecordsTheHashAndLetsThePersonThrough(t *testing.T) {
	guard := &fakeGuard{hash: "argon2id$first", signedIn: true}
	m := middleware.NewAuthenticateSession(guard)
	store := liveStore(t)

	reached := false
	w := withSession(t, m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	})), store)

	if !reached || w.Code != http.StatusOK {
		t.Fatalf("reached %v, code %d", reached, w.Code)
	}
	if store.Get("password_hash_web") != "argon2id$first" {
		t.Fatalf("the hash was not recorded: %v", store.All())
	}
}

// TestAPasswordChangedElsewhereEndsThisSession is what the middleware is for:
// the session recorded the old hash, the database now holds a new one, and
// every browser that recorded the old one stops on its next request.
func TestAPasswordChangedElsewhereEndsThisSession(t *testing.T) {
	guard := &fakeGuard{hash: "argon2id$first", signedIn: true}
	m := middleware.NewAuthenticateSession(guard)
	store := liveStore(t)
	store.Put("password_hash_web", "argon2id$first")
	store.Put("cart", "kept until now")

	guard.hash = "argon2id$second"

	w := withSession(t, m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the handler ran on a session whose password has changed")
	})), store)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Code)
	}
	if guard.loggedOut != 1 {
		t.Fatalf("logged out %d times", guard.loggedOut)
	}
	if len(store.All()) != 0 {
		t.Fatalf("the session was not emptied: %v", store.All())
	}
}

func TestRedirectUsingSendsThemSomewhereInsteadOfAnswering401(t *testing.T) {
	guard := &fakeGuard{hash: "argon2id$second", signedIn: true}
	m := middleware.NewAuthenticateSession(guard).
		RedirectUsing(func(*http.Request) string { return "/login" })
	store := liveStore(t)
	store.Put("password_hash_web", "argon2id$first")

	w := withSession(t, m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), store)

	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("got %d to %q", w.Code, w.Header().Get("Location"))
	}
}

// TestAPasswordChangedBYThisRequestDoesNotSignItOutNextTime is the reason the
// hash is written again after the handler: the request that changed the
// password would otherwise sign itself out on its own next click.
func TestAPasswordChangedBYThisRequestDoesNotSignItOutNextTime(t *testing.T) {
	guard := &fakeGuard{hash: "argon2id$first", signedIn: true}
	m := middleware.NewAuthenticateSession(guard)
	store := liveStore(t)
	store.Put("password_hash_web", "argon2id$first")

	w := withSession(t, m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		guard.hash = "argon2id$changed"
	})), store)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	if store.Get("password_hash_web") != "argon2id$changed" {
		t.Fatalf("the new hash was not recorded: %v", store.Get("password_hash_web"))
	}
}

func TestNobodySignedInMeansNothingToCompare(t *testing.T) {
	m := middleware.NewAuthenticateSession(&fakeGuard{})
	store := liveStore(t)

	reached := false
	w := withSession(t, m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	})), store)

	if !reached || w.Code != http.StatusOK {
		t.Fatalf("reached %v, code %d", reached, w.Code)
	}
}

func TestNoSessionOnTheContextIsNotARefusal(t *testing.T) {
	m := middleware.NewAuthenticateSession(&fakeGuard{hash: "x", signedIn: true})

	reached := false
	w := httptest.NewRecorder()
	m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	})).ServeHTTP(w, pageRequest(http.MethodGet, "/"))

	if !reached || w.Code != http.StatusOK {
		t.Fatalf("reached %v, code %d", reached, w.Code)
	}
}
