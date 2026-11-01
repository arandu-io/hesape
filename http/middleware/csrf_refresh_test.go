package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

// refreshApp is the smallest application the renewal needs: the check in front
// of two routes, the way the kernel mounts it -- a page that draws its token and
// a route that changes something. The address a page renews its token at is
// not a route of the application's: the check answers it.
func refreshApp(ttl time.Duration, sessionID string) http.Handler {
	csrf := session.NewCSRF(appKey, ttl)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /page", func(w http.ResponseWriter, r *http.Request) {
		token, _ := hhttp.CSRFTokenFrom(r.Context())
		_, _ = w.Write([]byte(token))
	})
	mux.HandleFunc("POST /take", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("took")) })
	return middleware.CSRFProtect(csrf, func(*http.Request) string { return sessionID })(mux)
}

func postWith(h http.Handler, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/take", strings.NewReader(url.Values{"_token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func get(h http.Handler, path string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// TestAPageLeftOpenPastTheTokensLifeCanStillBeUsed reproduces the console that
// answered 419 to a click on a screen left open: the page is drawn, the time
// passes beyond the ttl, and the person presses the button. The token the page
// drew is refused -- it must be, a token that outlives the page it was drawn on
// can be replayed -- and the one the page asked for while it was open is
// accepted, by the same session.
func TestAPageLeftOpenPastTheTokensLifeCanStillBeUsed(t *testing.T) {
	support.FreezeTime()
	t.Cleanup(support.TravelBack)
	h := refreshApp(2*time.Hour, "session-1")

	drawn := get(h, "/page", nil).Body.String()
	if rec := postWith(h, drawn); rec.Code != http.StatusOK {
		t.Fatalf("the token a page was just drawn with answered %d, want 200", rec.Code)
	}

	// Three quarters of the way through the token's life, the page asks.
	support.Travel(90 * time.Minute)
	rec := get(h, middleware.CSRFRefreshPath, map[string]string{"Accept": "application/json", "Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("the renewal answered %d: %s", rec.Code, rec.Body.String())
	}
	var renewed struct {
		Token     string `json:"token"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &renewed); err != nil {
		t.Fatalf("the renewal is not JSON: %v: %s", err, rec.Body.String())
	}
	if renewed.Token == "" || renewed.Token == drawn {
		t.Fatalf("the renewal carried token %q, want a new one", renewed.Token)
	}
	if renewed.ExpiresIn != int64((2 * time.Hour).Seconds()) {
		t.Fatalf("expires_in = %d, want the whole ttl of a token issued now", renewed.ExpiresIn)
	}

	// Three hours after the page was drawn, the old token has lapsed and the
	// renewed one has not.
	support.Travel(90 * time.Minute)
	if rec := postWith(h, drawn); rec.Code != 419 {
		t.Fatalf("the token drawn three hours ago answered %d, want 419: the ttl is not to be stretched", rec.Code)
	}
	if rec := postWith(h, renewed.Token); rec.Code != http.StatusOK {
		t.Fatalf("the renewed token answered %d, want 200", rec.Code)
	}
}

// TestTheRenewalIsBoundToTheSessionItAsksFrom: a token another session was
// issued is useless in this one, so the address adds no authority.
func TestTheRenewalIsBoundToTheSessionItAsksFrom(t *testing.T) {
	asked := get(refreshApp(time.Hour, "session-1"), middleware.CSRFRefreshPath, nil)
	var got struct{ Token string }
	if err := json.Unmarshal(asked.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec := postWith(refreshApp(time.Hour, "session-2"), got.Token); rec.Code != 419 {
		t.Fatalf("a token renewed in session-1 answered %d in session-2, want 419", rec.Code)
	}
}

// TestTheRenewalIsNeverCached: a shared cache that kept it would hand one
// visitor's token to another, and a browser that kept it would hand a page a
// token that has lapsed.
func TestTheRenewalIsNeverCached(t *testing.T) {
	rec := get(refreshApp(time.Hour, "session-1"), middleware.CSRFRefreshPath, nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store, private" {
		t.Errorf("Cache-Control = %q, want no-store, private", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Cookie") {
		t.Errorf("Vary = %q, want it to name Cookie", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
}

// TestTheRenewalRefusesWhatABrowserSaysCameFromAnotherSite: a page of another
// site cannot read the answer, and is not shown it as a page either.
func TestTheRenewalRefusesWhatABrowserSaysCameFromAnotherSite(t *testing.T) {
	h := refreshApp(time.Hour, "session-1")
	for _, site := range []string{"cross-site", "same-site"} {
		if rec := get(h, middleware.CSRFRefreshPath, map[string]string{"Sec-Fetch-Site": site}); rec.Code != http.StatusForbidden {
			t.Errorf("Sec-Fetch-Site %s answered %d, want 403", site, rec.Code)
		}
	}
	for _, site := range []string{"same-origin", "none", ""} {
		if rec := get(h, middleware.CSRFRefreshPath, map[string]string{"Sec-Fetch-Site": site}); rec.Code != http.StatusOK {
			t.Errorf("Sec-Fetch-Site %q answered %d, want 200", site, rec.Code)
		}
	}
}

// TestTheRenewalIsAnsweredWhateverTheApplicationRoutes: the check owns the
// address, so an application that registered nothing for it -- and one whose
// router would have answered 404 -- still has it.
func TestTheRenewalIsAnsweredWhateverTheApplicationRoutes(t *testing.T) {
	rec := get(refreshApp(time.Hour, "session-1"), middleware.CSRFRefreshPath, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("%s answered %d %q, want a token", middleware.CSRFRefreshPath, rec.Code, rec.Body.String())
	}
}

// TestOnlyAGETIsTheRenewal: a POST to the address is a state-changing request
// like any other and is checked, not answered with a token.
func TestOnlyAGETIsTheRenewal(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, middleware.CSRFRefreshPath, nil)
	rec := httptest.NewRecorder()
	refreshApp(time.Hour, "session-1").ServeHTTP(rec, r)
	if rec.Code != 419 {
		t.Fatalf("a POST to the renewal answered %d, want 419", rec.Code)
	}
}

// TestTheScriptAsksWhereTheMiddlewareAnswers: ui.js is read by a browser and the
// check by Go, and nothing else ties the two spellings together.
func TestTheScriptAsksWhereTheMiddlewareAnswers(t *testing.T) {
	body, err := os.ReadFile("../../view/assets/ui.js")
	if err != nil {
		t.Fatal(err)
	}
	if want := "var CSRF_URL = '" + middleware.CSRFRefreshPath + "';"; !strings.Contains(string(body), want) {
		t.Fatalf("ui.js does not contain %q: the script and the route disagree about the address", want)
	}
}
