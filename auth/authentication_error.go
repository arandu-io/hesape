package auth

import "sync"

// defaultAuthenticationMessage is the PHP constructor's $message default.
const defaultAuthenticationMessage = "Unauthenticated."

// AuthenticationError is Illuminate\Auth\AuthenticationException: nobody is
// signed in, on a path that requires somebody to be.
//
// The PHP is an exception and this is an error, which is ADR 0044's second
// mechanical change. What it carries is the PHP's: the guards that were asked,
// so a middleware can say which ones, and the path to send the browser to,
// which is the whole reason the exception exists rather than a bare 401 -- a
// person who followed a link gets the sign-in page, an API client gets the
// status.
//
// It is not the authorization failure. That one is [ErrForbidden], and the
// difference is the difference between 401 and 403: this says the request had
// no identity, ErrForbidden says the identity it had may not do that.
type AuthenticationError struct {
	// Message is the exception's $message.
	Message string

	// guards is the exception's $guards.
	guards []string

	// redirectTo is the exception's $redirectTo.
	redirectTo string
}

// NewAuthenticationError is AuthenticationException::__construct.
//
// Go has no default arguments: an empty message is the PHP's
// "Unauthenticated.", a nil slice is its empty $guards array, and an empty
// redirect is its null.
func NewAuthenticationError(message string, guards []string, redirectTo string) *AuthenticationError {
	if message == "" {
		message = defaultAuthenticationMessage
	}
	return &AuthenticationError{Message: message, guards: guards, redirectTo: redirectTo}
}

// Error is what makes this an error, and it answers Exception::getMessage.
func (e *AuthenticationError) Error() string {
	if e.Message == "" {
		return defaultAuthenticationMessage
	}
	return e.Message
}

// Guards is AuthenticationException::guards: the guards that were checked.
func (e *AuthenticationError) Guards() []string {
	return e.guards
}

// RedirectTo is AuthenticationException::redirectTo: where the browser should
// be sent, or "" when the answer is the status code and not a page.
//
// The PHP falls back to a static callback registered with [RedirectUsing], and
// so does this.
func (e *AuthenticationError) RedirectTo(request Request) string {
	if e.redirectTo != "" {
		return e.redirectTo
	}

	redirectMu.RLock()
	callback := redirectToCallback
	redirectMu.RUnlock()

	if callback != nil {
		return callback(request)
	}
	return ""
}

// redirectToCallback is the exception's static $redirectToCallback.
//
// A process-wide variable is what a PHP static property is, and this one is
// written once at boot and read on every failed request, so it is behind a lock
// that the PHP does not need and a Go server does.
var (
	redirectMu         sync.RWMutex
	redirectToCallback func(request Request) string
)

// RedirectUsing is AuthenticationException::redirectUsing: the callback that
// builds the redirect path when the error carries none.
//
// Call it once, where the application boots. It is not a per-request setting.
func RedirectUsing(callback func(request Request) string) {
	redirectMu.Lock()
	defer redirectMu.Unlock()
	redirectToCallback = callback
}
