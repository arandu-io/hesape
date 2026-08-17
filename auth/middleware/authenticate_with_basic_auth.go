package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
)

// AuthenticateWithBasicAuth signs the request in from the Authorization: Basic
// header, matching the user by one column -- the e-mail address by default.
//
// It is for a machine calling an endpoint, and for the internal tool nobody
// wants to build a sign-in screen for; a browser-facing application uses
// [Authenticate].
type AuthenticateWithBasicAuth struct {
	// auth is the guard factory instance.
	auth Factory

	// guard and field are what Using bound this copy to. An empty field means
	// "email".
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

// Using returns a copy of the middleware bound to that guard and field, for the
// reason [Authenticate.Using] gives.
func (m *AuthenticateWithBasicAuth) Using(guard, field string) *AuthenticateWithBasicAuth {
	copied := *m
	copied.guard, copied.field = guard, field
	return &copied
}

// Handle handles an incoming request.
//
// A guard that refuses the credentials is answered 401 with the
// WWW-Authenticate header, which is what turns the status into the browser's
// own password box rather than a blank page.
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
			m.unauthorized(w, r)
			return
		}

		next.ServeHTTP(w, m.withSubject(r, guard.User()))
	})
}

// unauthorized answers 401 with the Basic challenge, under the realm
// "Restricted".
func (m *AuthenticateWithBasicAuth) unauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	exception.WriteProblem(w, r, http.StatusUnauthorized, "Invalid credentials.")
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
