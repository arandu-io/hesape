package http

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"

	stdhttp "net/http"

	"github.com/arandu-io/hesape/session"
)

// Route is the minimal view of the matched route the request needs.
//
// The concrete type is hesape/routing.Route, which is being written by another
// agent. The interface is declared here so that hesape/http does not import
// hesape/routing, which would be a cycle: routing builds the request, and the
// request needs the route's name and parameters, not the router itself.
//
// What is here is the half the PHP Request touches:
//
//	RouteName  answers to Route::getName()
//	Parameter  answers to Route::parameter($name, $default)
//	Parameters answers to Route::parameters()
//	Methods    answers to Route::methods()
//	Domain     answers to Route::getDomain()
//	URI        answers to Route::uri()
//
// routing.Route currently exposes RouteName and Pattern only. The rest is
// reported in stillMissing.
type Route interface {
	RouteName() string
	Parameter(name string, def ...any) any
	Parameters() map[string]any
	Methods() []string
	Domain() string
	URI() string
}

// Request mirrors Illuminate\Http\Request. It wraps a *net/http.Request and
// answers the methods a controller action reaches for: the input the body and
// the query string carried, the headers, the cookies, the files, the session
// that survived the redirect, and the route that matched.
//
// The tenant NEVER comes from the request (REGRA 14). There is no method here
// that reads a tenant id out of a path parameter, a query string, a header or a
// body field, and adding one would be the most direct route to a cross-tenant
// leak. The tenant is on the auth.Grant the policy mints, and the repository
// reads it from there. If a method here seems to offer tenant access, it does
// not -- it is here because the PHP has it, and the doc says what it is for.
//
// A note on names (ADR 0044): every method is the PHP method's name with the
// initial uppercase Go requires. Input is Input, never Get. Old is Old, never
// Previous. Initialisms are upper case: FullURL, IsJSON. The PHP throws and
// this returns (T, error) where it does.
type Request struct {
	// request is the standard library request this wraps. It is unexported so
	// that the Illuminate surface is the only surface; a caller that needs the
	// raw request reaches it through RawRequest.
	request *stdhttp.Request

	// session is the session store the middleware set. Old, Flash and Flush
	// read it; when it is nil they answer as the PHP does without a session:
	// Old returns the default, Flash panics.
	session *session.Store

	// json is the decoded JSON body, cached after the first read. The body of
	// a *http.Request is a one-shot reader; caching it here is what lets Input
	// and Json be called more than once.
	json       map[string]any
	jsonParsed bool

	// userResolver answers to $userResolver: the closure the auth middleware
	// installs, which returns the signed-in user.
	userResolver func(guard string) any

	// routeResolver answers to $routeResolver: the closure the router
	// installs, which returns the matched route.
	routeResolver func() Route

	// precognitive answers to $attributes->get('precognitive', false): whether
	// this request was marked as a precognitive validation request.
	precognitive bool

	// cachedAccept and acceptableContentTypes cache the Accept header and its
	// parsed form, the way Symfony's Request does.
	cachedAccept           string
	acceptableContentTypes []string
}

// NewRequest wraps a *net/http.Request. It is the constructor a handler or a
// test calls; the router calls it once per request.
func NewRequest(r *stdhttp.Request) *Request {
	return &Request{request: r}
}

// RawRequest returns the *net/http.Request this wraps, for the rare case that
// needs something the Illuminate surface does not cover. It is not the common
// path: the methods on Request are what a controller should reach for.
func (r *Request) RawRequest() *stdhttp.Request { return r.request }

// CreateFrom answers to Request::createFrom: a new Request built from another,
// sharing its query, body, headers, session and resolvers. It is what a
// middleware that needs a modified copy of the request uses; in Go the more
// common pattern is r.Clone(), but the PHP name is kept for the developer who
// looks for it.
func CreateFrom(from *Request) *Request {
	out := &Request{
		request:       from.request,
		session:       from.session,
		userResolver:  from.userResolver,
		routeResolver: from.routeResolver,
		precognitive:  from.precognitive,
	}
	return out
}

// Method answers to Request::method: the HTTP method, upper case.
func (r *Request) Method() string { return r.request.Method }

// IsMethod answers to Request::isMethod: case-insensitive method match.
func (r *Request) IsMethod(method string) bool {
	return strings.EqualFold(r.request.Method, method)
}

// Path answers to Request::path: the path without leading or trailing slashes,
// "/" when the path is empty.
func (r *Request) Path() string {
	pattern := strings.Trim(r.request.URL.Path, "/")
	if pattern == "" {
		return "/"
	}
	return pattern
}

// DecodedPath answers to Request::decodedPath: the path, URL-decoded.
func (r *Request) DecodedPath() string {
	decoded, err := url.PathUnescape(r.Path())
	if err != nil {
		return r.Path()
	}
	return decoded
}

// Segment answers to Request::segment: the nth segment of the path (1-based),
// or default when the index is out of range.
func (r *Request) Segment(index int, def ...string) string {
	segments := r.Segments()
	if index < 1 || index > len(segments) {
		if len(def) > 0 {
			return def[0]
		}
		return ""
	}
	return segments[index-1]
}

// Segments answers to Request::segments: the non-empty parts of the decoded
// path, split on "/".
func (r *Request) Segments() []string {
	decoded := r.DecodedPath()
	parts := strings.Split(decoded, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// URL answers to Request::url: the request URL without the query string. It is
// the address a link inside the page uses, so that the application keeps working
// behind a proxy, on a staging host and on somebody's laptop.
//
// The PHP builds this from scheme + host + path; the Go *http.Request carries
// scheme and host in URL when it was parsed from a full target, and in Host
// when it arrived over the wire. Both are checked, so a test that builds a
// request from "http://example.com/path" and a server that builds one from
// "/path" both get the right answer.
func (r *Request) URL() string {
	scheme := r.request.URL.Scheme
	host := r.request.URL.Host
	if host == "" {
		host = r.request.Host
	}
	if scheme == "" {
		scheme = "http"
		if r.request.TLS != nil {
			scheme = "https"
		}
	}
	path := r.request.URL.Path
	uri := scheme + "://" + host + path
	return strings.TrimRight(uri, "/")
}

// FullURL answers to Request::fullUrl: the full URL, scheme and host included,
// with the query string.
func (r *Request) FullURL() string {
	scheme := r.request.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.request.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.request.Host + r.request.URL.RequestURI()
}

// FullURLWithQuery answers to Request::fullUrlWithQuery: the full URL with the
// given query parameters merged into the existing ones.
func (r *Request) FullURLWithQuery(query map[string]string) string {
	values := r.request.URL.Query()
	for k, v := range query {
		values.Set(k, v)
	}
	base := r.URL()
	encoded := values.Encode()
	if encoded == "" {
		return base
	}
	separator := "?"
	if r.request.URL.Path == "/" && r.request.URL.RawQuery == "" {
		separator = "/?"
	}
	return base + separator + encoded
}

// FullURLWithoutQuery answers to Request::fullUrlWithoutQuery: the full URL
// without the given query parameters.
func (r *Request) FullURLWithoutQuery(keys ...string) string {
	values := r.request.URL.Query()
	for _, key := range keys {
		values.Del(key)
	}
	base := r.URL()
	encoded := values.Encode()
	if encoded == "" {
		return base
	}
	return base + "?" + encoded
}

// Root answers to Request::root: the root URL, scheme and host, without a
// trailing slash.
func (r *Request) Root() string {
	return strings.TrimRight(r.SchemeAndHttpHost(), "/")
}

// Is answers to Request::is: whether the decoded path matches any of the
// patterns. "*" is the only wildcard.
func (r *Request) Is(patterns ...string) bool {
	decoded := r.DecodedPath()
	for _, pattern := range patterns {
		if strIs(pattern, decoded) {
			return true
		}
	}
	return false
}

// RouteIs answers to Request::routeIs: whether the route name matches any of
// the patterns. Returns false when no route is set.
func (r *Request) RouteIs(patterns ...string) bool {
	route := r.matchedRoute()
	if route == nil {
		return false
	}
	name := route.RouteName()
	for _, pattern := range patterns {
		if strIs(pattern, name) {
			return true
		}
	}
	return false
}

// FullURLIs answers to Request::fullUrlIs: whether the full URL matches any of
// the patterns. "*" is the only wildcard.
func (r *Request) FullURLIs(patterns ...string) bool {
	full := r.FullURL()
	for _, pattern := range patterns {
		if strIs(pattern, full) {
			return true
		}
	}
	return false
}

// Host answers to Request::host: the host name, without the port.
func (r *Request) Host() string {
	host := r.request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// HTTPHost answers to Request::httpHost: the host as the browser sent it, port
// included when the browser sent one.
func (r *Request) HTTPHost() string { return r.request.Host }

// SchemeAndHttpHost answers to Request::schemeAndHttpHost: scheme + "://" +
// host.
func (r *Request) SchemeAndHttpHost() string {
	scheme := "http"
	if r.request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.request.Host
}

// Secure answers to Request::secure: whether the request was over TLS.
func (r *Request) Secure() bool { return r.request.TLS != nil }

// Ajax answers to Request::ajax: whether the request was made by XMLHttpRequest.
// htmx sends this header too; see WantsJSON for why htmx is deliberately
// excluded there.
func (r *Request) Ajax() bool {
	return r.request.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// PJAX answers to Request::pjax: whether the request carries the X-PJAX header.
func (r *Request) PJAX() bool {
	return r.request.Header.Get("X-PJAX") != ""
}

// PreferSafeContent answers to Symfony's isPreferSafeContent: whether the
// Prefer header asks for safe content.
func (r *Request) PreferSafeContent() bool {
	prefer := r.request.Header.Get("Prefer")
	for _, part := range strings.Split(prefer, ",") {
		token := strings.TrimSpace(part)
		if i := strings.Index(token, ";"); i >= 0 {
			token = strings.TrimSpace(token[:i])
		}
		if strings.EqualFold(token, "safe") {
			return true
		}
	}
	return false
}

// IP answers to Request::ip: the client address, without the port. Behind a
// proxy it is whatever the trust-proxies middleware wrote onto RemoteAddr.
func (r *Request) IP() string {
	addr := r.request.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// IPs answers to Request::ips: the chain of client addresses. With no proxy in
// front, this is a list of one.
func (r *Request) IPs() []string {
	ip := r.IP()
	if ip == "" {
		return nil
	}
	return []string{ip}
}

// UserAgent answers to Request::userAgent: the User-Agent header, or empty.
func (r *Request) UserAgent() string {
	return r.request.Header.Get("User-Agent")
}

// BearerToken answers to Request::bearerToken: the token of an Authorization:
// Bearer header, or empty. The scheme is compared case-insensitively (RFC 9110)
// and the value is trimmed.
func (r *Request) BearerToken() string {
	header := r.request.Header.Get("Authorization")
	position := strings.Index(strings.ToLower(header), "bearer ")
	if position < 0 {
		return ""
	}
	token := header[position+7:]
	if comma := strings.Index(token, ","); comma >= 0 {
		token = token[:comma]
	}
	return strings.TrimSpace(token)
}

// Header answers to Request::header: the first value of a header, or default
// when the header is absent. The name is canonicalised by net/http.
func (r *Request) Header(name string, def ...string) string {
	value := r.request.Header.Get(name)
	if value != "" {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// HasHeader answers to Request::hasHeader: whether a header is present,
// including when its value is empty.
func (r *Request) HasHeader(name string) bool {
	return len(r.request.Header.Values(name)) > 0
}

// Server answers to Request::server: a server variable, or default. The common
// CGI-style keys are mapped from the request; an unknown HTTP_* key falls back
// to the corresponding header.
func (r *Request) Server(key string, def ...string) string {
	value := r.serverVar(key)
	if value != "" {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

func (r *Request) serverVar(key string) string {
	switch key {
	case "REQUEST_METHOD":
		return r.request.Method
	case "REQUEST_URI":
		return r.request.URL.RequestURI()
	case "PATH_INFO":
		return r.request.URL.Path
	case "QUERY_STRING":
		return r.request.URL.RawQuery
	case "SERVER_NAME":
		return r.Host()
	case "SERVER_PORT":
		if _, port, err := net.SplitHostPort(r.request.Host); err == nil {
			return port
		}
		return ""
	case "SERVER_PROTOCOL":
		return r.request.Proto
	case "REMOTE_ADDR":
		return r.request.RemoteAddr
	case "HTTPS":
		if r.Secure() {
			return "on"
		}
		return ""
	case "CONTENT_TYPE":
		return r.request.Header.Get("Content-Type")
	case "CONTENT_LENGTH":
		return r.request.Header.Get("Content-Length")
	}
	if strings.HasPrefix(key, "HTTP_") {
		headerName := strings.ReplaceAll(key[5:], "_", "-")
		return r.request.Header.Get(headerName)
	}
	return ""
}

// Cookie answers to Request::cookie: the value of a cookie, or default when the
// browser sent none.
func (r *Request) Cookie(name string, def ...string) string {
	cookie, err := r.request.Cookie(name)
	if err == nil {
		return cookie.Value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// HasCookie answers to Request::hasCookie: whether a cookie is present.
func (r *Request) HasCookie(name string) bool {
	_, err := r.request.Cookie(name)
	return err == nil
}

// Keys answers to Request::keys: the keys of all input and files, merged.
func (r *Request) Keys() []string {
	input := r.inputMap()
	files := r.allFilesMap()
	seen := make(map[string]bool, len(input)+len(files))
	out := make([]string, 0, len(input)+len(files))
	for k := range input {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range files {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// Merge answers to Request::merge: merge the given input into the request's
// input source, overwriting existing keys. Returns the request, as the PHP
// returns $this.
func (r *Request) Merge(input map[string]any) *Request {
	source := r.inputSource()
	for k, v := range input {
		source[k] = v
	}
	r.replaceInputSource(source)
	return r
}

// MergeIfMissing answers to Request::mergeIfMissing: merge the given input only
// for keys that are missing from the request.
func (r *Request) MergeIfMissing(input map[string]any) *Request {
	source := r.inputSource()
	for k, v := range input {
		if _, ok := source[k]; !ok {
			source[k] = v
		}
	}
	r.replaceInputSource(source)
	return r
}

// Replace answers to Request::replace: replace the input source entirely.
func (r *Request) Replace(input map[string]any) *Request {
	r.replaceInputSource(input)
	return r
}

// replaceInputSource writes the merged input back to the request's form or
// JSON body, depending on which source Input reads.
func (r *Request) replaceInputSource(source map[string]any) {
	if r.IsJSON() {
		r.json = source
		r.jsonParsed = true
		return
	}
	if r.request.Method == "GET" || r.request.Method == "HEAD" {
		values := make(url.Values, len(source))
		for k, v := range source {
			values.Set(k, stringify(v))
		}
		r.request.URL.RawQuery = values.Encode()
		return
	}
	_ = r.request.ParseForm()
	values := make(url.Values, len(source))
	for k, v := range source {
		values.Set(k, stringify(v))
	}
	r.request.PostForm = values
	r.request.Form = values
}

// ToArray answers to Request::toArray: all input and files as a map.
func (r *Request) ToArray() map[string]any { return r.All() }

// Json answers to Request::json: the decoded JSON body. With a key, returns the
// value at the dotted path; without, returns the whole payload.
func (r *Request) Json(key string, def ...any) any {
	payload := r.jsonPayload()
	if key == "" {
		return payload
	}
	if len(def) > 0 {
		return dataGet(payload, key, def[0])
	}
	return dataGet(payload, key, nil)
}

// SetJson answers to Request::setJson: replace the cached JSON payload.
func (r *Request) SetJson(payload map[string]any) *Request {
	r.json = payload
	r.jsonParsed = true
	return r
}

// jsonPayload reads and caches the JSON body. The body reader is restored so
// that downstream code can still read it.
func (r *Request) jsonPayload() map[string]any {
	if r.jsonParsed {
		return r.json
	}
	r.jsonParsed = true
	body, err := io.ReadAll(r.request.Body)
	if err != nil || len(body) == 0 {
		r.json = map[string]any{}
		r.request.Body = io.NopCloser(strings.NewReader(""))
		return r.json
	}
	r.request.Body = io.NopCloser(strings.NewReader(string(body)))
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		r.json = map[string]any{}
		return r.json
	}
	r.json = parsed
	return r.json
}

// Fingerprint answers to Request::fingerprint: a unique hash of the route
// methods, domain, URI and client IP. Panics when no route is set, as the PHP
// throws RuntimeException.
func (r *Request) Fingerprint() string {
	route := r.matchedRoute()
	if route == nil {
		panic("http: unable to generate fingerprint: route unavailable")
	}
	parts := append(route.Methods(), route.Domain(), route.URI(), r.IP())
	return sha1Hex(strings.Join(parts, "|"))
}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// User answers to Request::user: the authenticated user, via the resolver the
// auth middleware installed. Returns nil when no resolver is set.
func (r *Request) User(guard ...string) any {
	if r.userResolver == nil {
		return nil
	}
	g := ""
	if len(guard) > 0 {
		g = guard[0]
	}
	return r.userResolver(g)
}

// SetUserResolver answers to Request::setUserResolver.
func (r *Request) SetUserResolver(resolver func(guard string) any) *Request {
	r.userResolver = resolver
	return r
}

// GetUserResolver answers to Request::getUserResolver: the installed resolver,
// or a no-op when none was set.
func (r *Request) GetUserResolver() func(guard string) any {
	if r.userResolver == nil {
		return func(string) any { return nil }
	}
	return r.userResolver
}

// Route answers to Request::route: the matched route, or a specific parameter
// from it. With no arguments, returns the Route. With a string argument,
// returns the parameter of that name from the route. With a default, returns
// the default when the parameter is absent.
//
// Returns nil when no resolver is set.
func (r *Request) Route(args ...any) any {
	route := r.matchedRoute()
	if len(args) == 0 {
		return route
	}
	if route == nil {
		if len(args) > 1 {
			return args[1]
		}
		return nil
	}
	param, _ := args[0].(string)
	return route.Parameter(param, args[1:]...)
}

// matchedRoute returns the matched route, or nil when no resolver is set.
func (r *Request) matchedRoute() Route {
	if r.routeResolver == nil {
		return nil
	}
	return r.routeResolver()
}

// SetRouteResolver answers to Request::setRouteResolver.
func (r *Request) SetRouteResolver(resolver func() Route) *Request {
	r.routeResolver = resolver
	return r
}

// GetRouteResolver answers to Request::getRouteResolver: the installed
// resolver, or a no-op when none was set.
func (r *Request) GetRouteResolver() func() Route {
	if r.routeResolver == nil {
		return func() Route { return nil }
	}
	return r.routeResolver
}

// HasSession answers to Request::hasSession: whether a session store was set.
func (r *Request) HasSession() bool { return r.session != nil }

// Session answers to Request::session: the session store. Panics when none is
// set, as the PHP throws RuntimeException.
func (r *Request) Session() *session.Store {
	if r.session == nil {
		panic("http: session store not set on request")
	}
	return r.session
}

// SetSession answers to Request::setLaravelSession.
func (r *Request) SetSession(s *session.Store) *Request {
	r.session = s
	return r
}

// SetPrecognitive marks the request as precognitive, the way the PHP sets the
// attribute the IsPrecognitive reader checks.
func (r *Request) SetPrecognitive() *Request {
	r.precognitive = true
	return r
}

// IsPrecognitive answers to Request::isPrecognitive.
func (r *Request) IsPrecognitive() bool { return r.precognitive }

// IsAttemptingPrecognition answers to Request::isAttemptingPrecognition:
// whether the Precognition header is "true".
func (r *Request) IsAttemptingPrecognition() bool {
	return r.request.Header.Get("Precognition") == "true"
}

// FilterPrecognitiveRules answers to Request::filterPrecognitiveRules: when the
// Precognition-Validate-Only header is present, returns only the rules whose
// attribute matches one of its patterns; otherwise returns the rules unchanged.
func (r *Request) FilterPrecognitiveRules(rules map[string]any) map[string]any {
	if !r.HasHeader("Precognition-Validate-Only") {
		return rules
	}
	validateOnly := strings.Split(r.request.Header.Get("Precognition-Validate-Only"), ",")
	out := make(map[string]any, len(rules))
	for attribute, rule := range rules {
		if r.shouldValidatePrecognitiveAttribute(attribute, validateOnly) {
			out[attribute] = rule
		}
	}
	return out
}

func (r *Request) shouldValidatePrecognitiveAttribute(attribute string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		regex := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `[^.]+`) + "$"
		if matched, _ := regexp.MatchString(regex, attribute); matched {
			return true
		}
	}
	return false
}

// Prefetch answers to Request::prefetch: whether the request is a browser
// prefetch.
func (r *Request) Prefetch() bool {
	return strings.EqualFold(r.serverVar("HTTP_X_MOZ"), "prefetch") ||
		strings.EqualFold(r.request.Header.Get("Purpose"), "prefetch") ||
		strings.EqualFold(r.request.Header.Get("Sec-Purpose"), "prefetch")
}

// Instance answers to Request::instance: the request itself. It exists for API
// parity with the PHP; in Go the caller already has the value.
func (r *Request) Instance() *Request { return r }

// URI answers to Request::uri: the URI of the request. The PHP returns a
// Uri instance; Go has none, so it returns the full URL string, which is what
// a caller uses it for.
func (r *Request) URI() string { return r.FullURL() }

// Capture answers to Request::capture: build a request from the PHP globals.
// Go has no globals to read from; a request arrives already built as a
// *http.Request. This is here for API parity and returns NewRequest called on
// the given *http.Request.
func Capture(r *stdhttp.Request) *Request { return NewRequest(r) }

// CreateFromBase answers to Request::createFromBase: build an Illuminate
// request from a base request. In Go there is no Symfony base to convert from,
// so it is NewRequest under the PHP name.
func CreateFromBase(r *stdhttp.Request) *Request { return NewRequest(r) }

// Duplicate answers to Request::duplicate: a copy of the request with optional
// overrides for query, post, cookies and server. Nil arguments keep the
// original.
func (r *Request) Duplicate(query, post, cookies, server map[string]any) *Request {
	out := CreateFrom(r)
	if query != nil {
		values := make(url.Values, len(query))
		for k, v := range query {
			values.Set(k, stringify(v))
		}
		out.request.URL.RawQuery = values.Encode()
	}
	if post != nil {
		values := make(url.Values, len(post))
		for k, v := range post {
			values.Set(k, stringify(v))
		}
		out.request.PostForm = values
		out.request.Form = values
	}
	return out
}

// SetRequestLocale answers to Request::setRequestLocale. The locale is stored
// on the request for the view layer to read. It is a string because Go has no
// locale type, and the view layer reads it through the same string.
func (r *Request) SetRequestLocale(locale string) *Request {
	if r.request == nil {
		return r
	}
	// Store on the request context as a simple value. The view layer reads it
	// from there; a struct field would not survive a clone.
	ctx := context.WithValue(r.request.Context(), requestLocaleKey{}, locale)
	r.request = r.request.WithContext(ctx)
	return r
}

// SetDefaultRequestLocale answers to Request::setDefaultRequestLocale.
func (r *Request) SetDefaultRequestLocale(locale string) *Request {
	if r.request == nil {
		return r
	}
	ctx := context.WithValue(r.request.Context(), defaultRequestLocaleKey{}, locale)
	r.request = r.request.WithContext(ctx)
	return r
}

// GetSession answers to Request::getSession: the session store. Panics when
// none is set, as the PHP throws SessionNotFoundException. It is Session()
// under the PHP name the Symfony interface declares.
func (r *Request) GetSession() *session.Store { return r.Session() }

// SetLaravelSession answers to Request::setLaravelSession. It is SetSession
// under the PHP name.
func (r *Request) SetLaravelSession(s *session.Store) *Request { return r.SetSession(s) }

// requestLocaleKey and defaultRequestLocaleKey are the context keys for the
// locale the view layer reads.
type requestLocaleKey struct{}
type defaultRequestLocaleKey struct{}
