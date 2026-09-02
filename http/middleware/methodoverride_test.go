package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/html"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/session"
)

// served is the request as the handler under the middleware saw it.
type served struct {
	ran    bool
	method string
	form   url.Values
	// parsed is whether the form had already been parsed when the handler was
	// entered, read before the handler parses anything itself. It is how a
	// request the middleware was supposed to leave alone can say whether it was
	// left alone: a parse is the one mark this leaves on a request it does not
	// rewrite.
	parsed bool
}

// through runs one request through mw and reports what reached the handler.
//
// The handler parses the form for itself, because that is what a handler does
// and it is the half of the contract the middleware is easiest to break: the
// body has been read by the time the handler is called, so the values have to
// have survived the parse that read them.
func through(mw hhttp.Middleware, r *http.Request) (*served, *httptest.ResponseRecorder) {
	got := &served{}
	rec := httptest.NewRecorder()

	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.ran = true
		got.method = r.Method
		got.parsed = r.PostForm != nil
		_ = r.ParseForm()
		got.form = r.PostForm
	})).ServeHTTP(rec, r)

	return got, rec
}

// formRequest builds a request carrying a urlencoded body.
func formRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// A form posts and names the method it means; the router matches on the method,
// so this is what makes the route reachable at all.
func TestAPostIsRewrittenToTheMethodItsFormNames(t *testing.T) {
	for _, want := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(want, func(t *testing.T) {
			got, _ := through(middleware.OverrideMethod(),
				formRequest(http.MethodPost, "/posts/1", "_method="+want))

			if got.method != want {
				t.Errorf("the handler was reached as %s, want %s", got.method, want)
			}
		})
	}
}

// Only a POST is rewritten.
//
// A GET is the one that matters: it is safe, so a CSRF check waves it through
// untouched, and a rewrite would hand the router a state-changing method that
// no token was validated for -- off a link, which is all it takes. The others
// are here because a request that already says what it is has nothing to spoof.
func TestOnlyAPostIsRewritten(t *testing.T) {
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			got, _ := through(middleware.OverrideMethod(),
				formRequest(method, "/posts/1?_method=DELETE", "_method=DELETE"))

			if got.method != method {
				t.Errorf("a %s was served as %s, and only a POST is rewritten", method, got.method)
			}
		})
	}
}

// Anything but PUT, PATCH and DELETE is left alone, and left alone is not
// refused: what arrived is a valid POST that a route may well answer, and a
// middleware asked to translate a field it did not recognise has no business
// failing the request over it.
func TestOnlyPutPatchAndDeleteAreAcceptedAndTheRestAreIgnored(t *testing.T) {
	for _, field := range []string{
		"GET", "HEAD", "POST", "OPTIONS", "TRACE", "CONNECT",
		"BOGUS", "", "DELETE ", "PUT;DROP",
	} {
		t.Run("field="+field, func(t *testing.T) {
			got, rec := through(middleware.OverrideMethod(),
				formRequest(http.MethodPost, "/posts/1", "_method="+url.QueryEscape(field)))

			if got.method != http.MethodPost {
				t.Errorf("_method=%q served the request as %s, want POST", field, got.method)
			}
			if !got.ran || rec.Code != http.StatusOK {
				t.Errorf("_method=%q was refused with %d, and an unrecognised value is ignored, not an error",
					field, rec.Code)
			}
		})
	}
}

// The builder that writes the field uppercases the method, and a hand-written
// form that does not means the same one: a method is uppercase on the wire, so
// the value is uppercased before it is checked rather than assigned verbatim as
// a method no route is registered for.
func TestTheFieldIsReadWithoutRegardToCase(t *testing.T) {
	got, _ := through(middleware.OverrideMethod(),
		formRequest(http.MethodPost, "/posts/1", "_method=delete"))

	if got.method != http.MethodDelete {
		t.Errorf("_method=delete was served as %s, want DELETE", got.method)
	}
}

// The middleware reads the body, so the question is what is left of it.
//
// It parses rather than reading the bytes itself, and the parse is remembered on
// the request: the handler's own ParseForm finds the values already there
// instead of going back to a body that has been read. Get this wrong and every
// form in the application submits empty -- with a 200 on it, which is the shape
// of failure nobody reports as a bug in a middleware.
func TestTheHandlerStillReadsTheFormTheOverrideParsed(t *testing.T) {
	for _, body := range []string{
		"_method=DELETE&title=hello&title=second",
		"title=hello&title=second",
	} {
		t.Run(body, func(t *testing.T) {
			got, _ := through(middleware.OverrideMethod(),
				formRequest(http.MethodPost, "/posts/1", body))

			if titles := got.form["title"]; len(titles) != 2 || titles[0] != "hello" || titles[1] != "second" {
				t.Errorf("the handler read title=%q, want [hello second]", titles)
			}
		})
	}
}

// Where the override sits relative to the check that validates the submission.
//
// It goes behind it. The field is a value out of a body nothing has
// authenticated yet, and in front of the check the sender picks which method the
// check reads: the POST it is deciding about arrives at it as a DELETE. Today
// that changes no outcome, because every unsafe method is checked the same way,
// which is exactly why the order has to be written down somewhere that fails
// when it moves.
//
// Both orders are asserted here, because only the pair says which one is the
// decision: the second is what the wrong order produces.
func TestTheCheckInFrontSeesThePostAndTheHandlerSeesTheOverride(t *testing.T) {
	var sawBehind, sawAhead string

	got, _ := through(func(next http.Handler) http.Handler {
		return recordMethod(&sawBehind)(middleware.OverrideMethod()(next))
	}, formRequest(http.MethodPost, "/posts/1", "_method=DELETE"))

	if sawBehind != http.MethodPost {
		t.Errorf("the middleware in front of the override saw %s, want the POST the client sent", sawBehind)
	}
	if got.method != http.MethodDelete {
		t.Errorf("the handler behind the override saw %s, want DELETE", got.method)
	}

	through(func(next http.Handler) http.Handler {
		return middleware.OverrideMethod()(recordMethod(&sawAhead)(next))
	}, formRequest(http.MethodPost, "/posts/1", "_method=DELETE"))

	if sawAhead != http.MethodDelete {
		t.Errorf("with the override in front, the middleware behind it saw %s, and the point of "+
			"this half is that it sees a method the sender chose", sawAhead)
	}
}

// recordMethod stands in for whatever runs beside the override -- the check that
// validates the submission -- and records the method it was handed.
func recordMethod(into *string) hhttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*into = r.Method
			next.ServeHTTP(w, r)
		})
	}
}

// Only a urlencoded body is read, and a body that is not one is not touched.
//
// The parameters a browser sends with the header are the reason the type is
// parsed rather than compared: a comparison misses "; charset=UTF-8" and leaves
// every real submission alone. multipart is a form and is still not read here --
// ParseForm does not touch a multipart body, and reading one would mean
// buffering every upload in a middleware that usually finds no field.
//
// The method alone would not hold this. ParseForm fills PostForm from a
// urlencoded body only, so calling it on a JSON POST already yields no field to
// read and no rewrite: the method matches on either side of the gate, and a
// check that cannot fail proves nothing. What the gate decides is whether the
// request is parsed at all, and that is visible -- a request the middleware
// skipped reaches the handler with PostForm still nil, which is how ParseForm
// and ParseMultipartForm tell a fresh request from a parsed one.
func TestOnlyAFormBodyIsReadAndTheRestAreUntouched(t *testing.T) {
	for _, c := range []struct {
		contentType string
		body        string
		want        string
		parsed      bool
	}{
		{"application/x-www-form-urlencoded", "_method=DELETE", http.MethodDelete, true},
		{"application/x-www-form-urlencoded; charset=UTF-8", "_method=DELETE", http.MethodDelete, true},
		{"APPLICATION/X-WWW-FORM-URLENCODED", "_method=DELETE", http.MethodDelete, true},
		{"multipart/form-data; boundary=x", "_method=DELETE", http.MethodPost, false},
		{"application/json", `{"_method":"DELETE"}`, http.MethodPost, false},
		{"text/plain", "_method=DELETE", http.MethodPost, false},
		{"", "_method=DELETE", http.MethodPost, false},
		{"application/x-www-form-urlencoded;;", "_method=DELETE", http.MethodPost, false},
	} {
		t.Run("type="+c.contentType, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/posts/1", strings.NewReader(c.body))
			if c.contentType != "" {
				r.Header.Set("Content-Type", c.contentType)
			}

			got, _ := through(middleware.OverrideMethod(), r)

			if got.method != c.want {
				t.Errorf("a %q body was served as %s, want %s", c.contentType, got.method, c.want)
			}
			if got.parsed != c.parsed {
				t.Errorf("a %q body reached the handler with PostForm parsed=%t, want %t: "+
					"a request this does not read is a request it does not parse",
					c.contentType, got.parsed, c.parsed)
			}
		})
	}
}

// The field is read out of the body and never out of the query string. A form
// puts it in the body; ?_method=DELETE is something a link carries, and a link
// is what the POST-only rule exists to keep out.
func TestTheQueryStringCannotNameTheMethod(t *testing.T) {
	got, _ := through(middleware.OverrideMethod(),
		formRequest(http.MethodPost, "/posts/1?_method=DELETE", ""))

	if got.method != http.MethodPost {
		t.Errorf("a query parameter served the request as %s, want POST", got.method)
	}
}

// A body that will not parse steers nothing. The escape is invalid, so the parse
// fails; the request is left as the POST it arrived as, and the route that
// matches answers it.
func TestABodyThatWillNotParseLeavesTheMethodAlone(t *testing.T) {
	got, rec := through(middleware.OverrideMethod(),
		formRequest(http.MethodPost, "/posts/1", "_method=DELETE&broken=%zz"))

	if got.method != http.MethodPost {
		t.Errorf("an unparseable body served the request as %s, want POST", got.method)
	}
	if !got.ran || rec.Code != http.StatusOK {
		t.Errorf("the request was refused with %d, and refusing a malformed body is not this middleware's decision", rec.Code)
	}
}

// TestAFormSubmissionIsCheckedBeforeItsMethodIsOverridden exercises the public
// seam from markup to handler. The field names are read from FormBuilder's HTML
// instead of repeated in the request body, so a writer and reader that drift
// apart cannot keep this test green by each agreeing only with itself.
func TestAFormSubmissionIsCheckedBeforeItsMethodIsOverridden(t *testing.T) {
	const sessionID = "session-1"
	csrf := session.NewCSRF([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	token, err := csrf.Issue(sessionID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			form := html.NewFormBuilder(html.NewHtmlBuilder(formURLs{}), formURLs{}, token)
			markup, err := form.Open(html.OpenOptions{Method: method, URL: []string{"/posts/1"}})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			body := hiddenFields(t, string(markup)).Encode()
			r := formRequest(http.MethodPost, "/posts/1", body)
			stack := func(handler http.Handler) http.Handler {
				return checkCSRF(csrf, sessionID)(middleware.OverrideMethod()(handler))
			}
			got, rec := through(stack, r)

			if rec.Code != http.StatusOK || !got.ran {
				t.Fatalf("status = %d, want the checked form to reach the handler", rec.Code)
			}
			if got.method != method {
				t.Fatalf("handler method = %s, want %s", got.method, method)
			}
		})
	}
}

var hiddenInput = regexp.MustCompile(`<input name="([^"]+)" type="hidden" value="([^"]+)">`)

func hiddenFields(t *testing.T, markup string) url.Values {
	t.Helper()

	fields := url.Values{}
	for _, match := range hiddenInput.FindAllStringSubmatch(markup, -1) {
		fields.Add(match[1], match[2])
	}
	if len(fields) != 2 {
		t.Fatalf("hidden fields in %q = %v, want method and token", markup, fields)
	}
	return fields
}

// checkCSRF is the component-level half of the Framework's CSRFProtect: it
// reads the form field and validates it against the public session issuer. The
// Framework wrapper additionally performs the browser-origin check and renders
// its refusal response; importing it here would reverse the module dependency.
func checkCSRF(csrf *session.CSRF, sessionID string) hhttp.Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := csrf.Validate(sessionID, r.PostFormValue("_token")); err != nil {
				w.WriteHeader(419)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

// formURLs supplies only the URL behavior FormBuilder.Open observes in this
// test. The remaining methods complete the public UrlGenerator contract.
type formURLs struct{}

func (formURLs) To(path string, _ ...string) string                { return path }
func (formURLs) Secure(path string, _ ...string) string            { return path }
func (formURLs) Asset(path string) string                          { return path }
func (formURLs) SecureAsset(path string) string                    { return path }
func (formURLs) Route(name string, _ ...string) (string, error)    { return name, nil }
func (formURLs) Action(action string, _ ...string) (string, error) { return action, nil }
func (formURLs) Current() string                                   { return "/" }
