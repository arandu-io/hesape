package http

import (
	"context"
	"net/url"
	"sync"

	"github.com/arandu-io/hesape/validation"
)

// State is what the framework knows about a request before the handler runs.
//
// It is what the request that redirected here left for this one: what it failed
// on, or what it said it did. It is a struct rather than any one of those so the
// next piece of per-request framework state does not add a second context key,
// a second middleware and a second accessor that every page has to be taught
// about.
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
	// Messages is what the request that redirected here said about what it
	// did, by key -- "status" for the sentence a form that worked leaves for
	// the page it lands on. See Context.With.
	Messages map[string]string
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

// csrfToken is the token a request carries, issued the first time somebody asks
// for it and never twice.
type csrfToken struct {
	once  sync.Once
	issue func() string
	token string
}

func (t *csrfToken) get() string {
	t.once.Do(func() { t.token = t.issue() })
	return t.token
}

// WithCSRFToken returns a context carrying the CSRF token issued for this
// request.
//
// The contract is with the middleware that protects forms: on every request
// that may render a page, it makes the token bound to this visitor -- the
// session id, or the guest binding for somebody who has none -- available here
// before the handler runs. view.New reads it back, so a controller that renders
// a form never sees the issuer, the session store or the binding, and a page
// cannot carry a token issued for another request.
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
	return WithCSRFTokenIssuer(parent, func() string { return token })
}

// WithCSRFTokenIssuer returns a context that issues the CSRF token of this
// request the first time CSRFTokenFrom asks for it, and answers the same one
// afterwards.
//
// It is what keeps a response that draws no form from carrying the cookie a
// token is bound to. The guest binding of a visitor is a cookie, and a cookie
// written for every request is one written for a script's API call, for an
// image, for a 401 that a client reads and discards: it is set on the response
// of whatever asks for the token, and on no other. issue runs at most once, on
// the goroutine that first asks, and has to write what it writes to the response
// before the response is committed; view.New asks while the page is built, which
// is before.
//
// issue returns the empty string when no token could be made, which is what
// CSRFTokenFrom answers as no token. A nil issue panics at wiring.
func WithCSRFTokenIssuer(parent context.Context, issue func() string) context.Context {
	if issue == nil {
		panic("http: WithCSRFTokenIssuer was given a nil issuer")
	}
	return context.WithValue(parent, csrfTokenKey{}, &csrfToken{issue: issue})
}

// CSRFTokenFrom returns the CSRF token on a request context, and whether one
// was issued for it.
//
// Asking is what issues it, when the middleware passed an issuer: the first call
// writes the cookie the token is bound to, if the visitor has none.
//
// False is the answer for a request no protecting middleware ran on: an API
// route, a test that builds its own request. A page drawn there carries an
// empty token, and a form posted from it is refused.
func CSRFTokenFrom(ctx context.Context) (string, bool) {
	holder, ok := ctx.Value(csrfTokenKey{}).(*csrfToken)
	if !ok {
		return "", false
	}
	token := holder.get()
	return token, token != ""
}

// appNameKey is the context key of the application name, of an unexported
// type for the same reason as stateKey.
type appNameKey struct{}

// WithAppName returns a context carrying the name of the application serving
// this request.
//
// The name is configuration rather than a per-request decision: it is the
// same on every request, so the framework puts it here once, before the
// handler runs, from the configuration that already reads it. view.New reads
// it back, which is how every page -- the application's own, a generated one,
// one drawn by a module that never sees the application's configuration --
// carries the same brand without a controller passing it along.
//
// It is a key of its own rather than a field of State for the reason the CSRF
// token is: the middleware that writes State knows nothing of this one, and one
// struct written twice is a struct where the second write erases the first.
//
// An empty name is not stored, so a context passed through here with nothing
// to carry answers AppNameFrom exactly as one that never was.
func WithAppName(parent context.Context, name string) context.Context {
	if name == "" {
		return parent
	}
	return context.WithValue(parent, appNameKey{}, name)
}

// AppNameFrom returns the application name on a request context, or empty.
//
// Empty is the answer for a request the framework did not put a name on: a
// test that builds its own request, a handler mounted outside the
// application. It needs no second result, because an empty name and no name
// draw the same thing -- a navigation bar with no brand in it.
func AppNameFrom(ctx context.Context) string {
	name, _ := ctx.Value(appNameKey{}).(string)
	return name
}
