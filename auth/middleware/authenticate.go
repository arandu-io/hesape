package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// Authenticate is Illuminate\Auth\Middleware\Authenticate.
//
// It refuses a request that nobody is signed in on, and -- this is the part
// Illuminate has no equivalent of -- puts the auth.Subject on the context, which
// is where every policy, every repository and hesape/http's Context.User read it
// back from. It is the ONLY thing in the framework that calls auth.WithSubject:
// a second caller would be a second answer to "who is acting", and one of them
// would be a subject somebody assembled from the request.
//
// It implements Illuminate\Contracts\Auth\Middleware\AuthenticatesRequests,
// which in PHP is one method, using(). There is no Go interface for it: the
// contract exists so a project can swap its own class in under the same route
// string, and there are no route strings here.
type Authenticate struct {
	guestRedirect

	// auth is the authentication factory instance.
	auth Factory

	// guards are the guards this copy was bound to by Using. Empty means the
	// default guard, which is PHP's [null].
	guards []string

	// subjectFor is how the resolved user becomes the Subject. See
	// [SubjectResolver].
	subjectFor SubjectResolver

	// redirectToCallback is the callback that should be used to generate the
	// authentication redirect path.
	//
	// PHP keeps it in a static property, so that setting it once in a service
	// provider changes every instance. It is a field here for the reason
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
// tenant would be a Grant that reads every customer (RULE 14).
func NewAuthenticate(a Factory, subjectFor SubjectResolver) *Authenticate {
	return &Authenticate{auth: a, subjectFor: subjectFor}
}

// Using specifies the guards for the middleware.
//
// It is Authenticate::using. PHP returns the string a route declares --
// "Authenticate:web,api" -- because middleware parameters travel as text
// through the router. Go composes values, so it returns a copy of the
// middleware bound to those guards, which is the same statement with the
// stringly-typed step removed.
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
// It is Authenticate::handle. PHP's third parameter is the guards; here they are
// on the value, put there by [Authenticate.Using].
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
// It is Authenticate::authenticate, which is protected. It returns the request
// carrying the subject rather than mutating one, because a context is only ever
// derived: net/http gives no way to put a value on the request everybody already
// holds.
func (m *Authenticate) authenticate(r *http.Request) (*http.Request, bool) {
	guards := m.guards
	if len(guards) == 0 {
		// PHP: `if (empty($guards)) { $guards = [null]; }`. The empty name is
		// this Factory's spelling of null: the default guard.
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
// It is Authenticate::unauthenticated, which throws AuthenticationException
// carrying the guards and, for a request that is not asking for JSON, the
// redirect path. Go has no exceptions, so the answer the Illuminate handler
// would have rendered is written here: 401 with a message for a client that
// wants JSON, and a guest redirect for a browser.
//
// A request that wants a page and has nowhere to be sent -- nothing called
// [Authenticate.RedirectUsing] -- is answered 401 as well. The alternative is a
// Location header that is empty, which is a browser that stays where it is with
// no explanation.
func (m *Authenticate) unauthenticated(w http.ResponseWriter, r *http.Request) {
	to := m.RedirectTo(r)
	if to == "" || expectsJSON(r) {
		writeJSON(w, http.StatusUnauthorized, "Unauthenticated.")
		return
	}
	m.redirect(w, r, to)
}

// RedirectTo is the path the user should be redirected to when they are not
// authenticated.
//
// It is Authenticate::redirectTo, which is protected in PHP so that a subclass
// can override it. Go has no subclassing and no protected; the override is
// [Authenticate.RedirectUsing], and the method is exported because a project
// that composes its own answer wants to ask what this one would have said.
//
// The empty string means "nowhere configured", which is PHP's null.
func (m *Authenticate) RedirectTo(r *http.Request) string {
	if m.redirectToCallback == nil {
		return ""
	}
	return m.redirectToCallback(r)
}

// RedirectUsing specifies the callback that should be used to generate the
// redirect path.
//
// It is Authenticate::redirectUsing, which is static. See
// [Authenticate.redirectToCallback] for why this one is not, and returns a copy
// for the reason [Authenticate.Using] does.
func (m *Authenticate) RedirectUsing(redirectToCallback func(r *http.Request) string) *Authenticate {
	copied := *m
	copied.redirectToCallback = redirectToCallback
	return &copied
}
