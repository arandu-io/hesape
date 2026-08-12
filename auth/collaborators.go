package auth

import (
	"context"
	"net/http"
	"time"
)

// This file holds what the guards need from the rest of the framework: the
// session the user id is kept in, the cookie jar the recaller goes into, the
// event dispatcher, the request, and the timebox that makes a failed sign-in
// take as long as a successful one.
//
// They are interfaces, declared here in the consuming package, for the reason
// contracts.go gives for Hasher: the root of auth imports nothing but the
// standard library, because everything that scopes itself by tenant imports it
// to read Tenant off a Grant. The signatures below are the ones hesape/session,
// hesape/cookie, hesape/events and hesape/http already have, method for method,
// so those packages satisfy these structurally without either side importing
// the other.
//
// Each is the half of an Illuminate contract that authentication touches, never
// the whole of it. What the PHP names them:
//
//	Session    Illuminate\Contracts\Session\Session
//	CookieJar  Illuminate\Contracts\Cookie\QueueingFactory
//	Dispatcher Illuminate\Contracts\Events\Dispatcher
//	Request    Symfony's HttpFoundation\Request in SessionGuard, and
//	           Illuminate\Http\Request in TokenGuard and RequestGuard
//	Timebox    Illuminate\Support\Timebox

// Session is the half of Illuminate\Contracts\Session\Session the guards use.
//
// SessionGuard writes the authenticated user's id under [SessionGuard.GetName]
// and regenerates the session id on the way in, which is the defence against a
// session fixed by whoever handed the browser its first cookie.
//
// The four signatures are hesape/session.Store's, so *session.Store is a
// Session with nothing in between.
type Session interface {
	// Get is Session::get. The variadic is the PHP's $default.
	Get(key string, def ...any) any

	// Put is Session::put.
	Put(key string, value any)

	// Remove is Session::remove: forget the key and return what was there.
	Remove(key string) any

	// Regenerate is Session::regenerate($destroy). It takes the context the
	// PHP has no equivalent of, because the store it writes through is a
	// database or a cache.
	Regenerate(ctx context.Context, destroy bool) error
}

// CookieJar is the half of Illuminate\Contracts\Cookie\QueueingFactory that
// SessionGuard uses to carry the "remember me" cookie.
//
// It is five of the factory's methods and no more: make and queue put the
// recaller on the response, unqueue and forget take it off on the way out, and
// hasQueued is how LogoutOtherDevices knows a recaller was already queued
// during this request and has to be reissued with the new password hash.
//
// The signatures are hesape/cookie.CookieJar's, which is why they carry
// Symfony's full argument list rather than the three arguments SessionGuard
// passes. A *net/http.Cookie is the Symfony one: net/http is the standard
// library, so naming it here costs the root of auth no dependency.
type CookieJar interface {
	// Make is Factory::make.
	Make(name, value string, minutes int, path, domain string, secure *bool, httpOnly, raw bool, sameSite http.SameSite) *http.Cookie

	// Queue is QueueingFactory::queue.
	Queue(cookie *http.Cookie)

	// Unqueue is QueueingFactory::unqueue.
	Unqueue(name, path string)

	// Forget is Factory::forget: the cookie that tells the browser to drop it.
	Forget(name, path, domain string) *http.Cookie

	// HasQueued is QueueingFactory::hasQueued.
	HasQueued(key, path string) bool
}

// Dispatcher is the half of Illuminate\Contracts\Events\Dispatcher that
// SessionGuard uses: it fires the eight authentication events and it registers
// the listener [SessionGuard.Attempting] takes.
//
// The signatures are hesape/events.Dispatcher's. An event is dispatched as a
// value and a listener is registered against a value of the same type, which is
// how that dispatcher names an event that is not a string.
type Dispatcher interface {
	// Dispatch is Dispatcher::dispatch.
	Dispatch(event any, payload ...any) []any

	// Listen is Dispatcher::listen.
	Listen(events any, listener ...any)
}

// Request is the half of the HTTP request the guards read.
//
// SessionGuard reads the recaller cookie and the HTTP Basic pair; TokenGuard
// reads the query string, the input, the bearer token and the Basic password;
// RequestGuard hands the whole thing to the callback it was built with. Nothing
// here reads a tenant, and nothing here may: the tenant is on the Grant a
// policy minted, never on the request (RULE 14).
//
// Query, Input, BearerToken and Cookie are hesape/http.Request's signatures.
// GetUser, GetPassword and Context are not on it yet:
//
//   - GetUser and GetPassword are Symfony's Request::getUser and
//     ::getPassword, the HTTP Basic pair. hesape/http.Request has a User
//     method, but that one is the authenticated user, not the Basic username,
//     so the PHP names are kept here and the two are not confused.
//   - Context is what a Go server carries and PHP does not. The Guard contract
//     gives User and ID no context.Context -- the PHP has no argument there to
//     transpose -- and both of them read the user store, so the guard takes the
//     request's context. Cancel the request and the lookup it started stops.
type Request interface {
	// Query is Request::query.
	Query(key string, def ...string) string

	// Input is Request::input.
	Input(key string, def ...any) any

	// BearerToken is Request::bearerToken.
	BearerToken() string

	// Cookie is $request->cookies->get.
	Cookie(name string, def ...string) string

	// GetUser is Symfony's Request::getUser: the HTTP Basic username.
	GetUser() string

	// GetPassword is Symfony's Request::getPassword: the HTTP Basic password.
	GetPassword() string

	// Context is the request's context. See the type's doc for why it is here.
	Context() context.Context
}

// Timebox is Illuminate\Support\Timebox: run a callback and do not come back
// before the given number of microseconds has passed.
//
// It is why [SessionGuard.Attempt] with an address nobody has registered takes
// as long as one with the wrong password. Without it, the time an attempt took
// says whether the account exists, and that is a list of your customers
// readable by anyone with a stopwatch.
//
// hesape/support has the whole class. This is an interface rather than that
// type because the root of auth imports nothing but the standard library, and
// [NewTimebox] is the implementation SessionGuard falls back to.
type Timebox interface {
	// Call is Timebox::call. The callback is handed the timebox, as the PHP's
	// is, so it can ask to return early. An error the callback returns is held
	// until the box has been waited out and then returned, which is how the PHP
	// rethrows.
	Call(callback func(timebox Timebox) (any, error), microseconds int) (any, error)

	// ReturnEarly is Timebox::returnEarly.
	ReturnEarly() Timebox

	// DontReturnEarly is Timebox::dontReturnEarly.
	DontReturnEarly() Timebox
}

// NewTimebox is the `new Timebox` that SessionGuard's constructor writes when
// it is handed none.
//
// Like the PHP's, the returned timebox does not reset itself between calls:
// once something asked it to return early it keeps returning early, and a guard
// is given a fresh one per request.
func NewTimebox() Timebox {
	return &plainTimebox{}
}

// plainTimebox is [NewTimebox]'s Timebox, and it is Illuminate\Support\Timebox
// with the sleep taken straight from the standard library.
type plainTimebox struct {
	// earlyReturn is the PHP's public $earlyReturn.
	earlyReturn bool
}

// Call is Timebox::call.
func (t *plainTimebox) Call(callback func(timebox Timebox) (any, error), microseconds int) (any, error) {
	start := time.Now()

	result, err := callback(t)

	remainder := time.Duration(microseconds)*time.Microsecond - time.Since(start)

	if !t.earlyReturn && remainder > 0 {
		time.Sleep(remainder)
	}

	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReturnEarly is Timebox::returnEarly.
func (t *plainTimebox) ReturnEarly() Timebox {
	t.earlyReturn = true
	return t
}

// DontReturnEarly is Timebox::dontReturnEarly.
func (t *plainTimebox) DontReturnEarly() Timebox {
	t.earlyReturn = false
	return t
}
