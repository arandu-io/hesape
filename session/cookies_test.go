package session_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

var cookieKey = []byte(strings.Repeat("k", 32))

func TestCookieNamesFollowTheBrand(t *testing.T) {
	got := session.CookieNamesFor(support.NewBrand("Peráta"))
	want := session.CookieNames{Session: "perata_session", CSRFGuest: "perata_csrf_guest", Flash: "perata_flash", Intended: "perata_intended"}
	if got != want {
		t.Fatalf("CookieNamesFor = %+v, want %+v", got, want)
	}
	if def := session.DefaultCookieNames(); def.Session != session.CookieName || def.CSRFGuest != session.GuestCookieName || def.Flash != session.FlashCookieName {
		t.Fatalf("DefaultCookieNames = %+v, want the constants", def)
	}
}

// A store built with no names writes the names the collection always wrote, so an
// application that configures nothing is not changed.
func TestStoresWithoutNamesKeepTheOldOnes(t *testing.T) {
	if got := session.NewRecordStore[string](cookieKey, time.Hour, true, nil).CookieName(); got != "arandu_session" {
		t.Errorf("session cookie = %q", got)
	}
	if got := session.NewCSRF(cookieKey, time.Hour).GuestCookieName(); got != "arandu_csrf_guest" {
		t.Errorf("guest cookie = %q", got)
	}
	if got := session.NewFlash(cookieKey, true).CookieName(); got != "arandu_flash" {
		t.Errorf("flash cookie = %q", got)
	}
}

// One option, handed to every constructor, names every cookie: nothing the
// browser receives carries the collection's name.
func TestTheConfiguredNamesReachEveryCookie(t *testing.T) {
	names := session.WithCookieNames(session.CookieNamesFor(support.NewBrand("Peráta")))

	rec := httptest.NewRecorder()
	store := session.NewRecordStore[string](cookieKey, time.Hour, true, nil, names)
	if _, err := store.Start(t.Context(), rec, session.Record[string]{Tenant: "t", SubjectID: "1"}); err != nil {
		t.Fatal(err)
	}

	csrf := session.NewCSRF(cookieKey, time.Hour, names)
	csrf.Binding(rec, httptest.NewRequest(http.MethodGet, "/", nil), "")

	flash := session.NewFlash(cookieKey, true, names)
	flash.Write(rec, map[string][]string{"email": {"is required"}}, nil)

	seen := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		seen[c.Name] = true
		if strings.Contains(c.Name, "arandu") {
			t.Errorf("cookie %q carries the collection's name", c.Name)
		}
	}
	for _, want := range []string{"perata_session", "perata_csrf_guest", "perata_flash"} {
		if !seen[want] {
			t.Errorf("no %s cookie was written; wrote %v", want, seen)
		}
	}
}

// What one store writes under a name, the same store reads back under it, and a
// cookie of the old name is not a session for it.
func TestAStoreReadsOnlyItsOwnName(t *testing.T) {
	names := session.WithCookieNames(session.CookieNamesFor(support.NewBrand("Peráta")))
	store := session.NewRecordStore[string](cookieKey, time.Hour, true, nil, names)

	rec := httptest.NewRecorder()
	if _, err := store.Start(t.Context(), rec, session.Record[string]{Tenant: "t", SubjectID: "1"}); err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	if store.ID(r) == "" {
		t.Fatal("the store did not read its own cookie")
	}

	old := httptest.NewRequest(http.MethodGet, "/", nil)
	old.AddCookie(&http.Cookie{Name: "arandu_session", Value: cookie.Value})
	if id := store.ID(old); id != "" {
		t.Fatalf("a cookie under the old name was accepted as session %q", id)
	}
}

func TestANameLeftEmptyKeepsItsDefault(t *testing.T) {
	got := session.ResolveCookieNames(session.WithCookieNames(session.CookieNames{Session: "x_session"}))
	if got.Session != "x_session" || got.Flash != "arandu_flash" {
		t.Fatalf("names = %+v, want the session renamed and the rest left", got)
	}
}
