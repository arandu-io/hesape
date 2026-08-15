package events

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// Attempting is Illuminate\Auth\Events\Attempting.
//
// It is dispatched before the credentials are checked, which is what makes it
// the place to count an attempt rather than a failure: a throttle that only
// sees Failed cannot tell a slow password list from a busy morning.
type Attempting struct {
	// Guard is the authentication guard name.
	Guard string

	// Credentials are the credentials for the user.
	//
	// They hold the plain password. PHP marks the constructor parameter
	// #[\SensitiveParameter] so that a stack trace redacts it; Go has no such
	// attribute, so the rule lives here: a listener that logs this map writes
	// a password to the log, and every log ships somewhere.
	Credentials map[string]any

	// Remember indicates if the user should be "remembered".
	Remember bool
}

// Authenticated is Illuminate\Auth\Events\Authenticated.
//
// It is dispatched every time a request resolves a user out of an existing
// session, not only on the request that signed in. Login is the one that fires
// once.
type Authenticated struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// CurrentDeviceLogout is dispatched when somebody signs out of this browser
// alone. The session is cleared and the "remember me" cookie is dropped, but
// the remember token on the user is left as it was, so the same person stays
// signed in wherever else they used it. That is the whole difference from
// [Logout], which replaces the token and ends every session at once.
//
// Answers Illuminate\Auth\Events\CurrentDeviceLogout.
type CurrentDeviceLogout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// Failed is dispatched when a sign-in attempt did not work: the credentials
// named no account at all, or they named one and the password did not check
// out. It is what an audit trail listens for, and what a "somebody tried to
// sign in to your account" notice hangs off.
//
// It is not every refusal. A guard asked only to check credentials without
// signing anybody in dispatches neither this nor [Attempting], and an attempt
// the throttle turned away never reached the guard at all -- that one is
// [Lockout].
//
// Answers Illuminate\Auth\Events\Failed.
type Failed struct {
	// Guard is the authentication guard name.
	Guard string

	// User is the user the attempter was trying to authenticate as, and is nil
	// when no account matched at all.
	//
	// The two cases must not be told apart in anything the attempter can see:
	// "no such account" and "wrong password" is an account enumeration oracle.
	// They are told apart here because a listener writing an audit trail needs
	// to know which happened.
	User auth.Authenticatable

	// Credentials are the credentials provided by the attempter. See
	// [Attempting.Credentials]: they hold the plain password.
	Credentials map[string]any
}

// Lockout is Illuminate\Auth\Events\Lockout.
//
// It is dispatched when the sign-in throttle refuses an attempt. The counter
// itself is auth.Throttle in the root package; this is the announcement, which
// is what a project hangs an alert or a notification off.
type Lockout struct {
	// Request is the throttled request.
	Request *http.Request
}

// Login is dispatched when a session is started for a user: a login form whose
// credentials checked out, a sign-in by identifier, or a request that arrived
// with a valid "remember me" cookie and had its session rebuilt from it. That
// last one is why Remember can be true on a request nobody typed a password
// into.
//
// It fires once per session, not once per request. A later request that merely
// finds the user already in the session dispatches [Authenticated].
//
// Answers Illuminate\Auth\Events\Login.
type Login struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
	// Remember indicates if the user should be "remembered".
	Remember bool
}

// Logout is dispatched when somebody signs out everywhere. The session is
// cleared, the "remember me" cookie is dropped, and the remember token on the
// user is replaced -- which is what makes the cookie left in another browser
// stop naming anybody.
//
// User is the account that was signed in, read before the guard forgot it, so a
// listener still has somebody to write down. [CurrentDeviceLogout] is the
// narrower one, which ends this browser's session and leaves the others alone.
//
// Answers Illuminate\Auth\Events\Logout.
type Logout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// OtherDeviceLogout is dispatched when somebody ends their sessions everywhere
// except here -- the "sign out on my other devices" button, which asks for the
// password again and rehashes it on the way through. The new hash changes the
// signature every other session's cookie carries, so those sessions are turned
// away on their next request; this browser is handed a fresh cookie and carries
// on.
//
// Answers Illuminate\Auth\Events\OtherDeviceLogout.
type OtherDeviceLogout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// PasswordReset announces that somebody finished a reset: a valid token was
// redeemed, the new password was stored, and the token was destroyed. It is the
// moment an application signs the person in, ends their other sessions, or
// writes the change down.
//
// Nothing in this collection dispatches it, and nothing in Illuminate does
// either. Redeeming the token is
// [github.com/arandu-io/hesape/auth/passwords.PasswordBroker.Reset], which
// hands the new password to a callback the application supplied; whoever wrote
// that callback is the only one who knows the password was really stored, so
// the announcement is theirs to make.
//
// Answers Illuminate\Auth\Events\PasswordReset.
type PasswordReset struct {
	// User is the user.
	User auth.Authenticatable
}

// PasswordResetLinkSent announces that a reset link has gone out, right after
// the notification was handed over for delivery. Nothing is announced for an
// address that matched no account, or for one the throttle turned down: the
// form answers all three the same way, and an event that fired only for real
// accounts would be that difference showing up somewhere else.
//
// Illuminate dispatches it from the broker.
// [github.com/arandu-io/hesape/auth/passwords.PasswordBroker] does not yet --
// it takes no dispatcher, which its package documents among the things
// deliberately absent.
//
// Answers Illuminate\Auth\Events\PasswordResetLinkSent.
type PasswordResetLinkSent struct {
	// User is the user instance.
	//
	// PHP documents it as CanResetPassword rather than Authenticatable, and the
	// type is kept: the only thing a listener can ask of it is the address the
	// link went to.
	User auth.CanResetPassword
}

// Registered is Illuminate\Auth\Events\Registered.
//
// [github.com/arandu-io/hesape/auth/listeners.SendEmailVerificationNotification]
// is what listens for it out of the box.
type Registered struct {
	// User is the authenticated user.
	User auth.Authenticatable
}

// Validated is Illuminate\Auth\Events\Validated.
//
// It is dispatched when a user provider has retrieved an account and its
// password checked out, and before anything is written to the session. A guard
// running Once dispatches it and never dispatches Login.
type Validated struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the user retrieved and validated from the User Provider.
	User auth.Authenticatable
}

// Verified announces that somebody confirmed their e-mail address, after the
// confirmation has been stamped on the account. A listener is where the welcome
// message goes, or the first-run provisioning that was waiting on a real
// address.
//
// Nothing in this collection dispatches it, and nothing in Illuminate does
// either: following the link is a route, and the route belongs to the
// application. What is here is the stamp,
// [auth.MustVerifyEmailTrait.MarkEmailAsVerified], and the middleware that
// turns an unconfirmed account away.
//
// Answers Illuminate\Auth\Events\Verified.
type Verified struct {
	// User is the verified user.
	//
	// PHP documents it as MustVerifyEmail rather than Authenticatable, and the
	// type is kept: an event announcing a verification is about an account that
	// had one to do.
	User auth.MustVerifyEmail
}
