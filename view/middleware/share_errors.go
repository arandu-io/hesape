package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/view"
)

// ShareErrorsFromSession mirrors Illuminate\View\Middleware\ShareErrorsFromSession.
//
// It reads validation errors from the session flash and shares them with every
// view via Factory.Share("errors", ...). The middleware must run before any
// route handler that renders views, so that error bags are always available to
// the layout without every controller passing them.
//
// In Arandu, the errors are read from the httpx.State on the context (set by
// the session middleware), not from a session interface directly. This is the
// same mechanism that view.New uses to fill Page.Errors.
type ShareErrorsFromSession struct {
	Factory *view.Factory
}

// NewShareErrorsFromSession returns a middleware that shares errors with all views.
func NewShareErrorsFromSession(f *view.Factory) *ShareErrorsFromSession {
	return &ShareErrorsFromSession{Factory: f}
}

// Middleware returns an HTTP middleware handler.
//
// Usage:
//
//	router.Use(middleware.NewShareErrorsFromSession(factory).Middleware())
func (m *ShareErrorsFromSession) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if errs, ok := errorsFromContext(r.Context()); ok {
				m.Factory.Share("errors", errs)
			} else {
				// Always share an empty bag so views can unconditionally check.
				m.Factory.Share("errors", validation.Errors{})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// errorsFromContext extracts the validation errors from the context state.
func errorsFromContext(ctx interface{ Value(any) any }) (validation.Errors, bool) {
	// The state is stored via httpx.WithState and accessed via httpx.StateFrom.
	// We import the type directly to avoid a circular dependency.
	type stateKeyType struct{}
	stateVal := ctx.Value(stateKeyType{})
	if stateVal == nil {
		return nil, false
	}
	type state struct {
		Errors validation.Errors
	}
	if s, ok := stateVal.(state); ok {
		return s.Errors, len(s.Errors) > 0
	}
	return nil, false
}
