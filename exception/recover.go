package exception

import (
	"net/http"

	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/pipeline"
)

// Recover captures panics and hands them to the Handler.
//
// Order matters and is not a matter of taste: Recover must be the outermost
// middleware, or a panic raised in any other middleware escapes without a page;
// the observability middleware must come right after it, because everything
// below depends on the context it builds.
//
// In development the Handler draws the full debug page -- stack, request,
// queries, dumps. Anywhere else it draws the status page, which leaks nothing
// and carries the request id so the operator can correlate it with the
// structured log.
//
// It returns a pipeline.Middleware[http.Handler] rather than naming httpx's
// alias, which is what lets this package produce middleware without importing
// the routing layer that will call it.
func Recover(h *Handler) pipeline.Middleware[http.Handler] {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Being outermost has a consequence: the Collector is created
			// further in, so the context here is the one from before it existed.
			// The slot is the reference the panic path reads it through. It is
			// installed in development only, which is also the only place the
			// debug page may exist.
			if h.cfg.Dev {
				r = r.WithContext(log.WithCollectorSlot(r.Context()))
			}

			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// A client that hung up mid-response is not an application bug,
				// and net/http expects this value to keep unwinding.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				// Register was told the environment is "testing", where a
				// process is not meant to be protected from itself: the panic
				// travels up to the test that provoked it instead of being
				// drawn as a page nobody is looking at. It is the same
				// exception the PHP makes for "testing" when it decides not to
				// install the shutdown handler.
				if h.runningInTests() {
					panic(v)
				}

				// Captured here and nowhere else: this is the only place where
				// the line that panicked is still on the stack. skip is 4 --
				// runtime.Callers, Capture, this closure, runtime.gopanic --
				// so the first frame is the one that failed.
				frames := Capture(4, h.cfg.AppModule)

				ctx := r.Context()

				if log.IsDumpDie(v) {
					if h.cfg.Dev {
						h.renderDump(w, r)
						return
					}
					// DumpDie is a no-op outside development, so arriving here
					// means the sentinel came from somewhere else.
					log.For(ctx).Error("dump-and-die sentinel outside development")
				}

				// Always logged, and logged here rather than through Report: a
				// panic is news whatever it turns out to classify as, and
				// DontReport is about errors an application chose to return.
				log.For(ctx).Error("recovered panic", "panic", v)

				err, _ := v.(error)
				h.answer(w, r, v, err, frames)
			}()

			next.ServeHTTP(w, r)
		})
	}
}
