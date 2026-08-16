package middleware

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
)

// Factory is the part of a guard factory this package uses.
//
// It is declared here rather than imported so that a middleware depends on the
// question it asks -- "which guard, and is anybody signed in on it" -- and not
// on the manager an application happens to wire. Two methods are all a
// middleware needs; registering drivers is the manager's business, not one a
// middleware ever has.
type Factory interface {
	// Guard is the guard of that name. The empty name means the default one.
	Guard(name string) auth.Guard

	// ShouldUse records that the guard that answered is the one the rest of the
	// request means when it says "the user".
	ShouldUse(name string)
}

// SubjectResolver turns the user a guard resolved into the auth.Subject that
// travels on the context.
//
// It is not optional decoration: a Subject carries a tenant, roles and the
// verification stamp, and the seven methods of auth.Authenticatable carry none
// of them. Something has to make the mapping, and only the application knows
// how.
//
// It MUST read the tenant from the session or from the account row, never from
// the path, the body, the query string or a header. A resolver that takes a
// tenant off the request is a cross-tenant read with a policy in front of it,
// and the policy will allow it.
//
// Answering false means "this request carries no subject": the handler runs
// without one, and anything downstream that needs a decision refuses, because
// auth.Authorize refuses the zero Subject. Use auth.Guest for a reader who is
// deliberately anonymous.
type SubjectResolver func(r *http.Request, user auth.Authenticatable) (auth.Subject, bool)

// guestRedirect is a redirect that first records where the person was going.
//
// It is embedded by the three middlewares that turn somebody away, so that the
// remembering is written once. hesape/http.Intended does the remembering over a
// signed cookie, and it needs the application key -- so it is handed in rather
// than constructed here.
type guestRedirect struct {
	intended *hhttp.Intended
}

// Intended sets where-they-were-going to be remembered before the redirect, so
// that signing in finishes the journey instead of landing on the front page.
//
// Leaving it unset skips the remembering and changes nothing else.
func (g *guestRedirect) Intended(i *hhttp.Intended) { g.intended = i }

// redirect answers a turned-away request: remember the address, then send them.
func (g *guestRedirect) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if g.intended != nil {
		g.intended.Remember(w, r)
	}
	// hhttp.Redirect and not http.Redirect: an HTMX request is answered with
	// HX-Redirect and a plain one with 303, decided in one place for every
	// redirect the framework writes.
	hhttp.Redirect(w, r, to)
}

// expectsJSON reports that the client asked for JSON.
//
// It wraps rather than re-deciding, because content negotiation is one
// definition in hesape/http and a second one here would be the copy that is
// wrong about htmx.
func expectsJSON(r *http.Request) bool { return hhttp.NewRequest(r).ExpectsJSON() }

// writeJSON answers with a JSON object carrying one message, which is the body
// an unauthenticated or unauthorized request gets when it asked for JSON.
//
// The message is a constant at every call site, so it is written directly rather
// than encoded: there is nothing in it that needs escaping, and an encoder here
// would be an error return nobody can act on.
func writeJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// A refusal is one person's, and must never be held by a cache shared
	// between people.
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"message":"` + message + `"}`))
}
