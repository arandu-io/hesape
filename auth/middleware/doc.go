// Package middleware holds the authentication and authorization middleware.
//
// [Authenticate] turns away a request nobody is signed in for,
// [AuthenticateWithBasicAuth] signs one in from the Authorization header,
// [Authorize] runs a gate check, [EnsureEmailIsVerified] turns away an account
// whose address was never confirmed, [RedirectIfAuthenticated] sends a
// signed-in visitor away from the login form, and [RequirePassword] asks for
// the password again before something destructive.
//
// # The shape of a middleware here
//
// Each one is a struct with a Handle method, and Handle is
// func(http.Handler) http.Handler -- which is what http.Middleware is an alias
// of, so they compose with pipeline.Chain like everything else. The method is
// called Handle, never ServeHTTP.
//
// A middleware's parameters are bound to the value rather than passed at the
// call site: Using and RedirectTo return a COPY of the middleware with them
// set, and Handle takes only the next handler.
//
//	authenticate := middleware.NewAuthenticate(factory, subjectFor)
//	guarded := pipeline.Chain(handler, authenticate.Using("web").Handle)
//
// Each one writes the refusal itself rather than raising it for somebody else
// to render: 401 as a problem document when the request asked for JSON, and a
// redirect otherwise. hesape/session's own AuthenticateSession already answered
// this way, and a second convention would be a second answer to the same
// question.
//
// The document is exception.WriteProblem's, and not one written here. A refusal
// from a middleware and a failure from a handler are one thing to the client,
// so they are one shape.
//
// # The collaborators, and why they are interfaces
//
// [Factory] is the part of a guard factory these use, and [Gate] the part of a
// gate that [Authorize] uses. Both are declared here, minimally, so that this
// package compiles against hesape/auth and hesape/auth/access without depending
// on which concrete guard or gate an application wires. auth.Guard,
// auth.Authenticatable and auth.MustVerifyEmail are NOT redeclared -- they are
// contracts, they already exist in hesape/auth, and a second copy is how two
// definitions drift.
//
// # The subject
//
// [Authenticate] is what calls auth.WithSubject, which is how the rest of the
// request reads who is acting. Turning an auth.Authenticatable into an
// auth.Subject needs a tenant and roles, and the seven methods of Authenticatable
// carry neither, so the mapping is a [SubjectResolver] the application supplies.
// It must read the tenant from the session, never from the request.
package middleware
