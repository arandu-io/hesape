package cookie

import (
	"net/http"
	"sync"
	"time"

	"github.com/arandu-io/hesape/support"
)

// foreverMinutes is 576000, the minutes [CookieJar.Forever] passes to
// [CookieJar.Make]: 400 days, the longest expiry a browser will keep since
// Chrome 104 clamped it.
const foreverMinutes = 576000

// forgetMinutes is -2628000, the minutes [CookieJar.Forget] passes to
// [CookieJar.Make]: five years in the past, which is how a cookie is
// expired.
const forgetMinutes = -2628000

// CookieJar holds the application's default path, domain, secure and
// SameSite settings, and a queue of cookies waiting to be written onto a
// response.
//
// A handler that wants to set a cookie calls [CookieJar.Queue] and never
// touches the http.ResponseWriter; the middleware in cookie/middleware
// writes the queue onto the response on its way out. That indirection is the
// point of the component -- whoever writes the cookie does not need the
// response in hand.
//
// The cookie built is a *net/http.Cookie, not a type of this package's own:
// net/http already defines one, and every handler, test and middleware
// already speaks it.
//
// A CookieJar is safe for use from several goroutines. The queue is still
// per request: the middleware calls [CookieJar.Clone] once per request and
// puts the copy in the context, because one process here serves many
// requests at once.
type CookieJar struct {
	mu sync.Mutex

	// path is the default cookie path, "/" out of the constructor.
	path string
	// domain is the default cookie domain, empty out of the constructor.
	domain string
	// secure is the default secure flag: nil means unset, distinct from a
	// false that was set explicitly. [CookieJar.Make] falls back to it only
	// when its own argument is nil too.
	secure *bool
	// sameSite is the default SameSite mode, lax out of the constructor.
	sameSite http.SameSite

	// queued is every cookie waiting to be written, keyed by name and then by
	// path. names and the paths inside each pathQueue track insertion order
	// explicitly, since a Go map does not, and [CookieJar.GetQueuedCookies]
	// reads it back in that order.
	queued map[string]*pathQueue
	names  []string
}

// pathQueue is one entry of queued: the cookies for a single name, keyed by
// path, in the order the paths were first queued.
type pathQueue struct {
	byPath map[string]*http.Cookie
	paths  []string
}

// NewCookieJar returns a jar with the default path "/", no domain, secure
// unset, SameSite lax, and an empty queue.
func NewCookieJar() *CookieJar {
	return &CookieJar{
		path:     "/",
		sameSite: http.SameSiteLaxMode,
		queued:   map[string]*pathQueue{},
	}
}

// Clone returns a shallow copy of the jar, carrying the same defaults and
// the cookies queued so far.
//
// The middleware calls it once per request: the process outlives the
// request, and a shared queue would hand one visitor's cookie to the next.
func (j *CookieJar) Clone() *CookieJar {
	j.mu.Lock()
	defer j.mu.Unlock()

	c := &CookieJar{
		path:     j.path,
		domain:   j.domain,
		sameSite: j.sameSite,
		queued:   make(map[string]*pathQueue, len(j.queued)),
		names:    append([]string(nil), j.names...),
	}
	if j.secure != nil {
		secure := *j.secure
		c.secure = &secure
	}
	for name, q := range j.queued {
		copied := &pathQueue{
			byPath: make(map[string]*http.Cookie, len(q.byPath)),
			paths:  append([]string(nil), q.paths...),
		}
		for path, cookie := range q.byPath {
			copied.byPath[path] = cookie
		}
		c.queued[name] = copied
	}
	return c
}

// Make builds a cookie with the jar's defaults filled in wherever the call
// left an argument unset. It does not queue it.
//
// Go has no default arguments, so a call passes all nine positions
// explicitly. Each parameter has its own spelling for "unset":
//
//   - path "" and domain "" fall back to the jar's default
//   - secure nil falls back to the jar, and a non-nil pointer wins even when
//     it points at false -- which is why the argument is a pointer and not a
//     bool
//   - sameSite http.SameSiteDefaultMode, the zero value, falls back to the jar
//
// minutes becomes two fields. Zero is a session cookie: no Expires and no
// MaxAge are written, which a browser keeps only until it closes. Anything
// else is [support.Now] plus that many minutes in Expires, so a test that
// freezes the clock sees a fixed date, and the same span in seconds in
// MaxAge. A negative count leaves MaxAge negative, which net/http renders as
// Max-Age=0, clamping the cookie to an immediate expiry.
//
// raw becomes no field: net/http never percent-encodes a cookie value, and
// quotes it only when it holds a space or a comma, so there is nothing for
// the argument to switch. It stays in the signature, accepted and ignored.
func (j *CookieJar) Make(name, value string, minutes int, path, domain string, secure *bool, httpOnly, raw bool, sameSite http.SameSite) *http.Cookie {
	_ = raw

	path, domain, isSecure, site := j.getPathAndDomain(path, domain, secure, sameSite)

	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Domain:   domain,
		Secure:   isSecure,
		HttpOnly: httpOnly,
		SameSite: site,
	}
	if minutes != 0 {
		c.Expires = support.Now().Add(time.Duration(minutes) * time.Minute)
		c.MaxAge = minutes * 60
	}
	return c
}

// Forever is [CookieJar.Make] with foreverMinutes.
func (j *CookieJar) Forever(name, value, path, domain string, secure *bool, httpOnly, raw bool, sameSite http.SameSite) *http.Cookie {
	return j.Make(name, value, foreverMinutes, path, domain, secure, httpOnly, raw, sameSite)
}

// Forget builds an empty cookie dated five years ago, which is how the
// browser is told to drop the one it has.
//
// It only takes name, path and domain: everything else is the default, so a
// cookie made with non-default secure or SameSite settings is forgotten by a
// header that does not match it. That mismatch is deliberate: deleting a
// cookie the browser will not match is a bug worth surfacing, not one this
// method should silently paper over.
//
// It does not queue. [CookieJar.Expire] is the one that queues.
func (j *CookieJar) Forget(name, path, domain string) *http.Cookie {
	return j.Make(name, "", forgetMinutes, path, domain, nil, true, false, http.SameSiteDefaultMode)
}

// Queue schedules cookie to be sent with the next response, by the
// AddQueuedCookiesToResponse middleware. A cookie built with
// [CookieJar.Make] can be queued in the same call:
//
//	jar.Queue(jar.Make("name", "value", 60, "", "", nil, true, false, 0))
//
// A cookie already queued under the same name and path is replaced in
// place, keeping the position it had. A nil cookie is ignored rather than
// panicking.
func (j *CookieJar) Queue(cookie *http.Cookie) {
	if cookie == nil {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if j.queued == nil {
		j.queued = map[string]*pathQueue{}
	}
	q, ok := j.queued[cookie.Name]
	if !ok {
		q = &pathQueue{byPath: map[string]*http.Cookie{}}
		j.queued[cookie.Name] = q
		j.names = append(j.names, cookie.Name)
	}
	if _, ok := q.byPath[cookie.Path]; !ok {
		q.paths = append(q.paths, cookie.Path)
	}
	q.byPath[cookie.Path] = cookie
}

// Expire queues the cookie [CookieJar.Forget] builds, so the browser drops
// it when the response goes out.
func (j *CookieJar) Expire(name, path, domain string) {
	j.Queue(j.Forget(name, path, domain))
}

// Unqueue takes a cookie back off the queue before the response is written.
//
// An empty path means every path queued under that name goes. A path that
// is not queued is not an error. When the last path under a name goes, so
// does the name.
func (j *CookieJar) Unqueue(name, path string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	q, ok := j.queued[name]
	if !ok {
		return
	}
	if path == "" {
		j.dropName(name)
		return
	}
	if _, ok := q.byPath[path]; !ok {
		return
	}
	delete(q.byPath, path)
	q.paths = without(q.paths, path)
	if len(q.byPath) == 0 {
		j.dropName(name)
	}
}

// dropName removes a name from the queue. The caller holds the lock.
func (j *CookieJar) dropName(name string) {
	delete(j.queued, name)
	j.names = without(j.names, name)
}

// HasQueued reports whether a cookie is queued under key. An empty path asks
// about any path.
func (j *CookieJar) HasQueued(key, path string) bool {
	return j.Queued(key, nil, path) != nil
}

// Queued returns the queued cookie, or def when there is none.
//
// def is a *http.Cookie rather than an any, because every caller in this
// framework passes nil for it; a caller that wants something else can write
// the comparison itself.
//
// An empty path returns the cookie queued most recently under that name --
// the last path in insertion order. Re-queueing an existing path does not
// move it to the end: only the first time a path is queued affects the
// order.
func (j *CookieJar) Queued(key string, def *http.Cookie, path string) *http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	q, ok := j.queued[key]
	if !ok || len(q.paths) == 0 {
		return def
	}
	if path == "" {
		return q.byPath[q.paths[len(q.paths)-1]]
	}
	if c, ok := q.byPath[path]; ok {
		return c
	}
	return def
}

// GetQueuedCookies returns every queued cookie, flattened out of the
// name-then-path nesting.
//
// The order is the order they were first queued, name by name and then path
// by path. It matters: two Set-Cookie headers for the same name and
// different paths are both sent, and the browser keeps both, so the order
// is what a test can assert on.
func (j *CookieJar) GetQueuedCookies() []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	out := make([]*http.Cookie, 0, len(j.queued))
	for _, name := range j.names {
		q := j.queued[name]
		for _, path := range q.paths {
			out = append(out, q.byPath[path])
		}
	}
	return out
}

// FlushQueuedCookies empties the queue and returns the jar, for chaining.
func (j *CookieJar) FlushQueuedCookies() *CookieJar {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.queued = map[string]*pathQueue{}
	j.names = nil
	return j
}

// SetDefaultPathAndDomain sets what [CookieJar.Make] fills in for a call
// that leaves an argument unset, and returns the jar, for chaining.
//
// The assignment is direct: passing "" for path clears the "/" the jar
// started with rather than keeping it, and passing http.SameSiteDefaultMode
// clears the lax default. secure is a bool and not a pointer here, because
// this is the call that decides the default -- there is nothing left to
// fall back to.
func (j *CookieJar) SetDefaultPathAndDomain(path, domain string, secure bool, sameSite http.SameSite) *CookieJar {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.path, j.domain, j.secure, j.sameSite = path, domain, &secure, sameSite
	return j
}

// getPathAndDomain is the method every builder goes through: the argument
// if it says something, the jar's default if it does not.
func (j *CookieJar) getPathAndDomain(path, domain string, secure *bool, sameSite http.SameSite) (string, string, bool, http.SameSite) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if path == "" {
		path = j.path
	}
	if domain == "" {
		domain = j.domain
	}
	isSecure := false
	switch {
	case secure != nil:
		isSecure = *secure
	case j.secure != nil:
		isSecure = *j.secure
	}
	if sameSite == http.SameSiteDefaultMode {
		sameSite = j.sameSite
	}
	return path, domain, isSecure, sameSite
}

// without returns list with the first occurrence of value removed.
func without(list []string, value string) []string {
	for i, v := range list {
		if v == value {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}
