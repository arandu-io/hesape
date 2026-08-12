package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// Gate is the part of Illuminate\Contracts\Auth\Access\Gate that [Authorize]
// uses.
//
// It is declared here, minimally, rather than imported from hesape/auth/access:
// the middleware needs one question answered, and an interface is what lets this
// package compile and be tested without the concrete gate -- and what stops the
// route layer depending on which gate an application wired.
//
// The subject is a parameter because there is no container to fetch the current
// user out of. It comes off the context, where [Authenticate] put it, so the
// middleware cannot invent one.
type Gate interface {
	// Authorize is Gate::authorize. It returns the auth.Grant that proves a
	// policy ran, and an error when the answer is no.
	Authorize(ctx context.Context, subject auth.Subject, ability string, args ...any) (auth.Grant, error)
}

// Authorize is Illuminate\Auth\Middleware\Authorize.
//
// It runs a policy before the handler and refuses the request when the answer
// is no. What it deliberately does NOT do is hand the resulting auth.Grant to
// the handler: a Grant is minted for one action on one resource, and a handler
// that received one from the routing layer would be a handler authorizing
// itself. The handler calls auth.Authorize for the work it actually does; this
// middleware is the early refusal, so a request that was never going to be
// allowed does not reach the code that loads the row.
type Authorize struct {
	// gate is the gate instance.
	gate Gate

	// ability and models are the parameters PHP reads off the route string.
	ability string
	models  []string
}

// NewAuthorize returns the middleware over a gate.
func NewAuthorize(g Gate) *Authorize { return &Authorize{gate: g} }

// Using specifies the ability and models for the middleware.
//
// It is Authorize::using. PHP takes a UnitEnum or a string and calls
// enum_value() on it; Go has no enum type to unwrap, so the ability is the
// string, which is what enum_value would have produced.
//
// Each model is the name of a route parameter, a quoted literal, or a class
// name -- see [Authorize.getModel].
func (m *Authorize) Using(ability string, models ...string) *Authorize {
	copied := *m
	copied.ability = ability
	copied.models = append([]string(nil), models...)
	return &copied
}

// Handle handles an incoming request.
//
// It is Authorize::handle. PHP throws AuthenticationException when nobody is
// signed in and AuthorizationException when the policy says no; here the first
// is 401 and the second 403, which is what the Illuminate handler renders for
// them.
func (m *Authorize) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, ok := auth.SubjectFrom(r.Context())
		if !ok {
			// No subject at all is not a refusal, it is a request that never
			// loaded a session -- which is the mistake auth.SubjectFrom's second
			// result exists to tell apart from a declared guest.
			writeJSON(w, http.StatusUnauthorized, "Unauthenticated.")
			return
		}

		if _, err := m.gate.Authorize(r.Context(), subject, m.ability, m.getGateArguments(r, m.models)...); err != nil {
			// The sentence the policy wrote is deliberately not sent: it says
			// why, and why is a description of data the refused caller was not
			// allowed to see. It belongs in the log, which is where the error
			// returned by the gate goes.
			writeJSON(w, http.StatusForbidden, "This action is unauthorized.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getGateArguments gets the arguments parameter for the gate.
//
// It is Authorize::getGateArguments, which is protected. PHP maps a model
// instance straight through and resolves anything else through getModel; there
// are no model instances on a route string here, so every entry goes through
// [Authorize.getModel].
func (m *Authorize) getGateArguments(r *http.Request, models []string) []any {
	if len(models) == 0 {
		return nil
	}
	args := make([]any, 0, len(models))
	for _, model := range models {
		args = append(args, m.getModel(r, model))
	}
	return args
}

// getModel gets the model to authorize.
//
// It is Authorize::getModel, which is protected. Three shapes, in PHP's order:
//
//   - a class name passes through, trimmed. It is how "can:create,App\Post" says
//     "this ability is about the type, not about a row".
//   - a route parameter is resolved by name. PHP asks the matched Route; here it
//     is http.Request.PathValue, which is the same lookup in net/http's router.
//   - a quoted literal yields what is inside the quotes, which is how a constant
//     is passed on a route string.
//
// An unmatched name yields nil, exactly as PHP's `?? null` does: the gate is
// asked about nothing, and a policy that was expecting a row refuses.
func (m *Authorize) getModel(r *http.Request, model string) any {
	model = strings.TrimSpace(model)
	if m.isClassName(model) {
		return model
	}
	if value := r.PathValue(model); value != "" {
		return value
	}
	if len(model) >= 2 {
		if (model[0] == '\'' && model[len(model)-1] == '\'') ||
			(model[0] == '"' && model[len(model)-1] == '"') {
			return model[1 : len(model)-1]
		}
	}
	return nil
}

// isClassName checks if the given string looks like a fully-qualified class
// name.
//
// It is Authorize::isClassName, which is protected. PHP looks for a backslash,
// the separator in its namespaces. Go's is the slash in an import path, and a
// type is named package.Type -- so both are accepted, and a route parameter
// carrying either was never a legal identifier.
func (m *Authorize) isClassName(value string) bool {
	return strings.ContainsAny(value, `\/.`)
}
