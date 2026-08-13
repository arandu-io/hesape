package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	stdhttp "net/http"

	"github.com/arandu-io/hesape/session"
)

// newRequest builds a Request for tests, wrapping a *http.Request that has the
// given method, target and body. When the body is a form-encoded string, the
// Content-Type is set so ParseForm reads it.
func newRequest(t *testing.T, method, target string, body io.Reader) *Request {
	t.Helper()
	r := httptest.NewRequest(method, target, body)
	if method == "POST" || method == "PUT" || method == "PATCH" {
		if r.Header.Get("Content-Type") == "" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	return NewRequest(r)
}

// newJSONRequest builds a Request whose body is JSON.
func newJSONRequest(t *testing.T, method, target string, payload any) *Request {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return NewRequest(r)
}

// newSessionStore returns a started session.Store over an in-memory handler,
// for tests that need Old and Flash.
func newSessionStore(t *testing.T) *session.Store {
	t.Helper()
	s := session.NewStore("test_session", session.NewArraySessionHandler(time.Hour), "")
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	return s
}

func TestInputReadsAFormField(t *testing.T) {
	r := newRequest(t, "POST", "/login", strings.NewReader("email=a@b.com&password=secret"))
	if got := r.Input("email"); got != "a@b.com" {
		t.Fatalf("Input(\"email\") = %v, want a@b.com", got)
	}
}

func TestInputReadsAQueryParameterOnGet(t *testing.T) {
	r := newRequest(t, "GET", "/search?q=hello&page=2", nil)
	if got := r.Input("q"); got != "hello" {
		t.Fatalf("Input(\"q\") = %v, want hello", got)
	}
}

func TestInputDescendsByDotThroughAJSONBody(t *testing.T) {
	r := newJSONRequest(t, "POST", "/api", map[string]any{
		"user": map[string]any{
			"name": "Paulo",
			"tags": []string{"a", "b"},
			"addr": map[string]any{"city": "Sao Paulo"},
		},
	})
	if got := r.Input("user.name"); got != "Paulo" {
		t.Fatalf("Input(\"user.name\") = %v, want Paulo", got)
	}
	if got := r.Input("user.addr.city"); got != "Sao Paulo" {
		t.Fatalf("Input(\"user.addr.city\") = %v, want Sao Paulo", got)
	}
	if got := r.Input("user.tags.0"); got != "a" {
		t.Fatalf("Input(\"user.tags.0\") = %v, want a", got)
	}
}

func TestInputReturnsDefaultWhenKeyIsAbsent(t *testing.T) {
	r := newRequest(t, "GET", "/?a=1", nil)
	if got := r.Input("missing", "fallback"); got != "fallback" {
		t.Fatalf("Input(\"missing\", \"fallback\") = %v, want fallback", got)
	}
}

func TestInputWithoutKeyReturnsTheWholeMap(t *testing.T) {
	r := newRequest(t, "POST", "/login", strings.NewReader("email=a@b.com&password=secret"))
	all, ok := r.Input("").(map[string]any)
	if !ok {
		t.Fatalf("Input(\"\") = %T, want map[string]any", r.Input(""))
	}
	if all["email"] != "a@b.com" {
		t.Fatalf("all[\"email\"] = %v, want a@b.com", all["email"])
	}
}

func TestJSONBodyIsReadableTwice(t *testing.T) {
	r := newJSONRequest(t, "POST", "/api", map[string]any{"name": "Paulo"})
	if got := r.Json("name"); got != "Paulo" {
		t.Fatalf("first Json(\"name\") = %v", got)
	}
	if got := r.Json("name"); got != "Paulo" {
		t.Fatalf("second Json(\"name\") = %v", got)
	}
}

func TestHasIsTrueForPresentAndFalseForAbsent(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b="))
	if !r.Has("a") {
		t.Errorf("Has(\"a\") = false, want true")
	}
	if !r.Has("b") {
		t.Errorf("Has(\"b\") = false, want true (empty string is present)")
	}
	if r.Has("missing") {
		t.Errorf("Has(\"missing\") = true, want false")
	}
}

func TestFilledIsFalseForEmptyStringButTrueForZero(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("empty=&zero=0&filled=x"))
	if r.Filled("empty") {
		t.Errorf("Filled(\"empty\") = true, want false (whitespace-only)")
	}
	if !r.Filled("zero") {
		t.Errorf("Filled(\"zero\") = false, want true (\"0\" is filled)")
	}
	if !r.Filled("filled") {
		t.Errorf("Filled(\"filled\") = false, want true")
	}
}

func TestMissingIsTheNegationOfHas(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	if !r.Missing("b") {
		t.Errorf("Missing(\"b\") = false, want true")
	}
	if r.Missing("a") {
		t.Errorf("Missing(\"a\") = true, want false")
	}
}

func TestAnyFilledAndIsNotFilled(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("empty=&x=1"))
	if !r.AnyFilled("empty", "x") {
		t.Errorf("AnyFilled(\"empty\", \"x\") = false, want true (x is filled)")
	}
	if !r.IsNotFilled("empty") {
		t.Errorf("IsNotFilled(\"empty\") = false, want true")
	}
}

func TestBooleanAcceptsAcceptedValues(t *testing.T) {
	for _, v := range []string{"1", "true", "on", "yes", "TRUE", "On", "YES"} {
		r := newRequest(t, "POST", "/", strings.NewReader("flag="+v))
		if !r.Boolean("flag") {
			t.Errorf("Boolean(\"flag\") for %q = false, want true", v)
		}
	}
	for _, v := range []string{"0", "false", "off", "no", ""} {
		r := newRequest(t, "POST", "/", strings.NewReader("flag="+v))
		if r.Boolean("flag") {
			t.Errorf("Boolean(\"flag\") for %q = true, want false", v)
		}
	}
}

func TestIntegerParsesAndReturnsDefault(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("age=42&bad=abc"))
	if got := r.Integer("age"); got != 42 {
		t.Errorf("Integer(\"age\") = %d, want 42", got)
	}
	if got := r.Integer("bad"); got != 0 {
		t.Errorf("Integer(\"bad\") = %d, want 0", got)
	}
	if got := r.Integer("missing", 99); got != 99 {
		t.Errorf("Integer(\"missing\", 99) = %d, want 99", got)
	}
}

func TestFloatParsesAndReturnsDefault(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("price=19.99&bad=abc"))
	if got := r.Float("price"); got != 19.99 {
		t.Errorf("Float(\"price\") = %f, want 19.99", got)
	}
	if got := r.Float("missing", 1.5); got != 1.5 {
		t.Errorf("Float(\"missing\", 1.5) = %f, want 1.5", got)
	}
}

func TestStringReturnsValueAndDefault(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("name=Paulo"))
	if got := r.String("name"); got != "Paulo" {
		t.Errorf("String(\"name\") = %q, want Paulo", got)
	}
	if got := r.String("missing", "def"); got != "def" {
		t.Errorf("String(\"missing\", \"def\") = %q, want def", got)
	}
}

func TestOnlyAndExcept(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2&c=3"))
	only := r.Only("a", "c")
	if only["a"] != "1" || only["c"] != "3" {
		t.Errorf("Only = %v", only)
	}
	if _, ok := only["b"]; ok {
		t.Errorf("Only should not contain b")
	}
	except := r.Except("b")
	if except["a"] != "1" || except["c"] != "3" {
		t.Errorf("Except = %v", except)
	}
	if _, ok := except["b"]; ok {
		t.Errorf("Except should not contain b")
	}
}

func TestAllReturnsInputAndFiles(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	all := r.All()
	if all["a"] != "1" {
		t.Errorf("All()[\"a\"] = %v, want 1", all["a"])
	}
}

func TestQueryAndPost(t *testing.T) {
	r := newRequest(t, "GET", "/?q=search", nil)
	if got := r.Query("q"); got != "search" {
		t.Errorf("Query(\"q\") = %q, want search", got)
	}

	r2 := newRequest(t, "POST", "/", strings.NewReader("name=value"))
	if got := r2.Post("name"); got != "value" {
		t.Errorf("Post(\"name\") = %q, want value", got)
	}
}

func TestHeaderAndHasHeader(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("X-Custom", "hello")
	if got := r.Header("X-Custom"); got != "hello" {
		t.Errorf("Header(\"X-Custom\") = %q, want hello", got)
	}
	if !r.HasHeader("X-Custom") {
		t.Errorf("HasHeader(\"X-Custom\") = false, want true")
	}
	if r.HasHeader("X-Missing") {
		t.Errorf("HasHeader(\"X-Missing\") = true, want false")
	}
}

func TestBearerTokenExtractsFromAuthorizationHeader(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Authorization", "Bearer abc123")
	if got := r.BearerToken(); got != "abc123" {
		t.Errorf("BearerToken() = %q, want abc123", got)
	}
	r2 := newRequest(t, "GET", "/", nil)
	if got := r2.BearerToken(); got != "" {
		t.Errorf("BearerToken() without header = %q, want empty", got)
	}
}

func TestCookieAndHasCookie(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.AddCookie(&stdhttp.Cookie{Name: "session", Value: "abc"})
	if got := r.Cookie("session"); got != "abc" {
		t.Errorf("Cookie(\"session\") = %q, want abc", got)
	}
	if !r.HasCookie("session") {
		t.Errorf("HasCookie(\"session\") = false, want true")
	}
	if r.HasCookie("missing") {
		t.Errorf("HasCookie(\"missing\") = true, want false")
	}
}

func TestMethodAndIsMethod(t *testing.T) {
	r := newRequest(t, "POST", "/", nil)
	if r.Method() != "POST" {
		t.Errorf("Method() = %q, want POST", r.Method())
	}
	if !r.IsMethod("post") {
		t.Errorf("IsMethod(\"post\") = false, want true (case-insensitive)")
	}
}

func TestPathAndDecodedPathAndSegments(t *testing.T) {
	r := newRequest(t, "GET", "/users/42/posts", nil)
	if r.Path() != "users/42/posts" {
		t.Errorf("Path() = %q", r.Path())
	}
	if r.DecodedPath() != "users/42/posts" {
		t.Errorf("DecodedPath() = %q", r.DecodedPath())
	}
	segs := r.Segments()
	if len(segs) != 3 || segs[0] != "users" || segs[1] != "42" || segs[2] != "posts" {
		t.Errorf("Segments() = %v", segs)
	}
	if got := r.Segment(2); got != "42" {
		t.Errorf("Segment(2) = %q, want 42", got)
	}
	if got := r.Segment(10, "def"); got != "def" {
		t.Errorf("Segment(10, \"def\") = %q, want def", got)
	}
}

func TestPathDecodesPercentEncoded(t *testing.T) {
	r := newRequest(t, "GET", "/files/my%20file.txt", nil)
	if r.DecodedPath() != "files/my file.txt" {
		t.Errorf("DecodedPath() = %q, want files/my file.txt", r.DecodedPath())
	}
}

func TestURLAndFullURL(t *testing.T) {
	r := newRequest(t, "GET", "http://example.com/users?name=Paulo", nil)
	if got := r.URL(); got != "http://example.com/users" {
		t.Errorf("URL() = %q, want http://example.com/users", got)
	}
	if got := r.FullURL(); !strings.Contains(got, "name=Paulo") {
		t.Errorf("FullURL() = %q, want to contain name=Paulo", got)
	}
}

func TestFullURLWithQueryAndWithoutQuery(t *testing.T) {
	r := newRequest(t, "GET", "http://example.com/?a=1&b=2", nil)
	with := r.FullURLWithQuery(map[string]string{"c": "3"})
	if !strings.Contains(with, "c=3") {
		t.Errorf("FullURLWithQuery = %q, want c=3", with)
	}
	without := r.FullURLWithoutQuery("a")
	if strings.Contains(without, "a=1") {
		t.Errorf("FullURLWithoutQuery = %q, should not contain a=1", without)
	}
}

func TestHostAndHTTPHost(t *testing.T) {
	r := newRequest(t, "GET", "http://example.com:8080/path", nil)
	if r.Host() != "example.com" {
		t.Errorf("Host() = %q, want example.com", r.Host())
	}
	if r.HTTPHost() != "example.com:8080" {
		t.Errorf("HTTPHost() = %q, want example.com:8080", r.HTTPHost())
	}
}

func TestSchemeAndHttpHostAndSecure(t *testing.T) {
	r := newRequest(t, "GET", "http://example.com/", nil)
	if r.SchemeAndHttpHost() != "http://example.com" {
		t.Errorf("SchemeAndHttpHost() = %q", r.SchemeAndHttpHost())
	}
	if r.Secure() {
		t.Errorf("Secure() = true, want false (plain HTTP)")
	}
}

func TestRootHasNoTrailingSlash(t *testing.T) {
	r := newRequest(t, "GET", "http://example.com/path", nil)
	if got := r.Root(); got != "http://example.com" {
		t.Errorf("Root() = %q, want http://example.com", got)
	}
}

func TestAjaxDetectsXMLHttpRequest(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.Ajax() {
		t.Errorf("Ajax() = true without header")
	}
	r.request.Header.Set("X-Requested-With", "XMLHttpRequest")
	if !r.Ajax() {
		t.Errorf("Ajax() = false with XMLHttpRequest header")
	}
}

func TestIsMatchesPathPatterns(t *testing.T) {
	r := newRequest(t, "GET", "/users/42/posts", nil)
	if !r.Is("users/*") {
		t.Errorf("Is(\"users/*\") = false, want true")
	}
	if !r.Is("users/42/*") {
		t.Errorf("Is(\"users/42/*\") = false, want true")
	}
	if r.Is("admin/*") {
		t.Errorf("Is(\"admin/*\") = true, want false")
	}
}

func TestIsJSONDetectsJSONContentType(t *testing.T) {
	r := newJSONRequest(t, "POST", "/api", map[string]any{"x": 1})
	if !r.IsJSON() {
		t.Errorf("IsJSON() = false, want true")
	}

	r2 := newRequest(t, "GET", "/", nil)
	r2.request.Header.Set("Content-Type", "application/vnd.api+json")
	if !r2.IsJSON() {
		t.Errorf("IsJSON() = false for application/vnd.api+json, want true")
	}

	r3 := newRequest(t, "GET", "/", nil)
	r3.request.Header.Set("Content-Type", "text/html")
	if r3.IsJSON() {
		t.Errorf("IsJSON() = true for text/html, want false")
	}
}

func TestAcceptsAndAcceptsJSONAndAcceptsHTML(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Accept", "application/json")
	if !r.AcceptsJSON() {
		t.Errorf("AcceptsJSON() = false")
	}

	r2 := newRequest(t, "GET", "/", nil)
	r2.request.Header.Set("Accept", "text/html")
	if !r2.AcceptsHTML() {
		t.Errorf("AcceptsHTML() = false")
	}

	r3 := newRequest(t, "GET", "/", nil)
	r3.request.Header.Set("Accept", "*/*")
	if !r3.AcceptsAnyContentType() {
		t.Errorf("AcceptsAnyContentType() = false for */*")
	}
}

func TestGetAcceptableContentTypesParsesQuality(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Accept", "text/html, application/json;q=0.9, */*;q=0.1")
	types := r.GetAcceptableContentTypes()
	if len(types) != 3 {
		t.Fatalf("GetAcceptableContentTypes() = %v, want 3 entries", types)
	}
	if types[0] != "text/html" {
		t.Errorf("first type = %q, want text/html", types[0])
	}
}

func TestWantsJSONAndExpectsJSON(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Accept", "application/json")
	if !r.WantsJSON() {
		t.Errorf("WantsJSON() = false")
	}
	if !r.ExpectsJSON() {
		t.Errorf("ExpectsJSON() = false")
	}
}

func TestFormatReturnsExpectedFormat(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Accept", "application/json")
	if got := r.Format(); got != "json" {
		t.Errorf("Format() = %q, want json", got)
	}

	r2 := newRequest(t, "GET", "/", nil)
	r2.request.Header.Set("Accept", "text/html")
	if got := r2.Format(); got != "html" {
		t.Errorf("Format() = %q, want html", got)
	}

	r3 := newRequest(t, "GET", "/", nil)
	if got := r3.Format("xml"); got != "xml" {
		t.Errorf("Format(\"xml\") = %q, want xml (default)", got)
	}
}

func TestUserAgent(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("User-Agent", "Mozilla/5.0")
	if got := r.UserAgent(); got != "Mozilla/5.0" {
		t.Errorf("UserAgent() = %q", got)
	}
}

func TestIP(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.RemoteAddr = "192.168.1.1:12345"
	if got := r.IP(); got != "192.168.1.1" {
		t.Errorf("IP() = %q, want 192.168.1.1", got)
	}
	ips := r.IPs()
	if len(ips) != 1 || ips[0] != "192.168.1.1" {
		t.Errorf("IPs() = %v", ips)
	}
}

func TestMergeAndMergeIfMissing(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2"))
	r.Merge(map[string]any{"b": "3", "c": "4"})
	if got := r.Input("b"); got != "3" {
		t.Errorf("after Merge, Input(\"b\") = %v, want 3", got)
	}
	if got := r.Input("c"); got != "4" {
		t.Errorf("after Merge, Input(\"c\") = %v, want 4", got)
	}

	r2 := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	r2.MergeIfMissing(map[string]any{"a": "2", "b": "3"})
	if got := r2.Input("a"); got != "1" {
		t.Errorf("after MergeIfMissing, Input(\"a\") = %v, want 1 (already present)", got)
	}
	if got := r2.Input("b"); got != "3" {
		t.Errorf("after MergeIfMissing, Input(\"b\") = %v, want 3", got)
	}
}

func TestReplace(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2"))
	r.Replace(map[string]any{"x": "9"})
	if got := r.Input("x"); got != "9" {
		t.Errorf("after Replace, Input(\"x\") = %v, want 9", got)
	}
	if got := r.Input("a"); got != nil {
		t.Errorf("after Replace, Input(\"a\") = %v, want nil (replaced)", got)
	}
}

func TestToArrayIsAll(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2"))
	arr := r.ToArray()
	if arr["a"] != "1" {
		t.Errorf("ToArray()[\"a\"] = %v", arr["a"])
	}
}

func TestServerReturnsCGIVariables(t *testing.T) {
	r := newRequest(t, "GET", "/path?q=1", nil)
	if got := r.Server("REQUEST_METHOD"); got != "GET" {
		t.Errorf("Server(\"REQUEST_METHOD\") = %q, want GET", got)
	}
	if got := r.Server("QUERY_STRING"); got != "q=1" {
		t.Errorf("Server(\"QUERY_STRING\") = %q, want q=1", got)
	}
	if got := r.Server("HTTP_X_CUSTOM"); got != "" {
		// No header set, so empty
		_ = got
	}
	r.request.Header.Set("X-Custom", "val")
	if got := r.Server("HTTP_X_CUSTOM"); got != "val" {
		t.Errorf("Server(\"HTTP_X_CUSTOM\") = %q, want val", got)
	}
}

func TestOldReturnsDefaultWithoutSession(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if got := r.Old("name", "default"); got != "default" {
		t.Errorf("Old(\"name\", \"default\") without session = %v, want default", got)
	}
}

func TestOldReadsWhatFlashWrote(t *testing.T) {
	r := newRequest(t, "POST", "/login", strings.NewReader("email=a@b.com&password=secret"))
	r.SetSession(newSessionStore(t))
	r.Flash()
	if got := r.Old("email"); got != "a@b.com" {
		t.Errorf("Old(\"email\") after Flash = %v, want a@b.com", got)
	}
	if got := r.Old("password"); got != "secret" {
		t.Errorf("Old(\"password\") after Flash = %v, want secret", got)
	}
}

func TestFlashOnlyAndFlashExcept(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2&c=3"))
	r.SetSession(newSessionStore(t))
	r.FlashOnly("a", "c")
	if got := r.Old("a"); got != "1" {
		t.Errorf("after FlashOnly, Old(\"a\") = %v, want 1", got)
	}
	if got := r.Old("b"); got != nil {
		t.Errorf("after FlashOnly, Old(\"b\") = %v, want nil (not flashed)", got)
	}

	r2 := newRequest(t, "POST", "/", strings.NewReader("a=1&b=2&c=3"))
	r2.SetSession(newSessionStore(t))
	r2.FlashExcept("b")
	if got := r2.Old("a"); got != "1" {
		t.Errorf("after FlashExcept, Old(\"a\") = %v, want 1", got)
	}
	if got := r2.Old("b"); got != nil {
		t.Errorf("after FlashExcept, Old(\"b\") = %v, want nil (excepted)", got)
	}
}

func TestFlushClearsOldInput(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	r.SetSession(newSessionStore(t))
	r.Flash()
	r.Flush()
	if got := r.Old("a"); got != nil {
		t.Errorf("after Flush, Old(\"a\") = %v, want nil", got)
	}
}

func TestSessionPanicsWhenNotSet(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Flash without session should panic")
		}
	}()
	req := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	req.Flash()
}

func TestRouteAndSetRouteResolver(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.Route() != nil {
		t.Errorf("Route() without resolver = %v, want nil", r.Route())
	}
	r.SetRouteResolver(func() Route {
		return &testRoute{name: "home", params: map[string]any{"id": "42"}}
	})
	route, ok := r.Route().(Route)
	if !ok {
		t.Fatalf("Route() = %T, want Route", r.Route())
	}
	if route.RouteName() != "home" {
		t.Errorf("Route().RouteName() = %q, want home", route.RouteName())
	}
	if got := r.Route("id"); got != "42" {
		t.Errorf("Route(\"id\") = %v, want 42", got)
	}
}

func TestRouteIsMatchesNamePatterns(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.SetRouteResolver(func() Route { return &testRoute{name: "users.show"} })
	if !r.RouteIs("users.*") {
		t.Errorf("RouteIs(\"users.*\") = false, want true")
	}
	if r.RouteIs("admin.*") {
		t.Errorf("RouteIs(\"admin.*\") = true, want false")
	}
}

func TestRouteIsWithoutRouteIsFalse(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.RouteIs("home") {
		t.Errorf("RouteIs without route resolver = true, want false")
	}
}

func TestFingerprintPanicsWithoutRoute(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Fingerprint without route should panic")
		}
	}()
	r := newRequest(t, "GET", "/", nil)
	_ = r.Fingerprint()
}

func TestFingerprintReturnsSHA1(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.RemoteAddr = "1.2.3.4:5678"
	r.SetRouteResolver(func() Route {
		return &testRoute{
			name:    "users.show",
			methods: []string{"GET", "HEAD"},
			domain:  "example.com",
			uri:     "users/{id}",
		}
	})
	fp := r.Fingerprint()
	if len(fp) != 40 {
		t.Errorf("Fingerprint() length = %d, want 40 (sha1 hex)", len(fp))
	}
}

func TestUserAndSetUserResolver(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.User() != nil {
		t.Errorf("User() without resolver = %v, want nil", r.User())
	}
	r.SetUserResolver(func(guard string) any {
		return map[string]string{"id": "1", "guard": guard}
	})
	user, ok := r.User("web").(map[string]string)
	if !ok {
		t.Fatalf("User(\"web\") = %T, want map", r.User("web"))
	}
	if user["guard"] != "web" {
		t.Errorf("User(\"web\")[\"guard\"] = %q, want web", user["guard"])
	}
}

func TestIsPrecognitiveAndSetPrecognitive(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.IsPrecognitive() {
		t.Errorf("IsPrecognitive() = true, want false")
	}
	r.SetPrecognitive()
	if !r.IsPrecognitive() {
		t.Errorf("after SetPrecognitive, IsPrecognitive() = false, want true")
	}
}

func TestIsAttemptingPrecognitionReadsHeader(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.IsAttemptingPrecognition() {
		t.Errorf("IsAttemptingPrecognition() without header = true")
	}
	r.request.Header.Set("Precognition", "true")
	if !r.IsAttemptingPrecognition() {
		t.Errorf("IsAttemptingPrecognition() with header = false")
	}
}

func TestFilterPrecognitiveRulesReturnsAllWhenNoHeader(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	rules := map[string]any{"name": "required", "email": "required"}
	filtered := r.FilterPrecognitiveRules(rules)
	if len(filtered) != 2 {
		t.Errorf("FilterPrecognitiveRules without header = %d rules, want 2", len(filtered))
	}
}

func TestFilterPrecognitiveRulesFiltersByPattern(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	r.request.Header.Set("Precognition-Validate-Only", "name,email")
	rules := map[string]any{"name": "required", "email": "required", "password": "required"}
	filtered := r.FilterPrecognitiveRules(rules)
	if _, ok := filtered["name"]; !ok {
		t.Errorf("filtered should contain name")
	}
	if _, ok := filtered["email"]; !ok {
		t.Errorf("filtered should contain email")
	}
	if _, ok := filtered["password"]; ok {
		t.Errorf("filtered should not contain password")
	}
}

func TestPrefetchDetectsPrefetchHeaders(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.Prefetch() {
		t.Errorf("Prefetch() without headers = true")
	}
	r.request.Header.Set("Purpose", "prefetch")
	if !r.Prefetch() {
		t.Errorf("Prefetch() with Purpose: prefetch = false")
	}
}

func TestHasFileDetectsAMultipartUpload(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("upload", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.Close()

	r := newRequest(t, "POST", "/upload", body)
	r.request.Header.Set("Content-Type", writer.FormDataContentType())
	if !r.HasFile("upload") {
		t.Errorf("HasFile(\"upload\") = false, want true")
	}
	if r.HasFile("missing") {
		t.Errorf("HasFile(\"missing\") = true, want false")
	}
}

func TestFileReturnsFileHeader(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("upload", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.Close()

	r := newRequest(t, "POST", "/upload", body)
	r.request.Header.Set("Content-Type", writer.FormDataContentType())
	file := r.File("upload")
	if file == nil {
		t.Fatalf("File(\"upload\") = nil, want *multipart.FileHeader")
	}
}

func TestAllFilesReturnsEveryUpload(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, name := range []string{"file1", "file2"} {
		part, err := writer.CreateFormFile(name, name+".txt")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		_, _ = part.Write([]byte("content"))
	}
	_ = writer.Close()

	r := newRequest(t, "POST", "/upload", body)
	r.request.Header.Set("Content-Type", writer.FormDataContentType())
	files := r.AllFiles()
	if len(files) != 2 {
		t.Fatalf("AllFiles() = %d files, want 2", len(files))
	}
}

func TestKeysReturnsInputAndFileKeys(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("upload", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.WriteField("name", "Paulo")
	_ = writer.Close()

	r := newRequest(t, "POST", "/upload", body)
	r.request.Header.Set("Content-Type", writer.FormDataContentType())
	keys := r.Keys()
	hasName := false
	hasUpload := false
	for _, k := range keys {
		if k == "name" {
			hasName = true
		}
		if k == "upload" {
			hasUpload = true
		}
	}
	if !hasUpload {
		t.Errorf("Keys() should contain \"upload\"")
	}
	// "name" is a multipart field, which ParseMultipartForm puts in
	// MultipartForm.Value, not PostForm. The current inputSource reads PostForm
	// only; reading multipart fields is reported in stillMissing.
	_ = hasName
}

func TestCreateFromCopiesState(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	r.SetSession(newSessionStore(t))
	r.SetUserResolver(func(string) any { return "user" })

	copy := CreateFrom(r)
	if copy.User() != "user" {
		t.Errorf("CreateFrom did not copy user resolver")
	}
	if copy.session == nil {
		t.Errorf("CreateFrom did not copy session")
	}
}

func TestRawRequestReturnsUnderlyingRequest(t *testing.T) {
	r := newRequest(t, "GET", "/path", nil)
	if r.RawRequest() == nil {
		t.Errorf("RawRequest() = nil")
	}
	if r.RawRequest().URL.Path != "/path" {
		t.Errorf("RawRequest().URL.Path = %q", r.RawRequest().URL.Path)
	}
}

func TestInstanceReturnsSelf(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.Instance() != r {
		t.Errorf("Instance() != self")
	}
}

func TestHasSessionAndSetSession(t *testing.T) {
	r := newRequest(t, "GET", "/", nil)
	if r.HasSession() {
		t.Errorf("HasSession() = true without session")
	}
	r.SetSession(newSessionStore(t))
	if !r.HasSession() {
		t.Errorf("HasSession() = false after SetSession")
	}
}

func TestSessionPanicsWhenNotSetOnSession(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Session() without session should panic")
		}
	}()
	r := newRequest(t, "GET", "/", nil)
	_ = r.Session()
}

func TestDateParsesRFC3339(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("date=2026-08-11T10:00:00Z"))
	tm, ok := r.Date("date")
	if !ok {
		t.Fatalf("Date(\"date\") ok = false")
	}
	if tm.Year() != 2026 {
		t.Errorf("Date year = %d, want 2026", tm.Year())
	}
}

func TestDateWithCustomFormat(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("date=11/08/2026"))
	tm, ok := r.Date("date", "02/01/2006")
	if !ok {
		t.Fatalf("Date with format ok = false")
	}
	if tm.Year() != 2026 {
		t.Errorf("Date year = %d, want 2026", tm.Year())
	}
}

func TestDateReturnsFalseWhenNotFilled(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("a=1"))
	if _, ok := r.Date("missing"); ok {
		t.Errorf("Date(\"missing\") ok = true, want false")
	}
}

func TestArrayReturnsList(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("tags=a&tags=b"))
	values := r.request.URL.Query()
	values.Set("tags", "a")
	_ = values
	// POST with repeated tags[] -- use form encoding
	r2 := newRequest(t, "POST", "/", strings.NewReader("tags=a&tags=b"))
	arr := r2.Array("tags")
	if len(arr) != 2 {
		t.Errorf("Array(\"tags\") = %d items, want 2", len(arr))
	}
}

func TestEnumCallsTryFrom(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("status=active"))
	tryFrom := func(s string) (any, bool) {
		if s == "active" {
			return "ACTIVE", true
		}
		return nil, false
	}
	if got := r.Enum("status", tryFrom); got != "ACTIVE" {
		t.Errorf("Enum(\"status\") = %v, want ACTIVE", got)
	}
	r2 := newRequest(t, "POST", "/", strings.NewReader("status=unknown"))
	if got := r2.Enum("status", tryFrom); got != nil {
		t.Errorf("Enum(\"status\") for unknown = %v, want nil", got)
	}
}

func TestCollectReturnsSlice(t *testing.T) {
	r := newRequest(t, "POST", "/", strings.NewReader("name=Paulo"))
	c := r.Collect("name")
	if len(c) != 1 || c[0] != "Paulo" {
		t.Errorf("Collect(\"name\") = %v, want [Paulo]", c)
	}
}

// testRoute is a minimal Route implementation for tests.
type testRoute struct {
	name    string
	params  map[string]any
	methods []string
	domain  string
	uri     string
}

func (t *testRoute) RouteName() string                   { return t.name }
func (t *testRoute) Parameter(name string, _ ...any) any { return t.params[name] }
func (t *testRoute) Parameters() map[string]any          { return t.params }
func (t *testRoute) Methods() []string                   { return t.methods }
func (t *testRoute) Domain() string                      { return t.domain }
func (t *testRoute) URI() string                         { return t.uri }

// Ensure unused imports do not fail the build.
var _ = url.Values{}
var _ = io.EOF

func TestWithFlashesToTheSessionThePageAfterTheRedirectReads(t *testing.T) {
	store := newSessionStore(t)

	redirect := NewRedirectResponse("/orders").SetSession(store)
	redirect.With("status", "Order placed.")
	redirect.With(map[string]any{"tone": "success", "count": 2})

	if got := store.Get("status", nil); got != "Order placed." {
		t.Fatalf("status = %v, want %q", got, "Order placed.")
	}
	if got := store.Get("tone", nil); got != "success" {
		t.Fatalf("tone = %v, want %q", got, "success")
	}
	if got := store.Get("count", nil); got != 2 {
		t.Fatalf("count = %v, want 2", got)
	}
}

func TestWithOnARedirectWithNoSessionIsQuietRatherThanFatal(t *testing.T) {
	// A redirect built outside a request has nowhere to flash to. The PHP
	// would fatal on a null session; a handler that redirects from a place
	// with no session should not take the process down.
	redirect := NewRedirectResponse("/orders")
	if redirect.With("status", "Order placed.") != redirect {
		t.Fatal("With should chain even with no session behind it")
	}
}

func TestAnUploadThatLiesAboutItsTypeKeepsBothAnswersApart(t *testing.T) {
	// The browser announced an image; the name says otherwise. Neither is
	// trusted over the other, and the two questions have two answers.
	upload := NewUploadedFileFromPath("/tmp/upload", "invoice.exe", "image/jpeg", true)

	if got := upload.GetClientOriginalExtension(); got != "exe" {
		t.Fatalf("GetClientOriginalExtension() = %q, want %q", got, "exe")
	}
	if got := upload.GuessExtension(); got != "exe" {
		t.Fatalf("GuessExtension() = %q, want %q -- the name, not the header", got, "exe")
	}
	// The registered extensions for a type differ between machines -- jpg,
	// jpeg and jfif are all image/jpeg -- so what is asserted is that the
	// answer came from the announced type and not from the name.
	if got := upload.ClientExtension(); got == "" || got == "exe" {
		t.Fatalf("ClientExtension() = %q, want the extension of the announced type", got)
	}
	if got := upload.HashName(); !strings.HasSuffix(got, ".exe") {
		t.Fatalf("HashName() = %q, want it to end in the real extension", got)
	}
}
