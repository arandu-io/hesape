package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/arandu-io/hesape/session"
	sessionmw "github.com/arandu-io/hesape/session/middleware"
)

// PasswordConfirmURI is where somebody is sent to type their password again.
//
// PHP resolves the named route "password.confirm"; there is no router in this
// package, so the default is the URI that route has in a Laravel application.
// [RequirePassword.Using] is how a project that mounts it elsewhere says so.
const PasswordConfirmURI = "/confirm-password"

// RequirePassword is Illuminate\Auth\Middleware\RequirePassword.
//
// It guards the part of an application that a stolen open session must not
// reach: changing the e-mail address, adding a payout account, deleting
// everything. Being signed in is not enough there -- the password has to have
// been typed recently, on this session.
//
// The window is the only thing it decides. session.Store.PasswordConfirmed is
// what stamps the session, and the screen that asks for the password is the
// application's.
type RequirePassword struct {
	guestRedirect

	// redirectToRoute is the parameter PHP reads off the route string. Empty
	// means [PasswordConfirmURI].
	redirectToRoute string

	// passwordTimeout is the password timeout. Zero means
	// session.PasswordConfirmationWindow.
	//
	// PHP holds an int of seconds, defaulting to 10800. It is a time.Duration
	// here because Go has a type for a span of time and a bare int of seconds is
	// how a caller passes milliseconds by accident.
	passwordTimeout time.Duration
}

// NewRequirePassword returns the middleware.
//
// A passwordTimeout of zero or less means session.PasswordConfirmationWindow,
// which is three hours -- the same number as PHP's default of 10800 seconds,
// and stated once in hesape/session so the middleware and anything else asking
// "recently" agree.
//
// PHP's constructor also takes a ResponseFactory and a UrlGenerator. The
// response factory is hesape/http's Redirect, which decides HX-Redirect against
// 303 in one place for the whole framework; the URL generator resolves a route
// name, which needs the router this package must not import, so the redirect
// target is a path.
func NewRequirePassword(passwordTimeout time.Duration) *RequirePassword {
	return &RequirePassword{passwordTimeout: passwordTimeout}
}

// Using specifies the redirect route and timeout for the middleware.
//
// It is RequirePassword::using. See [Authenticate.Using] for why it returns a
// copy of the middleware rather than a route string. Either parameter may be
// zero-valued, which means "leave what the constructor set".
func (m *RequirePassword) Using(redirectToRoute string, passwordTimeoutSeconds time.Duration) *RequirePassword {
	copied := *m
	if redirectToRoute != "" {
		copied.redirectToRoute = redirectToRoute
	}
	if passwordTimeoutSeconds > 0 {
		copied.passwordTimeout = passwordTimeoutSeconds
	}
	return &copied
}

// Handle handles an incoming request.
//
// It is RequirePassword::handle. A request that wants JSON is answered 423
// Locked, which is the status PHP uses and which says something a 401 does not:
// the credentials are fine, the resource is closed until one more step happens.
func (m *RequirePassword) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.shouldConfirmPassword(r) {
			next.ServeHTTP(w, r)
			return
		}

		if expectsJSON(r) {
			writeJSON(w, http.StatusLocked, "Password confirmation required.")
			return
		}

		to := m.redirectToRoute
		if to == "" {
			to = PasswordConfirmURI
		}
		m.redirect(w, r, to)
	})
}

// shouldConfirmPassword determines if the confirmation timeout has expired.
//
// It is RequirePassword::shouldConfirmPassword, which is protected. PHP:
//
//	$confirmedAt = Date::now()->unix() - $request->session()->get('auth.password_confirmed_at', 0);
//	return $confirmedAt > ($passwordTimeoutSeconds ?? $this->passwordTimeout);
//
// The stamp is read from the session under session.PasswordConfirmedKey, which
// is Illuminate's key character for character, and which session.Store's own
// PasswordConfirmed writes.
//
// A request with no session at all is asked to confirm. That is the direction
// this has to fail in: treating a missing session as recently confirmed would
// walk every unauthenticated request straight past the guard.
func (m *RequirePassword) shouldConfirmPassword(r *http.Request) bool {
	timeout := m.passwordTimeout
	if timeout <= 0 {
		timeout = session.PasswordConfirmationWindow
	}

	store, ok := sessionmw.Session(r.Context())
	if !ok || store == nil {
		return true
	}

	confirmedAt := unixSeconds(store.Get(session.PasswordConfirmedKey, 0))
	if confirmedAt <= 0 {
		return true
	}

	elapsed := time.Now().Unix() - confirmedAt
	return elapsed > int64(timeout.Seconds())
}

// unixSeconds reads the confirmation stamp back out of the session, whatever
// numeric shape it survived storage in.
//
// It exists because a session is a bag of any: Store.PasswordConfirmed puts an
// int64 in, and a session that was serialised to JSON and read back holds a
// float64 -- or a json.Number, when the decoder was configured for one. A type
// switch that knew only about int64 would read every round-tripped session as
// never confirmed, which is a password screen that appears on every click and
// that nobody can reproduce in a test using an in-memory store.
//
// Anything it cannot read as a number is 0, which means unconfirmed. Same
// direction as everything else here.
func unixSeconds(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0
		}
		return parsed
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
