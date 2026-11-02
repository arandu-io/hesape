package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
)

// spyHandler counts the writes a handler receives and remembers the ttl of the
// last one, which is what a renewal is made of.
type spyHandler struct {
	*session.ArrayHandler[account]
	mu     sync.Mutex
	writes int
	ttl    time.Duration
}

func newSpy() *spyHandler { return &spyHandler{ArrayHandler: session.NewArrayHandler[account]()} }

func (h *spyHandler) Write(ctx context.Context, id string, rec session.Record[account], ttl time.Duration) error {
	h.mu.Lock()
	h.writes++
	h.ttl = ttl
	h.mu.Unlock()
	return h.ArrayHandler.Write(ctx, id, rec, ttl)
}

func (h *spyHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writes
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	return nil
}

// A request inside the first tenth of the lifetime has nothing to renew: no
// write, no cookie. This is what keeps a page that fires ten requests from
// writing the session ten times.
func TestSlideLeavesAFreshSessionAlone(t *testing.T) {
	ctx := context.Background()
	h := newSpy()
	store := session.NewRecordStore(appKey, time.Hour, true, h)
	start := httptest.NewRecorder()
	if _, err := store.Start(ctx, start, signedIn("u1", "t1")); err != nil {
		t.Fatal(err)
	}
	r := requestWithCookies(start)
	got, err := store.All(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	before := h.count()

	out := httptest.NewRecorder()
	if err := store.Slide(ctx, out, r, got); err != nil {
		t.Fatalf("Slide: %v", err)
	}
	if h.count() != before {
		t.Errorf("a session started a moment ago was written again: %d writes, want %d", h.count(), before)
	}
	if c := sessionCookie(out); c != nil {
		t.Errorf("a session started a moment ago got its cookie rewritten: %+v", c)
	}
}

// Once a tenth of the lifetime has gone, the next request renews the record with
// the full lifetime and sets the cookie again with the same Max-Age. Both
// halves: a record renewed behind a cookie that still counts down from sign-in
// is signed out by the browser.
func TestSlideRenewsTheRecordAndTheCookie(t *testing.T) {
	ctx := context.Background()
	h := newSpy()
	store := session.NewRecordStore(appKey, time.Hour, true, h)
	start := httptest.NewRecorder()
	id, err := store.Start(ctx, start, signedIn("u1", "t1", "admin"))
	if err != nil {
		t.Fatal(err)
	}
	r := requestWithCookies(start)

	stale, _ := store.All(ctx, r)
	stale.TouchedAt = time.Now().Add(-10 * time.Minute)
	if err := h.ArrayHandler.Write(ctx, id, stale, time.Hour); err != nil {
		t.Fatal(err)
	}
	before := h.count()

	out := httptest.NewRecorder()
	if err := store.Slide(ctx, out, r, stale); err != nil {
		t.Fatalf("Slide: %v", err)
	}
	if h.count() != before+1 {
		t.Fatalf("writes = %d, want %d: a session idle past a tenth of its life must be renewed once", h.count(), before+1)
	}
	if h.ttl != time.Hour {
		t.Errorf("the renewal wrote a ttl of %v, want the store's hour", h.ttl)
	}
	c := sessionCookie(out)
	if c == nil {
		t.Fatal("the renewal did not set the cookie again, so the browser still counts down from sign-in")
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("Max-Age = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("the renewed cookie lost an attribute: %+v", c)
	}

	renewed, err := store.All(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(renewed.TouchedAt) > time.Minute {
		t.Errorf("TouchedAt = %v, want now", renewed.TouchedAt)
	}
	if renewed.SubjectID != "u1" || len(renewed.Payload.Roles) != 1 {
		t.Errorf("the renewal changed what the session holds: %+v", renewed)
	}

	// And a second request right after has nothing left to renew.
	again := httptest.NewRecorder()
	if err := store.Slide(ctx, again, r, renewed); err != nil {
		t.Fatal(err)
	}
	if h.count() != before+1 || sessionCookie(again) != nil {
		t.Error("a session renewed a moment ago was renewed again")
	}
}

// A record written before TouchedAt existed has the zero time and is renewed at
// once, which stamps it. Treating zero as recent would leave every session that
// survived the deploy on its fixed deadline.
func TestSlideRenewsARecordThatPredatesTheStamp(t *testing.T) {
	ctx := context.Background()
	h := newSpy()
	store := session.NewRecordStore(appKey, time.Hour, true, h)
	start := httptest.NewRecorder()
	id, _ := store.Start(ctx, start, signedIn("u1", "t1"))
	r := requestWithCookies(start)

	legacy, _ := store.All(ctx, r)
	legacy.TouchedAt = time.Time{}
	_ = h.ArrayHandler.Write(ctx, id, legacy, time.Hour)
	before := h.count()

	out := httptest.NewRecorder()
	if err := store.Slide(ctx, out, r, legacy); err != nil {
		t.Fatal(err)
	}
	if h.count() != before+1 || sessionCookie(out) == nil {
		t.Error("a record with no stamp was not renewed")
	}
}

// "Stay signed in" is 30 days and ends on its own. A visit that extended it would
// make it end never, on the shared computer the box warns about.
func TestSlideDoesNotExtendARememberedSession(t *testing.T) {
	ctx := context.Background()
	h := newSpy()
	store := session.NewRecordStore(appKey, time.Hour, true, h)
	start := httptest.NewRecorder()
	id, _ := store.Start(ctx, start, signedIn("u1", "t1"), session.Remember(true))
	r := requestWithCookies(start)

	stale, _ := store.All(ctx, r)
	stale.TouchedAt = time.Now().Add(-48 * time.Hour)
	_ = h.ArrayHandler.Write(ctx, id, stale, session.RememberLifetime)
	before := h.count()

	out := httptest.NewRecorder()
	if err := store.Slide(ctx, out, r, stale); err != nil {
		t.Fatal(err)
	}
	if h.count() != before || sessionCookie(out) != nil {
		t.Error("a remembered session was extended by a visit")
	}
}

func TestSlideOnASessionThatIsGone(t *testing.T) {
	ctx := context.Background()
	store := session.NewRecordStore(appKey, time.Hour, true, newSpy())
	start := httptest.NewRecorder()
	id, _ := store.Start(ctx, start, signedIn("u1", "t1"))
	r := requestWithCookies(start)
	got, _ := store.All(ctx, r)
	got.TouchedAt = time.Time{}
	if err := store.Invalidate(ctx, httptest.NewRecorder(), id); err != nil {
		t.Fatal(err)
	}

	out := httptest.NewRecorder()
	if err := store.Slide(ctx, out, r, got); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Slide = %v, want ErrExpired: renewing must not bring a signed-out session back", err)
	}
	if _, err := store.All(ctx, r); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("the session is back after Slide: %v", err)
	}
	if sessionCookie(out) != nil {
		t.Error("a cookie was set for a session that is gone")
	}
}

func TestSlideWithoutACookie(t *testing.T) {
	store := newStore(true)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := store.Slide(context.Background(), httptest.NewRecorder(), r, signedIn("u1", "t1")); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("Slide = %v, want ErrNoSession", err)
	}
}

// The behaviour end to end, on a real clock and a short life: a session used
// every 0.6 s outlives its 1 s ttl, and the same session left alone does not.
func TestAnActiveSessionOutlivesItsTTLAndAnIdleOneDoesNot(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps for a few seconds")
	}
	ctx := context.Background()
	store := session.NewRecordStore(appKey, time.Second, true, session.NewArrayHandler[account]())

	start := httptest.NewRecorder()
	if _, err := store.Start(ctx, start, signedIn("u1", "t1")); err != nil {
		t.Fatal(err)
	}
	r := requestWithCookies(start)

	for i := 0; i < 4; i++ {
		time.Sleep(600 * time.Millisecond)
		rec, err := store.All(ctx, r)
		if err != nil {
			t.Fatalf("after %d idle gaps of 0.6s the session was gone, though each gap was shorter than the 1s ttl: %v", i+1, err)
		}
		if err := store.Slide(ctx, httptest.NewRecorder(), r, rec); err != nil {
			t.Fatalf("Slide: %v", err)
		}
	}

	time.Sleep(1200 * time.Millisecond)
	if _, err := store.All(ctx, r); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("a session idle past its ttl still loads: %v", err)
	}
}
