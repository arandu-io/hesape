package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	rmiddleware "github.com/arandu-io/hesape/routing/middleware"
	"github.com/arandu-io/hesape/session"
)

// headerSessions reads the session id from a test header and holds exactly the
// ids in live, so a test can name a session the cookie still carries and the
// store has already dropped.
type headerSessions struct{ live map[string]bool }

func (s headerSessions) IDFromRequest(r *http.Request) string {
	return r.Header.Get("X-Test-Session")
}

func (s headerSessions) Load(_ context.Context, r *http.Request) (auth.Subject, error) {
	if !s.live[s.IDFromRequest(r)] {
		return auth.Subject{}, session.ErrNoSession
	}
	return auth.Subject{ID: "u1", Tenant: "t1"}, nil
}

// hesapeSessions is the same store in the shape the counter asks for, so the
// key can be compared against the one hesape builds.
type hesapeSessions struct{ headerSessions }

func (s hesapeSessions) ID(r *http.Request) string { return s.IDFromRequest(r) }

func (s hesapeSessions) Exists(_ context.Context, id string) bool { return s.live[id] }

func TestKeyBySessionFallsBackToTheAddress(t *testing.T) {
	key := middleware.KeyBySession(headerSessions{live: map[string]bool{"sess-1": true}})

	authenticated := httptest.NewRequest(http.MethodGet, "/", nil)
	authenticated.Header.Set("X-Test-Session", "sess-1")
	if got := key(authenticated); got != "session:sess-1" {
		t.Fatalf("authenticated key = %q, want session:sess-1", got)
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/", nil)
	anonymous.RemoteAddr = "10.0.0.9:1"
	if got := key(anonymous); got != "ip:10.0.0.9" {
		t.Fatalf("anonymous key = %q, want ip:10.0.0.9", got)
	}
}

// TestASessionTheStoreDroppedIsKeyedByTheAddress: a signed cookie proves only
// that the id was issued once. Keyed on the cookie alone, every expired or
// signed-out id a client kept was a budget of its own.
func TestASessionTheStoreDroppedIsKeyedByTheAddress(t *testing.T) {
	key := middleware.KeyBySession(headerSessions{live: map[string]bool{}})

	stale := httptest.NewRequest(http.MethodGet, "/", nil)
	stale.RemoteAddr = "10.0.0.9:1"
	stale.Header.Set("X-Test-Session", "expired-1")
	if got := key(stale); got != "ip:10.0.0.9" {
		t.Fatalf("a session the store no longer holds is keyed %q, want ip:10.0.0.9", got)
	}
}

// TestASignedOutSessionStopsBeingABudget runs the same through the real store:
// the cookie survives the sign-out in the client's jar, and the key moves to
// the address the moment the session is destroyed.
func TestASignedOutSessionStopsBeingABudget(t *testing.T) {
	key32 := []byte("0123456789abcdef0123456789abcdef")
	sessions := hhttp.NewSessionStore(key32, time.Hour, false, hhttp.NewMemoryBackend())

	rec := httptest.NewRecorder()
	id, err := sessions.Start(context.Background(), rec, auth.Subject{ID: "u1", Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.7:1"
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}

	key := middleware.KeyBySession(sessions)
	if got := key(r); got != "session:"+id {
		t.Fatalf("a live session is keyed %q, want session:%s", got, id)
	}
	if err := sessions.Destroy(context.Background(), httptest.NewRecorder(), id); err != nil {
		t.Fatal(err)
	}
	if got := key(r); got != "ip:10.0.0.7" {
		t.Fatalf("after sign-out the kept cookie is keyed %q, want ip:10.0.0.7", got)
	}
}

// TestTheKeyIsTheSameStringOnBothSides: the counter lives in a shared store,
// keyed by this string, so if this function and the one it forwards to ever
// disagreed every caller would be handed a fresh budget on the deploy that
// introduced the difference.
func TestTheKeyIsTheSameStringOnBothSides(t *testing.T) {
	sessions := headerSessions{live: map[string]bool{"sess-1": true}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Test-Session", "sess-1")

	if got, want := middleware.KeyBySession(sessions)(r), rmiddleware.KeyBySession(hesapeSessions{sessions})(r); got != want {
		t.Errorf("a session is keyed %q here and %q where it is counted", got, want)
	}
}

// TestOneMachineWithARoutedBlockIsOneClient: over IPv6 the address is not a
// scarce resource. A single connection is handed a /64 -- eighteen quintillion
// addresses, all of which reach this server -- so a limit keyed on the whole
// address is a limit an attacker steps out of by picking a new source address
// for every request, which costs nothing.
