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
// over an address. What is in httpx/middleware applies to every request that
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
// SubstituteBindings.php has no counterpart and will not get one: it is
// Laravel's implicit model binding, which resolves a route parameter into a
// loaded record by reflecting on a controller's type hints. Reaching the record
// is the service's job, behind a Policy, and the mechanism that would do it
// here is the one docs/01 rejected by name.
//
// ThrottleRequestsWithRedis.php has no counterpart either, for the reason
// hesape/cache gives: there is one RateLimiter and which store it counts in is
// wiring, not a second middleware.
package middleware
