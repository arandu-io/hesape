package middleware

import (
	"net/http"

	hesapehttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/view"
)

// ShareErrorsFromSession answers to
// Illuminate\View\Middleware\ShareErrorsFromSession.
//
// It reads what the request that redirected here failed validation on and
// shares it with every view as "errors", so that a layout can draw the messages
// without every handler passing them along. A handler that had to carry them is
// a handler that can forget to, and forgetting is invisible: the form comes back
// blank.
//
// The errors are read from the framework's per-request state, which the session
// middleware puts on the context when it spends the flash cookie. The PHP reads
// them off the session store through the container; there is no container
// (ADR 0001), and the state is already decoded by the time a view runs.
type ShareErrorsFromSession struct {
	// Factory is the view factory the errors are shared with. It answers to
	// $view in the PHP, which is the Factory the container hands the middleware.
	Factory *view.Factory
}

// NewShareErrorsFromSession answers to ShareErrorsFromSession::__construct.
func NewShareErrorsFromSession(f *view.Factory) *ShareErrorsFromSession {
	return &ShareErrorsFromSession{Factory: f}
}

// Handle answers to ShareErrorsFromSession::handle.
//
// The signature is the one every middleware in this framework has --
// func(http.Handler) http.Handler -- rather than PHP's ($request, $next),
// because net/http passes the request to the handler and not to the middleware.
//
// An empty bag is shared when there are none, which is what the PHP's
// `new ViewErrorBag` does: a view can read "errors" without asking first.
func (m *ShareErrorsFromSession) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errs := hesapehttp.StateFrom(r.Context()).Errors
		if errs == nil {
			errs = validation.Errors{}
		}
		m.Factory.Share("errors", errs)
		next.ServeHTTP(w, r)
	})
}
