package middleware

import (
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
)

// RedirectIfAuthenticated is
// Illuminate\Auth\Middleware\RedirectIfAuthenticated.
//
// It is the mirror of [Authenticate], and it guards the sign-in and register
// screens: somebody who is already signed in and follows a bookmark to /login
// is sent on rather than shown a form that would sign them in a second time.
type RedirectIfAuthenticated struct {
	// auth is the authentication factory instance. PHP reads the Auth facade;
	// there are no facades (ADR 0002), so it is handed in.
	auth Factory

	// guards are the guards this copy was bound to by Using. Empty means the
	// default guard, which is PHP's [null].
	guards []string

	// redirectToCallback is the callback that should be used to generate the
	// authentication redirect path. PHP keeps it in a static; see
	// [Authenticate.redirectToCallback] for why this one is a field.
	redirectToCallback func(r *http.Request) string
}

// NewRedirectIfAuthenticated returns the middleware over an authentication
// factory.
func NewRedirectIfAuthenticated(a Factory) *RedirectIfAuthenticated {
	return &RedirectIfAuthenticated{auth: a}
}

// Using specifies the guards for the middleware.
//
// It is RedirectIfAuthenticated::using. See [Authenticate.Using] for why it
// returns a copy of the middleware rather than a route string.
func (m *RedirectIfAuthenticated) Using(guard string, others ...string) *RedirectIfAuthenticated {
	copied := *m
	copied.guards = append([]string{guard}, others...)
	return &copied
}

// Handle handles an incoming request.
//
// It is RedirectIfAuthenticated::handle.
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
// It is RedirectIfAuthenticated::redirectTo, which is protected. Exported for
// the reason [Authenticate.RedirectTo] is.
func (m *RedirectIfAuthenticated) RedirectTo(r *http.Request) string {
	if m.redirectToCallback != nil {
		return m.redirectToCallback(r)
	}
	return m.defaultRedirectURI()
}

// defaultRedirectURI gets the default URI the user should be redirected to when
// they are authenticated.
//
// It is RedirectIfAuthenticated::defaultRedirectUri, which is protected. PHP
// probes the route registry for a route named "dashboard" and then one named
// "home", then for the URIs of the same two names, and falls back to "/".
//
// The probe is skipped: it needs the router, and this package must not import
// it. What is left is the fallback, and [RedirectIfAuthenticated.RedirectUsing]
// is how an application names somewhere else -- which is what a Laravel project
// with a dashboard ends up doing anyway, because the guessed route is right
// only for the two names Laravel's own starter kits use.
func (m *RedirectIfAuthenticated) defaultRedirectURI() string { return "/" }

// RedirectUsing specifies the callback that should be used to generate the
// redirect path.
//
// It is RedirectIfAuthenticated::redirectUsing, which is static. See
// [Authenticate.RedirectUsing].
func (m *RedirectIfAuthenticated) RedirectUsing(redirectToCallback func(r *http.Request) string) *RedirectIfAuthenticated {
	copied := *m
	copied.redirectToCallback = redirectToCallback
	return &copied
}
