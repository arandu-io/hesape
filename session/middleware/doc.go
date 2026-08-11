// Package middleware mirrors Illuminate\Session\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/Session/Middleware:
//
//	AuthenticateSession.php
//	StartSession.php
//
// [StartSession] loads the session at the start of a request, puts it on the
// context where [Session] reads it back out, and writes it -- and its cookie --
// before the first byte of the response. [AuthenticateSession] ends a session
// whose password has changed underneath it, which is what makes "change my
// password" sign out the other browsers.
//
// Both return pipeline.Middleware[http.Handler], which is what httpx.Middleware
// is an alias of, so they compose with everything else without this package
// importing the HTTP layer.
//
// The two collaborators neither of them may import -- the lock store and the
// authentication guard -- are declared here as [LockFactory] and [Guard], with
// the smallest surface each of them uses. hesape/cache and hesape/auth are what
// an application wires behind them.
package middleware
