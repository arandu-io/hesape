package middleware

import (
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
)

// LimitBodySize refuses a request body larger than max bytes.
//
// Without it a request body is whatever the client sends, and net/http will read
// all of it: one POST is enough to take a process down, and it needs no
// credentials, no route that does anything and no bug to exploit. That is why it
// is a middleware over the whole server and not a check inside the handlers that
// happen to accept an upload.
//
// A request that declares a Content-Length above max is answered 413 here, and
// the handler never runs. Every other body is wrapped in http.MaxBytesReader,
// which stops reading at the limit and tells the connection not to bother with
// the rest: a length is a header the client wrote, and a chunked body does not
// carry one at all, so the reader is what enforces the limit. A read that meets
// it fails with *http.MaxBytesError, which the request readers in hesape/http --
// Context.Bind and Request.Validate -- answer as a 413 of their own.
//
// max is in bytes and there is no default: a limit that defaults to something is
// a limit nobody notices is wrong, and the right number is a property of what an
// application accepts. An upload route that needs more takes a bigger one -- the
// middleware is applied per group, like every other.
func LimitBodySize(max int64) hhttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > max {
				// The body is not read, so the connection cannot carry another
				// request after this answer.
				w.Header().Set("Connection", "close")
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
