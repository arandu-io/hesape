package cookie

import (
	"net/http"
	"sync"
	"time"

	"github.com/arandu-io/hesape/support"
)

// foreverMinutes is the 576000 CookieJar::forever() passes to make(): 400 days,
// the longest expiry a browser will keep since Chrome 104 clamped it.
const foreverMinutes = 576000

// forgetMinutes is the -2628000 CookieJar::forget() passes to make(): five years
// in the past, which is how the class expires a cookie.
const forgetMinutes = -2628000

// CookieJar answers to Illuminate\Cookie\CookieJar.
//
// It builds cookies with the application's defaults filled in, and it holds a
// queue: a handler that wants to set a cookie calls [CookieJar.Queue] and never
// touches the http.ResponseWriter, and the middleware in cookie/middleware
// writes the queue onto the response on its way out. That indirection is the
// point of the component -- whoever writes the cookie does not need the
// response in hand.
//
// The cookie built is a *net/http.Cookie, not a type of this package's own.
// Illuminate returns a Symfony Cookie because PHP has no cookie in its standard
// library; Go does, and every handler, test and middleware already speaks it.
// Illuminate's constructor arguments land on it like this:
//
//	$name      -> Name
//	$value     -> Value
//	$minutes   -> Expires and MaxAge together (see [CookieJar.Make])
//	$path      -> Path
//	$domain    -> Domain
//	$secure    -> Secure
//	$httpOnly  -> HttpOnly
//	$sameSite  -> SameSite
//	$raw       -> nothing; see [CookieJar.Make]
//
// A CookieJar is safe for use from several goroutines, which the PHP class has
// no need to be. The queue is still per request: the middleware calls
// [CookieJar.Clone] once per request and puts the copy in the context, because
// one process here serves many requests at once and one process there served
// exactly one.
type CookieJar struct {
	mu sync.Mutex

	// path is the $path property, "/" out of the constructor.
	path string
	// domain is the $domain property, null out of the constructor.
	domain string
	// secure is the $secure property, which the PHP docblock describes as
	// "defaults to null" -- unset, distinct from false. A nil pointer is that
	// null, and [CookieJar.Make] falls back to it only when its own argument
	// is nil too.
	secure *bool
	// sameSite is the $sameSite property, 'lax' out of the constructor.
	sameSite http.SameSite

	// queued is the $queued property: cookies keyed by name and then by path.
	// names and the paths inside each entry carry the insertion order PHP gets
	// from its arrays for free, which queued() and getQueuedCookies() read.
	queued map[string]*pathQueue
	names  []string
}

// pathQueue is one entry of $queued: the cookies for a single name, keyed by
// path, in the order the paths were first queued.
type pathQueue struct {
	byPath map[string]*http.Cookie
	paths  []string
}

// NewCookieJar answers to CookieJar's property initializers: path "/", no
// domain, secure unset, SameSite lax, and an empty queue.
//
// PHP writes no constructor for this class, so there is nothing to mirror by
// name. Go has no property initializers, so the defaults live here.
func NewCookieJar() *CookieJar {
	return &CookieJar{
		path:     "/",
		sameSite: http.SameSiteLaxMode,
		queued:   map[string]*pathQueue{},
	}
}

// Clone answers to PHP's clone on a CookieJar: a shallow copy, carrying the
// same defaults and the cookies queued so far.
//
// The middleware calls it once per request. In PHP the jar is a singleton and
// the process dies with the response, so its queue is a per-request queue by
// accident of the runtime; here the process outlives the request and a shared
// queue would hand one visitor's cookie to the next.
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

// Make answers to CookieJar::make(). It builds a cookie with the jar's defaults
// filled in wherever the call left an argument unset. It does not queue it.
//
// PHP gives every argument after $value a default; Go has none, so a call
// passes all nine. The unset values are spelled the way PHP spells null:
//
//   - path "" and domain "" fall back to the jar, exactly as PHP's `?:` does,
//     where an empty string is falsy and lands on the default too
//   - secure nil falls back to the jar, and a non-nil pointer wins even when it
//     points at false -- PHP's is_bool($secure) check, which is why the argument
//     is a pointer and not a bool
//   - sameSite http.SameSiteDefaultMode, the zero value, falls back to the jar
//
// $minutes becomes two fields. Zero is a session cookie: no Expires, no MaxAge,
// which is the 0 expiry Symfony reads as "until the browser closes". Anything
// else is [support.Now] plus that many minutes in Expires -- the availableAt()
// of InteractsWithTime, so a test that freezes the clock sees a fixed date --
// and the same span in seconds in MaxAge. Both are written, as Symfony writes
// both. A negative count leaves MaxAge negative, which net/http renders as
// Max-Age=0, the same header Symfony emits when it clamps a past expiry.
//
// $raw becomes no field, and this is the one argument that does not survive.
// Symfony percent-encodes a cookie value unless $raw says not to; net/http
// never percent-encodes one, it writes the value through and quotes it only
// when it holds a space or a comma. So a cookie made here is always raw in
// Symfony's sense. The argument stays in its position so the call reads like
// the PHP one, and so that the day this package needs to speak to a PHP
// application's encoding there is a place to put it.
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

// Forever answers to CookieJar::forever(). It is [CookieJar.Make] with 576000
// minutes, the 400 days the PHP docblock calls "forever".
func (j *CookieJar) Forever(name, value, path, domain string, secure *bool, httpOnly, raw bool, sameSite http.SameSite) *http.Cookie {
	return j.Make(name, value, foreverMinutes, path, domain, secure, httpOnly, raw, sameSite)
}

// Forget answers to CookieJar::forget(). It builds an empty cookie dated five
// years ago, which is how the browser is told to drop the one it has.
//
// It only takes name, path and domain, as PHP does: everything else is the
// default, so a cookie made with non-default secure or SameSite settings is
// forgotten by a header that does not match it. That is the PHP behaviour, kept
// on purpose -- deleting a cookie the browser will not match is a bug worth
// finding in the same place a Laravel developer already knows to look.
//
// It does not queue. CookieJar::expire() is the one that queues.
func (j *CookieJar) Forget(name, path, domain string) *http.Cookie {
	return j.Make(name, "", forgetMinutes, path, domain, nil, true, false, http.SameSiteDefaultMode)
}

// Queue answers to CookieJar::queue(). The cookie is sent with the next
// response, by the AddQueuedCookiesToResponse middleware.
//
// PHP takes a variadic that is either a Cookie or the argument list of make().
// Go has no such dispatch, and the second form is written by nesting the calls:
//
//	jar.Queue(jar.Make("name", "value", 60, "", "", nil, true, false, 0))
//
// A cookie already queued under the same name and path is replaced, and keeps
// the position it had, which is what assigning into a PHP array does. A nil
// cookie is ignored rather than panicking.
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

// Expire answers to CookieJar::expire(). It queues the cookie [CookieJar.Forget]
// builds, so the browser drops it when the response goes out.
func (j *CookieJar) Expire(name, path, domain string) {
	j.Queue(j.Forget(name, path, domain))
}

// Unqueue answers to CookieJar::unqueue(). It takes a cookie back off the queue
// before the response is written.
//
// An empty path is PHP's null: every path queued under that name goes. A path
// that is not queued is not an error, as unset() on a missing key is not. When
// the last path under a name goes, so does the name, which is the empty() check
// in the PHP.
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

// HasQueued answers to CookieJar::hasQueued(). An empty path asks about any
// path, the way PHP's null does.
func (j *CookieJar) HasQueued(key, path string) bool {
	return j.Queued(key, nil, path) != nil
}

// Queued answers to CookieJar::queued(). It returns the queued cookie, or def
// when there is none.
//
// PHP's $default is mixed and this one is a *http.Cookie, because every caller
// in the framework passes null and a caller that wants something else can write
// the comparison. Passing nil is that null.
//
// An empty path is PHP's null and returns the cookie queued most recently under
// that name -- Arr::last over the paths. Re-queueing an existing path does not
// make it the most recent, because reassigning a PHP array key does not move it
// to the end either.
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

// GetQueuedCookies answers to CookieJar::getQueuedCookies(): every queued
// cookie, flattened out of the name-then-path nesting.
//
// The order is the order they were first queued, name by name and then path by
// path, which is what Arr::flatten gives PHP. It matters: two Set-Cookie headers
// for the same name and different paths are both sent, and the browser keeps
// both, so the order is what a test can assert on.
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

// FlushQueuedCookies answers to CookieJar::flushQueuedCookies(). It empties the
// queue and returns the jar, as the PHP returns $this.
func (j *CookieJar) FlushQueuedCookies() *CookieJar {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.queued = map[string]*pathQueue{}
	j.names = nil
	return j
}

// SetDefaultPathAndDomain answers to CookieJar::setDefaultPathAndDomain(). It
// sets what [CookieJar.Make] fills in for a call that leaves an argument unset,
// and returns the jar, as the PHP returns $this.
//
// The assignment is direct, as it is in PHP: passing "" for path clears the "/"
// the jar started with rather than keeping it, and passing
// http.SameSiteDefaultMode clears the lax default, which is PHP's null. secure
// is a bool and not a pointer here because the PHP signature defaults it to
// false rather than to null -- this is the call that decides, so there is
// nothing left to fall back to.
func (j *CookieJar) SetDefaultPathAndDomain(path, domain string, secure bool, sameSite http.SameSite) *CookieJar {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.path, j.domain, j.secure, j.sameSite = path, domain, &secure, sameSite
	return j
}

// getPathAndDomain answers to CookieJar::getPathAndDomain(), the protected
// method every builder goes through: the argument if it says something, the
// jar's default if it does not.
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
