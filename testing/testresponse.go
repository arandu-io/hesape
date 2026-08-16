package testing

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/session"
	sessionmiddleware "github.com/arandu-io/hesape/session/middleware"
	"github.com/arandu-io/hesape/support"
	"github.com/arandu-io/hesape/testing/constraints"
)

// TestResponse is what came back from a request, with the assertions worth
// making about it.
//
// Every assertion returns the response.
//
//	response.AssertOK().AssertSee("Invoices").AssertDontSee("Draft")
type TestResponse struct {
	t T

	// BaseRequest is the request that produced this response.
	BaseRequest *http.Request

	// BaseResponse is the response itself.
	BaseResponse *http.Response

	// Exceptions is what the request logged on its way through. The last one is
	// appended to a failure message by messageWithContext.
	Exceptions LoggedExceptionCollection

	// Original is what the handler returned before it became bytes -- a
	// *view.View, for the view assertions. It is recorded rather than inferred,
	// because the bytes no longer say what produced them.
	Original any

	// URL is the generator the location assertions resolve against.
	//
	// AssertLocation, AssertRedirectBack, AssertRedirectToRoute and
	// AssertRedirectToSignedRoute need it, and say so when it is missing rather
	// than guessing. Nothing else here touches it.
	URL *routing.UrlGenerator

	// Encrypter is what [TestResponse.AssertCookie] decrypts with. A test
	// asserting about an unencrypted cookie does not need it: that is
	// [TestResponse.AssertPlainCookie].
	Encrypter *encryption.Encrypter

	// Flash is what reads the validation errors and the old input back off the
	// response.
	//
	// They travel in a signed one-shot cookie rather than in the session -- see
	// session.Flash, and the reason it gives: the three forms that need them most
	// are submitted by somebody who has no session at all.
	//
	// A test that asserts about errors or old input sets it. AssertSessionHas
	// and AssertSessionMissing do not need it: those read the session store,
	// which is a general key/value store in both languages.
	Flash *session.Flash

	// Streamed records that the handler streamed the response.
	//
	// Every response arrives as an *http.Response, so what the handler did is
	// recorded rather than inferred -- the same reason Original is a field. A
	// response that arrived chunked is streamed whether or not anybody set this,
	// which AssertStreamed also reads.
	Streamed bool

	// content is the body, read once. An http.Response body is a stream that
	// can be read to the end exactly once, and an assertion that consumed it
	// would make the next one see an empty page.
	content []byte
}

// NewTestResponse wraps a response and the request that produced it.
//
// The body is read here and closed, because it is a stream that can be read
// once and every assertion below reads it.
//
// The request is what the session, the URL resolution and the view are read
// through, so an assertion that needs one says so rather than reaching for an
// ambient request.
func NewTestResponse(t T, response *http.Response, request *http.Request) *TestResponse {
	r := &TestResponse{t: t, BaseResponse: response, BaseRequest: request}

	if response != nil && response.Body != nil {
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err == nil {
			r.content = body
		}
	}

	return r
}

// FromBaseResponse is [NewTestResponse] under the name that reads better at a
// call site building one from a response already in hand.
func FromBaseResponse(t T, response *http.Response, request *http.Request) *TestResponse {
	return NewTestResponse(t, response, request)
}

// WithExceptions sets what the request logged, which a failure message then
// carries. It returns the response.
func (r *TestResponse) WithExceptions(exceptions LoggedExceptionCollection) *TestResponse {
	r.Exceptions = exceptions
	return r
}

// GetStatusCode returns the status, or 0 when there is no response.
func (r *TestResponse) GetStatusCode() int {
	if r.BaseResponse == nil {
		return 0
	}
	return r.BaseResponse.StatusCode
}

// Headers returns the response headers, or an empty set when there is no
// response.
func (r *TestResponse) Headers() http.Header {
	if r.BaseResponse == nil {
		return http.Header{}
	}
	return r.BaseResponse.Header
}

// GetContent returns the body, which was read once when the response was
// wrapped.
func (r *TestResponse) GetContent() string { return string(r.content) }

// Content is [TestResponse.GetContent] under a shorter name.
func (r *TestResponse) Content() string { return r.GetContent() }

// isSuccessful reports whether the status is in the 2xx range.
func (r *TestResponse) isSuccessful() bool {
	code := r.GetStatusCode()
	return code >= 200 && code < 300
}

// isServerError reports whether the status is in the 5xx range.
func (r *TestResponse) isServerError() bool {
	code := r.GetStatusCode()
	return code >= 500 && code < 600
}

// isRedirect reports whether the status is one of the redirect codes the
// redirect assertions print: 201, 301, 302, 303, 307 and 308.
func (r *TestResponse) isRedirect() bool {
	switch r.GetStatusCode() {
	case http.StatusCreated,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	}
	return false
}

// AssertSuccessful asserts a status in the 2xx range.
func (r *TestResponse) AssertSuccessful() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isSuccessful(),
		r.messageWithContext(r.statusMessageWithDetails(">=200, <300", r.GetStatusCode())))
	return r
}

// AssertSuccessfulPrecognition asserts an empty 204 carrying a
// Precognition-Success header set to "true".
func (r *TestResponse) AssertSuccessfulPrecognition() *TestResponse {
	r.t.Helper()

	r.AssertNoContent()

	assertTrue(r.t, r.hasHeader("Precognition-Success"),
		"Header [Precognition-Success] not present on response.")
	assertSame(r.t, "true", r.Headers().Get("Precognition-Success"),
		"The Precognition-Success header was found, but the value is not `true`.")
	return r
}

// AssertServerError asserts a status in the 5xx range.
func (r *TestResponse) AssertServerError() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isServerError(),
		r.messageWithContext(r.statusMessageWithDetails(">=500, < 600", r.GetStatusCode())))
	return r
}

// AssertStatus asserts the status is the one expected.
func (r *TestResponse) AssertStatus(status int) *TestResponse {
	r.t.Helper()

	actual := r.GetStatusCode()
	assertSame(r.t, status, actual, r.messageWithContext(r.statusMessageWithDetails(status, actual)))
	return r
}

// statusMessageWithDetails is the message the status assertions share, naming
// what was expected and what arrived.
func (r *TestResponse) statusMessageWithDetails(expected any, actual int) string {
	return fmt.Sprintf("Expected response status code [%v] but received %d.", expected, actual)
}

// AssertRedirect asserts a redirect status, and the location too when one is
// given. At most one location may be given.
func (r *TestResponse) AssertRedirect(uri ...string) *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	if len(uri) > 0 {
		r.AssertLocation(uri[0])
	}
	return r
}

// AssertRedirectContains asserts a redirect status whose location contains the
// given text.
func (r *TestResponse) AssertRedirectContains(uri string) *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	location := r.Headers().Get("Location")
	assertTrue(r.t, strings.Contains(location, uri),
		fmt.Sprintf("Redirect location [%s] does not contain [%s].", location, uri))
	return r
}

// AssertRedirectBack asserts a redirect to the previous location. It needs
// [TestResponse.URL] to know where back is, and says so when it is missing.
func (r *TestResponse) AssertRedirectBack() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	if r.URL == nil {
		fail(r.t, "AssertRedirectBack needs the URL generator to know where back is; "+
			"set TestResponse.URL, which is what app('url') resolves to in the PHP.")
		return r
	}

	return r.AssertLocation(r.URL.Previous(""))
}

// AssertRedirectToRoute asserts a redirect to the named route, built with the
// parameters given. It needs [TestResponse.URL] to turn the name into a URL.
func (r *TestResponse) AssertRedirectToRoute(name string, parameters ...map[string]any) *TestResponse {
	r.t.Helper()

	if r.URL == nil {
		fail(r.t, "AssertRedirectToRoute needs the URL generator to turn a route name into a URL; "+
			"set TestResponse.URL, which is what route() reaches through the container in the PHP.")
		return r
	}

	uri, err := r.URL.Route(name, firstMap(parameters), true)
	if err != nil {
		fail(r.t, "Route [%s] could not be built: %v", name, err)
		return r
	}

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	return r.AssertLocation(uri)
}

// AssertRedirectToSignedRoute asserts a redirect to a route whose signature is
// valid, and to the named route too when a name is given. An empty name checks
// only the signature. It needs [TestResponse.URL] to check the signature.
func (r *TestResponse) AssertRedirectToSignedRoute(name string, parameters map[string]any, absolute bool) *TestResponse {
	r.t.Helper()

	if r.URL == nil {
		fail(r.t, "AssertRedirectToSignedRoute needs the URL generator to check the signature; "+
			"set TestResponse.URL, which is what app('url') resolves to in the PHP.")
		return r
	}

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	location := r.Headers().Get("Location")

	signed, err := http.NewRequest(http.MethodGet, r.to(location), nil)
	if err != nil {
		fail(r.t, "The response is not a redirect to a signed route: %v", err)
		return r
	}

	assertTrue(r.t, r.URL.HasValidSignature(signed, absolute),
		"The response is not a redirect to a signed route.")

	if name == "" {
		return r
	}

	uri, err := r.URL.Route(name, parameters, true)
	if err != nil {
		fail(r.t, "Route [%s] could not be built: %v", name, err)
		return r
	}

	assertEquals(r.t, r.to(uri), withoutSignature(signed.URL), "")
	return r
}

// withoutSignature returns the location with the two query parameters the
// signature added -- signature and expires -- taken back off, so it can be
// compared against the route as it was built.
func withoutSignature(u *url.URL) string {
	if u == nil {
		return ""
	}

	clean := *u
	query := clean.Query()
	query.Del("signature")
	query.Del("expires")
	clean.RawQuery = query.Encode()

	return strings.TrimSuffix(clean.String(), "?")
}

// AssertHeader asserts the header is present, and holds the value too when one
// is given. At most one value may be given, and a nil one checks only presence.
func (r *TestResponse) AssertHeader(headerName string, value ...any) *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.hasHeader(headerName),
		fmt.Sprintf("Header [%s] not present on response.", headerName))

	if len(value) == 0 || value[0] == nil {
		return r
	}

	actual := r.Headers().Get(headerName)
	assertEquals(r.t, value[0], actual, fmt.Sprintf(
		"Header [%s] was found, but value [%s] does not match [%v].", headerName, actual, value[0]))
	return r
}

// AssertHeaderMissing asserts the header is not present.
func (r *TestResponse) AssertHeaderMissing(headerName string) *TestResponse {
	r.t.Helper()

	assertFalse(r.t, r.hasHeader(headerName),
		fmt.Sprintf("Unexpected header [%s] is present on response.", headerName))
	return r
}

// hasHeader reports whether the header is present, even when its value is
// empty.
func (r *TestResponse) hasHeader(name string) bool {
	_, ok := r.Headers()[http.CanonicalHeaderKey(name)]
	return ok
}

// AssertLocation asserts the Location header points at the given URI, compared
// as absolute URLs.
func (r *TestResponse) AssertLocation(uri string) *TestResponse {
	r.t.Helper()

	assertEquals(r.t, r.to(uri), r.to(r.Headers().Get("Location")), "")
	return r
}

// to renders a URI as an absolute URL, so that a path and the full URL of the
// same place compare equal.
//
// With no URL generator it resolves against the request instead, which is where
// the generator's own root comes from; with neither, the URI is compared as it
// was written.
func (r *TestResponse) to(uri string) string {
	if r.URL != nil {
		return r.URL.To(uri, nil, nil)
	}

	if r.BaseRequest == nil || r.BaseRequest.URL == nil {
		return uri
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return r.BaseRequest.URL.ResolveReference(parsed).String()
}

// AssertDownload asserts the response offers a file download, and that it
// carries the given filename when one is given.
func (r *TestResponse) AssertDownload(filename ...string) *TestResponse {
	r.t.Helper()

	disposition := strings.Split(r.Headers().Get("Content-Disposition"), ";")

	if strings.TrimSpace(disposition[0]) != "attachment" {
		fail(r.t, "Response does not offer a file download.\nDisposition [%s] found in header, [attachment] expected.",
			strings.TrimSpace(disposition[0]))
		return r
	}

	if len(filename) == 0 {
		return r
	}

	message := fmt.Sprintf("Expected file [%s] is not present in Content-Disposition header.", filename[0])

	if len(disposition) < 2 {
		fail(r.t, "%s", message)
		return r
	}

	parts := strings.SplitN(disposition[1], "=", 2)
	if strings.TrimSpace(parts[0]) != "filename" {
		fail(r.t, "Unsupported Content-Disposition header provided.\nDisposition [%s] found in header, [filename] expected.",
			strings.TrimSpace(parts[0]))
		return r
	}

	actual := ""
	if len(parts) > 1 {
		actual = strings.Trim(parts[1], " \"'")
	}

	assertSame(r.t, filename[0], actual, message)
	return r
}

// AssertPlainCookie asserts the cookie is there and holds the value,
// unencrypted.
func (r *TestResponse) AssertPlainCookie(cookieName string, value ...any) *TestResponse {
	r.t.Helper()
	return r.assertCookie(cookieName, value, false, false)
}

// AssertCookie asserts the cookie is there and, when a value is given,
// decrypts to it.
//
// Passing nothing asserts only that the cookie is there; passing a value
// asserts what it holds, decrypting first.
func (r *TestResponse) AssertCookie(cookieName string, value ...any) *TestResponse {
	r.t.Helper()
	return r.assertCookie(cookieName, value, true, false)
}

func (r *TestResponse) assertCookie(cookieName string, value []any, encrypted, unserialize bool) *TestResponse {
	r.t.Helper()

	hasValue := len(value) > 0 && value[0] != nil

	found := r.GetCookie(cookieName, encrypted && hasValue, unserialize)
	assertNotNull(r.t, found, fmt.Sprintf("Cookie [%s] not present on response.", cookieName))

	if found == nil || !hasValue {
		return r
	}

	assertEquals(r.t, value[0], found.Value, fmt.Sprintf(
		"Cookie [%s] was found, but value [%s] does not match [%v].", cookieName, found.Value, value[0]))
	return r
}

// AssertCookieExpired asserts the cookie is there and its expiry is in the
// past.
func (r *TestResponse) AssertCookieExpired(cookieName string) *TestResponse {
	r.t.Helper()

	found := r.GetCookie(cookieName, false, false)
	assertNotNull(r.t, found, fmt.Sprintf("Cookie [%s] not present on response.", cookieName))
	if found == nil {
		return r
	}

	expiresAt := found.Expires
	assertTrue(r.t, !expiresAt.IsZero() && expiresAt.Before(time.Now()),
		fmt.Sprintf("Cookie [%s] is not expired, it expires at [%s].", cookieName, expiresAt))
	return r
}

// AssertCookieNotExpired asserts the cookie is there and its expiry is unset
// or in the future.
func (r *TestResponse) AssertCookieNotExpired(cookieName string) *TestResponse {
	r.t.Helper()

	found := r.GetCookie(cookieName, false, false)
	assertNotNull(r.t, found, fmt.Sprintf("Cookie [%s] not present on response.", cookieName))
	if found == nil {
		return r
	}

	expiresAt := found.Expires
	assertTrue(r.t, expiresAt.IsZero() || expiresAt.After(time.Now()),
		fmt.Sprintf("Cookie [%s] is expired, it expired at [%s].", cookieName, expiresAt))
	return r
}

// AssertCookieMissing asserts the cookie is not on the response.
func (r *TestResponse) AssertCookieMissing(cookieName string) *TestResponse {
	r.t.Helper()

	assertNull(r.t, r.GetCookie(cookieName, false, false),
		fmt.Sprintf("Cookie [%s] is present on response.", cookieName))
	return r
}

// GetCookie returns the named cookie, or nil when it is not on the response.
//
// decrypt needs the Encrypter. Without one the test is told to set it rather
// than handed the ciphertext, because an assertion comparing a value against
// ciphertext fails for a response that is right.
func (r *TestResponse) GetCookie(cookieName string, decrypt, unserialize bool) *http.Cookie {
	if r.BaseResponse == nil {
		return nil
	}

	for _, c := range r.BaseResponse.Cookies() {
		if c.Name != cookieName {
			continue
		}
		if !decrypt {
			return c
		}

		if r.Encrypter == nil {
			r.t.Helper()
			fail(r.t, "Cookie [%s] was asked for decrypted and no encrypter was set; "+
				"set TestResponse.Encrypter, which is what app('encrypter') resolves to in the PHP, "+
				"or assert about it with AssertPlainCookie.", cookieName)
			return nil
		}

		plain, err := r.Encrypter.DecryptString(c.Value)
		if err != nil {
			r.t.Helper()
			fail(r.t, "Cookie [%s] could not be decrypted: %v", cookieName, err)
			return nil
		}

		decrypted := *c
		decrypted.Value = cookie.CookieValuePrefix.Remove(plain)
		return &decrypted
	}

	return nil
}

// AssertContent asserts the body is exactly this.
func (r *TestResponse) AssertContent(value string) *TestResponse {
	r.t.Helper()

	assertSame(r.t, value, r.GetContent(), "")
	return r
}

// AssertSee asserts the text is on the page. The text is HTML-escaped first
// unless escape is given as false.
func (r *TestResponse) AssertSee(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	for _, wanted := range escapeAll(wrapStrings(value), escape) {
		assertStringContainsString(r.t, wanted, r.GetContent(), "")
	}
	return r
}

// AssertSeeHTML is [TestResponse.AssertSee] without the escaping.
func (r *TestResponse) AssertSeeHTML(value any) *TestResponse {
	r.t.Helper()
	return r.AssertSee(value, false)
}

// AssertSeeInOrder asserts the strings are on the page, each after the last.
//
// It is the assertion three AssertSee calls are not: they pass on a page that
// shows the three in any order, and "the total is under the line items" is an
// order.
func (r *TestResponse) AssertSeeInOrder(values []string, escape ...bool) *TestResponse {
	r.t.Helper()

	assertThat(r.t, escapeAll(values, escape), constraints.NewSeeInOrder(r.GetContent()), "")
	return r
}

// AssertSeeHTMLInOrder is [TestResponse.AssertSeeInOrder] without the
// escaping.
func (r *TestResponse) AssertSeeHTMLInOrder(values []string) *TestResponse {
	r.t.Helper()
	return r.AssertSeeInOrder(values, false)
}

// AssertSeeText asserts the text is on the page once the tags are taken off.
//
// It is the one to reach for when the words are broken up by markup: "Hello
// <b>Alice</b>" contains "Hello Alice" only after the tags are gone.
func (r *TestResponse) AssertSeeText(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	content := stripTags(r.GetContent())
	for _, wanted := range escapeAll(wrapStrings(value), escape) {
		assertStringContainsString(r.t, wanted, content, "")
	}
	return r
}

// AssertSeeTextInOrder asserts the strings are on the page once the tags are
// taken off, each after the last.
func (r *TestResponse) AssertSeeTextInOrder(values []string, escape ...bool) *TestResponse {
	r.t.Helper()

	assertThat(r.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(r.GetContent())), "")
	return r
}

// AssertDontSee asserts the text is not on the page.
//
// It is the half people skip and the half that catches a leak: a draft in a
// public listing, an address on a page that should not name one, a button
// somebody without the permission can see.
func (r *TestResponse) AssertDontSee(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	for _, unwanted := range escapeAll(wrapStrings(value), escape) {
		assertStringNotContainsString(r.t, unwanted, r.GetContent(), "")
	}
	return r
}

// AssertDontSeeHTML is [TestResponse.AssertDontSee] without the escaping.
func (r *TestResponse) AssertDontSeeHTML(value any) *TestResponse {
	r.t.Helper()
	return r.AssertDontSee(value, false)
}

// AssertDontSeeText asserts the text is not on the page once the tags are
// taken off.
func (r *TestResponse) AssertDontSeeText(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	content := stripTags(r.GetContent())
	for _, unwanted := range escapeAll(wrapStrings(value), escape) {
		assertStringNotContainsString(r.t, unwanted, content, "")
	}
	return r
}

// Dump logs the body, decoded and indented when it is JSON. A key logs only
// that part of the payload.
func (r *TestResponse) Dump(key ...string) *TestResponse {
	r.t.Helper()

	content := r.GetContent()

	decoded := NewAssertableJSONString(r.t, content).Decoded()
	if decoded == nil {
		r.t.Logf("%s", content)
		return r
	}

	if len(key) > 0 && key[0] != "" {
		r.t.Logf("%s", mustEncodePretty(dataGet(decoded, key[0])))
		return r
	}
	r.t.Logf("%s", mustEncodePretty(decoded))
	return r
}

// DumpHeaders logs the response headers.
func (r *TestResponse) DumpHeaders() *TestResponse {
	r.t.Helper()

	r.t.Logf("%s", mustEncodePretty(r.Headers()))
	return r
}

// DumpSession logs the session, or only the given keys when there are any. It
// logs nothing when the response carries no session.
func (r *TestResponse) DumpSession(keys ...string) *TestResponse {
	r.t.Helper()

	store := r.session()
	if store == nil {
		return r
	}

	if len(keys) == 0 {
		r.t.Logf("%s", mustEncodePretty(store.All()))
		return r
	}
	r.t.Logf("%s", mustEncodePretty(store.Only(keys)))
	return r
}

// DDHeaders logs the response headers and stops the test.
//
// Ending the process would take every other test with it, so this ends the
// test instead: the dump is printed and the test stops where the call is.
func (r *TestResponse) DDHeaders() *TestResponse {
	r.t.Helper()

	r.DumpHeaders()
	fail(r.t, "DDHeaders")
	return r
}

// DDBody logs the body and stops the test.
func (r *TestResponse) DDBody(key ...string) *TestResponse {
	r.t.Helper()

	r.Dump(key...)
	fail(r.t, "DDBody")
	return r
}

// DDJSON logs the decoded payload and stops the test.
func (r *TestResponse) DDJSON(key ...string) *TestResponse {
	r.t.Helper()

	r.t.Logf("%s", mustEncodePretty(r.JSON(key...)))
	fail(r.t, "DDJSON")
	return r
}

// DDSession logs the session and stops the test.
func (r *TestResponse) DDSession(keys ...string) *TestResponse {
	r.t.Helper()

	r.DumpSession(keys...)
	fail(r.t, "DDSession")
	return r
}

// session returns the session store the request carried, or nil.
//
// The session travels on the request's context, which is where the middleware
// that started it put it and where every handler reads it. A response built
// without a request, or from one that never went through the session
// middleware, has none -- and the session assertions say so rather than
// passing over an empty store, which would be an assertion that proves
// nothing.
func (r *TestResponse) session() *session.Store {
	if r.BaseRequest == nil {
		return nil
	}
	store, _ := sessionmiddleware.Session(r.BaseRequest.Context())
	return store
}

// requireSession is the check every session assertion opens with.
func (r *TestResponse) requireSession() *session.Store {
	r.t.Helper()

	store := r.session()
	if store == nil {
		fail(r.t, "The response carries no session: the request it was built from had none on its context. "+
			"app('session.store') is what the PHP reaches for here, and the session middleware is what puts one there.")
	}
	return store
}

// messageWithContext returns the failure message with the last exception the
// request logged appended to it.
//
// The context is added on the way in rather than caught on the way out. The
// errors flashed to the session and the ones in the JSON body are appended by
// the assertions that read them, which is where they are already decoded.
func (r *TestResponse) messageWithContext(message string) string {
	if len(r.Exceptions) == 0 {
		return message
	}

	last := r.Exceptions[len(r.Exceptions)-1]
	if last == nil {
		return message
	}

	return fmt.Sprintf("%s\n\nThe following exception occurred during the last request:\n\n%v\n", message, last)
}

// wrapStrings normalises what the see assertions take -- a string, a list of
// them, or anything printable -- into a list of strings.
func wrapStrings(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(v)}
	}
}

// escapeAll HTML-escapes every value unless escape is given as false, which is
// the choice the see assertions pass through.
func escapeAll(values []string, escape []bool) []string {
	if len(escape) > 0 && !escape[0] {
		return values
	}

	out := make([]string, len(values))
	for i, value := range values {
		out[i] = support.E(value)
	}
	return out
}

// stripTags returns the text of a page with the markup taken off.
//
// It is what makes [TestResponse.AssertSeeText] find "Hello Alice" in
// "Hello <b>Alice</b>". An unclosed tag swallows the rest of the input.
func stripTags(html string) string {
	var out strings.Builder
	out.Grow(len(html))

	for i := 0; i < len(html); {
		if html[i] != '<' {
			out.WriteByte(html[i])
			i++
			continue
		}

		// A comment runs to its own terminator, which may hold a bare '>'.
		if strings.HasPrefix(html[i:], "<!--") {
			end := strings.Index(html[i+4:], "-->")
			if end < 0 {
				return out.String()
			}
			i += 4 + end + 3
			continue
		}

		end := strings.IndexByte(html[i:], '>')
		if end < 0 {
			// An unclosed tag swallows the rest.
			return out.String()
		}
		i += end + 1
	}

	return out.String()
}

// firstMap returns the single optional map, or an empty one.
func firstMap(values []map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	return values[0]
}
