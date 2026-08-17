package middleware

import (
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/view"
)

// ShareErrorsFromSession reads what the request that redirected here failed
// validation on and shares it with every view as "errors", so that a layout
// can draw the messages without every handler passing them along. A handler
// that had to carry them is a handler that can forget to, and forgetting is
// invisible: the form comes back blank.
//
// The errors are read from the framework's per-request state, which the
// session middleware puts on the context when it spends the flash cookie.
// There is no container to read them through; the state is already decoded
// by the time a view runs.
type ShareErrorsFromSession struct {
	// Factory is the view factory the errors are shared with.
	Factory *view.Factory
}

// NewShareErrorsFromSession returns a middleware that shares errors with f.
func NewShareErrorsFromSession(f *view.Factory) *ShareErrorsFromSession {
	return &ShareErrorsFromSession{Factory: f}
}

// Handle shares the validation errors on the request's state with every
// view rendered downstream, as "errors".
//
// The signature is the one every middleware in this framework has --
// func(http.Handler) http.Handler -- because net/http passes the request to
// the handler, not to the middleware.
//
// An empty bag is shared when there are none, so a view can read "errors"
// without asking first.
func (m *ShareErrorsFromSession) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errs := hhttp.StateFrom(r.Context()).Errors
		if errs == nil {
			errs = validation.Errors{}
		}
		m.Factory.Share("errors", errs)
		next.ServeHTTP(w, r)
	})
}
