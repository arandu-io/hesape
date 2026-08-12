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

// CurrentDeviceLogout is Illuminate\Auth\Events\CurrentDeviceLogout.
type CurrentDeviceLogout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// Failed is Illuminate\Auth\Events\Failed.
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

// Login is Illuminate\Auth\Events\Login.
type Login struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
	// Remember indicates if the user should be "remembered".
	Remember bool
}

// Logout is Illuminate\Auth\Events\Logout.
type Logout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// OtherDeviceLogout is Illuminate\Auth\Events\OtherDeviceLogout.
type OtherDeviceLogout struct {
	// Guard is the authentication guard name.
	Guard string
	// User is the authenticated user.
	User auth.Authenticatable
}

// PasswordReset is Illuminate\Auth\Events\PasswordReset.
type PasswordReset struct {
	// User is the user.
	User auth.Authenticatable
}

// PasswordResetLinkSent is Illuminate\Auth\Events\PasswordResetLinkSent.
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

// Verified is Illuminate\Auth\Events\Verified.
type Verified struct {
	// User is the verified user.
	//
	// PHP documents it as MustVerifyEmail rather than Authenticatable, and the
	// type is kept: an event announcing a verification is about an account that
	// had one to do.
	User auth.MustVerifyEmail
}
