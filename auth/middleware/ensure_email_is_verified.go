package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
)

// VerificationNoticeURI is where somebody who has not verified their address is
// sent.
//
// There is no router in this package to resolve a route name against, so this
// is the path the middleware sends them to unless told otherwise.
// [EnsureEmailIsVerified.RedirectTo] is how a project that mounts the notice
// elsewhere says so.
const VerificationNoticeURI = "/email/verify"

// EnsureEmailIsVerified refuses a request from an account whose address was
// never confirmed.
//
// An account that is not required to verify -- a user type that does not
// implement auth.MustVerifyEmail -- passes, which is what lets one application
// have both kinds.
type EnsureEmailIsVerified struct {
	guestRedirect

	// auth is where the signed-in user comes from: the default guard's user.
	auth Factory

	// redirectToRoute is where an unverified account is sent. Empty means
	// [VerificationNoticeURI].
	redirectToRoute string
}

// NewEnsureEmailIsVerified returns the middleware over an authentication
// factory.
func NewEnsureEmailIsVerified(a Factory) *EnsureEmailIsVerified {
	return &EnsureEmailIsVerified{auth: a}
}

// RedirectTo specifies where an unverified account is sent.
//
// It returns a copy of the middleware bound to that path, for the reason
// [Authenticate.Using] gives. It takes a path rather than a route name because
// resolving a name needs the router, which this package must not import.
func (m *EnsureEmailIsVerified) RedirectTo(route string) *EnsureEmailIsVerified {
	copied := *m
	copied.redirectToRoute = route
	return &copied
}

// Handle handles an incoming request.
//
// A request that wants JSON is answered 403; anything else is redirected to the
// notice.
func (m *EnsureEmailIsVerified) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.verified(r) {
			next.ServeHTTP(w, r)
			return
		}

		if expectsJSON(r) {
			exception.WriteProblem(w, r, http.StatusForbidden, "Your email address is not verified.")
			return
		}

		to := m.redirectToRoute
		if to == "" {
			to = VerificationNoticeURI
		}
		m.redirect(w, r, to)
	})
}

// verified reports whether this request may go on.
func (m *EnsureEmailIsVerified) verified(r *http.Request) bool {
	guard := m.auth.Guard("")
	if guard == nil {
		return false
	}
	user := guard.User()
	if user == nil {
		// Nobody is signed in, and they are sent to the verification notice
		// rather than to the sign-in screen -- which is why this middleware runs
		// after [Authenticate] and not instead of it.
		return false
	}

	mustVerify, ok := user.(auth.MustVerifyEmail)
	if !ok {
		// A user type with nothing to verify: the request goes on.
		return true
	}
	return mustVerify.HasVerifiedEmail()
}
