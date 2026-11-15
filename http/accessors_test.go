package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
)

func TestTheAddressIsReadWithOrWithoutAPortOnIt(t *testing.T) {
	// httptest gives a RemoteAddr with a port, and middleware.TrustProxies
	// writes one without: the port belonged to a connection the request did not
	// arrive on. Both have to answer.
	for raw, want := range map[string]string{
		"192.0.2.9:54321": "192.0.2.9",
		"192.0.2.9":       "192.0.2.9",
		"[2001:db8::1]:8": "2001:db8::1",
	} {
		req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		req.RemoteAddr = raw
		if got := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil).IP(); got != want {
			t.Errorf("RemoteAddr %q read as %q, want %q", raw, got, want)
		}
	}
}

func TestABearerTokenIsReadWhateverCaseTheSchemeArrivedIn(t *testing.T) {
	for header, want := range map[string]string{
		"Bearer abc.def":  "abc.def",
		"bearer abc.def":  "abc.def",
		"BEARER  abc.def": "abc.def",
		"Basic abc":       "",
		"":                "",
		"Bearer":          "",
	} {
		req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		if got := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil).BearerToken(); got != want {
			t.Errorf("Authorization %q read as %q, want %q", header, got, want)
		}
	}
}

func TestACookieThatIsNotThereReadsAsEmptyAndNotAsAFailure(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	req.AddCookie(&stdhttp.Cookie{Name: "theme", Value: "dark"})
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if got := ctx.Cookie("theme"); got != "dark" {
		t.Errorf("Cookie = %q", got)
	}
	if got := ctx.Cookie("locale"); got != "" {
		t.Errorf("a cookie nobody set read as %q", got)
	}
}

func TestACookieIsWrittenOnTheAnswerAndTheLineStillChains(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, httptest.NewRequest(stdhttp.MethodPost, "/settings", nil), nil, nil)

	if err := ctx.WithCookie(&stdhttp.Cookie{Name: "theme", Value: "dark", Path: "/"}).Redirect("/settings"); err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "dark" {
		t.Fatalf("cookies on the answer: %v", cookies)
	}
	if rec.Header().Get("Location") != "/settings" {
		t.Error("the redirect was lost, so WithCookie does not chain")
	}

	// A nil cookie writes nothing rather than panicking: the caller that has one
	// only sometimes has no branch to get wrong.
	rec = httptest.NewRecorder()
	hhttp.NewContext(rec, httptest.NewRequest(stdhttp.MethodGet, "/", nil), nil, nil).WithCookie(nil)
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a nil cookie was written")
	}
}

func TestYPYAsksForHTMLAndIsNeverAnsweredWithJSON(t *testing.T) {
	// YPY swaps HTML, whatever a script or a proxy adds to its request.
	// Answering it with JSON puts a JSON document inside a div.
	req := httptest.NewRequest(stdhttp.MethodGet, "/inbox/rows", nil)
	req.Header.Set("YPY-Request", "true")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if !ctx.IsYPY() {
		t.Error("IsYPY did not recognise a YPY request")
	}
	if ctx.WantsJSON() {
		t.Error("YPY was told it wanted JSON")
	}

	asked := httptest.NewRequest(stdhttp.MethodGet, "/api/invoices", nil)
	asked.Header.Set("Accept", "application/json")
	if !hhttp.NewContext(httptest.NewRecorder(), asked, nil, nil).WantsJSON() {
		t.Error("a request that asked for JSON was going to get a page")
	}

	xhr := httptest.NewRequest(stdhttp.MethodGet, "/api/invoices", nil)
	xhr.Header.Set("X-Requested-With", "XMLHttpRequest")
	if !hhttp.NewContext(httptest.NewRecorder(), xhr, nil, nil).WantsJSON() {
		t.Error("an XHR that is not YPY was going to get a page")
	}

	plain := httptest.NewRequest(stdhttp.MethodGet, "/invoices", nil)
	plain.Header.Set("Accept", "text/html")
	if hhttp.NewContext(httptest.NewRecorder(), plain, nil, nil).WantsJSON() {
		t.Error("a browser navigation was going to get JSON")
	}
}

// TestWhatReadsAHeaderDeclaresItInVary: each action wrote its own Vary, and
// one that declared only Accept was cached across YPY-Request, which
// WantsJSON also reads.
func TestWhatReadsAHeaderDeclaresItInVary(t *testing.T) {
	answer := func(ask func(*hhttp.Context)) []string {
		w := httptest.NewRecorder()
		w.Header().Add("Vary", "accept-encoding, Accept")
		ask(hhttp.NewContext(w, httptest.NewRequest(stdhttp.MethodGet, "/", nil), nil, nil))
		return w.Header().Values("Vary")
	}
	for name, c := range map[string]struct {
		ask  func(*hhttp.Context)
		want []string
	}{
		"WantsJSON": {func(c *hhttp.Context) { c.WantsJSON() }, []string{"accept-encoding, Accept, YPY-Request, X-Requested-With"}},
		"IsYPY":     {func(c *hhttp.Context) { c.IsYPY(); c.IsYPY() }, []string{"accept-encoding, Accept, YPY-Request"}},
		"Targets":   {func(c *hhttp.Context) { c.Targets("note-7") }, []string{"accept-encoding, Accept, YPY-Target"}},
	} {
		if got := answer(c.ask); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Vary = %q, want %q", name, got, c.want)
		}
	}

	w := httptest.NewRecorder()
	w.Header().Set("Vary", "*")
	hhttp.NewContext(w, httptest.NewRequest(stdhttp.MethodGet, "/", nil), nil, nil).WantsJSON()
	if got := w.Header().Values("Vary"); !reflect.DeepEqual(got, []string{"*"}) {
		t.Errorf("Vary = %q, want * left alone", got)
	}
}

// TestVaryIsOneHeaderWithEveryName: each name went on a Vary line of its own,
// and a reader of one header -- Header().Get, a test kit, a proxy that keeps
// the first line -- saw Accept and lost the rest.
func TestVaryIsOneHeaderWithEveryName(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Add("Vary", "accept-encoding, Origin")
	c := hhttp.NewContext(w, httptest.NewRequest(stdhttp.MethodGet, "/", nil), nil, nil)
	c.WantsJSON()
	c.Targets("note-7")
	c.IsYPY()
	want := []string{"Accept-Encoding, Origin, Accept, YPY-Request, X-Requested-With, YPY-Target"}
	if got := w.Header().Values("Vary"); !reflect.DeepEqual(got, want) {
		t.Errorf("Vary = %q, want %q", got, want)
	}

	star := httptest.NewRecorder()
	star.Header().Add("Vary", "Origin")
	star.Header().Add("Vary", "*")
	hhttp.NewContext(star, httptest.NewRequest(stdhttp.MethodGet, "/", nil), nil, nil).WantsJSON()
	if got := star.Header().Values("Vary"); !reflect.DeepEqual(got, []string{"Origin", "*"}) {
		t.Errorf("Vary = %q, want a * on any line left alone", got)
	}
}

// TestTargetsReadsTheElementYPYSwaps: the fragment for one element is
// answered only to the swap that names it.
func TestTargetsReadsTheElementYPYSwaps(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodGet, "/notes/7", nil)
	req.Header.Set("YPY-Request", "true")
	req.Header.Set("YPY-Target", "div#note-7")
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)
	if !ctx.Targets("note-7") || ctx.Targets("note-8") || ctx.Targets("") {
		t.Fatal("Targets did not answer for the element named by YPY-Target alone")
	}

	// The header is tag#id, so the id is what follows the #; a tag#id asked for
	// directly is compared whole, and one of another element does not match.
	for _, tc := range []struct {
		header, id string
		hits       bool
	}{
		{"div#note-7", "note-7", true},
		{"div#note-7", "div#note-7", true},
		{"div#note-7", "section#note-7", false},
		{"div#note-7", "div#note-8", false},
		{"div#note-7", "note-8", false},
		{"note-7", "note-7", false},
		{"div", "div", false},
		{"div#", "", false},
		{"section#a%20b", "a b", true},
		{"section#a%20b", "section#a b", true},
		{"main#note-7#x", "main#note-7#x", true},
	} {
		r := httptest.NewRequest(stdhttp.MethodGet, "/notes/7", nil)
		r.Header.Set("YPY-Target", tc.header)
		if got := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Targets(tc.id); got != tc.hits {
			t.Errorf("YPY-Target %q: Targets(%q) = %v, want %v", tc.header, tc.id, got, tc.hits)
		}
	}
	plain := hhttp.NewContext(httptest.NewRecorder(), httptest.NewRequest(stdhttp.MethodGet, "/notes/7", nil), nil, nil)
	if plain.Targets("") || plain.Targets("note-7") {
		t.Fatal("a request with no YPY-Target targets an element")
	}
}

func TestTheFullAddressIsWhatTheBrowserUsedAndNotWhatTheProcessListensOn(t *testing.T) {
	// A link inside a page is a path. This is for the ones that leave: a mail.
	req := httptest.NewRequest(stdhttp.MethodGet, "http://example.test/invoices/42?tab=lines", nil)
	req.URL.Scheme = "" // as a server-side request arrives
	req.Host = "example.test"

	if got := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil).FullURL(); got != "http://example.test/invoices/42?tab=lines" {
		t.Errorf("FullURL = %q", got)
	}

	// Behind a proxy the scheme is what TrustProxies recorded, not what this
	// process is listening on.
	req.URL.Scheme = "https"
	if got := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil).FullURL(); got != "https://example.test/invoices/42?tab=lines" {
		t.Errorf("FullURL behind a proxy = %q", got)
	}
}

func TestAControllerAsksWhoIsSignedInAndGetsNobodyWhenNobodyIs(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodGet, "/dashboard", nil)
	if _, ok := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil).User(); ok {
		t.Error("a request nobody authenticated produced a subject")
	}

	signed := req.WithContext(auth.WithSubject(req.Context(), auth.Subject{ID: "u_1", Tenant: "t_1"}))
	who, ok := hhttp.NewContext(httptest.NewRecorder(), signed, nil, nil).User()
	if !ok || who.ID != "u_1" {
		t.Errorf("User = %+v, %v", who, ok)
	}
}

func TestPathAndMethodAreTheRequestsOwn(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodPost, "/invoices/42?tab=lines", nil)
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if got := ctx.Path(); got != "/invoices/42" {
		t.Errorf("Path = %q, want the path without the query", got)
	}
	if got := ctx.Method(); got != stdhttp.MethodPost {
		t.Errorf("Method = %q", got)
	}
	req.Header.Set("X-Request-Id", "01J")
	if got := ctx.Header("x-request-id"); got != "01J" {
		t.Errorf("Header = %q, and the name is canonicalised like net/http does", got)
	}
}
