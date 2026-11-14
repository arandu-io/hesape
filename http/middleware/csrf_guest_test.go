package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

// issuing runs CSRFProtect over csrf with a request that carries no session,
// and returns what the handler found on the context and what the response set.
func issuing(t *testing.T, csrf *session.CSRF, method string) (token string, found bool, guest *http.Cookie) {
	t.Helper()
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, found = hhttp.CSRFTokenFrom(r.Context())
	}), middleware.CSRFProtect(csrf, func(*http.Request) string { return "" }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/signin", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.GuestCookieName {
			guest = c
		}
	}
	return token, found, guest
}

// A page a guest loads carries a token, and the guest cookie it is bound to is
// one script cannot read and a browser sends only over HTTPS.
func TestAGuestPageIsIssuedATokenAndASecureGuestCookie(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		token, found, guest := issuing(t, session.NewCSRF(appKey, time.Hour), method)
		if !found || token == "" {
			t.Errorf("%s: no token on the request context, so view.New draws a form that is refused", method)
		}
		if guest == nil {
			t.Fatalf("%s: no %s cookie was set, so the token is bound to nothing", method, session.GuestCookieName)
		}
		if !guest.HttpOnly || !guest.Secure || guest.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s: guest cookie HttpOnly=%v Secure=%v SameSite=%v, want true, true, Lax",
				method, guest.HttpOnly, guest.Secure, guest.SameSite)
		}
	}
}

// Development over plain HTTP turns Secure off, or the browser never sends the
// cookie back and every guest form is refused.
func TestSecureFalseLeavesTheGuestCookieUsableOverPlainHTTP(t *testing.T) {
	_, _, guest := issuing(t, session.NewCSRF(appKey, time.Hour).Secure(false), http.MethodGet)
	if guest == nil || guest.Secure {
		t.Fatalf("guest cookie = %+v, want one without Secure", guest)
	}
}

// OPTIONS and TRACE draw no page, so nothing is issued and no cookie is set.
func TestARequestThatDrawsNoPageIsIssuedNothing(t *testing.T) {
	for _, method := range []string{http.MethodOptions, http.MethodTrace} {
		_, found, guest := issuing(t, session.NewCSRF(appKey, time.Hour), method)
		if found || guest != nil {
			t.Errorf("%s: token on context = %v, guest cookie = %v; want neither", method, found, guest)
		}
	}
}

// A request that passed the check carries the token it submitted, so a form
// drawn again on that same request posts a token that still validates.
func TestAnAcceptedRequestCarriesItsTokenOnTheContext(t *testing.T) {
	csrf := session.NewCSRF(appKey, time.Hour)
	token, err := csrf.Issue("session-1")
	if err != nil {
		t.Fatal(err)
	}

	var onContext string
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onContext, _ = hhttp.CSRFTokenFrom(r.Context())
	}), middleware.CSRFProtect(csrf, func(*http.Request) string { return "session-1" }))

	r := httptest.NewRequest(http.MethodPost, "/invoices", nil)
	r.Header.Set("X-CSRF-Token", token)
	h.ServeHTTP(httptest.NewRecorder(), r)

	if onContext != token {
		t.Fatalf("token on the context = %q, want the one submitted", onContext)
	}
}

// A request with a session is bound to the session and is not handed a guest
// cookie it has no use for.
func TestASessionIsNotGivenAGuestCookie(t *testing.T) {
	csrf := session.NewCSRF(appKey, time.Hour)
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.CSRFProtect(csrf, func(*http.Request) string { return "session-1" }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.GuestCookieName {
			t.Fatalf("a request with a session was given a %s cookie", session.GuestCookieName)
		}
	}
}

// A token bound to the empty id was one token for every visitor. Validation
// with no session and no guest cookie now refuses even a token signed that way.
func TestATokenWithNoBindingIsRefused(t *testing.T) {
	h, _ := csrfHandler("")
	guestless, err := session.NewCSRF(appKey, time.Hour).Issue("guest:someone-else")
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/invoices", nil)
	r.Header.Set("X-CSRF-Token", guestless)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != middleware.StatusCSRFExpired {
		t.Fatalf("status = %d, want %d for a request that carries neither a session nor a guest cookie",
			rec.Code, middleware.StatusCSRFExpired)
	}
}

// A response that never asks for the token is given no cookie: an API call, a
// stylesheet or a 401 a client reads and throws away does not set a guest binding
// in a browser that will never use it.
func TestAResponseThatDrawsNoFormIsGivenNoCookie(t *testing.T) {
	csrf := session.NewCSRF(appKey, time.Hour)
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}), middleware.CSRFProtect(csrf, func(*http.Request) string { return "" }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/things", nil))
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("a response that drew no form set cookies %v", got)
	}
	if rec.Header().Get("Cache-Control") != "" {
		t.Fatalf("Cache-Control = %q on a response that carries no token", rec.Header().Get("Cache-Control"))
	}
}

// The page that does draw a token is for one visitor: no cache keeps it, unless
// the handler already chose a policy.
func TestAPageThatCarriesATokenIsNotCached(t *testing.T) {
	csrf := session.NewCSRF(appKey, time.Hour)
	serve := func(policy string) *httptest.ResponseRecorder {
		h := pipeline.Chain[http.Handler](http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if policy != "" {
				w.Header().Set("Cache-Control", policy)
			}
			hhttp.CSRFTokenFrom(r.Context())
		}), middleware.CSRFProtect(csrf, func(*http.Request) string { return "" }))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/acessar-conta", nil))
		return rec
	}
	if got := serve("").Header().Get("Cache-Control"); got != "no-store, private" {
		t.Fatalf("Cache-Control = %q, want no-store, private", got)
	}
	if got := serve("no-cache").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want the handler's own policy kept", got)
	}
}

// The guest cookie takes the name the issuer was given, and the renewal answers
// at the application's address as well as at the old one.
func TestTheGuestCookieAndTheRefreshFollowTheBrand(t *testing.T) {
	brand := support.NewBrand("Peráta")
	csrf := session.NewCSRF(appKey, time.Hour, session.WithCookieNames(session.CookieNamesFor(brand)))
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hhttp.CSRFTokenFrom(r.Context())
	}), middleware.CSRFProtect(csrf, func(*http.Request) string { return "" }, middleware.CSRFRefreshUnder(brand)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/acessar-conta", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "perata_csrf_guest" {
		t.Fatalf("cookies = %v, want only perata_csrf_guest", cookies)
	}

	for _, path := range []string{"/_perata/csrf", middleware.CSRFRefreshPath} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token"`) {
			t.Errorf("%s answered %d %q, want a token", path, rec.Code, rec.Body.String())
		}
	}
}
