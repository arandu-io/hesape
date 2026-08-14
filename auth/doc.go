// Package auth mirrors Illuminate\Auth.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth:
//
//	AuthManager.php
//	AuthServiceProvider.php
//	Authenticatable.php
//	AuthenticationException.php
//	CreatesUserProviders.php
//	DatabaseUserProvider.php
//	EloquentUserProvider.php
//	GenericUser.php
//	GuardHelpers.php
//	MustVerifyEmail.php
//	Recaller.php
//	RequestGuard.php
//	SessionGuard.php
//	TokenGuard.php
//
// The root of this package is the Access half of Illuminate\Auth: who is
// acting (Subject), what they mean to do (Action), the Policy that decides,
// and the Grant that proves a decision happened. Nothing reaches a repository
// without one, which is the thesis of the framework stated as a type -- see
// Grant.
//
// It also holds the sign-in throttle, because the counter that stops a leaked
// password list from being tried against an account belongs next to the
// decision it protects, not in a route limiter that knows nothing about
// identities.
//
// # The guards
//
// The three Illuminate guards are here, with the trait they share and the
// pieces they read:
//
//	GuardHelpers.php   -> guard_helpers.go, a struct the guards embed
//	SessionGuard.php   -> session_guard.go
//	TokenGuard.php     -> token_guard.go
//	RequestGuard.php   -> request_guard.go
//	Recaller.php       -> recaller.go
//	GenericUser.php    -> generic_user.go
//	MustVerifyEmail.php -> must_verify_email.go, as MustVerifyEmailTrait
//	AuthenticationException.php -> authentication_error.go
//	AuthManager.php    -> manager.go
//	CreatesUserProviders.php -> manager.go, folded into AuthManager
//
// The eight Illuminate\Auth\Events the session guard fires are in
// session_guard.go, and the interfaces the guards need from the session, the
// cookie jar, the event dispatcher, the request and the timebox are in
// collaborators.go.
//
// The root imports nothing but the standard library, deliberately. Everything
// that scopes itself by tenant -- the database, the cache, the filesystem, the
// scheduler -- imports this package to read Tenant off a Grant, so a dependency
// here would be a dependency everywhere. That is why the collaborators are
// interfaces declared here, in the consuming package, with the signatures
// hesape/session, hesape/cookie, hesape/events and hesape/http already have:
// they satisfy them structurally, and nothing imports anything.
//
// # What is elsewhere
//
// EloquentUserProvider, DatabaseUserProvider and Authenticatable's Eloquent
// half are hesape/auth/users; the password reset flow is hesape/auth/passwords
// and hesape/auth/notifications; the middleware is hesape/auth/middleware; the
// Gate is hesape/auth/access.
//
// # What has no counterpart, and why
//
//   - AuthServiceProvider.php. A service provider registers things in a
//     container, and there is none (ADR 0001, ADR 0002).
//   - AuthManager::setApplication. The container again: the manager is handed
//     what it needs, in ManagerConfig.
//   - AuthManager::__call. PHP's dynamic dispatch, which forwards any unknown
//     method to the default guard so that Auth::user() reaches
//     SessionGuard::user(). Go has no such hook: call Guard, then the method.
//   - $this->app->refresh('request', $guard, 'setRequest'), in
//     createSessionDriver, createTokenDriver and viaRequest. It re-injects the
//     request into a guard that outlived it. Here a guard is built per request,
//     or given one with SetRequest.
//   - The Macroable trait on all three guards. Macros are methods added at run
//     time through __call, which is the same PHP feature and the same answer.
//   - CreatesUserProviders::createDatabaseProvider and createEloquentProvider.
//     Those two providers are hesape/auth/users, which this package cannot
//     import; register them with AuthManager.Provider, the registry the PHP's
//     customProviderCreators is.
//   - GenericUser::__get, __set, __isset and __unset. PHP's property hooks:
//     they are the exported Attributes map and the Get method.
//   - Recaller::__construct's unserialize(). PHP's serialization format, and
//     there are no legacy Arandu cookies written in it.
//   - SessionGuard::getRequest's Request::createFromGlobals(). Go has no
//     request in a global; a guard that was given none has none.
//   - Gate::guessPolicyNamesUsing and Gate::resolvePolicy. Both turn a class
//     name into a policy class name by string -- App\Models\Post becomes
//     App\Policies\PostPolicy -- and then instantiate it out of the container.
//     Go resolves nothing by name at run time and has no container for the
//     second half, so a policy is registered against the type it decides
//     about; see Gate.Policy in hesape/auth/access.
//   - ServiceProvider::register and ServiceProvider::provides, on
//     AuthServiceProvider and PasswordResetServiceProvider. Deferred
//     registration in a container, twice over.
//   - Gate::setContainer. The container, once more.
//   - Auth::routes, reason 2. It is a Facade method and nothing else: it asks
//     the application whether laravel/ui's service provider is loaded, throws
//     with the composer command to run if it is not, and otherwise calls the
//     router macro that provider registered. There is no facade (ADR 0002), no
//     application to ask (ADR 0001) and no macro to call (macros are __call).
//     The nine routes it registers are published into the project by
//     `go run github.com/arandu-io/ui@latest auth`, into the application's own
//     routes file, where they can be read and edited (ADR 0026). That is the
//     one way to get them: a framework method that mounts routes nobody wrote
//     would be the second, and it would mount them with a handler whose
//     auth.Grant nothing in the application chose -- which is where a tenant
//     starts coming from a path instead of from the Grant (RULE 14).
package auth
