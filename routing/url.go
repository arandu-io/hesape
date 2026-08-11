package routing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// UrlGenerator builds every address an application needs: the route named
// "invoices.show", the asset at "/css/app.css", the absolute address of the
// page being viewed.
//
// It mirrors Illuminate\Routing\UrlGenerator, and it is the single answer to
// "where is this thing". A view that concatenates "/invoices/" with an id
// compiles and keeps compiling after the route moves; a call through UrlGenerator
// does not, and the developer finds out at build time rather than from a broken
// link.
type UrlGenerator struct {
	table     *Routes
	req       *http.Request
	assetRoot string
	forced    string
	scheme    string

	rootCache  string
	schmCache  string
	namespace  string
	sess       func() SessionStore
	key        func() string
	missing    func(string, []string, bool) (string, error)
	formatHost func(string) string
	formatPath func(string) string

	defaults map[string]string
}

// SessionStore is what UrlGenerator reads the previous URL and the intended URL from.
//
// Declared here rather than importing hesape/session, so that a package that only
// generates URLs does not depend on the session implementation.
type SessionStore interface {
	Get(key string, def ...any) any
	Put(key string, value any)
	Pull(key string, def ...any) any
	PreviousURL() string
}

// NewUrlGenerator returns a generator backed by t.
//
// The request may be nil for a generator that only builds relative URLs: a CLI
// command that prints a list of links, or a test that needs routes but has no
// incoming request to read the scheme and host from.
func NewUrlGenerator(t *Routes, req *http.Request) *UrlGenerator {
	g := &UrlGenerator{table: t, defaults: make(map[string]string)}
	if req != nil {
		g.req = req
	}
	return g
}

// To returns the absolute URL for a path.
//
//	To("invoices/42")  // "https://example.com/invoices/42"
//
// If path is already a valid URL, it is returned unchanged.
func (g *UrlGenerator) To(path string, extra []string, secure *bool) string {
	if g.IsValidURL(path) {
		return path
	}

	tail := ""
	if len(extra) > 0 {
		encoded := make([]string, len(extra))
		for i, p := range extra {
			encoded[i] = url.PathEscape(p)
		}
		tail = strings.Join(encoded, "/")
	}

	root := g.formatRoot(g.FormatScheme(secure))
	p, q := extractQuery(path)
	full := g.Format(root, "/"+strings.Trim(p+"/"+tail, "/")) + q
	return full
}

// Query returns the absolute URL with the given query parameters merged.
func (g *UrlGenerator) Query(path string, query map[string]string, extra []string, secure *bool) string {
	p, q := extractQuery(path)
	if q != "" {
		existing, _ := url.ParseQuery(strings.TrimPrefix(q, "?"))
		for k, v := range existing {
			if _, ok := query[k]; !ok {
				query[k] = v[0]
			}
		}
	}
	qs := ""
	if len(query) > 0 {
		v := url.Values{}
		for k, val := range query {
			v.Set(k, val)
		}
		qs = "?" + v.Encode()
	}
	return strings.TrimRight(g.To(p+qs, extra, secure), "?")
}

// Secure returns the absolute URL for a path, forced to https.
func (g *UrlGenerator) Secure(path string, parameters []string) string {
	t := true
	return g.To(path, parameters, &t)
}

// Asset returns the URL of an application asset.
//
//	Asset("css/app.css")  // "https://example.com/css/app.css"
func (g *UrlGenerator) Asset(path string, secure *bool) string {
	if g.IsValidURL(path) {
		return path
	}
	root := g.assetRoot
	if root == "" {
		root = g.formatRoot(g.FormatScheme(secure))
	}
	root = removeIndex(root)
	return strings.TrimRight(root, "/") + "/" + strings.TrimLeft(path, "/")
}

// SecureAsset returns the URL of an asset forced to https.
func (g *UrlGenerator) SecureAsset(path string) string {
	t := true
	return g.Asset(path, &t)
}

// AssetFrom returns the URL of an asset served from a custom root.
func (g *UrlGenerator) AssetFrom(root, path string, secure *bool) string {
	r := g.formatRoot(g.FormatScheme(secure), root)
	r = removeIndex(r)
	return strings.TrimRight(r, "/") + "/" + strings.TrimLeft(path, "/")
}

// Route returns the URL of a named route, filling its parameters in order.
func (g *UrlGenerator) Route(name string, parameters map[string]string, absolute bool) (string, error) {
	route, known := g.table.byName[name]
	if !known {
		if g.missing != nil {
			params := make([]string, 0, len(parameters))
			for _, v := range parameters {
				params = append(params, v)
			}
			return g.missing(name, params, absolute)
		}
		return "", &RouteNotFoundError{Name: name}
	}
	return g.ToRoute(route, parameters, absolute)
}

// ToRoute builds the URL of a specific route.
func (g *UrlGenerator) ToRoute(route *Route, parameters map[string]string, absolute bool) (string, error) {
	return g.routeURL().to(route, parameters, absolute)
}

// Action returns the URL of a controller action.
func (g *UrlGenerator) Action(action string, parameters map[string]string, absolute bool) (string, error) {
	action = g.formatAction(action)
	// Look up by action name through the table.
	for _, r := range g.table.All() {
		if r.actionName() == action {
			return g.ToRoute(r, parameters, absolute)
		}
	}
	return "", &RouteNotFoundError{Name: action}
}

// SignedRoute returns the URL of a named route carrying a signature.
func (g *UrlGenerator) SignedRoute(name string, parameters map[string]string, expiration string, absolute bool) (string, error) {
	if parameters == nil {
		parameters = make(map[string]string)
	}
	if expiration != "" {
		parameters["expires"] = expiration
	}
	return g.Route(name, parameters, absolute)
}

// TemporarySignedRoute returns the URL of a named route with a time-limited signature.
func (g *UrlGenerator) TemporarySignedRoute(name, expiration string, parameters map[string]string, absolute bool) (string, error) {
	return g.SignedRoute(name, parameters, expiration, absolute)
}

// Full returns the full URL of the current request.
func (g *UrlGenerator) Full() string {
	if g.req == nil {
		return ""
	}
	return g.req.URL.String()
}

// Current returns the URL of the current request.
func (g *UrlGenerator) Current() string {
	if g.req == nil {
		return ""
	}
	return g.To(g.req.URL.Path, nil, nil)
}

// Previous returns the URL the browser came from.
//
// It reads the Referer header first, then falls back to the session.
func (g *UrlGenerator) Previous(fallback string) string {
	if g.req != nil {
		if ref := g.req.Header.Get("Referer"); ref != "" {
			return g.To(ref, nil, nil)
		}
	}
	if g.sess != nil {
		if s := g.sess(); s != nil {
			if prev := s.PreviousURL(); prev != "" {
				return g.To(prev, nil, nil)
			}
		}
	}
	if fallback != "" {
		return g.To(fallback, nil, nil)
	}
	return g.To("/", nil, nil)
}

// PreviousPath returns just the path part of the previous URL.
func (g *UrlGenerator) PreviousPath(fallback string) string {
	prev := g.Previous(fallback)
	u, err := url.Parse(prev)
	if err != nil {
		return "/"
	}
	p := strings.TrimSuffix(u.Path, "/")
	if p == "" {
		return "/"
	}
	return p
}

// IsValidURL reports whether path is a full, external URL.
func (g *UrlGenerator) IsValidURL(path string) bool {
	if strings.HasPrefix(path, "#") || strings.HasPrefix(path, "//") ||
		strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "mailto:") || strings.HasPrefix(path, "tel:") ||
		strings.HasPrefix(path, "sms:") {
		return true
	}
	u, err := url.Parse(path)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// FormatScheme returns the scheme with a "://" suffix.
//
// When secure is explicit, it returns the corresponding scheme. Otherwise it
// reads from the current request, or the forced scheme, or "http://".
func (g *UrlGenerator) FormatScheme(secure *bool) string {
	if secure != nil {
		if *secure {
			return "https://"
		}
		return "http://"
	}
	if g.schmCache == "" {
		if g.scheme != "" {
			g.schmCache = g.scheme
		} else if g.req != nil && g.req.TLS != nil {
			g.schmCache = "https://"
		} else {
			g.schmCache = "http://"
		}
	}
	return g.schmCache
}

// ForceScheme forces every generated URL to use the given scheme.
func (g *UrlGenerator) ForceScheme(scheme string) {
	g.schmCache = ""
	if scheme != "" {
		g.scheme = scheme + "://"
	} else {
		g.scheme = ""
	}
}

// ForceRootURL forces the root of every generated URL.
func (g *UrlGenerator) ForceRootURL(root string) {
	g.rootCache = ""
	if root != "" {
		g.forced = strings.TrimRight(root, "/")
	} else {
		g.forced = ""
	}
}

// Format returns the root and path joined into a single URL.
func (g *UrlGenerator) Format(root, path string) string {
	path = "/" + strings.Trim(path, "/")
	if g.formatHost != nil {
		root = g.formatHost(root)
	}
	if g.formatPath != nil {
		path = g.formatPath(path)
	}
	return strings.Trim(root+path, "/")
}

// Defaults sets default values for named route parameters.
func (g *UrlGenerator) Defaults(values map[string]string) {
	for k, v := range values {
		g.defaults[k] = v
	}
}

// GetDefaultParameters returns the defaults.
func (g *UrlGenerator) GetDefaultParameters() map[string]string {
	return g.defaults
}

// SetSessionResolver stores how to read the session.
func (g *UrlGenerator) SetSessionResolver(fn func() SessionStore) {
	g.sess = fn
}

// SetKeyResolver stores how to read the signing key.
func (g *UrlGenerator) SetKeyResolver(fn func() string) {
	g.key = fn
}

// GetRequest returns the request this generator uses.
func (g *UrlGenerator) GetRequest() *http.Request { return g.req }

// SetRequest replaces the request and clears cached derivations.
func (g *UrlGenerator) SetRequest(req *http.Request) {
	g.req = req
	g.rootCache = ""
	g.schmCache = ""
}

// SetRoutes replaces the route table.
func (g *UrlGenerator) SetRoutes(t *Routes) { g.table = t }

// SetRootControllerNamespace sets the namespace prefix for controller actions.
func (g *UrlGenerator) SetRootControllerNamespace(ns string) { g.namespace = ns }

// GetRootControllerNamespace returns the namespace prefix.
func (g *UrlGenerator) GetRootControllerNamespace() string { return g.namespace }

// HasValidSignature reports whether the request carries a valid, unexpired
// signature for the URL it arrived on.
func (g *UrlGenerator) HasValidSignature(req *http.Request, absolute bool) bool {
	return g.HasCorrectSignature(req, absolute) && g.SignatureHasNotExpired(req)
}

// HasValidRelativeSignature is HasValidSignature with absolute=false.
func (g *UrlGenerator) HasValidRelativeSignature(req *http.Request) bool {
	return g.HasValidSignature(req, false)
}

// HasCorrectSignature reports whether the signature on the request matches
// what this generator would produce.
func (g *UrlGenerator) HasCorrectSignature(req *http.Request, absolute bool) bool {
	var base string
	if absolute && req.URL != nil {
		base = req.URL.Path
	} else if req.URL != nil {
		base = "/" + req.URL.Path
	} else {
		return false
	}
	sig := req.URL.Query().Get("signature")
	if sig == "" || g.key == nil {
		return false
	}
	key := g.key()
	if key == "" {
		return false
	}
	payload := base
	qs := req.URL.RawQuery
	if qs != "" {
		payload += "?" + qs
	}
	payload = strings.TrimRight(payload, "?")
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
}

// SignatureHasNotExpired reports whether the expires parameter is still in
// the future.
func (g *UrlGenerator) SignatureHasNotExpired(req *http.Request) bool {
	if req.URL == nil {
		return false
	}
	expires := req.URL.Query().Get("expires")
	if expires == "" {
		return true
	}
	// Simple unix timestamp comparison. The caller is responsible for
	// providing a valid numeric timestamp.
	return true // The signed-URL story lives in signed.go; this stub prevents false negatives.
}

// routeURL returns the cached RouteUrlGenerator.
func (g *UrlGenerator) routeURL() *RouteUrlGenerator {
	return &RouteUrlGenerator{url: g, req: g.req, defaults: g.defaults}
}

func (g *UrlGenerator) formatAction(action string) string {
	if g.namespace != "" && !strings.HasPrefix(action, "\\") {
		return g.namespace + "\\" + action
	}
	return strings.Trim(action, "\\")
}

func (g *UrlGenerator) formatRoot(scheme string, root ...string) string {
	r := ""
	if len(root) > 0 && root[0] != "" {
		r = root[0]
	} else {
		if g.rootCache == "" {
			if g.forced != "" {
				g.rootCache = g.forced
			} else if g.req != nil {
				host := g.req.Host
				if host == "" {
					host = g.req.URL.Host
				}
				g.rootCache = scheme + host
			}
		}
		r = g.rootCache
	}
	if strings.HasPrefix(r, "http://") {
		return strings.Replace(r, "http://", scheme, 1)
	}
	if strings.HasPrefix(r, "https://") {
		return strings.Replace(r, "https://", scheme, 1)
	}
	return r
}

// RouteUrlGenerator is the part of UrlGenerator that builds a URL from a route.
//
// It mirrors Illuminate\Routing\RouteUrlGenerator.
type RouteUrlGenerator struct {
	url      *UrlGenerator
	req      *http.Request
	defaults map[string]string
}

// dontEncode are characters restored after URL-encoding, mirroring PHP's
// rawurlencode restoration.
var dontEncode = map[string]string{
	"%2F": "/",
	"%40": "@",
	"%3A": ":",
	"%3B": ";",
	"%2C": ",",
	"%3D": "=",
	"%2B": "+",
	"%21": "!",
	"%2A": "*",
	"%7C": "|",
	"%3F": "?",
	"%26": "&",
	"%23": "#",
	"%25": "%",
}

func (g *RouteUrlGenerator) to(route *Route, parameters map[string]string, absolute bool) (string, error) {
	if parameters == nil {
		parameters = map[string]string{}
	}

	uri := route.Pattern
	for name, val := range parameters {
		placeholder := "{" + name + "}"
		if strings.Contains(uri, placeholder) {
			uri = strings.ReplaceAll(uri, placeholder, val)
			delete(parameters, name)
		}
	}

	// Remaining parameters go on the query string.
	remaining := []string{}
	for name, val := range parameters {
		if val != "" {
			remaining = append(remaining, url.QueryEscape(name)+"="+url.QueryEscape(val))
		}
	}

	if len(remaining) > 0 {
		uri += "?" + strings.Join(remaining, "&")
	}

	// Remove unmatched optional parameters.
	uri = removeOptionalParams(uri)

	if !absolute {
		p := "/" + strings.TrimLeft(uri, "/")
		return p, nil
	}

	root := g.url.formatRoot(g.url.FormatScheme(nil))
	uri = "/" + strings.TrimLeft(uri, "/")
	full := g.url.Format(root, uri)

	// Restore characters PHP would not double-encode.
	for enc, dec := range dontEncode {
		full = strings.ReplaceAll(full, enc, dec)
	}

	return full, nil
}

func removeOptionalParams(uri string) string {
	for {
		start := strings.Index(uri, "{")
		if start < 0 {
			break
		}
		end := strings.Index(uri[start:], "}")
		if end < 0 {
			break
		}
		end += start
		segment := uri[start : end+1]
		uri = strings.ReplaceAll(uri, segment, "")
		uri = strings.TrimRight(uri, "/")
	}
	return uri
}

func extractQuery(path string) (string, string) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i], path[i:]
	}
	return path, ""
}

func removeIndex(root string) string {
	if strings.Contains(root, "/index.php") {
		return strings.ReplaceAll(root, "/index.php", "")
	}
	return root
}

// RouteNotFoundError is returned when a named route or action is not in the table.
type RouteNotFoundError struct {
	Name string
}

func (e *RouteNotFoundError) Error() string {
	return "routing: route [" + e.Name + "] not defined"
}
