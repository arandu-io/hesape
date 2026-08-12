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

// TestResponse answers to Illuminate\Testing\TestResponse: what came back from
// a request, with the assertions worth making about it.
//
// It is the largest and most used class in Illuminate\Testing, and it is
// written across three files here: this one for the status, the headers, the
// cookies and the body; testresponse_json.go for the JSON payload; and
// testresponse_session.go for the session, the view and the validation errors.
//
// Every assertion returns the response, so they chain the way the PHP's do:
//
//	response.AssertOK().AssertSee("Invoices").AssertDontSee("Draft")
type TestResponse struct {
	t T

	// BaseRequest answers to the public $baseRequest.
	BaseRequest *http.Request

	// BaseResponse answers to the public $baseResponse.
	BaseResponse *http.Response

	// Exceptions answers to the public $exceptions: what the request logged on
	// its way through. TestResponseAssert reads the last one to put it in the
	// failure message, which is done by messageWithContext here.
	Exceptions LoggedExceptionCollection

	// Original answers to $response->original, which the PHP reaches through
	// __get. It is what the handler returned before it became bytes -- a
	// *view.View, for the assertView* family -- and it is a field because Go
	// has no __get and no response object to proxy to yet.
	Original any

	// URL is what app('url') resolves to in the PHP.
	//
	// assertLocation, assertRedirectBack, assertRedirectToRoute and
	// assertRedirectToSignedRoute are written against the URL generator, and
	// the PHP reaches it through the container. Go has no container, so the
	// test that needs those four hands one over; the rest of the class does
	// not touch it.
	URL *routing.UrlGenerator

	// Encrypter is what app('encrypter') resolves to in the PHP, and it is what
	// assertCookie decrypts with. A test asserting about a plain cookie does
	// not need it: that is assertPlainCookie.
	Encrypter *encryption.Encrypter

	// content is the body, read once. An http.Response body is a stream that
	// can be read to the end exactly once, and an assertion that consumed it
	// would make the next one see an empty page.
	content []byte
}

// NewTestResponse answers to TestResponse::__construct.
//
// The body is read here and closed, because it is a stream that can be read
// once and every assertion below reads it.
//
// request stands for the PHP's `$request = null`. It is what the session, the
// URL resolution and the view are read through, so an assertion that needs one
// says so rather than reaching for an ambient request.
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

// FromBaseResponse answers to TestResponse::fromBaseResponse.
func FromBaseResponse(t T, response *http.Response, request *http.Request) *TestResponse {
	return NewTestResponse(t, response, request)
}

// WithExceptions answers to TestResponse::withExceptions.
func (r *TestResponse) WithExceptions(exceptions LoggedExceptionCollection) *TestResponse {
	r.Exceptions = exceptions
	return r
}

// GetStatusCode answers to Response::getStatusCode, which TestResponse reaches
// through __call.
func (r *TestResponse) GetStatusCode() int {
	if r.BaseResponse == nil {
		return 0
	}
	return r.BaseResponse.StatusCode
}

// Headers answers to $response->headers, which TestResponse reaches through
// __get.
func (r *TestResponse) Headers() http.Header {
	if r.BaseResponse == nil {
		return http.Header{}
	}
	return r.BaseResponse.Header
}

// GetContent answers to Response::getContent.
func (r *TestResponse) GetContent() string { return string(r.content) }

// Content answers to Illuminate\Http\Response::content, which is getContent
// under the name the Illuminate side of the hierarchy gives it. Both are here
// because both are in the PHP surface, and they are the same bytes.
func (r *TestResponse) Content() string { return r.GetContent() }

// isSuccessful answers to Response::isSuccessful.
func (r *TestResponse) isSuccessful() bool {
	code := r.GetStatusCode()
	return code >= 200 && code < 300
}

// isServerError answers to Response::isServerError.
func (r *TestResponse) isServerError() bool {
	code := r.GetStatusCode()
	return code >= 500 && code < 600
}

// isRedirect answers to Response::isRedirect, whose list is the one the
// redirect assertions print.
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

// AssertSuccessful answers to TestResponse::assertSuccessful.
func (r *TestResponse) AssertSuccessful() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isSuccessful(),
		r.messageWithContext(r.statusMessageWithDetails(">=200, <300", r.GetStatusCode())))
	return r
}

// AssertSuccessfulPrecognition answers to
// TestResponse::assertSuccessfulPrecognition.
func (r *TestResponse) AssertSuccessfulPrecognition() *TestResponse {
	r.t.Helper()

	r.AssertNoContent()

	assertTrue(r.t, r.hasHeader("Precognition-Success"),
		"Header [Precognition-Success] not present on response.")
	assertSame(r.t, "true", r.Headers().Get("Precognition-Success"),
		"The Precognition-Success header was found, but the value is not `true`.")
	return r
}

// AssertServerError answers to TestResponse::assertServerError.
func (r *TestResponse) AssertServerError() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isServerError(),
		r.messageWithContext(r.statusMessageWithDetails(">=500, < 600", r.GetStatusCode())))
	return r
}

// AssertStatus answers to TestResponse::assertStatus.
func (r *TestResponse) AssertStatus(status int) *TestResponse {
	r.t.Helper()

	actual := r.GetStatusCode()
	assertSame(r.t, status, actual, r.messageWithContext(r.statusMessageWithDetails(status, actual)))
	return r
}

// statusMessageWithDetails answers to
// TestResponse::statusMessageWithDetails.
func (r *TestResponse) statusMessageWithDetails(expected any, actual int) string {
	return fmt.Sprintf("Expected response status code [%v] but received %d.", expected, actual)
}

// AssertRedirect answers to TestResponse::assertRedirect.
//
// The variadic uri stands for the PHP's `$uri = null`: without one it asserts
// only that the response redirects.
func (r *TestResponse) AssertRedirect(uri ...string) *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	if len(uri) > 0 {
		r.AssertLocation(uri[0])
	}
	return r
}

// AssertRedirectContains answers to TestResponse::assertRedirectContains.
func (r *TestResponse) AssertRedirectContains(uri string) *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isRedirect(), r.messageWithContext(
		r.statusMessageWithDetails("201, 301, 302, 303, 307, 308", r.GetStatusCode())))

	location := r.Headers().Get("Location")
	assertTrue(r.t, strings.Contains(location, uri),
		fmt.Sprintf("Redirect location [%s] does not contain [%s].", location, uri))
	return r
}

// AssertRedirectBack answers to TestResponse::assertRedirectBack.
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

// AssertRedirectToRoute answers to TestResponse::assertRedirectToRoute.
//
// The variadic parameters stand for the PHP's `$parameters = []`.
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

// AssertRedirectToSignedRoute answers to
// TestResponse::assertRedirectToSignedRoute.
//
// name stands for the PHP's `$name = null`: the empty string asserts only that
// the location carries a valid signature, without saying which route it is.
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

// withoutSignature answers to the fullUrlWithQuery(['signature' => null,
// 'expires' => null]) in assertRedirectToSignedRoute: the location with the two
// parameters the signature added taken back off.
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

// AssertHeader answers to TestResponse::assertHeader.
//
// The variadic value stands for the PHP's `$value = null`: without one it
// asserts only that the header is there.
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

// AssertHeaderMissing answers to TestResponse::assertHeaderMissing.
func (r *TestResponse) AssertHeaderMissing(headerName string) *TestResponse {
	r.t.Helper()

	assertFalse(r.t, r.hasHeader(headerName),
		fmt.Sprintf("Unexpected header [%s] is present on response.", headerName))
	return r
}

// hasHeader answers to HeaderBag::has.
func (r *TestResponse) hasHeader(name string) bool {
	_, ok := r.Headers()[http.CanonicalHeaderKey(name)]
	return ok
}

// AssertLocation answers to TestResponse::assertLocation.
func (r *TestResponse) AssertLocation(uri string) *TestResponse {
	r.t.Helper()

	assertEquals(r.t, r.to(uri), r.to(r.Headers().Get("Location")), "")
	return r
}

// to answers to app('url')->to($uri): the URI as an absolute URL, so that a
// path and the full URL of the same place compare equal.
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

// AssertDownload answers to TestResponse::assertDownload.
//
// The variadic filename stands for the PHP's `$filename = null`: without one it
// asserts only that a download was offered.
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

// AssertPlainCookie answers to TestResponse::assertPlainCookie: the cookie is
// there and holds the value, unencrypted.
//
// The variadic value stands for the PHP's `$value = null`.
func (r *TestResponse) AssertPlainCookie(cookieName string, value ...any) *TestResponse {
	r.t.Helper()
	return r.assertCookie(cookieName, value, false, false)
}

// AssertCookie answers to TestResponse::assertCookie: the cookie is there and,
// when a value is given, decrypts to it.
//
// The variadic value stands for the PHP's `$value = null, $encrypted = true,
// $unserialize = false`. Passing nothing asserts only that the cookie is there;
// passing a value asserts what it holds, decrypting first.
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

// AssertCookieExpired answers to TestResponse::assertCookieExpired.
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

// AssertCookieNotExpired answers to TestResponse::assertCookieNotExpired.
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

// AssertCookieMissing answers to TestResponse::assertCookieMissing.
func (r *TestResponse) AssertCookieMissing(cookieName string) *TestResponse {
	r.t.Helper()

	assertNull(r.t, r.GetCookie(cookieName, false, false),
		fmt.Sprintf("Cookie [%s] is present on response.", cookieName))
	return r
}

// GetCookie answers to TestResponse::getCookie.
//
// decrypt needs the Encrypter, which is what app('encrypter') resolves to in
// the PHP. Without one the test is told to set it rather than handed the
// ciphertext, because an assertion comparing a value against ciphertext fails
// for a response that is right.
//
// unserialize is the PHP's second decrypt argument. PHP serialisation is not a
// format this collection writes, so it is accepted and ignored: a cookie here
// carries a string.
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

// AssertContent answers to TestResponse::assertContent: the body is exactly
// this.
func (r *TestResponse) AssertContent(value string) *TestResponse {
	r.t.Helper()

	assertSame(r.t, value, r.GetContent(), "")
	return r
}

// AssertSee answers to TestResponse::assertSee: the text is on the page.
//
// value stands for the PHP's `string|array`: one string, or a []string. escape
// stands for `$escape = true`, which is what makes AssertSee("Tom & Jerry")
// match a page that wrote it as "Tom &amp; Jerry".
func (r *TestResponse) AssertSee(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	for _, wanted := range escapeAll(wrapStrings(value), escape) {
		assertStringContainsString(r.t, wanted, r.GetContent(), "")
	}
	return r
}

// AssertSeeHTML answers to TestResponse::assertSeeHtml: assertSee without the
// escaping.
func (r *TestResponse) AssertSeeHTML(value any) *TestResponse {
	r.t.Helper()
	return r.AssertSee(value, false)
}

// AssertSeeInOrder answers to TestResponse::assertSeeInOrder: the strings are
// on the page, each after the last.
//
// It is the assertion three AssertSee calls are not: they pass on a page that
// shows the three in any order, and "the total is under the line items" is an
// order.
func (r *TestResponse) AssertSeeInOrder(values []string, escape ...bool) *TestResponse {
	r.t.Helper()

	assertThat(r.t, escapeAll(values, escape), constraints.NewSeeInOrder(r.GetContent()), "")
	return r
}

// AssertSeeHTMLInOrder answers to TestResponse::assertSeeHtmlInOrder.
func (r *TestResponse) AssertSeeHTMLInOrder(values []string) *TestResponse {
	r.t.Helper()
	return r.AssertSeeInOrder(values, false)
}

// AssertSeeText answers to TestResponse::assertSeeText: the text is on the
// page once the tags are taken off.
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

// AssertSeeTextInOrder answers to TestResponse::assertSeeTextInOrder.
func (r *TestResponse) AssertSeeTextInOrder(values []string, escape ...bool) *TestResponse {
	r.t.Helper()

	assertThat(r.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(r.GetContent())), "")
	return r
}

// AssertDontSee answers to TestResponse::assertDontSee: the text is not on the
// page.
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

// AssertDontSeeHTML answers to TestResponse::assertDontSeeHtml.
func (r *TestResponse) AssertDontSeeHTML(value any) *TestResponse {
	r.t.Helper()
	return r.AssertDontSee(value, false)
}

// AssertDontSeeText answers to TestResponse::assertDontSeeText.
func (r *TestResponse) AssertDontSeeText(value any, escape ...bool) *TestResponse {
	r.t.Helper()

	content := stripTags(r.GetContent())
	for _, unwanted := range escapeAll(wrapStrings(value), escape) {
		assertStringNotContainsString(r.t, unwanted, content, "")
	}
	return r
}

// Dump answers to TestResponse::dump: print the body, decoded when it is JSON.
//
// The PHP writes to the output; this writes to the test's log, which go test
// shows beside the test that produced it and hides when the test passes.
//
// The variadic key stands for the PHP's `$key = null`.
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

// DumpHeaders answers to TestResponse::dumpHeaders.
func (r *TestResponse) DumpHeaders() *TestResponse {
	r.t.Helper()

	r.t.Logf("%s", mustEncodePretty(r.Headers()))
	return r
}

// DumpSession answers to TestResponse::dumpSession.
//
// The variadic keys stand for the PHP's `$keys = []`: without any, the whole
// session.
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

// DDHeaders answers to TestResponse::ddHeaders.
//
// The PHP dumps and calls exit(1), which ends the process. Ending the process
// under go test would take every other test with it, so this ends the test:
// the dump is printed and the test stops where the call is, which is what
// exit(1) does for the one test PHPUnit is running.
func (r *TestResponse) DDHeaders() *TestResponse {
	r.t.Helper()

	r.DumpHeaders()
	fail(r.t, "DDHeaders")
	return r
}

// DDBody answers to TestResponse::ddBody.
func (r *TestResponse) DDBody(key ...string) *TestResponse {
	r.t.Helper()

	r.Dump(key...)
	fail(r.t, "DDBody")
	return r
}

// DDJSON answers to TestResponse::ddJson.
func (r *TestResponse) DDJSON(key ...string) *TestResponse {
	r.t.Helper()

	r.t.Logf("%s", mustEncodePretty(r.JSON(key...)))
	fail(r.t, "DDJSON")
	return r
}

// DDSession answers to TestResponse::ddSession.
func (r *TestResponse) DDSession(keys ...string) *TestResponse {
	r.t.Helper()

	r.DumpSession(keys...)
	fail(r.t, "DDSession")
	return r
}

// session answers to TestResponse::session.
//
// The PHP resolves app('session.store') and starts it. Go has no container: the
// session travels on the request's context, which is where the middleware that
// started it put it and where every handler reads it. A response built without
// a request, or from one that never went through the session middleware, has
// none -- and the session assertions say so rather than passing over an empty
// store, which would be an assertion that proves nothing.
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

// messageWithContext answers to
// TestResponseAssert::injectResponseContext: the failure, with what the request
// logged appended to it.
//
// The PHP catches the failed expectation and rewrites its message. Go has no
// exception to catch, so the context is added on the way in instead. The
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

// wrapStrings answers to Arr::wrap over the `string|array $value` the see
// assertions take.
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

// escapeAll answers to `$escape ? array_map(e(...), $value) : $value`. The
// variadic stands for the PHP's `$escape = true`.
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

// stripTags answers to strip_tags: the text of a page, with the markup taken
// off.
//
// It is what makes assertSeeText find "Hello Alice" in "Hello <b>Alice</b>",
// and PHP has it as a builtin where Go does not.
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
			// An unclosed tag swallows the rest, which is what strip_tags does.
			return out.String()
		}
		i += end + 1
	}

	return out.String()
}

// firstMap answers to a PHP parameter whose default is an empty array.
//
// The value type is any and not string because the PHP passes route parameters
// straight to route(), which accepts anything a URL segment can be written
// from -- an int id, a record with a route key, a string slug.
func firstMap(values []map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	return values[0]
}
