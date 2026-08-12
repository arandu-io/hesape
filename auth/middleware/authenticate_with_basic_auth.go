package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// AuthenticateWithBasicAuth is
// Illuminate\Auth\Middleware\AuthenticateWithBasicAuth.
//
// It signs the request in from the Authorization: Basic header, matching the
// user by one column -- the e-mail address by default. It is for a machine
// calling an endpoint, and for the internal tool nobody wants to build a
// sign-in screen for; a browser-facing application uses [Authenticate].
type AuthenticateWithBasicAuth struct {
	// auth is the guard factory instance.
	auth Factory

	// guard and field are the two parameters PHP reads off the route string.
	// The empty field means "email", which is PHP's `$field ?: 'email'`.
	guard string
	field string

	// subjectFor is how the resolved user becomes the Subject. See
	// [SubjectResolver]; [Authenticate] documents why it exists.
	subjectFor SubjectResolver
}

// NewAuthenticateWithBasicAuth returns the middleware over an authentication
// factory. subjectFor may be nil; see [NewAuthenticate].
func NewAuthenticateWithBasicAuth(a Factory, subjectFor SubjectResolver) *AuthenticateWithBasicAuth {
	return &AuthenticateWithBasicAuth{auth: a, subjectFor: subjectFor}
}

// Using specifies the guard and field for the middleware.
//
// It is AuthenticateWithBasicAuth::using. PHP returns the route string; this
// returns a copy of the middleware bound to the two parameters, for the reason
// [Authenticate.Using] gives.
func (m *AuthenticateWithBasicAuth) Using(guard, field string) *AuthenticateWithBasicAuth {
	copied := *m
	copied.guard, copied.field = guard, field
	return &copied
}

// Handle handles an incoming request.
//
// It is AuthenticateWithBasicAuth::handle, whose whole body is
// `$this->auth->guard($guard)->basic($field ?: 'email')`.
//
// PHP lets the guard throw UnauthorizedHttpException, which the handler renders
// as 401 with the WWW-Authenticate header that makes a browser show its own
// password box. There are no exceptions here, so the error the guard returns is
// answered the same way -- and the header is what turns a 401 into a prompt
// rather than a blank page.
func (m *AuthenticateWithBasicAuth) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guard := m.auth.Guard(m.guard)
		basic, ok := guard.(auth.SupportsBasicAuth)
		if !ok {
			// A guard that cannot answer basic auth is a wiring mistake, not a
			// credential one, and answering 401 would send the caller round the
			// loop of typing a password that can never work.
			http.Error(w, "The guard does not support HTTP basic authentication.", http.StatusInternalServerError)
			return
		}

		field := m.field
		if field == "" {
			field = "email"
		}

		if err := basic.Basic(r.Context(), field, nil); err != nil {
			m.unauthorized(w)
			return
		}

		next.ServeHTTP(w, m.withSubject(r, guard.User()))
	})
}

// unauthorized answers the 401 PHP's UnauthorizedHttpException renders.
//
// The realm is "Restricted", which is what Illuminate's SessionGuard passes to
// the exception it throws.
func (m *AuthenticateWithBasicAuth) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	writeJSON(w, http.StatusUnauthorized, "Invalid credentials.")
}

// withSubject puts the Subject on the request's context, when there is a
// resolver and it recognises the user.
func (m *AuthenticateWithBasicAuth) withSubject(r *http.Request, user auth.Authenticatable) *http.Request {
	if m.subjectFor == nil || user == nil {
		return r
	}
	subject, ok := m.subjectFor(r, user)
	if !ok {
		return r
	}
	return r.WithContext(auth.WithSubject(r.Context(), subject))
}
