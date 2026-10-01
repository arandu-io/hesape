package http

import (
	"context"
	"net/url"

	"github.com/arandu-io/hesape/validation"
)

// State is what the framework knows about a request before the handler runs.
//
// Today it is one thing: what the request that redirected here failed on. It is
// a struct rather than that one thing so the next piece of per-request framework
// state -- there will be one -- does not add a second context key, a second
// middleware and a second accessor that every page has to be taught about.
//
// It is put on the request context by the middleware that spends the flash
// cookie, in hesape/session, and read by view.New. Nothing else writes it, and
// no handler ever has to: a handler that had to carry the errors from the
// middleware to the page would be a handler that can forget to, and forgetting
// is invisible -- the form comes back blank, exactly as it did before any of
// this existed.
type State struct {
	// Errors is what the request that redirected here failed validation on,
	// keyed by the name of the form input.
	Errors validation.Errors
	// Old is what was typed on that request, minus every password field. See
	// session.Flash for what "every password field" means and why the messages
	// for those fields survive when their values do not.
	Old url.Values
}

// stateKey is the context key, of an unexported type, so that nothing outside
// this package can collide with it or overwrite it.
type stateKey struct{}

// WithState returns a context carrying the framework's per-request state.
//
// The session middleware calls it. It is exported for that, and for the test
// that drives a request past the middleware, and for nothing else.
func WithState(parent context.Context, s State) context.Context {
	return context.WithValue(parent, stateKey{}, s)
}

// StateFrom returns the state on a request context, or the zero State.
//
// The zero value is the answer for every request that was not redirected here
// from a rejection, which is nearly all of them: no errors and no old input is
// a page that draws neither, not a page that has to check first.
func StateFrom(ctx context.Context) State {
	s, _ := ctx.Value(stateKey{}).(State)
	return s
}

// State returns what the framework knows about this request.
func (c *Context) State() State { return StateFrom(c.Ctx()) }

// Old returns what was typed in a field on the request that was rejected, or
// empty.
//
// A view reads it through view.Page.OldValue rather than through here -- the
// page has the state on it by the time a view runs. This is for the handler that
// needs the value in Go: re-deriving a select's options from what was chosen,
// for instance.
//
// It is always empty for a password field, by construction. See session.Flash.
func (c *Context) Old(field string) string {
	return c.State().Old.Get(field)
}

// csrfTokenKey is the context key of the CSRF token issued for this request,
// of an unexported type for the same reason as stateKey.
type csrfTokenKey struct{}

// WithCSRFToken returns a context carrying the CSRF token issued for this
// request.
//
// The contract is with the middleware that protects forms: on every request
// that may render a page, it issues the token bound to this visitor -- the
// session id, or the guest binding for somebody who has none -- and puts it
// here before the handler runs. view.New reads it back, so a controller that
// renders a form never sees the issuer, the session store or the binding, and
// a page cannot carry a token issued for another request.
//
// It is a key of its own rather than a field of State because the two are
// written by different middleware that know nothing of each other's order: one
// struct written twice is a struct where the second write erases the first.
//
// An empty token is not stored, so a context passed through here with nothing
// to carry answers CSRFTokenFrom exactly as one that never was.
func WithCSRFToken(parent context.Context, token string) context.Context {
	if token == "" {
		return parent
	}
	return context.WithValue(parent, csrfTokenKey{}, token)
}

// CSRFTokenFrom returns the CSRF token on a request context, and whether one
// was issued for it.
//
// False is the answer for a request no protecting middleware ran on: an API
// route, a test that builds its own request. A page drawn there carries an
// empty token, and a form posted from it is refused.
func CSRFTokenFrom(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(csrfTokenKey{}).(string)
	return token, ok && token != ""
}
