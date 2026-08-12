package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// VerificationNoticeURI is where somebody who has not verified their address is
// sent.
//
// PHP resolves the named route "verification.notice"; there is no router in this
// package, so the default is the URI that route has in a Laravel application.
// [EnsureEmailIsVerified.RedirectTo] is how a project that mounts it elsewhere
// says so.
const VerificationNoticeURI = "/email/verify"

// EnsureEmailIsVerified is Illuminate\Auth\Middleware\EnsureEmailIsVerified.
//
// It refuses a request from an account whose address was never confirmed. An
// account that is not required to verify -- a user type that does not implement
// auth.MustVerifyEmail -- passes, which is PHP's `instanceof` test and is what
// lets one application have both kinds.
type EnsureEmailIsVerified struct {
	guestRedirect

	// auth is where the signed-in user comes from. PHP reads $request->user(),
	// which is the default guard's user; this asks the factory for it.
	auth Factory

	// redirectToRoute is the parameter PHP reads off the route string. Empty
	// means [VerificationNoticeURI].
	redirectToRoute string
}

// NewEnsureEmailIsVerified returns the middleware over an authentication
// factory.
//
// PHP's has no constructor at all, because it reaches the user through the
// request, which reaches it through the container. There is no container
// (ADR 0001), so the factory is handed in.
func NewEnsureEmailIsVerified(a Factory) *EnsureEmailIsVerified {
	return &EnsureEmailIsVerified{auth: a}
}

// RedirectTo specifies the redirect route for the middleware.
//
// It is EnsureEmailIsVerified::redirectTo, which is static and returns the route
// string. This returns a copy of the middleware bound to the path, for the
// reason [Authenticate.Using] gives. It takes a path rather than a route name
// because resolving a name needs the router, which this package must not
// import.
func (m *EnsureEmailIsVerified) RedirectTo(route string) *EnsureEmailIsVerified {
	copied := *m
	copied.redirectToRoute = route
	return &copied
}

// Handle handles an incoming request.
//
// It is EnsureEmailIsVerified::handle. PHP's condition, character for
// character:
//
//	! $request->user() ||
//	($request->user() instanceof MustVerifyEmail && ! $request->user()->hasVerifiedEmail())
//
// A request that wants JSON is answered 403 with the sentence PHP aborts with;
// anything else is redirected to the notice.
func (m *EnsureEmailIsVerified) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.verified(r) {
			next.ServeHTTP(w, r)
			return
		}

		if expectsJSON(r) {
			writeJSON(w, http.StatusForbidden, "Your email address is not verified.")
			return
		}

		to := m.redirectToRoute
		if to == "" {
			to = VerificationNoticeURI
		}
		m.redirect(w, r, to)
	})
}

// verified answers PHP's condition, inverted: may this request go on.
func (m *EnsureEmailIsVerified) verified(r *http.Request) bool {
	guard := m.auth.Guard("")
	if guard == nil {
		return false
	}
	user := guard.User()
	if user == nil {
		// Nobody is signed in. PHP refuses here too, and sends them to the
		// verification notice rather than to the sign-in screen -- which is why
		// this middleware runs after [Authenticate] and not instead of it.
		return false
	}

	mustVerify, ok := user.(auth.MustVerifyEmail)
	if !ok {
		// A user type with nothing to verify. PHP's instanceof answers the
		// same, and the request goes on.
		return true
	}
	return mustVerify.HasVerifiedEmail()
}
