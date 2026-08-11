package arandutest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Client is a browser for a test: it keeps cookies and it does not follow
// redirects.
//
// Not following them is deliberate. A test that asserts about a page after a
// redirect cannot tell a 200 from a 302 that happened to land somewhere with the
// same words on it, and "it redirected me to the sign-in screen" is the most
// common way a feature test passes while proving the opposite of what it says.
type Client struct {
	t       *testing.T
	handler http.Handler
	cookies []*http.Cookie

	// subject is who this client is, when ActingAs said so. It is a pointer
	// because the zero Subject is a meaningful value -- the session somebody
	// forgot to load, which Authorize refuses -- and nil has to mean "nobody
	// said", not "somebody empty".
	subject *auth.Subject

	// lastBody is the page this client loaded most recently, and the CSRF token
	// is read out of it. It is a field and not a package variable: two clients
	// in one test would otherwise post each other's tokens, and t.Parallel would
	// make that a race rather than a wrong answer.
	lastBody string
}

// NewClient returns a client over a handler.
func NewClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	return &Client{t: t, handler: h}
}

// ActingAs makes every request from here on carry the subject, and returns the
// client so it reads as one line:
//
//	arandutest.NewClient(t, app).ActingAs(alice).Get("/invoices").OK()
//
// The subject travels under auth.WithSubject, which is the one key a policy
// reads. There is no second context key and no test-only guard: this is the
// same value the edge middleware writes when it loads a real session, so a
// handler cannot tell a test apart from a request, which is the only way the
// test proves anything.
//
// It is applied to the outgoing request, before any middleware runs. A
// middleware that loads a real session therefore wins, and that ordering is the
// right one -- a session that exists beats a pretend one -- but it also means
// ActingAs and signing in through the form do not quietly combine. Pick one per
// test: the sign-in flow is tested by posting the form, everything downstream
// of it by acting as somebody.
//
// A subject with no id is refused here rather than three assertions later.
// Authorize refuses it too, so every request would come back forbidden with
// nothing saying why. auth.Guest is how a test says "anonymous reader" on
// purpose.
func (c *Client) ActingAs(s auth.Subject) *Client {
	c.t.Helper()
	if s.ID == "" && !s.IsGuest() {
		c.t.Fatalf("ActingAs was handed a subject with no id; " +
			"that is the session nobody loaded, Authorize refuses it, and every " +
			"request would answer 403. Use auth.Guest(tenant) for an anonymous reader.")
	}
	if s.Tenant == "" {
		c.t.Fatalf("ActingAs was handed a subject with no tenant; " +
			"every query is scoped by auth.Tenant(g) and a subject without one reaches no rows (RULE 14).")
	}
	c.subject = &s
	return c
}

// Get sends a GET.
func (c *Client) Get(path string) *Response { return c.do(http.MethodGet, path, nil, "") }

// Post sends a form.
//
// The CSRF token is read off the last page this client loaded and sent with the
// body, because that is what a browser does -- and a test that skips it is a
// test that proves the form works with protection disabled.
func (c *Client) Post(path string, form map[string]string) *Response {
	values := make([]string, 0, len(form)+1)
	if token := c.token(); token != "" {
		values = append(values, "_csrf="+token)
	}
	for k, v := range form {
		values = append(values, k+"="+strings.ReplaceAll(v, " ", "+"))
	}
	return c.do(http.MethodPost, path, strings.NewReader(strings.Join(values, "&")),
		"application/x-www-form-urlencoded")
}

func (c *Client) token() string {
	const marker = `name="_csrf" value="`
	at := strings.Index(c.lastBody, marker)
	if at < 0 {
		return ""
	}
	rest := c.lastBody[at+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func (c *Client) do(method, path string, body io.Reader, contentType string) *Response {
	c.t.Helper()

	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if c.subject != nil {
		req = req.WithContext(auth.WithSubject(req.Context(), *c.subject))
	}

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	// Cookies the response set are kept, so a session survives the next call.
	// Without this every request after a login is anonymous, and the test that
	// notices is the one that fails three cases later.
	for _, ck := range rec.Result().Cookies() {
		c.keep(ck)
	}
	c.lastBody = rec.Body.String()

	return &Response{t: c.t, rec: rec, path: method + " " + path}
}

// keep stores one Set-Cookie the way a browser stores it: by name and path, so
// a second value REPLACES the first, and a cookie the server told the browser to
// drop is dropped.
//
// It used to append, and a jar that only appends cannot express a sign-out.
// http.Request.Cookie returns the first cookie of a name, so the cleared value
// that logout sends sat behind the live one and was never read, and a second
// sign-in in the same test put a second session behind the first -- which by
// then the server had deleted, so the client silently went back to being
// anonymous while the test read on. Both of those are tests that pass while
// proving the opposite of what they say, which is the one failure this whole
// package exists to prevent.
func (c *Client) keep(ck *http.Cookie) {
	kept := c.cookies[:0]
	for _, have := range c.cookies {
		if have.Name != ck.Name || have.Path != ck.Path {
			kept = append(kept, have)
		}
	}
	c.cookies = kept

	// MaxAge below zero is "delete this now", and an Expires in the past is the
	// older spelling of the same instruction. Either way there is nothing to
	// store: the cookie is gone from the jar and the next request carries none.
	if ck.MaxAge < 0 || (!ck.Expires.IsZero() && !ck.Expires.After(time.Now())) {
		return
	}
	c.cookies = append(c.cookies, ck)
}

// Response is what came back, with the assertions worth having.
type Response struct {
	t    *testing.T
	rec  *httptest.ResponseRecorder
	path string
}

// AssertStatus answers TestResponse::assertStatus, and fails unless the status is the one expected.
//
// The body is printed on failure, because the answer to "why is this a 500" is
// in it and finding out otherwise means running the test again with a print.
func (r *Response) AssertStatus(want int) *Response {
	r.t.Helper()
	if r.rec.Code != want {
		r.t.Fatalf("%s answered %d, want %d\n%s", r.path, r.rec.Code, want, r.body())
	}
	return r
}

// AssertOk answers TestResponse::assertOk: AssertStatus(200).
func (r *Response) AssertOk() *Response { return r.AssertStatus(http.StatusOK) }

// AssertSee answers TestResponse::assertSee, and fails unless the text is in the body.
func (r *Response) AssertSee(text string) *Response {
	r.t.Helper()
	if !strings.Contains(r.rec.Body.String(), text) {
		r.t.Errorf("%s does not show %q\n%s", r.path, text, r.body())
	}
	return r
}

// AssertDontSee answers TestResponse::assertDontSee, and fails when the text is in the body.
//
// It is the half people skip, and the half that catches a leak: a draft in a
// public listing, an address in a page that should not name one, a button
// somebody without the permission can see.
func (r *Response) AssertDontSee(text string) *Response {
	r.t.Helper()
	if strings.Contains(r.rec.Body.String(), text) {
		r.t.Errorf("%s shows %q and should not", r.path, text)
	}
	return r
}

// AssertRedirect answers TestResponse::assertRedirect, and fails unless the response sends the client to that address.
//
// It reads HX-Redirect as well as Location, because a handler answering an HTMX
// request redirects with a header and a 204 -- and a test that only reads
// Location says "redirected to \"\"" for a response that is correct.
func (r *Response) AssertRedirect(want string) *Response {
	r.t.Helper()

	got := r.rec.Header().Get("Location")
	if got == "" {
		got = r.rec.Header().Get("HX-Redirect")
	}
	if got != want {
		r.t.Errorf("%s redirected to %q, want %q", r.path, got, want)
	}
	return r
}

// GetContent answers TestResponse::getContent: what came back, for an assertion this package does not have.
func (r *Response) GetContent() string { return r.rec.Body.String() }

// Header reads one response header.
func (r *Response) Header(name string) string { return r.rec.Header().Get(name) }

func (r *Response) body() string {
	body := r.rec.Body.String()
	if len(body) > 2000 {
		return body[:2000] + "\n… (truncated)"
	}
	return body
}
