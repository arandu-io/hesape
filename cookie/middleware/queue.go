package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/cookie"
)

// AddQueuedCookiesToResponse writes the cookies a handler queued on the jar
// onto the response, so that a handler never has to hold the
// http.ResponseWriter to set one.
//
// It is a struct with a Handle method, and Handle has the shape
// pipeline.Middleware[http.Handler] wants, so the method value is the
// middleware:
//
//	queue := middleware.NewAddQueuedCookiesToResponse(jar)
//	r.Get("/", h, queue.Handle)
type AddQueuedCookiesToResponse struct {
	// cookies is the jar this middleware drains onto the response.
	cookies *cookie.CookieJar
}

// NewAddQueuedCookiesToResponse returns a middleware that flushes cookies
// queued on cookies onto the response.
func NewAddQueuedCookiesToResponse(cookies *cookie.CookieJar) *AddQueuedCookiesToResponse {
	return &AddQueuedCookiesToResponse{cookies: cookies}
}

// Handle drains the jar's queue onto the response after next runs.
//
// Go writes the response as the handler runs, so a middleware that waited
// for the handler to return would be adding headers that already went out.
// Handle therefore takes the next handler and returns one, which is what
// every middleware in this framework does, and it hangs the queue on the
// point of no return: the first WriteHeader or Write, or the handler
// returning without either.
//
// It clones the jar for this request and puts the clone in the context, so
// the queue a handler fills is the queue this response drains. See
// [cookie.WithCookieJar].
//
// A queued cookie replaces one the handler already set under the same name,
// path and domain, rather than joining it: two Set-Cookie headers for the
// same cookie leave the browser to pick, and which one it picks is not
// something to build on.
func (m *AddQueuedCookiesToResponse) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jar := m.cookies.Clone()
		qw := &queueWriter{ResponseWriter: w, jar: jar}
		next.ServeHTTP(qw, r.WithContext(cookie.WithCookieJar(r.Context(), jar)))
		qw.setCookies()
	})
}

// queueWriter is the response writer AddQueuedCookiesToResponse wraps around
// the real one so the queue lands before the header goes out.
type queueWriter struct {
	http.ResponseWriter
	jar  *cookie.CookieJar
	done bool
}

// setCookies writes every queued cookie onto the response. It runs once,
// whichever of the three ways to finish a response happens first.
func (w *queueWriter) setCookies() {
	if w.done {
		return
	}
	w.done = true

	for _, c := range w.jar.GetQueuedCookies() {
		setCookie(w.Header(), c)
	}
}

func (w *queueWriter) WriteHeader(status int) {
	w.setCookies()
	w.ResponseWriter.WriteHeader(status)
}

func (w *queueWriter) Write(b []byte) (int, error) {
	w.setCookies()
	return w.ResponseWriter.Write(b)
}

// Unwrap gives http.ResponseController the writer underneath, so hijacking and
// the deadline calls still reach it.
func (w *queueWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// FlushError drains the queue and then flushes the writer underneath.
//
// It is not optional here. A flush commits the header, so it is the third
// way to reach the point of no return alongside WriteHeader and Write. Without
// this method http.ResponseController would walk [queueWriter.Unwrap] down to
// the real writer and flush that one, and the queued cookies would be added to
// a header that had already gone out.
//
// It is FlushError and not Flush because that is what http.ResponseController
// looks for first, and because the error is worth passing on: a writer that
// cannot flush says so, where a Flush method would have to swallow it. A
// handler that reaches for http.Flusher by type assertion instead finds nothing
// here, as it did before -- that costs it the flush, but nothing is committed,
// so the cookies still go out.
func (w *queueWriter) FlushError() error {
	w.setCookies()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// setCookie writes c onto h: it replaces any cookie already on the response
// with the same name, path and domain, and is appended otherwise.
func setCookie(h http.Header, c *http.Cookie) {
	line := c.String()
	if line == "" {
		// net/http refuses to render a cookie whose name or value holds a byte
		// that cannot go in a header. Adding "" would emit an empty Set-Cookie.
		return
	}

	existing := h.Values("Set-Cookie")
	kept := make([]string, 0, len(existing)+1)
	for _, old := range existing {
		if parsed, err := http.ParseSetCookie(old); err == nil &&
			parsed.Name == c.Name && parsed.Path == c.Path && parsed.Domain == c.Domain {
			continue
		}
		kept = append(kept, old)
	}
	h["Set-Cookie"] = append(kept, line)
}
