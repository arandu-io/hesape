package middleware

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/session"
)

// storeKey is the context key the session travels under. A struct{} type, so
// nothing outside this package can collide with it.
type storeKey struct{}

// WithSession is Request::setLaravelSession.
//
// It puts a session on a context, because Go's request has no field to hang one
// on.
//
// It is what [StartSession] does before calling the handler, and what a test
// does to run a handler without the middleware in front of it.
func WithSession(ctx context.Context, s *session.Store) context.Context {
	return context.WithValue(ctx, storeKey{}, s)
}

// Session is Request::session, with Request::hasSession folded into the second
// result.
//
// It returns the session [StartSession] put on the request, and false when there
// is none. The session travels on the context, because Go's request has no
// methods to hang one on, and this is the one way back out of it.
//
// The false case is not hypothetical and must not be ignored: a route mounted
// without this middleware, a request answered by the error handler before it
// ran, and a console binary all reach a handler with no session on the context.
// A caller that dereferences without checking panics on exactly those paths.
func Session(ctx context.Context) (*session.Store, bool) {
	s, ok := ctx.Value(storeKey{}).(*session.Store)
	return s, ok
}

// LockFactory hands out the lock that makes requests of one session wait for
// each other.
//
// It is declared here, minimally, rather than imported: the lock lives in the
// cache and this package must not depend on it. hesape/cache's locks are what an
// application wires behind it.
type LockFactory interface {
	// Lock takes the named lock, waiting up to wait for it, and returns the
	// release. It returns false when the wait ran out, which is a request that
	// must not proceed: proceeding is the lost write the lock exists to stop.
	Lock(ctx context.Context, name string, hold, wait time.Duration) (release func(), ok bool)
}

// StartSession loads the session at the start of a request and writes it back
// at the end.
//
// It is Illuminate\Session\Middleware\StartSession, and it is what makes
// old(), $errors and the CSRF token work: the session it puts on the context is
// what a handler reads with [Session], and the flash it ages on the way out is
// what makes a message survive exactly one redirect.
//
// # The order it does things in, and why
//
// The session cookie and the session record have to be written BEFORE the
// handler writes the response body, because a header set after the first byte
// is a header that never reaches the browser. Illuminate has no such problem --
// PHP buffers the whole response -- so this wraps the ResponseWriter and does
// the work on the first write, or after the handler returns when it wrote
// nothing. That is the one structural difference from the PHP, and it is
// invisible from a handler.
type StartSession struct {
	manager *session.SessionManager
	locks   LockFactory
}

// NewStartSession is StartSession::__construct.
//
// The PHP's second argument is a resolver that fetches the cache factory out of
// the container; this takes the [LockFactory] itself, because there is no
// container to fetch one from (ADR 0001).
//
// locks may be nil. When it is and the configuration asks for blocking,
// [StartSession.Handle] refuses the request rather than serving it unlocked:
// running unprotected is the lost write that blocking was turned on to stop,
// and doing it silently means nobody finds out until a customer reports data
// that reverted.
func NewStartSession(manager *session.SessionManager, locks LockFactory) *StartSession {
	return &StartSession{manager: manager, locks: locks}
}

// Handle is StartSession::handle, with StartSession::handleRequestWhileBlocking
// and StartSession::handleStatefulRequest behind it.
//
// It returns pipeline.Middleware[http.Handler], which is what http.Middleware
// is an alias of -- so this composes with everything else without this package
// importing the HTTP layer.
//
// Two things the PHP reads off the route and this does not: Route::locksFor and
// Route::waitsFor. Blocking is configuration-wide here, so a route that asked
// for a longer hold in Laravel gets the configured one. And a request the PHP
// would have refused to block -- one with no route bound -- is blocked here,
// because the lock is named from the session id and needs nothing else.
func (m *StartSession) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.sessionConfigured() {
			next.ServeHTTP(w, r)
			return
		}

		store, err := m.GetSession(r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if m.manager.ShouldBlock() {
			if m.locks == nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			release, ok := m.locks.Lock(
				r.Context(),
				"session:"+store.GetID(),
				m.manager.DefaultRouteBlockLockSeconds(),
				m.manager.DefaultRouteBlockWaitSeconds(),
			)
			if !ok {
				// 429 and not 500: the request is fine, the session is busy, and
				// the browser retrying is the correct response.
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			defer release()
		}

		m.handleStatefulRequest(w, r, store, next)
	})
}

// handleStatefulRequest is the body of the middleware, once the lock question
// is settled.
func (m *StartSession) handleStatefulRequest(w http.ResponseWriter, r *http.Request, store *session.Store, next http.Handler) {
	store.SetRequestOnHandler(r)
	if err := store.Start(r.Context()); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	m.collectGarbage(r.Context(), store)

	finish := &finisher{
		middleware: m,
		store:      store,
		request:    r,
		writer:     w,
	}
	wrapped := &sessionWriter{ResponseWriter: w, finish: finish}

	next.ServeHTTP(wrapped, r.WithContext(WithSession(r.Context(), store)))

	// The handler may have written nothing at all, in which case net/http is
	// about to write an implicit 200 and the headers are still pending.
	finish.run()
}

// GetSession is StartSession::getSession.
//
// It returns the session for this request, with the id the browser sent on it,
// and it is exported for the reason the PHP's is public: a test and a handler
// outside the pipeline need the same session the middleware would have built.
func (m *StartSession) GetSession(r *http.Request) (*session.Store, error) {
	store, err := m.manager.Driver("")
	if err != nil {
		return nil, err
	}
	if c, err := r.Cookie(store.GetName()); err == nil {
		// A cookie that is not a session id is replaced with a fresh one by
		// SetID, so a forged value starts an empty session rather than an error.
		store.SetID(c.Value)
	}
	return store, nil
}

// collectGarbage asks the handler to sweep expired sessions, on the odds the
// configuration names.
//
// A lottery and not a schedule, which is Illuminate's design and is worth
// keeping: it needs no scheduler, it spreads the cost over every request rather
// than spiking one, and the odds are the only knob. A zero denominator turns it
// off, which is right for a handler whose store expires entries itself.
func (m *StartSession) collectGarbage(ctx context.Context, store *session.Store) {
	cfg := m.manager.GetSessionConfig()
	if cfg.Lottery[1] <= 0 || cfg.Lottery[0] <= 0 {
		return
	}
	if rand.IntN(cfg.Lottery[1])+1 > cfg.Lottery[0] {
		return
	}
	// A sweep that fails is not this request's problem: the person is served,
	// and the next request that wins the lottery tries again.
	_, _ = store.GetHandler().GC(ctx, cfg.Lifetime)
}

// sessionConfigured reports whether there is a driver to start a session on.
func (m *StartSession) sessionConfigured() bool {
	return m.manager.GetDefaultDriver() != ""
}

// finisher does the three things that must happen before the body: store the
// address to come back to, write the session, and set the cookie.
type finisher struct {
	once       sync.Once
	middleware *StartSession
	store      *session.Store
	request    *http.Request
	writer     http.ResponseWriter
}

// run does the work exactly once, whichever of the two callers gets there
// first.
func (f *finisher) run() {
	f.once.Do(func() {
		f.middleware.storeCurrentURL(f.request, f.store)
		f.middleware.addCookieToResponse(f.writer, f.store)
		// The save is last because ageing the flash is part of it, and the
		// address stored above has to survive into the next request.
		_ = f.store.Save(f.request.Context())
	})
}

// storeCurrentURL remembers where somebody was, so a guard can send them back
// after they sign in.
//
// Only a GET that is a whole page: a POST is not somewhere to return to, and
// neither is the stylesheet or the HTMX fragment that the page fires on
// arrival. Remembering one of those is a sign-in that lands on a fragment.
//
// What is stored is the whole address, which is $request->fullUrl() in the PHP.
// It used to be r.URL.RequestURI() -- path and query and nothing else -- so a
// redirect built from it lost the scheme and the host. On one host that reads
// the same; across two, or behind a redirect the browser resolves against
// whatever it is currently on, it is a sign-in that lands somewhere else, and
// http:// is what a scheme-less address downgrades to.
func (m *StartSession) storeCurrentURL(r *http.Request, store *session.Store) {
	if r.Method != http.MethodGet {
		return
	}
	if r.Header.Get("HX-Request") != "" || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return
	}
	store.SetPreviousURL(fullURL(r))
}

// fullURL is Request::fullUrl: scheme, host, path and query.
//
// The scheme is read off the connection and never off X-Forwarded-Proto. A
// header a client sets is a value a client controls, and this one ends up in a
// redirect the browser follows after a sign-in -- so trusting it hands whoever
// sends the request the choice of where the session lands. A deployment behind a
// terminating proxy sets the scheme where the proxy is trusted, which is not
// here.
func fullURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

// addCookieToResponse writes the cookie carrying the session id.
func (m *StartSession) addCookieToResponse(w http.ResponseWriter, store *session.Store) {
	cfg := m.manager.GetSessionConfig()
	maxAge := int(cfg.Lifetime.Seconds())
	if cfg.ExpireOnClose {
		// No Max-Age and no Expires: the browser drops it when it closes.
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:        store.GetName(),
		Value:       store.GetID(),
		Path:        cfg.Path,
		Domain:      cfg.Domain,
		MaxAge:      maxAge,
		Secure:      cfg.Secure,
		HttpOnly:    cfg.HTTPOnly,
		SameSite:    cfg.SameSite,
		Partitioned: cfg.Partitioned,
	})
}

// sessionWriter finishes the session on the first write, because that is the
// last moment a header still reaches the browser.
type sessionWriter struct {
	http.ResponseWriter
	finish *finisher
}

// WriteHeader finishes the session and then sends the status.
func (w *sessionWriter) WriteHeader(status int) {
	w.finish.run()
	w.ResponseWriter.WriteHeader(status)
}

// Write finishes the session and then sends the bytes.
func (w *sessionWriter) Write(b []byte) (int, error) {
	w.finish.run()
	return w.ResponseWriter.Write(b)
}

// Flush passes a flush through, so a streaming handler keeps working. The
// session is finished first for the same reason Write does it.
func (w *sessionWriter) Flush() {
	w.finish.run()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap gives net/http's ResponseController the writer underneath, so
// hijacking and deadline control keep working through this wrapper.
func (w *sessionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
