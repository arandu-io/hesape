package middleware

import (
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
)

// RedirectIfAuthenticated is the mirror of [Authenticate], and it guards the
// sign-in and register screens: somebody who is already signed in and follows a
// bookmark to /login is sent on rather than shown a form that would sign them
// in a second time.
type RedirectIfAuthenticated struct {
	// auth is the authentication factory instance.
	auth Factory

	// guards are the guards this copy was bound to by Using. Empty means the
	// default guard.
	guards []string

	// redirectToCallback is the callback that should be used to generate the
	// authentication redirect path. See [Authenticate.redirectToCallback] for
	// why it is a field and not package-level state.
	redirectToCallback func(r *http.Request) string
}

// NewRedirectIfAuthenticated returns the middleware over an authentication
// factory.
func NewRedirectIfAuthenticated(a Factory) *RedirectIfAuthenticated {
	return &RedirectIfAuthenticated{auth: a}
}

// Using returns a copy of the middleware bound to those guards, for the reason
// [Authenticate.Using] gives.
func (m *RedirectIfAuthenticated) Using(guard string, others ...string) *RedirectIfAuthenticated {
	copied := *m
	copied.guards = append([]string{guard}, others...)
	return &copied
}

// Handle handles an incoming request.
func (m *RedirectIfAuthenticated) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guards := m.guards
		if len(guards) == 0 {
			guards = []string{""}
		}

		for _, name := range guards {
			guard := m.auth.Guard(name)
			if guard != nil && guard.Check() {
				// hhttp.Redirect and not this package's guestRedirect: there is
				// nothing to remember. The person is not being turned away from
				// somewhere they wanted to go, they are being taken off a screen
				// that no longer applies to them.
				hhttp.Redirect(w, r, m.RedirectTo(r))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// RedirectTo gets the path the user should be redirected to when they are
// authenticated.
//
// It is exported for the reason [Authenticate.RedirectTo] is.
func (m *RedirectIfAuthenticated) RedirectTo(r *http.Request) string {
	if m.redirectToCallback != nil {
		return m.redirectToCallback(r)
	}
	return m.defaultRedirectURI()
}

// defaultRedirectURI gets the default URI the user should be redirected to when
// they are authenticated.
//
// It is "/", and nothing is guessed: probing the route registry for a likely
// landing page would need the router, which this package must not import.
// [RedirectIfAuthenticated.RedirectUsing] is how an application names somewhere
// else, which any application with a dashboard has to do anyway.
func (m *RedirectIfAuthenticated) defaultRedirectURI() string { return "/" }

// RedirectUsing specifies the callback that should be used to generate the
// redirect path.
//
// It returns a copy of the middleware. See [Authenticate.RedirectUsing].
func (m *RedirectIfAuthenticated) RedirectUsing(redirectToCallback func(r *http.Request) string) *RedirectIfAuthenticated {
	copied := *m
	copied.redirectToCallback = redirectToCallback
	return &copied
}
