package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
)

// Authenticate refuses a request that nobody is signed in on, and puts the
// auth.Subject on the context.
//
// The context is where every policy, every repository and hesape/http's
// Context.User read the subject back from, and this is the ONLY thing in the
// framework that calls auth.WithSubject: a second caller would be a second
// answer to "who is acting", and one of them would be a subject somebody
// assembled from the request.
type Authenticate struct {
	guestRedirect

	// auth is the authentication factory instance.
	auth Factory

	// guards are the guards this copy was bound to by Using. Empty means the
	// default guard.
	guards []string

	// subjectFor is how the resolved user becomes the Subject. See
	// [SubjectResolver].
	subjectFor SubjectResolver

	// redirectToCallback is the callback that should be used to generate the
	// authentication redirect path.
	//
	// It is a field and not package-level state, for the reason
	// session/middleware.AuthenticateSession gives for the same decision:
	// package-level mutable state is a data race the moment two tests set it,
	// under -race, and nothing can unset it afterwards.
	redirectToCallback func(r *http.Request) string
}

// NewAuthenticate returns the middleware over an authentication factory.
//
// subjectFor may be nil, in which case the request proceeds with no subject on
// its context and every authorization downstream refuses. That is the safe
// direction to fail in, and it is the only default available: a Subject with no
// tenant would be a Grant that reads every customer.
func NewAuthenticate(a Factory, subjectFor SubjectResolver) *Authenticate {
	return &Authenticate{auth: a, subjectFor: subjectFor}
}

// Using returns a copy of the middleware bound to those guards.
//
// The copy is what makes it safe to bind one middleware two ways in one
// application: mutating the receiver would mean the second call changed the
// first route's guards.
func (m *Authenticate) Using(guard string, others ...string) *Authenticate {
	copied := *m
	copied.guards = append([]string{guard}, others...)
	return &copied
}

// Handle handles an incoming request.
//
// The guards it tries are on the value, put there by [Authenticate.Using].
func (m *Authenticate) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated, ok := m.authenticate(r)
		if !ok {
			m.unauthenticated(w, r)
			return
		}
		next.ServeHTTP(w, authenticated)
	})
}

// authenticate determines if the user is logged in to any of the given guards.
//
// It returns the request carrying the subject rather than mutating one, because
// a context is only ever derived: net/http gives no way to put a value on the
// request everybody already holds.
func (m *Authenticate) authenticate(r *http.Request) (*http.Request, bool) {
	guards := m.guards
	if len(guards) == 0 {
		// The empty name is this Factory's spelling of the default guard.
		guards = []string{""}
	}

	for _, name := range guards {
		guard := m.auth.Guard(name)
		if guard == nil || !guard.Check() {
			continue
		}
		m.auth.ShouldUse(name)
		return m.withSubject(r, guard.User()), true
	}
	return r, false
}

// withSubject puts the Subject on the request's context, when there is a
// resolver and it recognises the user.
func (m *Authenticate) withSubject(r *http.Request, user auth.Authenticatable) *http.Request {
	if m.subjectFor == nil || user == nil {
		return r
	}
	subject, ok := m.subjectFor(r, user)
	if !ok {
		return r
	}
	return r.WithContext(auth.WithSubject(r.Context(), subject))
}

// unauthenticated handles an unauthenticated user.
//
// It writes the refusal itself rather than raising it: 401 with a message for a
// client that wants JSON, and a guest redirect for a browser.
//
// A request that wants a page and has nowhere to be sent -- nothing called
// [Authenticate.RedirectUsing] -- is answered 401 as well. The alternative is a
// Location header that is empty, which is a browser that stays where it is with
// no explanation.
func (m *Authenticate) unauthenticated(w http.ResponseWriter, r *http.Request) {
	to := m.RedirectTo(r)
	if to == "" || expectsJSON(r) {
		exception.WriteProblem(w, r, http.StatusUnauthorized, "Unauthenticated.")
		return
	}
	m.redirect(w, r, to)
}

// RedirectTo is the path the user should be redirected to when they are not
// authenticated.
//
// It is set with [Authenticate.RedirectUsing], and exported because a project
// that composes its own answer wants to ask what this one would have said.
//
// The empty string means nowhere is configured.
func (m *Authenticate) RedirectTo(r *http.Request) string {
	if m.redirectToCallback == nil {
		return ""
	}
	return m.redirectToCallback(r)
}

// RedirectUsing specifies the callback that should be used to generate the
// redirect path.
//
// It returns a copy of the middleware, for the reason [Authenticate.Using]
// gives.
func (m *Authenticate) RedirectUsing(redirectToCallback func(r *http.Request) string) *Authenticate {
	copied := *m
	copied.redirectToCallback = redirectToCallback
	return &copied
}
