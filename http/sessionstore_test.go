package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
)

var sessionAppKey = []byte("0123456789abcdef0123456789abcdef")

// sessionBackend is the adapter over the handler every deployment has, which
// is the one that needs nothing running: a rename or a swapped argument fails
// here without a server having to exist.
func sessionBackend() hhttp.SessionBackend {
	return hhttp.NewSessionBackend(session.NewArrayHandler[auth.Subject]())
}

func sessionSubject(tenant, id string) auth.Subject {
	return auth.Subject{ID: id, Tenant: tenant, Roles: []string{"member"}}
}

// TestASessionSurvivesTheRoundTrip is the whole of the first three renames:
// what Put wrote is what Get reads, and Delete makes it stop being there.
//
// The refusal is asserted and not just the absence, because the caller branches
// on it. A backend that answered some other not-found would send somebody to a
// generic error page instead of back to the sign-in screen, and only in the
// deployment that wired this handler.
func TestASessionSurvivesTheRoundTrip(t *testing.T) {
	b := sessionBackend()
	ctx := context.Background()

	want := sessionSubject("acme", "u-1")
	if err := b.Put(ctx, "session-1", want, time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := b.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get after Put: %v", err)
	}
	if got.ID != want.ID || got.Tenant != want.Tenant {
		t.Fatalf("Get = %+v, want the subject Put was given", got)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "member" {
		t.Errorf("the roles did not survive the round trip: %v", got.Roles)
	}

	if err := b.Delete(ctx, "session-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.Get(ctx, "session-1"); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Get after Delete = %v, want ErrSessionExpired", err)
	}
}

// TestDeleteSubjectReachesOneSubjectAndNoOther is the fourth rename, and the
// half an inattentive adapter loses.
//
// DestroyIndex takes three arguments that are all strings, so a pair swapped on
// the way across still compiles and still deletes something. What tells the
// right wiring from the wrong one is what is left afterwards: the session that
// was to be kept, the other account of the same customer, and the same account
// id in a different customer.
func TestDeleteSubjectReachesOneSubjectAndNoOther(t *testing.T) {
	b := sessionBackend()
	ctx := context.Background()

	sessions := map[string]auth.Subject{
		"keep-this":    sessionSubject("acme", "u-1"),
		"drop-this":    sessionSubject("acme", "u-1"),
		"drop-this-2":  sessionSubject("acme", "u-1"),
		"other-person": sessionSubject("acme", "u-2"),
		"other-tenant": sessionSubject("globex", "u-1"),
	}
	for id, sub := range sessions {
		if err := b.Put(ctx, id, sub, time.Hour); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}

	if err := b.DeleteSubject(ctx, "acme", "u-1", "keep-this"); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}

	for _, id := range []string{"keep-this", "other-person", "other-tenant"} {
		if _, err := b.Get(ctx, id); err != nil {
			t.Errorf("%s was signed out and should not have been: %v", id, err)
		}
	}
	for _, id := range []string{"drop-this", "drop-this-2"} {
		if _, err := b.Get(ctx, id); !errors.Is(err, session.ErrExpired) {
			t.Errorf("%s is still signed in: %v", id, err)
		}
	}
}

// TestTheStoreAcceptsTheAdapter is the reason the adapter exists: the type the
// application wires is what NewSessionStore takes.
//
// A compile-time assertion written as code, because the failure it guards
// against is a signature, not a value.
func TestTheStoreAcceptsTheAdapter(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	if hhttp.NewSessionStore(key, time.Hour, false, sessionBackend()) == nil {
		t.Fatal("NewSessionStore returned nothing")
	}
}

// TestMemoryBackendKeepsTheOldMethodNames is the assertion a backend written
// elsewhere depends on and that go build here cannot make: a backend
// implements SessionBackend by these four names, so SessionBackend may not
// become an alias for session.Handler.
func TestMemoryBackendKeepsTheOldMethodNames(t *testing.T) {
	ctx := context.Background()
	var b hhttp.SessionBackend = hhttp.NewMemoryBackend()

	who := auth.Subject{ID: "u1", Tenant: "acme", Roles: []string{"author"}, Verified: true}
	if err := b.Put(ctx, "sid-1", who, time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := b.Get(ctx, "sid-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != who.ID || got.Tenant != who.Tenant || !got.Verified || !got.HasRole("author") {
		t.Errorf("the subject did not survive the round trip: %+v", got)
	}

	if _, err := b.Get(ctx, "no-such-id"); !errors.Is(err, session.ErrExpired) {
		t.Errorf("an unknown id did not answer ErrSessionExpired: %v", err)
	}

	// The refusal that keeps a bulk sign-out from reaching every guest of a
	// tenant, and every tenant at once.
	if err := b.DeleteSubject(ctx, "acme", "", ""); err == nil {
		t.Error("signing out the empty subject of a tenant was accepted")
	}
	if err := b.DeleteSubject(ctx, "", "u1", ""); err == nil {
		t.Error("signing out a subject with no tenant was accepted")
	}

	if err := b.Put(ctx, "sid-2", who, time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := b.DeleteSubject(ctx, "acme", "u1", "sid-1"); err != nil {
		t.Fatalf("delete subject: %v", err)
	}
	if _, err := b.Get(ctx, "sid-1"); err != nil {
		t.Errorf("the kept session was destroyed: %v", err)
	}
	if _, err := b.Get(ctx, "sid-2"); !errors.Is(err, session.ErrExpired) {
		t.Errorf("the other session survived a bulk sign-out: %v", err)
	}

	if err := b.Delete(ctx, "sid-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := b.Get(ctx, "sid-1"); !errors.Is(err, session.ErrExpired) {
		t.Errorf("a deleted session still reads back: %v", err)
	}
}

// TestSessionStoreReachesTheRenamedMethods walks one session through the whole
// store: Start, Load, Rotate, Confirm and Destroy are each a different name
// on session.RecordStore, and each one wired to the wrong one would still compile.
func TestSessionStoreReachesTheRenamedMethods(t *testing.T) {
	ctx := context.Background()
	store := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	who := auth.Subject{ID: "u1", Tenant: "acme"}

	w := httptest.NewRecorder()
	id, err := store.Start(ctx, w, who)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	signedIn := carryCookies(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := store.IDFromRequest(signedIn); got != id {
		t.Errorf("IDFromRequest answered %q for the session it just wrote (%q)", got, id)
	}
	got, err := store.Load(ctx, signedIn)
	if err != nil || got.ID != "u1" || got.Tenant != "acme" {
		t.Fatalf("load: %+v, %v", got, err)
	}
	if hhttp.PasswordConfirmedWithin(got, time.Hour) {
		t.Error("a session nobody has confirmed a password on reads as confirmed")
	}

	// Confirm writes the stamp, and Load reads it back off the payload -- which
	// is where a caller of Load reads it, and where the session record does not
	// put it.
	w = httptest.NewRecorder()
	if err := store.Confirm(ctx, w, signedIn); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	got, err = store.Load(ctx, signedIn)
	if err != nil {
		t.Fatalf("load after confirm: %v", err)
	}
	if !hhttp.PasswordConfirmedWithin(got, time.Hour) {
		t.Error("the password confirmation did not survive the record translation")
	}

	// Rotate mints a new id and ends the old one: session fixation is the bug
	// this call exists to close, so the old id has to stop working.
	w = httptest.NewRecorder()
	rotated, err := store.Rotate(ctx, w, id, who)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated == id {
		t.Fatal("Rotate returned the id it was told to replace")
	}
	if _, err := store.Load(ctx, signedIn); !errors.Is(err, session.ErrExpired) {
		t.Errorf("the pre-rotation session still loads: %v", err)
	}

	afterRotate := carryCookies(w, httptest.NewRequest(http.MethodGet, "/", nil))
	w = httptest.NewRecorder()
	if err := store.Destroy(ctx, w, rotated); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := store.Load(ctx, afterRotate); !errors.Is(err, session.ErrExpired) {
		t.Errorf("a destroyed session still loads: %v", err)
	}
	// Destroy clears two cookies, and the second one is the Intended's: an address remembered before a sign-out is one the next
	// person at a shared machine must not be carried to.
	if !clears(w, session.CookieName) {
		t.Error("Destroy did not clear the session cookie")
	}
	if !clears(w, hhttp.IntendedCookieName) {
		t.Error("Destroy did not clear the intended destination")
	}
}

// TestRememberIsCarriedThrough proves the option reaches RecordStore.Start rather
// than being accepted and dropped: the checkbox on the published sign-in screen
// was decoration for exactly that reason once before.
func TestRememberIsCarriedThrough(t *testing.T) {
	ctx := context.Background()
	store := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)

	w := httptest.NewRecorder()
	if _, err := store.Start(ctx, w, auth.Subject{ID: "u1", Tenant: "acme"}, session.Remember(true)); err != nil {
		t.Fatalf("start: %v", err)
	}

	got, err := store.Load(ctx, carryCookies(w, httptest.NewRequest(http.MethodGet, "/", nil)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !got.Remembered {
		t.Error("Remember(true) did not reach the record")
	}
	if c := cookieNamed(w, session.CookieName); c == nil || time.Duration(c.MaxAge)*time.Second < session.RememberLifetime {
		t.Errorf("the cookie was not written for RememberLifetime: %v", c)
	}
}

// TestIntendedIsCarriedThrough covers the two methods that left the session
// package altogether. The address has to survive from the guard that writes it
// to the sign-in handler that spends it, and be spent exactly once.
func TestIntendedIsCarriedThrough(t *testing.T) {
	store := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)

	w := httptest.NewRecorder()
	store.RememberIntended(w, httptest.NewRequest(http.MethodGet, "/invoices/44", nil))
	refused := carryCookies(w, httptest.NewRequest(http.MethodGet, "/login", nil))

	w = httptest.NewRecorder()
	if got := store.TakeIntended(w, refused, "/"); got != "/invoices/44" {
		t.Errorf("the intended address came back as %q", got)
	}
	if !clears(w, hhttp.IntendedCookieName) {
		t.Error("TakeIntended did not spend the address")
	}
	if got := store.TakeIntended(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/login", nil), "/home"); got != "/home" {
		t.Errorf("with no cookie the fallback was not answered: %q", got)
	}
}

func carryCookies(w *httptest.ResponseRecorder, r *http.Request) *http.Request {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge >= 0 {
			r.AddCookie(c)
		}
	}
	return r
}
func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
func clears(w *httptest.ResponseRecorder, name string) bool {
	c := cookieNamed(w, name)
	return c != nil && c.MaxAge < 0
}
