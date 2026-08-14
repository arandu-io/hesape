// Package middleware holds the two middlewares that decide whether a request
// reaches a route at all.
//
// Throttle counts attempts against a hesape/cache rate limit and turns the
// caller away when the budget is gone. ValidateSignature refuses a request
// whose URL was not signed by this application, and is the other half of
// routing.SignedRoute.
//
// Both are here rather than in hesape/http/middleware because both are about
// the route: the throttle's budget belongs to an endpoint, and the signature is
// over an address. What is in http/middleware applies to every request that
// arrives, whichever route it is for.
//
// It mirrors Illuminate\Routing\Middleware. The files it answers to, in the
// clone at laravel_illuminate/routing/Middleware:
//
//	SubstituteBindings.php
//	ThrottleRequests.php
//	ThrottleRequestsWithRedis.php
//	ValidateSignature.php
//
// SubstituteBindings.php has no counterpart HERE, and the reason is the Grant.
//
// The binding itself does exist -- [github.com/arandu-io/hesape/routing.Router.SubstituteBindings]
// and [github.com/arandu-io/hesape/routing.ImplicitRouteBinding.ResolveForRoute]
// -- but it lives in the routing package, where it takes an auth.Grant as a
// parameter and refuses a Grant with no tenant before it looks anything up.
//
// A middleware cannot do that. It sees an http.Handler and a *http.Request, so
// the Grant would have to be read out of the context, and a Grant read out of
// the context is a Grant that can be absent: the middleware would then either
// resolve without one -- reading every customer's rows for that id -- or push
// the tenant filter into whatever the application wrote as its binder, which is
// RULE 17 handed to the caller and therefore not enforced at all.
//
// There was a SubstituteBindings middleware here. It did exactly that: its doc
// comment said "the binder MUST filter by tenant", it took no Grant, and its
// parameter extraction never matched a parameter, so nothing it resolved was
// ever observed to be wrong. It was deleted rather than fixed, because fixing
// it would have made a second way to resolve a binding (RULE 9) and the way
// that stays is the one the compiler checks.
//
// ThrottleRequestsWithRedis.php has no counterpart either, for the reason
// hesape/cache gives: there is one RateLimiter and which store it counts in is
// wiring, not a second middleware.
package middleware
