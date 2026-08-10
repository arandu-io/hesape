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
// The root imports nothing but the standard library, deliberately. Everything
// that scopes itself by tenant -- the database, the cache, the filesystem, the
// scheduler -- imports this package to read Tenant off a Grant, so a dependency
// here would be a dependency everywhere.
//
// What the Illuminate files above become elsewhere: SessionGuard and Recaller
// are the session store in hesape/session; EloquentUserProvider and
// Authenticatable are hesape/auth/users; MustVerifyEmail and the reset flow are
// hesape/auth/passwords and hesape/auth/notifications; the middleware is
// hesape/auth/middleware. AuthManager and AuthServiceProvider have no
// counterpart at all: there is no guard to select and no container to register
// one in (ADR 0001, ADR 0002).
package auth
