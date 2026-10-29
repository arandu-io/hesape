package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
)

func intended() *hhttp.Intended {
	return hhttp.NewIntended([]byte(strings.Repeat("k", 32)), true)
}

// remembered runs the guard's half of the round trip and hands back a request
// carrying whatever cookie it wrote, which is what the browser would send to the
// sign-in screen next.
func remembered(i *hhttp.Intended, refused *stdhttp.Request) *stdhttp.Request {
	rec := httptest.NewRecorder()
	i.Remember(rec, refused)

	next := httptest.NewRequest(stdhttp.MethodGet, "/auth/login", nil)
	for _, c := range rec.Result().Cookies() {
		next.AddCookie(c)
	}
	return next
}

// asked builds the request net/http would build from this request target.
//
// Not httptest.NewRequest, which parses the target as a URL and hands back
// something already normalised: the shapes worth testing here are exactly the
// ones that survive the server's own parse, and url.ParseRequestURI is what the
// server calls.
func asked(t *testing.T, target string) *stdhttp.Request {
	t.Helper()

	r := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	u, err := url.ParseRequestURI(target)
	if err != nil {
		t.Fatalf("a server could not have received %q at all: %v", target, err)
	}
	r.URL = u
	return r
}

func TestSomebodyTurnedAwayComesBackToThePageTheyAskedForAndNotTheFrontPage(t *testing.T) {
	i := intended()

	next := remembered(i, httptest.NewRequest(stdhttp.MethodGet, "/invoices/42?tab=lines", nil))
	got := i.Take(httptest.NewRecorder(), next, "/")

	if got != "/invoices/42?tab=lines" {
		t.Fatalf("after signing in they were sent to %q; they had followed a link to /invoices/42?tab=lines and now have to find it again", got)
	}
}

func TestSomebodyWhoWasNotTurnedAwayFromAnywhereGoesWhereTheApplicationSays(t *testing.T) {
	got := intended().Take(httptest.NewRecorder(), httptest.NewRequest(stdhttp.MethodPost, "/auth/login", nil), "/dashboard")

	if got != "/dashboard" {
		t.Fatalf("a sign-in with no intended address went to %q, and the application asked for /dashboard", got)
	}
}

// The address is spent by the sign-in that uses it. Left standing, it is a
// destination somebody meets again at their next sign-in from this browser,
// weeks later, with nothing to explain it.
func TestAnIntendedAddressIsUsedOnceAndThenForgotten(t *testing.T) {
	i := intended()
	next := remembered(i, httptest.NewRequest(stdhttp.MethodGet, "/invoices/42", nil))

	rec := httptest.NewRecorder()
	if got := i.Take(rec, next, "/"); got != "/invoices/42" {
		t.Fatalf("the first sign-in went to %q, want /invoices/42", got)
	}

	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == hhttp.IntendedCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the browser was never told to drop the cookie, so the address is still there at the next sign-in")
	}

	// And the value itself does not work twice, whatever the browser does with
	// the instruction above.
	replay := httptest.NewRequest(stdhttp.MethodGet, "/auth/login", nil)
	for _, c := range rec.Result().Cookies() {
		replay.AddCookie(c)
	}
	if got := i.Take(httptest.NewRecorder(), replay, "/"); got != "/" {
		t.Errorf("the same intended address was handed out a second time: %q", got)
	}
}

// The open redirect, in the shape that reaches a handler looking like a path.
//
// "sign in, and then continue to somewhere that is not us" is the oldest
// phishing link there is, and the person following it has just typed a password.
// net/http parses a request target beginning with "//" into URL.Path, host and
// all, so it arrives here looking exactly like a local address -- and a browser
// resolves it to another origin.
//
// The other shapes are refused by LocalPath, which is where they are stated one
// per line; see local_test.go.
func TestAnAddressPointingAtAnotherSiteNeverBecomesARedirect(t *testing.T) {
	i := intended()

	next := remembered(i, asked(t, "//evil.example/takeover"))

	if got := i.Take(httptest.NewRecorder(), next, "/"); got != "/" {
		t.Fatalf("signing in would have sent the browser to %q, which is not this application", got)
	}
}

// Why the cookie is signed rather than merely same-site.
//
// SameSite keeps another SITE from setting it and does nothing about another
// HOST on the same registrable domain -- a customer's CNAME, a forgotten staging
// box -- which can write a cookie on the parent domain that arrives here
// indistinguishable from ours. Whoever writes it chooses where every person in
// the application lands right after authenticating.
func TestAnIntendedAddressThisApplicationDidNotWriteIsIgnored(t *testing.T) {
	ours := intended()
	theirs := hhttp.NewIntended([]byte(strings.Repeat("f", 32)), true)

	planted := remembered(theirs, httptest.NewRequest(stdhttp.MethodGet, "/admin/impersonate/1", nil))

	if got := ours.Take(httptest.NewRecorder(), planted, "/"); got != "/" {
		t.Fatalf("a cookie signed with somebody else's key chose the destination: %q", got)
	}
}

// A hx-get is a piece of a page, and sending somebody to it after they sign in
// leaves them looking at a partial: no layout, no navigation. It reads as a
// broken deploy.
//
// A boosted navigation is a whole page and is kept -- hx-boost is how most links
// in this stack are followed, so dropping it would drop nearly everything.
func TestAFragmentIsNotRememberedAsAPageAndABoostedNavigationIs(t *testing.T) {
	i := intended()

	fragment := httptest.NewRequest(stdhttp.MethodGet, "/inbox/rows?page=2", nil)
	fragment.Header.Set("HX-Request", "true")
	if got := i.Take(httptest.NewRecorder(), remembered(i, fragment), "/"); got != "/" {
		t.Errorf("after signing in they would land on the bare fragment %q, with no layout around it", got)
	}

	page := httptest.NewRequest(stdhttp.MethodGet, "/inbox?page=2", nil)
	page.Header.Set("HX-Request", "true")
	page.Header.Set("HX-Boosted", "true")
	if got := i.Take(httptest.NewRecorder(), remembered(i, page), "/"); got != "/inbox?page=2" {
		t.Errorf("a boosted link is an ordinary navigation and was forgotten: %q", got)
	}
}

// A script that runs in the background -- a bell polling for new notifications
// every few seconds -- is turned away when the session expires, exactly like a
// page. Remembering it sends the person, after signing in, to the JSON of the
// poll instead of to a page. Only a navigation of the document is kept.
func TestOnlyANavigationOfTheDocumentIsRememberedAndNotABackgroundRequest(t *testing.T) {
	const poll = "/painel/notificacoes/novas?desde=1730000000"

	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"a navigation the browser describes", map[string]string{
			"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Sec-Fetch-Site": "none",
			"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		}, poll},
		{"a fetch in cors mode", map[string]string{
			"Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty", "Accept": "*/*",
		}, "/"},
		{"a fetch in cors mode that says nothing else", map[string]string{"Sec-Fetch-Mode": "cors"}, "/"},
		{"a destination that is not a document", map[string]string{"Sec-Fetch-Dest": "empty"}, "/"},
		{"a frame", map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, "/"},
		{"a client that wants JSON and sends no Fetch Metadata", map[string]string{"Accept": "application/json"}, "/"},
		{"a client that accepts anything and sends no Fetch Metadata", map[string]string{"Accept": "*/*"}, "/"},
		{"an old browser that names text/html and sends no Fetch Metadata", map[string]string{
			"Accept": "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8",
		}, poll},
		{"text/html refused with a zero quality", map[string]string{"Accept": "text/html;q=0,application/json"}, "/"},
		{"a client that sends no headers at all", nil, poll},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := intended()
			r := httptest.NewRequest(stdhttp.MethodGet, poll, nil)
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}

			if got := i.Take(httptest.NewRecorder(), remembered(i, r), "/"); got != c.want {
				t.Errorf("after signing in they would land on %q, want %q", got, c.want)
			}
		})
	}
}

// The HTMX rule holds beside the Fetch Metadata one: a browser describes an
// hx-get as a fetch, so it is turned away on either ground, and a boosted
// navigation is kept when the browser calls it a navigation or says nothing.
func TestTheHTMXRuleStillHoldsBesideFetchMetadata(t *testing.T) {
	i := intended()

	boosted := httptest.NewRequest(stdhttp.MethodGet, "/inbox?page=2", nil)
	boosted.Header.Set("HX-Request", "true")
	boosted.Header.Set("HX-Boosted", "true")
	boosted.Header.Set("Accept", "*/*")
	boosted.Header.Set("Sec-Fetch-Mode", "cors")
	boosted.Header.Set("Sec-Fetch-Dest", "empty")
	// hx-boost swaps a page through XMLHttpRequest, which the browser calls a
	// cors fetch, so the Fetch Metadata alone would drop nearly every link in
	// this stack: a boosted request is a page whatever the browser says it is.
	if got := i.Take(httptest.NewRecorder(), remembered(i, boosted), "/"); got != "/inbox?page=2" {
		t.Errorf("a boosted navigation was forgotten: %q", got)
	}

	fragment := httptest.NewRequest(stdhttp.MethodGet, "/inbox/rows?page=2", nil)
	fragment.Header.Set("HX-Request", "true")
	fragment.Header.Set("Sec-Fetch-Mode", "navigate")
	fragment.Header.Set("Sec-Fetch-Dest", "document")
	if got := i.Take(httptest.NewRecorder(), remembered(i, fragment), "/"); got != "/" {
		t.Errorf("a fragment was remembered as %q", got)
	}
}

// The address is replayed by a browser navigation, which is a GET. A POST
// remembered here is a form submission turned into a link.
func TestOnlySomethingABrowserCanNavigateToIsRemembered(t *testing.T) {
	i := intended()

	posted := httptest.NewRequest(stdhttp.MethodPost, "/invoices/42/pay", nil)
	if got := i.Take(httptest.NewRecorder(), remembered(i, posted), "/"); got != "/" {
		t.Fatalf("signing in would have navigated to %q, which was a form submission", got)
	}
}

func TestTheIntendedCookieIsNotReadableByScriptsAndDoesNotTravelInTheClear(t *testing.T) {
	rec := httptest.NewRecorder()
	intended().Remember(rec, httptest.NewRequest(stdhttp.MethodGet, "/invoices/42", nil))

	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name != hhttp.IntendedCookieName {
			continue
		}
		found = true
		if !c.HttpOnly {
			t.Error("the cookie is readable by script, so anything injected into a page can read where the person was going")
		}
		if !c.Secure {
			t.Error("the cookie travels over plain HTTP on an Intended configured for HTTPS")
		}
		if c.MaxAge <= 0 || time.Duration(c.MaxAge)*time.Second > hhttp.IntendedLifetime {
			t.Errorf("MaxAge = %d, and the address is worth keeping for %s", c.MaxAge, hhttp.IntendedLifetime)
		}
	}
	if !found {
		t.Fatal("nothing was remembered for a plain GET of a page")
	}
}

// Signing out is the moment a shared machine changes hands, and the intended
// address outlives a session by up to IntendedLifetime.
//
// Left standing, the next person to sign in on that browser is carried to the
// page the previous one was refused -- which tells them who was being worked on,
// and reads to them as the application choosing a page at random. Clear is
// exported for that caller, and this is the round trip it has to survive.
func TestSigningOutForgetsWhereTheLastPersonWasGoing(t *testing.T) {
	i := intended()

	refused := httptest.NewRecorder()
	i.Remember(refused, httptest.NewRequest(stdhttp.MethodGet, "/customers/98213/invoices/44", nil))

	// The sign-out, which clears it beside invalidating the session.
	out := httptest.NewRecorder()
	i.Clear(out)

	// The next person, with the browser as the sign-out left it.
	next := httptest.NewRequest(stdhttp.MethodGet, "/auth/login", nil)
	jar := map[string]*stdhttp.Cookie{}
	for _, c := range refused.Result().Cookies() {
		jar[c.Name] = c
	}
	for _, c := range out.Result().Cookies() {
		jar[c.Name] = c
	}
	for _, c := range jar {
		if c.MaxAge < 0 {
			continue // the browser dropped it
		}
		next.AddCookie(c)
	}

	if got := i.Take(httptest.NewRecorder(), next, "/"); got != "/" {
		t.Fatalf("the next person to sign in on this machine was sent to %q, which is where the last one was going", got)
	}
}
