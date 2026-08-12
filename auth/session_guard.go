package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
)

const (
	// defaultRememberDuration is the PHP's $rememberDuration: 576000 minutes,
	// the 400 days a browser will keep a cookie for.
	defaultRememberDuration = 576000

	// defaultTimeboxDuration is the PHP constructor's $timeboxDuration default,
	// in microseconds.
	defaultTimeboxDuration = 200000

	// defaultHashKey is the PHP's 'base-key-for-password-hash-mac', the key
	// hashPasswordForCookie falls back to when the application has none.
	defaultHashKey = "base-key-for-password-hash-mac"

	// rememberTokenLength is the 60 of Str::random(60).
	rememberTokenLength = 60

	// sessionGuardClass stands in for the PHP's static::class in getName and
	// getRecallerName. Those two hash the class name so that two guards of
	// different classes cannot read each other's session key; Go has no
	// subclassing and no late static binding, so the name is fixed here.
	//
	// It is not the PHP's string, and it must not be: a session written by a
	// Laravel application is not one this guard should silently adopt.
	sessionGuardClass = "github.com/arandu-io/hesape/auth.SessionGuard"

	// rememberTokenAlphabet is what Str::random draws from.
	rememberTokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	// ErrCookieJarNotSet is the RuntimeException getCookieJar throws: the guard
	// was asked for the cookie jar and nobody set one.
	ErrCookieJarNotSet = errors.New("auth: cookie jar has not been set")

	// ErrHasherNotSet is what LogoutOtherDevices answers when the guard has no
	// hasher. The PHP reaches the Hash facade, which is the container path
	// (ADR 0001); here the hasher is a field, and an unset one is a wiring
	// mistake rather than a wrong password.
	ErrHasherNotSet = errors.New("auth: hasher has not been set")

	// ErrPasswordMismatch is the InvalidArgumentException
	// rehashUserPasswordForDeviceLogout throws: "The given password does not
	// match the current password."
	ErrPasswordMismatch = errors.New("auth: the given password does not match the current password")

	// ErrInvalidBasicCredentials is failedBasicResponse: the
	// UnauthorizedHttpException('Basic', 'Invalid credentials.') the PHP throws.
	//
	// The PHP's exception also carries the WWW-Authenticate challenge, because
	// in Laravel an exception renders itself into a response. Here the response
	// is the caller's: a middleware that gets this error writes 401 and the
	// Basic realm.
	ErrInvalidBasicCredentials = errors.New("auth: invalid credentials")
)

// SessionGuard is Illuminate\Auth\SessionGuard: the guard behind a browser
// session, and the one a form login goes through.
//
// It is the whole of the sign-in flow -- the attempt, the timebox that hides
// whether the account exists, the session id that is regenerated on the way in,
// the "remember me" cookie, and the logout that cycles the token so the cookie
// left in another browser stops working.
//
// It is stateful and it is per request: it caches the user it resolved, the
// request it reads cookies from, and whether logout was called. Build one per
// request, as Laravel does, and do not share it between goroutines.
type SessionGuard struct {
	GuardHelpers

	// Name is the PHP's `public readonly string $name`: the guard's name in the
	// authentication configuration, typically "web". Go has no readonly; set it
	// through [NewSessionGuard] and leave it alone, because [SessionGuard.GetName]
	// and [SessionGuard.GetRecallerName] are built from it.
	Name string

	// Hasher is the Hash facade that rehashUserPasswordForDeviceLogout calls.
	//
	// A facade is the container path (ADR 0001) and there is none here, so the
	// hasher is a field. [SessionGuard.LogoutOtherDevices] is the only method
	// that reads it, and it answers [ErrHasherNotSet] when it is nil.
	Hasher Hasher

	// lastAttempted is $lastAttempted: the user the last attempt retrieved,
	// whether or not the password matched.
	lastAttempted Authenticatable

	// viaRemember is $viaRemember.
	viaRemember bool

	// rememberDuration is $rememberDuration, in minutes.
	rememberDuration int

	// session is $session.
	session Session

	// cookie is $cookie, the Illuminate cookie creator service.
	cookie CookieJar

	// request is $request.
	request Request

	// events is $events.
	events Dispatcher

	// timebox is $timebox.
	timebox Timebox

	// timeboxDuration is $timeboxDuration, in microseconds.
	timeboxDuration int

	// rehashOnLogin is $rehashOnLogin.
	rehashOnLogin bool

	// hashKey is $hashKey, the application key hashPasswordForCookie uses.
	hashKey string

	// loggedOut is $loggedOut.
	loggedOut bool

	// recallAttempted is $recallAttempted.
	recallAttempted bool
}

var (
	_ StatefulGuard     = (*SessionGuard)(nil)
	_ SupportsBasicAuth = (*SessionGuard)(nil)
)

// NewSessionGuard is SessionGuard::__construct.
//
// The PHP defaults five of these arguments and Go has none, so every one is
// passed. Two are still filled in the way the PHP fills them: a nil timebox
// becomes [NewTimebox], its `?: new Timebox`, and a timeboxDuration of 0
// becomes 200000, its default -- a zero there would switch off the wait that
// keeps a failed attempt from being timed, and nobody would see it go.
// rehashOnLogin has no such treatment, because false is a real setting: pass
// true for the PHP's default.
//
// The cookie jar and the event dispatcher are not arguments in the PHP either.
// Set them with [SessionGuard.SetCookieJar] and [SessionGuard.SetDispatcher],
// which is what AuthManager does.
func NewSessionGuard(
	name string,
	provider UserProvider,
	session Session,
	request Request,
	timebox Timebox,
	rehashOnLogin bool,
	timeboxDuration int,
	hashKey string,
) *SessionGuard {
	if timebox == nil {
		timebox = NewTimebox()
	}
	if timeboxDuration == 0 {
		timeboxDuration = defaultTimeboxDuration
	}

	guard := &SessionGuard{
		Name:             name,
		session:          session,
		request:          request,
		timebox:          timebox,
		rehashOnLogin:    rehashOnLogin,
		timeboxDuration:  timeboxDuration,
		hashKey:          hashKey,
		rememberDuration: defaultRememberDuration,
	}
	guard.provider = provider
	guard.resolveUser = guard.User

	return guard
}

// User is SessionGuard::user: the authenticated user, or nil.
//
// It resolves once per request and caches: the id in the session names the
// user, and when it names nobody the recaller cookie gets one chance to. A user
// that came back from the cookie is signed in properly on the spot -- the
// session is written and the Login event fires with remember true -- which is
// why a person who ticked the box never sees the sign-in form again.
//
// The Guard contract gives it no context.Context, because the PHP has no
// argument there to transpose; the lookup runs on the request's context. A
// provider that fails is a provider that found nobody, for the same reason:
// there is nowhere to put the error, and a guard that cannot read the user
// store has no user.
func (g *SessionGuard) User() Authenticatable {
	if g.loggedOut {
		return nil
	}

	// If we have already retrieved the user for the current request we can just
	// return it back immediately.
	if g.user != nil {
		return g.user
	}

	ctx := g.context()

	// First we will try to load the user using the identifier in the session if
	// one exists.
	if id := g.session.Get(g.GetName()); id != nil {
		g.user = g.retrieveByID(ctx, id)

		if g.user != nil {
			g.fireAuthenticatedEvent(g.user)
		}
	}

	// If the user is null, but we read a "recaller" cookie we can attempt to
	// pull the user data on that cookie, which serves as a remember cookie.
	if g.user == nil {
		if recaller := g.recaller(); recaller != nil {
			g.user = g.userFromRecaller(ctx, recaller)

			if g.user != nil {
				g.updateSession(ctx, g.user.GetAuthIdentifier())

				g.fireLoginEvent(g.user, true)
			}
		}
	}

	return g.user
}

// userFromRecaller is SessionGuard::userFromRecaller: the user the "remember
// me" cookie names, at most once per request.
func (g *SessionGuard) userFromRecaller(ctx context.Context, recaller *Recaller) Authenticatable {
	if !recaller.Valid() || g.recallAttempted {
		return nil
	}

	g.recallAttempted = true

	user, err := g.provider.RetrieveByToken(ctx, recaller.ID(), recaller.Token())
	if err != nil {
		user = nil
	}

	g.viaRemember = user != nil

	return user
}

// recaller is SessionGuard::recaller: the recaller cookie on this request.
func (g *SessionGuard) recaller() *Recaller {
	if g.request == nil {
		return nil
	}

	if value := g.request.Cookie(g.GetRecallerName()); value != "" {
		return NewRecaller(value)
	}
	return nil
}

// ID is SessionGuard::id: the authenticated user's identifier.
//
// It falls back to the id in the session, so a request that has not resolved
// the user yet still knows who it is about.
func (g *SessionGuard) ID() any {
	if g.loggedOut {
		return nil
	}

	if user := g.User(); user != nil {
		return user.GetAuthIdentifier()
	}
	return g.session.Get(g.GetName())
}

// Once is SessionGuard::once: sign in for this request only, with no session
// and no cookie.
func (g *SessionGuard) Once(ctx context.Context, credentials map[string]any) bool {
	g.fireAttemptEvent(credentials, false)

	if g.Validate(ctx, credentials) {
		g.rehashPasswordIfRequired(ctx, g.lastAttempted, credentials)

		g.SetUser(g.lastAttempted)

		return true
	}

	g.fireFailedEvent(g.lastAttempted, credentials)

	return false
}

// OnceUsingID is SessionGuard::onceUsingId: sign the given id in for this
// request only.
//
// The PHP answers false when there is no such user; the contract answers with
// an Authenticatable, so this answers nil.
func (g *SessionGuard) OnceUsingID(ctx context.Context, id any) Authenticatable {
	if user := g.retrieveByID(ctx, id); user != nil {
		g.SetUser(user)

		return user
	}
	return nil
}

// Validate is SessionGuard::validate: are these credentials good, without
// signing anybody in.
//
// It runs inside the timebox, and only a match asks to return early. That is
// the whole point: an address nobody registered and an address with the wrong
// password take the same time to refuse, so the clock cannot be read as a list
// of customers.
func (g *SessionGuard) Validate(ctx context.Context, credentials map[string]any) bool {
	validated, _ := g.timebox.Call(func(timebox Timebox) (any, error) {
		user := g.retrieveByCredentials(ctx, credentials)

		g.lastAttempted = user

		validated := g.hasValidCredentials(ctx, user, credentials)

		if validated {
			timebox.ReturnEarly()
		}

		return validated, nil
	}, g.timeboxDuration)

	ok, _ := validated.(bool)
	return ok
}

// Basic is SessionGuard::basic: sign in from the HTTP Basic header, with a
// session, unless somebody is signed in already.
//
// The PHP returns a response or throws; this returns nil when the request may
// carry on and [ErrInvalidBasicCredentials] when it may not.
func (g *SessionGuard) Basic(ctx context.Context, field string, extraConditions map[string]any) error {
	if g.Check() {
		return nil
	}

	// If a username is set on the HTTP basic request, we will return out without
	// interrupting the request lifecycle.
	if g.attemptBasic(ctx, g.GetRequest(), field, extraConditions) {
		return nil
	}

	return g.failedBasicResponse()
}

// OnceBasic is SessionGuard::onceBasic: a stateless HTTP Basic sign-in.
func (g *SessionGuard) OnceBasic(ctx context.Context, field string, extraConditions map[string]any) error {
	credentials := g.basicCredentials(g.GetRequest(), field)

	if !g.Once(ctx, mergeCredentials(credentials, extraConditions)) {
		return g.failedBasicResponse()
	}
	return nil
}

// attemptBasic is SessionGuard::attemptBasic.
func (g *SessionGuard) attemptBasic(ctx context.Context, request Request, field string, extraConditions map[string]any) bool {
	if request == nil || request.GetUser() == "" {
		return false
	}

	return g.Attempt(ctx, mergeCredentials(g.basicCredentials(request, field), extraConditions), false)
}

// basicCredentials is SessionGuard::basicCredentials: the Basic header as a
// credentials map.
func (g *SessionGuard) basicCredentials(request Request, field string) map[string]any {
	if request == nil {
		return map[string]any{field: "", "password": ""}
	}
	return map[string]any{field: request.GetUser(), "password": request.GetPassword()}
}

// failedBasicResponse is SessionGuard::failedBasicResponse.
func (g *SessionGuard) failedBasicResponse() error {
	return ErrInvalidBasicCredentials
}

// Attempt is SessionGuard::attempt: the sign-in a login form calls.
//
// Everything happens inside the timebox, and only success returns early, so a
// refusal takes the configured minimum however early it was decided.
func (g *SessionGuard) Attempt(ctx context.Context, credentials map[string]any, remember bool) bool {
	attempted, _ := g.timebox.Call(func(timebox Timebox) (any, error) {
		g.fireAttemptEvent(credentials, remember)

		user := g.retrieveByCredentials(ctx, credentials)

		g.lastAttempted = user

		// If an implementation of Authenticatable was returned, we ask the
		// provider to validate it against the credentials, and if they are in
		// fact valid we log the user in and return true.
		if g.hasValidCredentials(ctx, user, credentials) {
			g.rehashPasswordIfRequired(ctx, user, credentials)

			g.Login(ctx, user, remember)

			timebox.ReturnEarly()

			return true, nil
		}

		// If the attempt fails we fire an event, so that the person may be told
		// about an attempt to reach their account from somewhere they do not
		// recognise.
		g.fireFailedEvent(user, credentials)

		return false, nil
	}, g.timeboxDuration)

	ok, _ := attempted.(bool)
	return ok
}

// AttemptWhen is SessionGuard::attemptWhen: [SessionGuard.Attempt], with
// callbacks that get a veto after the password matched.
//
// It is where "this account is suspended" belongs -- a check that must not
// change the answer to "is this the right password", and must not tell the
// person guessing which of the two failed.
//
// The PHP takes array|callable|null and runs it through Arr::wrap; Go takes the
// slice, and one callback is a slice of one.
func (g *SessionGuard) AttemptWhen(ctx context.Context, credentials map[string]any, callbacks []func(user Authenticatable, guard *SessionGuard) bool, remember bool) bool {
	attempted, _ := g.timebox.Call(func(timebox Timebox) (any, error) {
		g.fireAttemptEvent(credentials, remember)

		user := g.retrieveByCredentials(ctx, credentials)

		g.lastAttempted = user

		// This does the same thing as Attempt, and also runs the callbacks once
		// the user has been retrieved and validated. If one of them says no, the
		// person is not signed in.
		if g.hasValidCredentials(ctx, user, credentials) && g.shouldLogin(callbacks, user) {
			g.rehashPasswordIfRequired(ctx, user, credentials)

			g.Login(ctx, user, remember)

			timebox.ReturnEarly()

			return true, nil
		}

		g.fireFailedEvent(user, credentials)

		return false, nil
	}, g.timeboxDuration)

	ok, _ := attempted.(bool)
	return ok
}

// hasValidCredentials is SessionGuard::hasValidCredentials.
func (g *SessionGuard) hasValidCredentials(ctx context.Context, user Authenticatable, credentials map[string]any) bool {
	validated := user != nil && g.provider.ValidateCredentials(ctx, user, credentials)

	if validated {
		g.fireValidatedEvent(user)
	}

	return validated
}

// shouldLogin is SessionGuard::shouldLogin.
func (g *SessionGuard) shouldLogin(callbacks []func(user Authenticatable, guard *SessionGuard) bool, user Authenticatable) bool {
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		if !callback(user, g) {
			return false
		}
	}
	return true
}

// rehashPasswordIfRequired is SessionGuard::rehashPasswordIfRequired: upgrade a
// hash made with weaker parameters, now that the plain password is in hand.
//
// The PHP returns nothing and so does this: the sign-in already succeeded, and
// a hash that could not be rewritten is not a reason to refuse it.
func (g *SessionGuard) rehashPasswordIfRequired(ctx context.Context, user Authenticatable, credentials map[string]any) {
	if g.rehashOnLogin && user != nil {
		_ = g.provider.RehashPasswordIfRequired(ctx, user, credentials, false)
	}
}

// LoginUsingID is SessionGuard::loginUsingId.
//
// The PHP answers false when there is no such user; this answers nil, for the
// reason [SessionGuard.OnceUsingID] gives.
func (g *SessionGuard) LoginUsingID(ctx context.Context, id any, remember bool) Authenticatable {
	if user := g.retrieveByID(ctx, id); user != nil {
		g.Login(ctx, user, remember)

		return user
	}
	return nil
}

// Login is SessionGuard::login: put the user in the session, and in the cookie
// if they asked to be remembered.
//
// The session id is regenerated here, which is what stops a session handed to
// the browser before sign-in from being one after it.
func (g *SessionGuard) Login(ctx context.Context, user Authenticatable, remember bool) {
	g.updateSession(ctx, user.GetAuthIdentifier())

	// If the user should be permanently "remembered" we queue a cookie holding
	// the identifier, the remember token and a MAC of the password hash.
	if remember {
		g.ensureRememberTokenIsSet(ctx, user)

		// The contract returns nothing, so a cookie jar that was never set
		// cannot be reported from here. It surfaces at the next call to
		// GetCookieJar by somebody who can say so.
		_ = g.queueRecallerCookie(user)
	}

	g.fireLoginEvent(user, remember)

	g.SetUser(user)
}

// updateSession is SessionGuard::updateSession.
func (g *SessionGuard) updateSession(ctx context.Context, id any) {
	g.session.Put(g.GetName(), id)

	_ = g.session.Regenerate(ctx, true)
}

// ensureRememberTokenIsSet is SessionGuard::ensureRememberTokenIsSet.
func (g *SessionGuard) ensureRememberTokenIsSet(ctx context.Context, user Authenticatable) {
	if user.GetRememberToken() == "" {
		g.cycleRememberToken(ctx, user)
	}
}

// queueRecallerCookie is SessionGuard::queueRecallerCookie.
func (g *SessionGuard) queueRecallerCookie(user Authenticatable) error {
	jar, err := g.GetCookieJar()
	if err != nil {
		return err
	}

	recaller, err := g.createRecaller(
		fmt.Sprint(user.GetAuthIdentifier()) + "|" +
			user.GetRememberToken() + "|" +
			g.HashPasswordForCookie(user.GetAuthPassword()),
	)
	if err != nil {
		return err
	}

	jar.Queue(recaller)

	return nil
}

// createRecaller is SessionGuard::createRecaller.
func (g *SessionGuard) createRecaller(value string) (*http.Cookie, error) {
	jar, err := g.GetCookieJar()
	if err != nil {
		return nil, err
	}

	// The PHP passes three arguments and lets the factory default the rest.
	// hesape/cookie takes Symfony's whole list, so the defaults are written out:
	// the jar's path and domain, its secure setting, http only, not raw, and the
	// jar's SameSite.
	return jar.Make(g.GetRecallerName(), value, g.getRememberDuration(), "", "", nil, true, false, http.SameSiteDefaultMode), nil
}

// HashPasswordForCookie is SessionGuard::hashPasswordForCookie: a MAC of the
// password hash, for the third segment of the recaller.
//
// It is what lets AuthenticateSession notice that the password changed since
// the cookie was written, without the cookie carrying the hash itself.
func (g *SessionGuard) HashPasswordForCookie(passwordHash string) string {
	key := g.hashKey
	if key == "" {
		key = defaultHashKey
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(passwordHash))

	return hex.EncodeToString(mac.Sum(nil))
}

// Logout is SessionGuard::logout: out of this application, everywhere the
// cookie reached.
//
// It cycles the remember token, so the recaller cookie sitting in another
// browser stops naming anybody.
func (g *SessionGuard) Logout(ctx context.Context) {
	user := g.User()

	_ = g.clearUserDataFromStorage()

	if g.user != nil && user != nil && user.GetRememberToken() != "" {
		g.cycleRememberToken(ctx, user)
	}

	if g.events != nil {
		g.events.Dispatch(Logout{Guard: g.Name, User: user})
	}

	// Once the event has fired we clear the user out of memory: they are no
	// longer signed in and must not be available from here.
	g.user = nil

	g.loggedOut = true
}

// LogoutCurrentDevice is SessionGuard::logoutCurrentDevice: out of this
// browser only.
//
// It does not cycle the remember token, so the cookie in the other browser
// keeps working. That is the difference from [SessionGuard.Logout], and it is
// the whole method -- and the reason it takes no context: not cycling the token
// is not writing to the user store.
func (g *SessionGuard) LogoutCurrentDevice() {
	user := g.User()

	_ = g.clearUserDataFromStorage()

	if g.events != nil {
		g.events.Dispatch(CurrentDeviceLogout{Guard: g.Name, User: user})
	}

	g.user = nil

	g.loggedOut = true
}

// clearUserDataFromStorage is SessionGuard::clearUserDataFromStorage.
func (g *SessionGuard) clearUserDataFromStorage() error {
	g.session.Remove(g.GetName())

	jar, err := g.GetCookieJar()
	if err != nil {
		return err
	}

	jar.Unqueue(g.GetRecallerName(), "")

	if g.recaller() != nil {
		jar.Queue(jar.Forget(g.GetRecallerName(), "", ""))
	}

	return nil
}

// cycleRememberToken is SessionGuard::cycleRememberToken: a new remember token,
// on the user and in the store.
func (g *SessionGuard) cycleRememberToken(ctx context.Context, user Authenticatable) {
	token := randomToken(rememberTokenLength)

	user.SetRememberToken(token)

	_ = g.provider.UpdateRememberToken(ctx, user, token)
}

// LogoutOtherDevices is SessionGuard::logoutOtherDevices: invalidate this
// person's other sessions, keeping this one.
//
// It works by rehashing the password, which changes the MAC every other
// session's recaller carries -- so the AuthenticateSession middleware turns
// them away on their next request. The application must be using that
// middleware for this to mean anything.
//
// The PHP declares a user as its return and its body returns nothing, so the
// answer is always nil when the password matched; the signature is kept as the
// PHP declares it. A password that does not match is [ErrPasswordMismatch].
func (g *SessionGuard) LogoutOtherDevices(ctx context.Context, password string) (Authenticatable, error) {
	if g.User() == nil {
		return nil, nil
	}

	result, err := g.rehashUserPasswordForDeviceLogout(ctx, password)
	if err != nil {
		return nil, err
	}

	// The PHP asks the cookie jar only when there is no recaller on the request,
	// because || stops at the first true. So does this, which is why a guard
	// with no jar and a recaller in hand does not fail here.
	reissue := g.recaller() != nil
	if !reissue {
		jar, err := g.GetCookieJar()
		if err != nil {
			return nil, err
		}
		reissue = jar.HasQueued(g.GetRecallerName(), "")
	}

	if reissue {
		if err := g.queueRecallerCookie(g.User()); err != nil {
			return nil, err
		}
	}

	g.fireOtherDeviceLogoutEvent(g.User())

	return result, nil
}

// rehashUserPasswordForDeviceLogout is
// SessionGuard::rehashUserPasswordForDeviceLogout.
func (g *SessionGuard) rehashUserPasswordForDeviceLogout(ctx context.Context, password string) (Authenticatable, error) {
	user := g.User()

	if g.Hasher == nil {
		return nil, ErrHasherNotSet
	}

	if !g.Hasher.Check(password, user.GetAuthPassword()) {
		return nil, ErrPasswordMismatch
	}

	if err := g.provider.RehashPasswordIfRequired(ctx, user, map[string]any{"password": password}, true); err != nil {
		return nil, err
	}

	return nil, nil
}

// Attempting is SessionGuard::attempting: register a listener for the
// [Attempting] event.
//
// The PHP takes mixed, and so does this: what a listener may be is the
// dispatcher's business, not the guard's.
func (g *SessionGuard) Attempting(callback any) {
	if g.events != nil {
		g.events.Listen(Attempting{}, callback)
	}
}

// fireAttemptEvent is SessionGuard::fireAttemptEvent.
func (g *SessionGuard) fireAttemptEvent(credentials map[string]any, remember bool) {
	if g.events != nil {
		g.events.Dispatch(Attempting{Guard: g.Name, Credentials: credentials, Remember: remember})
	}
}

// fireValidatedEvent is SessionGuard::fireValidatedEvent.
func (g *SessionGuard) fireValidatedEvent(user Authenticatable) {
	if g.events != nil {
		g.events.Dispatch(Validated{Guard: g.Name, User: user})
	}
}

// fireLoginEvent is SessionGuard::fireLoginEvent.
func (g *SessionGuard) fireLoginEvent(user Authenticatable, remember bool) {
	if g.events != nil {
		g.events.Dispatch(Login{Guard: g.Name, User: user, Remember: remember})
	}
}

// fireAuthenticatedEvent is SessionGuard::fireAuthenticatedEvent.
func (g *SessionGuard) fireAuthenticatedEvent(user Authenticatable) {
	if g.events != nil {
		g.events.Dispatch(Authenticated{Guard: g.Name, User: user})
	}
}

// fireOtherDeviceLogoutEvent is SessionGuard::fireOtherDeviceLogoutEvent.
func (g *SessionGuard) fireOtherDeviceLogoutEvent(user Authenticatable) {
	if g.events != nil {
		g.events.Dispatch(OtherDeviceLogout{Guard: g.Name, User: user})
	}
}

// fireFailedEvent is SessionGuard::fireFailedEvent.
func (g *SessionGuard) fireFailedEvent(user Authenticatable, credentials map[string]any) {
	if g.events != nil {
		g.events.Dispatch(Failed{Guard: g.Name, User: user, Credentials: credentials})
	}
}

// GetLastAttempted is SessionGuard::getLastAttempted: the user the last attempt
// retrieved, whether or not the password matched.
func (g *SessionGuard) GetLastAttempted() Authenticatable {
	return g.lastAttempted
}

// GetName is SessionGuard::getName: the session key the user id is kept under.
func (g *SessionGuard) GetName() string {
	return "login_" + g.Name + "_" + classHash(sessionGuardClass)
}

// GetRecallerName is SessionGuard::getRecallerName: the name of the "remember
// me" cookie.
func (g *SessionGuard) GetRecallerName() string {
	return "remember_" + g.Name + "_" + classHash(sessionGuardClass)
}

// ViaRemember is SessionGuard::viaRemember: this session came from the cookie,
// not from a password typed in this browser session.
//
// A destructive action is the right moment to read it and ask for the password
// again.
func (g *SessionGuard) ViaRemember() bool {
	return g.viaRemember
}

// getRememberDuration is SessionGuard::getRememberDuration.
func (g *SessionGuard) getRememberDuration() int {
	return g.rememberDuration
}

// SetRememberDuration is SessionGuard::setRememberDuration: how many minutes
// the "remember me" cookie is good for.
func (g *SessionGuard) SetRememberDuration(minutes int) *SessionGuard {
	g.rememberDuration = minutes

	return g
}

// GetCookieJar is SessionGuard::getCookieJar.
//
// The PHP throws a RuntimeException when nobody set one; this answers
// [ErrCookieJarNotSet], which is ADR 0044's second mechanical change.
func (g *SessionGuard) GetCookieJar() (CookieJar, error) {
	if g.cookie == nil {
		return nil, ErrCookieJarNotSet
	}
	return g.cookie, nil
}

// SetCookieJar is SessionGuard::setCookieJar.
func (g *SessionGuard) SetCookieJar(cookie CookieJar) {
	g.cookie = cookie
}

// GetDispatcher is SessionGuard::getDispatcher.
func (g *SessionGuard) GetDispatcher() Dispatcher {
	return g.events
}

// SetDispatcher is SessionGuard::setDispatcher.
func (g *SessionGuard) SetDispatcher(events Dispatcher) {
	g.events = events
}

// GetSession is SessionGuard::getSession.
func (g *SessionGuard) GetSession() Session {
	return g.session
}

// GetUser is SessionGuard::getUser: the user already resolved, without
// resolving one.
func (g *SessionGuard) GetUser() Authenticatable {
	return g.user
}

// SetUser is SessionGuard::setUser.
//
// It undoes a logout, as the PHP's does, and it fires [Authenticated]. It
// returns nothing, because the Guard contract's SetUser returns nothing.
func (g *SessionGuard) SetUser(user Authenticatable) {
	g.user = user

	g.loggedOut = false

	g.fireAuthenticatedEvent(user)
}

// GetRequest is SessionGuard::getRequest.
//
// The PHP falls back to Request::createFromGlobals(). Go has no request in a
// global: it arrives at a handler and is passed in, so a guard that was given
// none has none, and this answers nil.
func (g *SessionGuard) GetRequest() Request {
	return g.request
}

// SetRequest is SessionGuard::setRequest.
func (g *SessionGuard) SetRequest(request Request) *SessionGuard {
	g.request = request

	return g
}

// GetTimebox is SessionGuard::getTimebox.
func (g *SessionGuard) GetTimebox() Timebox {
	return g.timebox
}

// context is the context the guard runs its lookups on: the request's, so that
// cancelling the request stops them. See [Request] for why it is read from
// there and not taken as an argument.
func (g *SessionGuard) context() context.Context {
	if g.request != nil {
		if ctx := g.request.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

// retrieveByID is $this->provider->retrieveById with the error read as the
// PHP's null.
func (g *SessionGuard) retrieveByID(ctx context.Context, id any) Authenticatable {
	user, err := g.provider.RetrieveByID(ctx, id)
	if err != nil {
		return nil
	}
	return user
}

// retrieveByCredentials is $this->provider->retrieveByCredentials with the
// error read as the PHP's null.
func (g *SessionGuard) retrieveByCredentials(ctx context.Context, credentials map[string]any) Authenticatable {
	user, err := g.provider.RetrieveByCredentials(ctx, credentials)
	if err != nil {
		return nil
	}
	return user
}

// mergeCredentials is the array_merge the two Basic methods write: the extra
// conditions win, as the second array does in PHP.
func mergeCredentials(credentials, extra map[string]any) map[string]any {
	merged := make(map[string]any, len(credentials)+len(extra))
	for key, value := range credentials {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

// classHash is the sha1(static::class) of getName and getRecallerName.
func classHash(class string) string {
	sum := sha1.Sum([]byte(class))
	return hex.EncodeToString(sum[:])
}

// randomToken is Str::random: a token of the given length, from the operating
// system's randomness.
func randomToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand does not fail on any platform this runs on, and a
		// remember token that is not random is worse than no token at all.
		panic("auth: the operating system refused randomness for a remember token: " + err.Error())
	}

	token := make([]byte, length)
	for i, b := range bytes {
		token[i] = rememberTokenAlphabet[int(b)%len(rememberTokenAlphabet)]
	}
	return string(token)
}

// Attempting is Illuminate\Auth\Events\Attempting: somebody is trying to sign
// in, before anything has been looked up.
//
// The eight events below are Illuminate\Auth\Events, and they are here rather
// than in hesape/auth/events because the root of auth imports nothing outside
// the standard library (see doc.go) and a guard cannot fire an event it cannot
// name. A listener registers against the value, as hesape/events does:
//
//	dispatcher.Listen(auth.Login{}, func(e auth.Login) { ... })
type Attempting struct {
	// Guard is $guard: the name of the guard.
	Guard string

	// Credentials is $credentials.
	Credentials map[string]any

	// Remember is $remember.
	Remember bool
}

// Authenticated is Illuminate\Auth\Events\Authenticated: a user was resolved
// for this request.
type Authenticated struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable
}

// Validated is Illuminate\Auth\Events\Validated: the credentials were good,
// before anybody was signed in.
type Validated struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable
}

// Login is Illuminate\Auth\Events\Login: somebody signed in.
type Login struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable

	// Remember is $remember.
	Remember bool
}

// Logout is Illuminate\Auth\Events\Logout: somebody signed out, everywhere.
type Logout struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable
}

// CurrentDeviceLogout is Illuminate\Auth\Events\CurrentDeviceLogout: somebody
// signed out of this browser only.
type CurrentDeviceLogout struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable
}

// OtherDeviceLogout is Illuminate\Auth\Events\OtherDeviceLogout: somebody
// invalidated their other sessions.
type OtherDeviceLogout struct {
	// Guard is $guard.
	Guard string

	// User is $user.
	User Authenticatable
}

// Failed is Illuminate\Auth\Events\Failed: an attempt that did not work.
//
// User is the account the credentials named, and it is nil when they named
// nobody. It is the event a "somebody tried to sign in to your account" notice
// listens for.
type Failed struct {
	// Guard is $guard.
	Guard string

	// User is $user, nil when the credentials matched no account.
	User Authenticatable

	// Credentials is $credentials.
	Credentials map[string]any
}
