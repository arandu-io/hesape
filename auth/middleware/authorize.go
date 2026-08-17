package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
)

// Gate is the part of a gate that [Authorize] uses.
//
// It is declared here, minimally, rather than imported from hesape/auth/access:
// the middleware needs one question answered, and an interface is what lets this
// package compile and be tested without the concrete gate -- and what stops the
// route layer depending on which gate an application wired.
//
// The subject is a parameter, and it comes off the context where [Authenticate]
// put it, so the middleware cannot invent one.
type Gate interface {
	// Authorize returns the auth.Grant that proves a policy ran, and an error
	// when the answer is no.
	Authorize(ctx context.Context, subject auth.Subject, ability string, args ...any) (auth.Grant, error)
}

// Authorize runs a policy before the handler and refuses the request when the
// answer is no.
//
// What it deliberately does NOT do is hand the resulting auth.Grant to the
// handler: a Grant is minted for one action on one resource, and a handler that
// received one from the routing layer would be a handler authorizing itself.
// The handler calls auth.Authorize for the work it actually does; this
// middleware is the early refusal, so a request that was never going to be
// allowed does not reach the code that loads the row.
type Authorize struct {
	// gate is the gate instance.
	gate Gate

	// ability and models are what Using bound this copy to.
	ability string
	models  []string
}

// NewAuthorize returns the middleware over a gate.
func NewAuthorize(g Gate) *Authorize { return &Authorize{gate: g} }

// Using returns a copy of the middleware bound to that ability and those
// models.
//
// Each model is the name of a route parameter, a quoted literal, or a type
// name -- see [Authorize.getModel].
func (m *Authorize) Using(ability string, models ...string) *Authorize {
	copied := *m
	copied.ability = ability
	copied.models = append([]string(nil), models...)
	return &copied
}

// Handle handles an incoming request.
//
// Nobody signed in is 401; a policy that says no is 403.
func (m *Authorize) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, ok := auth.SubjectFrom(r.Context())
		if !ok {
			// No subject at all is not a refusal, it is a request that never
			// loaded a session -- which is the mistake auth.SubjectFrom's second
			// result exists to tell apart from a declared guest.
			exception.WriteProblem(w, r, http.StatusUnauthorized, "Unauthenticated.")
			return
		}

		if _, err := m.gate.Authorize(r.Context(), subject, m.ability, m.getGateArguments(r, m.models)...); err != nil {
			// The sentence the policy wrote is deliberately not sent: it says
			// why, and why is a description of data the refused caller was not
			// allowed to see. It belongs in the log, which is where the error
			// returned by the gate goes.
			exception.WriteProblem(w, r, http.StatusForbidden, "This action is unauthorized.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getGateArguments gets the arguments parameter for the gate. Every entry goes
// through [Authorize.getModel].
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

// getModel gets the model to authorize. Three shapes, in this order:
//
//   - a type name passes through, trimmed. It is how "models.Post" says "this
//     ability is about the type, not about a row".
//   - a route parameter is resolved by name, through http.Request.PathValue.
//   - a quoted literal yields what is inside the quotes, which is how a
//     constant is passed.
//
// An unmatched name yields nil: the gate is asked about nothing, and a policy
// that was expecting a row refuses.
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

// isClassName checks if the given string looks like a qualified type name.
//
// A Go type is named package.Type and lives behind a slash-separated import
// path, so a dot, a slash or a backslash all count -- and a route parameter
// carrying any of them was never a legal identifier.
func (m *Authorize) isClassName(value string) bool {
	return strings.ContainsAny(value, `\/.`)
}
