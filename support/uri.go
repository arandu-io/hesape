package support

import (
	"errors"
	nethttp "net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/support/arr"
)

// Uri answers to Illuminate\Support\Uri: a parsed URI whose every writer hands
// back a new instance and leaves the old one alone.
//
// The PHP wraps league/uri; this wraps net/url, which is the standard library's
// answer to the same job and keeps the core free of third-party code.
type Uri struct {
	uri *url.URL
}

// UrlGenerator is the part of Illuminate\Contracts\Routing\UrlGenerator that
// Uri calls. The routing package fills it in; support declares only what it
// needs, so it carries no dependency on routing.
type UrlGenerator interface {
	// To answers to UrlGenerator::to.
	To(path string) string
	// Route answers to UrlGenerator::route.
	Route(name string, parameters map[string]any, absolute bool) (string, error)
	// SignedRoute answers to UrlGenerator::signedRoute.
	SignedRoute(name string, parameters map[string]any, expiration *time.Time, absolute bool) (string, error)
	// Action answers to UrlGenerator::action.
	Action(action string, parameters map[string]any, absolute bool) (string, error)
}

var (
	urlGeneratorMu       sync.RWMutex
	urlGeneratorResolver func() UrlGenerator
)

// ErrNoUrlGenerator is what Uri::to, Uri::route, Uri::signedRoute and
// Uri::action hit when no resolver was set. The PHP calls null and fatals; this
// returns the error instead.
var ErrNoUrlGenerator = errors.New("support: no URL generator resolver has been set")

// SetUrlGeneratorResolver answers to Uri::setUrlGeneratorResolver.
func SetUrlGeneratorResolver(resolver func() UrlGenerator) {
	urlGeneratorMu.Lock()
	defer urlGeneratorMu.Unlock()
	urlGeneratorResolver = resolver
}

func generator() (UrlGenerator, error) {
	urlGeneratorMu.RLock()
	resolver := urlGeneratorResolver
	urlGeneratorMu.RUnlock()
	if resolver == nil {
		return nil, ErrNoUrlGenerator
	}
	g := resolver()
	if g == nil {
		return nil, ErrNoUrlGenerator
	}
	return g, nil
}

// NewUri answers to Uri::__construct. The PHP throws on a URI it cannot parse,
// so this returns (*Uri, error).
func NewUri(uri string) (*Uri, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	return &Uri{uri: parsed}, nil
}

// Of answers to Uri::of.
func Of(uri string) (*Uri, error) { return NewUri(uri) }

// To answers to Uri::to: an absolute URL for the path, from the URL generator.
func To(path string) (*Uri, error) {
	g, err := generator()
	if err != nil {
		return nil, err
	}
	return NewUri(g.To(path))
}

// Route answers to Uri::route.
func Route(name string, parameters map[string]any, absolute bool) (*Uri, error) {
	g, err := generator()
	if err != nil {
		return nil, err
	}
	generated, err := g.Route(name, parameters, absolute)
	if err != nil {
		return nil, err
	}
	return NewUri(generated)
}

// SignedRoute answers to Uri::signedRoute.
func SignedRoute(name string, parameters map[string]any, expiration *time.Time, absolute bool) (*Uri, error) {
	g, err := generator()
	if err != nil {
		return nil, err
	}
	generated, err := g.SignedRoute(name, parameters, expiration, absolute)
	if err != nil {
		return nil, err
	}
	return NewUri(generated)
}

// TemporarySignedRoute answers to Uri::temporarySignedRoute, which is
// signedRoute with the expiration moved to the front.
func TemporarySignedRoute(name string, expiration time.Time, parameters map[string]any, absolute bool) (*Uri, error) {
	return SignedRoute(name, parameters, &expiration, absolute)
}

// Action answers to Uri::action.
func Action(action string, parameters map[string]any, absolute bool) (*Uri, error) {
	g, err := generator()
	if err != nil {
		return nil, err
	}
	generated, err := g.Action(action, parameters, absolute)
	if err != nil {
		return nil, err
	}
	return NewUri(generated)
}

// Scheme answers to Uri::scheme.
func (u *Uri) Scheme() string { return u.uri.Scheme }

// User answers to Uri::user. With withPassword it is the whole user info, the
// way the PHP getUserInfo is.
func (u *Uri) User(withPassword bool) string {
	if u.uri.User == nil {
		return ""
	}
	if withPassword {
		return u.uri.User.String()
	}
	return u.uri.User.Username()
}

// Password answers to Uri::password.
func (u *Uri) Password() string {
	if u.uri.User == nil {
		return ""
	}
	password, _ := u.uri.User.Password()
	return password
}

// Host answers to Uri::host.
func (u *Uri) Host() string { return u.uri.Hostname() }

// Port answers to Uri::port. A URI with no port is zero, which is the PHP null.
func (u *Uri) Port() int {
	port, err := strconv.Atoi(u.uri.Port())
	if err != nil {
		return 0
	}
	return port
}

// Path answers to Uri::path: the path with its slashes trimmed off both ends.
// An empty or missing path is a single "/", which is what the PHP returns.
func (u *Uri) Path() string {
	path := strings.Trim(u.uri.Path, "/")
	if path == "" {
		return "/"
	}
	return path
}

// PathSegments answers to Uri::pathSegments. An empty path is an empty list.
func (u *Uri) PathSegments() []string {
	path := u.Path()
	if path == "/" {
		return []string{}
	}
	return strings.Split(path, "/")
}

// Query answers to Uri::query.
func (u *Uri) Query() *UriQueryString { return NewUriQueryString(u) }

// Fragment answers to Uri::fragment.
func (u *Uri) Fragment() string { return u.uri.Fragment }

func (u *Uri) with(mutate func(*url.URL)) *Uri {
	copied := *u.uri
	if u.uri.User != nil {
		user := *u.uri.User
		copied.User = &user
	}
	mutate(&copied)
	return &Uri{uri: &copied}
}

// WithScheme answers to Uri::withScheme.
func (u *Uri) WithScheme(scheme string) *Uri {
	return u.with(func(n *url.URL) { n.Scheme = scheme })
}

// WithUser answers to Uri::withUser. The variadic argument stands for the PHP
// $password, which is null.
func (u *Uri) WithUser(user string, password ...string) *Uri {
	return u.with(func(n *url.URL) {
		switch {
		case user == "":
			n.User = nil
		case len(password) > 0:
			n.User = url.UserPassword(user, password[0])
		default:
			n.User = url.User(user)
		}
	})
}

// WithHost answers to Uri::withHost.
func (u *Uri) WithHost(host string) *Uri {
	return u.with(func(n *url.URL) {
		if port := u.uri.Port(); port != "" {
			n.Host = host + ":" + port
			return
		}
		n.Host = host
	})
}

// WithPort answers to Uri::withPort. A zero port is the PHP null and removes it.
func (u *Uri) WithPort(port int) *Uri {
	return u.with(func(n *url.URL) {
		if port == 0 {
			n.Host = u.uri.Hostname()
			return
		}
		n.Host = u.uri.Hostname() + ":" + strconv.Itoa(port)
	})
}

// WithPath answers to Uri::withPath. The path is given a leading slash, which
// is the Str::start the PHP calls.
func (u *Uri) WithPath(path string) *Uri {
	return u.with(func(n *url.URL) {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		n.Path = path
	})
}

// WithQuery answers to Uri::withQuery: the pairs written into the query string
// by dotted key. The variadic argument stands for the PHP $merge, which is
// true; with false the query is replaced instead.
func (u *Uri) WithQuery(query map[string]any, merge ...bool) *Uri {
	newQuery := map[string]any{}
	if firstOr(merge, true) {
		newQuery = u.Query().ToArray()
	}
	for _, key := range sortedMapKeys(query) {
		arr.Set(newQuery, key, query[key])
	}
	return u.with(func(n *url.URL) { n.RawQuery = arr.Query(newQuery) })
}

// WithQueryIfMissing answers to Uri::withQueryIfMissing: only the keys the
// query string does not already carry.
func (u *Uri) WithQueryIfMissing(query map[string]any) *Uri {
	current := u.Query()
	pending := map[string]any{}
	for key, v := range query {
		if current.Missing(key) {
			pending[key] = v
		}
	}
	return u.WithQuery(pending)
}

// PushOntoQuery answers to Uri::pushOntoQuery: append to the list a query
// parameter holds. A list already holding the value is left alone, which is the
// array_unique the PHP runs on a list; a keyed value is appended to blindly.
func (u *Uri) PushOntoQuery(key string, v any) *Uri {
	current := arr.Get(u.Query().ToArray(), key, nil)
	values := arr.Wrap(v)

	switch existing := current.(type) {
	case nil:
		return u.WithQuery(map[string]any{key: values})
	case []any:
		merged := append([]any{}, existing...)
		for _, candidate := range values {
			found := false
			for _, held := range merged {
				if held == candidate {
					found = true
					break
				}
			}
			if !found {
				merged = append(merged, candidate)
			}
		}
		return u.WithQuery(map[string]any{key: merged})
	default:
		return u.WithQuery(map[string]any{key: append([]any{existing}, values...)})
	}
}

// WithoutQuery answers to Uri::withoutQuery: the query string without the given
// keys.
func (u *Uri) WithoutQuery(keys ...string) *Uri {
	return u.ReplaceQuery(arr.Except(u.Query().ToArray(), keys...))
}

// ReplaceQuery answers to Uri::replaceQuery: the query string thrown away and
// written again from the given pairs.
func (u *Uri) ReplaceQuery(query map[string]any) *Uri {
	return u.WithQuery(query, false)
}

// WithFragment answers to Uri::withFragment.
func (u *Uri) WithFragment(fragment string) *Uri {
	return u.with(func(n *url.URL) { n.Fragment = fragment })
}

// ToHtml answers to Uri::toHtml.
func (u *Uri) ToHtml() string { return u.Value() }

// Redirect answers to Uri::redirect.
//
// The PHP returns an Illuminate\Http\RedirectResponse. Returning ours would
// make support import http, and http imports support -- so this hands back the
// standard library's net/http.Handler, which is what a router mounts and what
// a RedirectResponse ultimately is. The name and the arguments are Laravel's.
//
// headers is applied before the redirect is written, because WriteHeader
// freezes the header map.
func (u *Uri) Redirect(status int, headers map[string]string) nethttp.Handler {
	value := u.Value()
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		for name, v := range headers {
			w.Header().Set(name, v)
		}
		nethttp.Redirect(w, r, value, status)
	})
}

// ToResponse answers to Uri::toResponse.
//
// The PHP takes the request and ignores it, redirecting with the default
// status; this keeps the parameter for the same reason PHP has it -- it is the
// Responsable contract -- and ignores it for the same reason.
func (u *Uri) ToResponse(*nethttp.Request) nethttp.Handler {
	return u.Redirect(nethttp.StatusFound, nil)
}

// Decode answers to Uri::decode: the URI with its query string written out
// undecoded, which is what a person reads in a browser bar.
func (u *Uri) Decode() string {
	query := u.Query()
	if len(query.ToArray()) == 0 {
		return u.Value()
	}
	full := u.Value()
	index := strings.Index(full, "?")
	if index < 0 {
		return full
	}
	return full[:index+1] + query.Decode()
}

// Value answers to Uri::value, which is the PHP __toString.
func (u *Uri) Value() string { return u.uri.String() }

// String answers to Uri::__toString.
func (u *Uri) String() string { return u.Value() }

// IsEmpty answers to Uri::isEmpty.
func (u *Uri) IsEmpty() bool { return strings.TrimSpace(u.Value()) == "" }

// GetUri answers to Uri::getUri: the parsed URI underneath. The PHP hands back
// a League UriInterface; this hands back the *url.URL it wraps.
func (u *Uri) GetUri() *url.URL { return u.uri }

// UriQueryString answers to Illuminate\Support\UriQueryString: the query string
// of a [Uri], read as nested data. The typed readers of
// Illuminate\Support\Traits\InteractsWithData are promoted from the embedded
// trait, exactly as the PHP class gets them.
type UriQueryString struct {
	dataSource
	uri *Uri
}

// NewUriQueryString answers to UriQueryString::__construct.
func NewUriQueryString(uri *Uri) *UriQueryString {
	q := &UriQueryString{uri: uri}
	q.dataSource = dataSource{
		allFn:  q.All,
		dataFn: func(key string, def any) any { return q.Get(key, def) },
	}
	return q
}

// All answers to UriQueryString::all. With no key it is the whole query string;
// with keys it is the subset under them.
func (q *UriQueryString) All(keys ...string) map[string]any {
	query := q.ToArray()
	if len(keys) == 0 {
		return query
	}
	results := map[string]any{}
	for _, key := range keys {
		arr.Set(results, key, arr.Get(query, key, nil))
	}
	return results
}

// Get answers to UriQueryString::get: one parameter by dotted key.
func (q *UriQueryString) Get(key string, def ...any) any {
	return arr.Get(q.ToArray(), key, firstOr(def, nil))
}

// Decode answers to UriQueryString::decode, which is PHP's rawurldecode of the
// whole query string.
func (q *UriQueryString) Decode() string {
	decoded, err := url.PathUnescape(q.Value())
	if err != nil {
		return q.Value()
	}
	return decoded
}

// Value answers to UriQueryString::value, which is the PHP __toString.
func (q *UriQueryString) Value() string {
	if q.uri == nil || q.uri.uri == nil {
		return ""
	}
	return q.uri.uri.RawQuery
}

// ToArray answers to UriQueryString::toArray: the query string as nested data,
// which is what League's QueryString::extract builds.
//
// A key is decoded before its brackets are read, so a%5Bb%5D=c and a[b]=c are
// the same nesting, as they are in PHP.
func (q *UriQueryString) ToArray() map[string]any {
	return parseQueryString(q.Value())
}

// parseQueryString is QueryString::extract: key[sub]=value and key[]=value read
// back into nested maps and lists.
func parseQueryString(raw string) map[string]any {
	results := map[string]any{}
	if raw == "" {
		return results
	}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(pair, "=")
		key := rawURLDecode(rawKey)
		if key == "" {
			continue
		}
		setQueryValue(results, querySegments(key), rawURLDecode(rawValue))
	}
	return results
}

// querySegments splits a[b][] into "a", "b", "".
func querySegments(key string) []string {
	open := strings.Index(key, "[")
	if open < 0 {
		return []string{key}
	}
	segments := []string{key[:open]}
	rest := key[open:]
	for strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			segments = append(segments, rest[1:])
			return segments
		}
		segments = append(segments, rest[1:end])
		rest = rest[end+1:]
	}
	return segments
}

func setQueryValue(target map[string]any, segments []string, v string) {
	segment := segments[0]
	if len(segments) == 1 {
		if segment == "" {
			return
		}
		target[segment] = v
		return
	}
	if segments[1] == "" && len(segments) == 2 {
		list, _ := target[segment].([]any)
		target[segment] = append(list, v)
		return
	}
	child, ok := target[segment].(map[string]any)
	if !ok {
		child = map[string]any{}
		target[segment] = child
	}
	setQueryValue(child, segments[1:], v)
}

// rawURLDecode is PHP's rawurldecode: percent-triplets become bytes and a plus
// stays a plus.
func rawURLDecode(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
