package middleware_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/session/middleware"
)

func manager(cfg session.Config) *session.SessionManager {
	if cfg.Driver == "" {
		cfg.Driver = "array"
	}
	if cfg.Cookie == "" {
		cfg.Cookie = "arandu_session"
	}
	return session.NewSessionManager(cfg, nil)
}

// pageRequest is what a browser navigating sends, which is what the middleware
// treats as somewhere worth remembering.
func pageRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Accept", "text/html")
	return r
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie on the response: %v", name, w.Header())
	return nil
}

// TestTheSessionCookieIsOnTheResponseEvenWhenTheHandlerWroteFirst: a header
// set after the first byte never reaches the browser, so the middleware has
// to finish the session on the first write.
func TestTheSessionCookieIsOnTheResponseEvenWhenTheHandlerWroteFirst(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{HTTPOnly: true}), nil)

	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := middleware.Session(r.Context())
		if !ok {
			t.Error("the handler got no session")
			return
		}
		s.Put("subject", "1")
		w.Write([]byte("hello"))
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, pageRequest(http.MethodGet, "/"))

	c := sessionCookie(t, w, "arandu_session")
	if len(c.Value) != 40 {
		t.Fatalf("the cookie carries %q", c.Value)
	}
	if !c.HttpOnly {
		t.Fatal("the cookie is readable from JavaScript")
	}
	if w.Body.String() != "hello" {
		t.Fatalf("body: %q", w.Body.String())
	}
}

func TestTheSessionCookieIsThereWhenTheHandlerWroteNothing(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)
	handler := m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, pageRequest(http.MethodGet, "/"))

	sessionCookie(t, w, "arandu_session")
}

// TestTheSessionSurvivesFromOneRequestToTheNext is the whole middleware in one
// assertion: what a handler puts in the session on one request is what the next
// request reads, carried by the cookie.
func TestTheSessionSurvivesFromOneRequestToTheNext(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)

	write := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		s.Flash("status", "Saved.")
		w.WriteHeader(http.StatusFound)
	}))
	first := httptest.NewRecorder()
	write.ServeHTTP(first, pageRequest(http.MethodPost, "/invoices"))
	c := sessionCookie(t, first, "arandu_session")

	var seen any
	read := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		seen = s.Get("status")
	}))
	r := pageRequest(http.MethodGet, "/invoices")
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	second := httptest.NewRecorder()
	read.ServeHTTP(second, r)

	if seen != "Saved." {
		t.Fatalf("the flash did not cross the redirect: %v", seen)
	}

	// And the request after that no longer sees it.
	var again any
	third := httptest.NewRecorder()
	r2 := pageRequest(http.MethodGet, "/invoices")
	r2.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		again = s.Get("status")
	})).ServeHTTP(third, r2)
	if again != nil {
		t.Fatalf("the flash outlived its redirect: %v", again)
	}
}

// TestThePreviousURLIsOnlyRememberedForAPageSomebodyNavigatedTo. Remembering a
// POST, a fragment or a stylesheet is a sign-in that lands on one of those.
func TestThePreviousURLIsOnlyRememberedForAPageSomebodyNavigatedTo(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)

	remembered := func(r *http.Request) string {
		var out string
		m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, _ := middleware.Session(r.Context())
			// Read after the middleware has finished, through the same store.
			defer func() { out = s.PreviousURL() }()
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(httptest.NewRecorder(), r)
		return out
	}

	// The whole address, scheme and host included. It used to be
	// r.URL.RequestURI(): a "back" built from a path alone loses the host,
	// so a redirect after signing in on one host lands on whichever host
	// answers the redirect.
	if got := remembered(pageRequest(http.MethodGet, "/invoices?page=2")); got != "http://example.com/invoices?page=2" {
		t.Fatalf("a page navigation was not remembered whole: %q", got)
	}
	secure := pageRequest(http.MethodGet, "/invoices")
	secure.TLS = &tls.ConnectionState{}
	secure.Host = "app.example.test"
	if got := remembered(secure); got != "https://app.example.test/invoices" {
		t.Fatalf("the scheme and host were not remembered: %q", got)
	}
	if got := remembered(pageRequest(http.MethodPost, "/invoices")); got != "" {
		t.Fatalf("a POST was remembered: %q", got)
	}

	fragment := pageRequest(http.MethodGet, "/invoices/row")
	fragment.Header.Set("HX-Request", "true")
	if got := remembered(fragment); got != "" {
		t.Fatalf("an HTMX fragment was remembered: %q", got)
	}

	asset := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	asset.Header.Set("Accept", "text/css,*/*;q=0.1")
	if got := remembered(asset); got != "" {
		t.Fatalf("a stylesheet was remembered: %q", got)
	}
}

// servedThroughRoute registers the path under name, with the session
// middleware in the route's own chain, and answers one request through it. It
// hands back the store as it stood once the middleware had finished with it.
//
// The middleware goes in the route's chain rather than around the router
// because that is where a request carrying the matched route reaches it: the
// route installs itself in the context and then runs its middleware.
func servedThroughRoute(t *testing.T, name string, r *http.Request) *session.Store {
	t.Helper()

	m := middleware.NewStartSession(manager(session.Config{}), nil)

	var store *session.Store
	route := routing.NewRouter().Get(r.URL.Path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		// Read after the middleware has finished, through the same store.
		defer func() { store = s }()
		w.WriteHeader(http.StatusOK)
	}), m.Handle)
	if name != "" {
		route.Name(name)
	}

	route.ServeHTTP(httptest.NewRecorder(), r)
	if store == nil {
		t.Fatal("the handler never ran, so this proves nothing about what was remembered")
	}
	return store
}

// The address alone cannot say which page it was. Deciding whether to offer the
// way back, or whether somebody is already on the page they would be sent to,
// is a decision about the route -- and reaching it from a URL means matching it
// a second time, or comparing strings against a path that carries an id.
func TestThePreviousRouteIsRememberedBesideTheAddress(t *testing.T) {
	store := servedThroughRoute(t, "invoices.index", pageRequest(http.MethodGet, "/invoices"))

	if got := store.PreviousRoute(); got != "invoices.index" {
		t.Errorf("the previous route is %q, want invoices.index", got)
	}
	if got := store.PreviousURL(); got != "http://example.com/invoices" {
		t.Errorf("the previous address is %q, and the two have to describe one page", got)
	}
}

// One filter, or two definitions of "where I was". A fragment that updated the
// name and not the address would leave a name pointing at a page nobody was on.
func TestThePreviousRouteIsFilteredExactlyAsTheAddressIs(t *testing.T) {
	fragment := pageRequest(http.MethodGet, "/invoices")
	fragment.Header.Set("HX-Request", "true")

	for _, c := range []struct {
		name    string
		request *http.Request
	}{
		{"a submission", pageRequest(http.MethodPost, "/invoices")},
		{"an HTMX fragment", fragment},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := servedThroughRoute(t, "invoices.index", c.request)

			if got := store.PreviousRoute(); got != "" {
				t.Errorf("%s was remembered as %q", c.name, got)
			}
			if got := store.PreviousURL(); got != "" {
				t.Errorf("%s was remembered at %q", c.name, got)
			}
		})
	}
}

// A page whose route carries no name is remembered at its address under the
// empty name, and the name of the page before it is cleared rather than left.
// Leaving it is worse than saying nothing: the pair would then describe two
// different pages, and nothing in the session says which half is stale.
func TestAnUnnamedRouteClearsTheNameRatherThanLeavingTheOneBeforeIt(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)
	router := routing.NewRouter()

	var store *session.Store
	report := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		defer func() { store = s }()
		w.WriteHeader(http.StatusOK)
	})
	named := router.Get("/invoices", report, m.Handle).Name("invoices.index")
	unnamed := router.Get("/health", report, m.Handle)

	first := httptest.NewRecorder()
	named.ServeHTTP(first, pageRequest(http.MethodGet, "/invoices"))
	if got := store.PreviousRoute(); got != "invoices.index" {
		t.Fatalf("the first page was remembered as %q, so the rest of this proves nothing", got)
	}

	// The same session, carried on to a page whose route has no name.
	c := sessionCookie(t, first, "arandu_session")
	second := pageRequest(http.MethodGet, "/health")
	second.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	unnamed.ServeHTTP(httptest.NewRecorder(), second)

	if got := store.PreviousRoute(); got != "" {
		t.Errorf("the name of the page before it survived as %q", got)
	}
	if got := store.PreviousURL(); got != "http://example.com/health" {
		t.Errorf("the previous address is %q, and it has to be the page with no name", got)
	}
}

func TestAForgedCookieStartsAnEmptySessionRatherThanAnError(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)

	var id string
	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		id = s.GetID()
	}))
	r := pageRequest(http.MethodGet, "/")
	r.AddCookie(&http.Cookie{Name: "arandu_session", Value: "../../etc/passwd"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("a cookie anybody can set produced %d", w.Code)
	}
	if strings.Contains(id, "/") || len(id) != 40 {
		t.Fatalf("the forged value became the session id: %q", id)
	}
}

func TestNoDriverMeansNoSessionAndNoRefusal(t *testing.T) {
	m := middleware.NewStartSession(session.NewSessionManager(session.Config{}, nil), nil)

	reached := false
	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := middleware.Session(r.Context()); ok {
			t.Error("there is a session with no driver configured")
		}
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, pageRequest(http.MethodGet, "/"))

	if !reached {
		t.Fatal("the handler was not reached")
	}
}

// fakeLocks records what was locked, and can refuse.
type fakeLocks struct {
	names   []string
	refuse  bool
	held    int
	release int
}

func (l *fakeLocks) Lock(_ context.Context, name string, _, _ time.Duration) (func(), bool) {
	l.names = append(l.names, name)
	if l.refuse {
		return nil, false
	}
	l.held++
	return func() { l.release++ }, true
}

func TestBlockingTakesTheLockAndReleasesIt(t *testing.T) {
	locks := &fakeLocks{}
	m := middleware.NewStartSession(manager(session.Config{Block: true}), locks)

	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, pageRequest(http.MethodGet, "/"))

	if len(locks.names) != 1 || !strings.HasPrefix(locks.names[0], "session:") {
		t.Fatalf("locked %v", locks.names)
	}
	if locks.held != 1 || locks.release != 1 {
		t.Fatalf("held %d, released %d", locks.held, locks.release)
	}
}

func TestBlockingRefusesRatherThanRunningUnlocked(t *testing.T) {
	// No lock factory at all: running unprotected is the lost write blocking was
	// turned on to stop, and doing it quietly means nobody finds out.
	m := middleware.NewStartSession(manager(session.Config{Block: true}), nil)
	w := httptest.NewRecorder()
	m.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the handler ran with blocking on and no lock")
	})).ServeHTTP(w, pageRequest(http.MethodGet, "/"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", w.Code)
	}

	// A wait that ran out is the session being busy, not the request being
	// wrong, so it is a 429 the browser can retry.
	busy := middleware.NewStartSession(manager(session.Config{Block: true}), &fakeLocks{refuse: true})
	second := httptest.NewRecorder()
	busy.Handle(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the handler ran without the lock")
	})).ServeHTTP(second, pageRequest(http.MethodGet, "/"))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d", second.Code)
	}
}

func TestGetSessionIsUsableWithoutThePipeline(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)

	r := pageRequest(http.MethodGet, "/")
	r.AddCookie(&http.Cookie{Name: "arandu_session", Value: strings.Repeat("a", 40)})
	s, err := m.GetSession(r)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if s.GetID() != strings.Repeat("a", 40) {
		t.Fatalf("got %q", s.GetID())
	}
}

func TestSessionOnAContextThatHasNoneAnswersFalse(t *testing.T) {
	if _, ok := middleware.Session(context.Background()); ok {
		t.Fatal("a bare context produced a session")
	}
}

func TestGarbageIsCollectedOnTheOddsConfigured(t *testing.T) {
	// Two in two: every request sweeps, which is what makes this assertable.
	m := middleware.NewStartSession(manager(session.Config{Lottery: [2]int{2, 2}, Lifetime: time.Nanosecond}), nil)

	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, pageRequest(http.MethodGet, "/"))
	c := sessionCookie(t, first, "arandu_session")

	// The first request's record is older than a nanosecond by now, so the
	// second request's sweep takes it and the session comes back empty.
	r := pageRequest(http.MethodGet, "/")
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	var token string
	m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		token = s.Token()
	})).ServeHTTP(httptest.NewRecorder(), r)

	if token == "" {
		t.Fatal("the swept session did not get a fresh token")
	}
}
