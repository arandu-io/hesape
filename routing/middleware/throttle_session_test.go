package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/routing/middleware"
	"github.com/arandu-io/hesape/session"
)

// fixedSession is a Sessions that names one id and holds it.
type fixedSession string

func (s fixedSession) ID(*http.Request) string                  { return string(s) }
func (s fixedSession) Exists(_ context.Context, id string) bool { return id != "" && id == string(s) }

// staleCookie starts a session, ends it, and returns the cookie it was issued
// -- still signed, and naming a session the store no longer holds.
func staleCookie(t *testing.T, store *session.RecordStore[struct{}]) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	id, err := store.Start(context.Background(), rec, session.Record[struct{}]{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	if err := store.Invalidate(context.Background(), httptest.NewRecorder(), id); err != nil {
		t.Fatal(err)
	}
	return cookie
}

// TestAStaleSessionCookieIsKeyedByTheAddress: KeyBySession trusted any cookie
// whose signature verified, and every id the application ever signed still
// verifies. A client holding two old cookies had two fresh budgets, and one
// holding a thousand had a thousand, so the limit on a route keyed this way
// limited nobody who kept their cookies.
func TestAStaleSessionCookieIsKeyedByTheAddress(t *testing.T) {
	store := session.NewRecordStore[struct{}]([]byte("an application key long enough to sign"), time.Hour, false, nil)
	key := middleware.KeyBySession(store)

	first, second := staleCookie(t, store), staleCookie(t, store)
	for _, cookie := range []*http.Cookie{first, second} {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.10:5000"
		req.AddCookie(cookie)
		if got := key(req); got != middleware.KeyByIP(req) {
			t.Fatalf("a stale cookie was keyed %q, want the address key %q", got, middleware.KeyByIP(req))
		}
	}
}

// TestALiveSessionIsKeyedBySession: the session a client holds right now is
// still its own budget.
func TestALiveSessionIsKeyedBySession(t *testing.T) {
	store := session.NewRecordStore[struct{}]([]byte("an application key long enough to sign"), time.Hour, false, nil)
	rec := httptest.NewRecorder()
	id, err := store.Start(context.Background(), rec, session.Record[struct{}]{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	if got := middleware.KeyBySession(store)(req); got != "session:"+id {
		t.Fatalf("key = %q, want session:%s", got, id)
	}
}
