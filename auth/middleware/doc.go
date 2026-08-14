// Package middleware mirrors Illuminate\Auth\Middleware.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Middleware:
//
//	Authenticate.php              -> [Authenticate]
//	AuthenticateWithBasicAuth.php -> [AuthenticateWithBasicAuth]
//	Authorize.php                 -> [Authorize]
//	EnsureEmailIsVerified.php     -> [EnsureEmailIsVerified]
//	RedirectIfAuthenticated.php   -> [RedirectIfAuthenticated]
//	RequirePassword.php           -> [RequirePassword]
//
// # The shape of a middleware here
//
// Each one is a struct with a Handle method, and Handle is
// func(http.Handler) http.Handler -- which is what http.Middleware is an alias
// of, so they compose with pipeline.Chain like everything else. The name is
// Laravel's (ADR 0044): Handle, never ServeHTTP.
//
// PHP passes the middleware's parameters as a colon-separated string on the
// route -- "auth:web,api" -- and reads them back as the variadic tail of
// handle(). Go has no such registry, so the parameters are bound to the value:
// Using and RedirectTo return a COPY of the middleware with them set, and Handle
// takes only the next handler. The names are unchanged.
//
//	authenticate := middleware.NewAuthenticate(factory, subjectFor)
//	guarded := pipeline.Chain(handler, authenticate.Using("web").Handle)
//
// PHP throws out of handle() and lets the exception handler decide the status.
// There are no exceptions in Go, so each middleware writes the answer the
// Illuminate handler would have rendered: 401 with a JSON body when the request
// asked for JSON, and a redirect otherwise. hesape/session's own
// AuthenticateSession already answered this way, and a second convention would
// be a second answer to the same question (RULE 9).
//
// # The collaborators, and why they are interfaces
//
// [Factory] is the part of Illuminate\Contracts\Auth\Factory these use, and
// [Gate] the part of Illuminate\Contracts\Auth\Access\Gate that [Authorize]
// uses. Both are declared here, minimally, so that this package compiles against
// hesape/auth and hesape/auth/access without depending on which concrete guard
// or gate an application wires. auth.Guard, auth.Authenticatable and
// auth.MustVerifyEmail are NOT redeclared -- they are contracts, they already
// exist in hesape/auth, and a second copy is how two definitions drift.
//
// # The subject
//
// [Authenticate] is what calls auth.WithSubject, which is how the rest of the
// request reads who is acting. Turning an auth.Authenticatable into an
// auth.Subject needs a tenant and roles, and the seven methods of Authenticatable
// carry neither, so the mapping is a [SubjectResolver] the application supplies.
// It must read the tenant from the session, never from the request (RULE 14).
//
// # What is skipped, and why
//
//   - Authenticate::__construct, AuthenticateWithBasicAuth::__construct and
//     Authorize::__construct take their collaborator from the container. There is
//     no container (ADR 0001), so each New takes it as an argument.
//   - RequirePassword::__construct takes a ResponseFactory and a UrlGenerator.
//     The response factory is hesape/http's package-level Redirect, and the URL
//     generator is a route name resolved by the router -- which this package must
//     not import. [RequirePassword] takes the path instead, defaulting to the URI
//     Laravel's password.confirm route has.
//   - RedirectIfAuthenticated::defaultRedirectUri probes the route registry for
//     "dashboard" and then "home". Same reason: no router here. It answers "/",
//     and RedirectUsing is how an application says otherwise.
//   - Authorize::getModel resolves a route parameter through Route::route($name).
//     Here it is http.Request.PathValue, which is the same lookup in net/http's
//     own router.
package middleware
