package access

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/access/events"
	"github.com/arandu-io/hesape/str"
)

// Ability is the callback Define registers, and the shape of a policy method.
//
// It answers with one type, and the four values it may hold are
//
//	nil            no opinion, carry on
//	true / false   allowed, or not
//	*Response      an answer with a sentence, a code and maybe a status
//	error          a refusal; an *AuthorizationError keeps its status and code
//
// The user is a value and never nil, so an ability that has to tell a visitor
// from an account asks user.IsGuest -- which is the framework's declared
// anonymous reader, not an absent one.
type Ability func(ctx context.Context, user auth.Subject, arguments ...any) any

// BeforeCallback is what Before registers: a callback that runs ahead of every
// check.
//
// A non-nil answer short-circuits the whole check, which is how an
// administrator override is written. A nil answer lets the check carry on.
type BeforeCallback func(ctx context.Context, user auth.Subject, ability string, arguments []any) any

// AfterCallback is what After registers: a callback that runs once the check has
// an answer.
//
// It receives the result the check produced and can only replace it when it was
// nil. An after callback cannot overturn a decision, which is the difference
// between a hook for logging and a second authorization path.
type AfterCallback func(ctx context.Context, user auth.Subject, ability string, result any, arguments []any) any

// Gate is the ability-name call site -- gate.Allows(ctx, subject, "update",
// post) -- over the one authorization path this framework has.
//
// Gate.Authorize does not decide anything itself: it hands the ability to
// auth.Authorize as a policy, so the Grant that comes back is the same Grant a
// hand-written auth.Policy produces, and no repository is reachable without one.
// See the package documentation for the adapter that does it.
//
// A Gate is built at boot and read afterwards. Define, Policy, Before, After and
// DefaultDenialResponse write to it and are not safe to call while another
// goroutine is checking: it is configuration, not request state.
type Gate struct {
	HandlesAuthorization

	// user is the subject ForUser fixed, and nil on a Gate nobody called ForUser
	// on.
	user *auth.Subject

	abilities             map[string]Ability
	policies              map[reflect.Type]any
	beforeCallbacks       []BeforeCallback
	afterCallbacks        []AfterCallback
	defaultDenialResponse *Response

	// observer receives a events.GateEvaluated after every decision, and is nil on a
	// Gate nobody called Observe on.
	//
	// It is a field and not a package-level dispatcher because a package-level
	// one is reachable from anywhere and assignable at init by anything in the
	// build -- which for an audit trail means the thing being audited can turn
	// it off from a file nobody reads.
	observer func(events.GateEvaluated)
}

// NewGate returns an empty Gate: no abilities, no policies, no callbacks.
//
// It takes nothing, because the subject is an argument to every check rather
// than something the Gate resolves for itself.
func NewGate() *Gate {
	return &Gate{
		abilities: map[string]Ability{},
		policies:  map[reflect.Type]any{},
	}
}

// Has reports that every named ability has been defined.
func (g *Gate) Has(abilities ...string) bool {
	for _, ability := range abilities {
		if _, ok := g.abilities[ability]; !ok {
			return false
		}
	}

	return true
}

// Condition is the callback form of what AllowIf and DenyIf take.
//
// Their condition parameter is any, so this is the shape a callback must have
// to be called rather than read as a value.
type Condition func(ctx context.Context, user auth.Subject) any

// AllowIf is an on-demand check that fails when the condition is false.
//
// The condition is a bool, a *Response or a [Condition]. A failure is returned
// as an error.
func (g *Gate) AllowIf(ctx context.Context, s auth.Subject, condition any, message string, code any) (*Response, error) {
	return g.authorizeOnDemand(ctx, s, condition, message, code, true)
}

// DenyIf is an on-demand check that fails when the condition is true.
func (g *Gate) DenyIf(ctx context.Context, s auth.Subject, condition any, message string, code any) (*Response, error) {
	return g.authorizeOnDemand(ctx, s, condition, message, code, false)
}

// authorizeOnDemand runs the condition and turns its answer into a Response,
// allowing when it matches allowWhenResponseIs.
func (g *Gate) authorizeOnDemand(ctx context.Context, s auth.Subject, condition any, message string, code any, allowWhenResponseIs bool) (*Response, error) {
	user := g.resolveUser(s)

	var response any
	switch c := condition.(type) {
	case Condition:
		response = c(ctx, user)
	case func(context.Context, auth.Subject) any:
		response = c(ctx, user)
	default:
		response = condition
	}

	if r, ok := response.(*Response); ok && r != nil {
		return r.Authorize()
	}

	return NewResponse(truthy(response) == allowWhenResponseIs, message, code).Authorize()
}

// Define registers the callback that decides an ability by name.
//
// The callback is an [Ability] and nothing else, so a callback of the wrong
// shape does not compile and there is nothing left to report at run time. To
// define a whole group of abilities from one policy, use [Gate.Resource].
func (g *Gate) Define(ability string, callback Ability) *Gate {
	g.abilities[ability] = callback

	return g
}

// Resource defines "<name>.<ability>" for each ability of a policy at once.
//
// It takes the policy value itself, and the policy's own before method still
// runs first on every ability it registers.
//
// A nil abilities map means viewAny, view, create, update and delete, each
// mapping to the method of the same name. Otherwise the keys are abilities and
// the values are the methods they call.
func (g *Gate) Resource(name string, policy any, abilities map[string]string) *Gate {
	if abilities == nil {
		abilities = map[string]string{
			"viewAny": "viewAny",
			"view":    "view",
			"create":  "create",
			"update":  "update",
			"delete":  "delete",
		}
	}

	for ability, method := range abilities {
		ability = name + "." + ability

		g.Define(ability, g.buildAbilityCallback(policy, ability, method))
	}

	return g
}

// buildAbilityCallback is the ability a [Gate.Resource] entry stands for, which
// calls the policy's before method and then the method the entry named.
func (g *Gate) buildAbilityCallback(policy any, ability, method string) Ability {
	name := str.Ucfirst(method)

	return func(ctx context.Context, user auth.Subject, arguments ...any) any {
		if result := callPolicyBefore(ctx, policy, user, ability, arguments); !isNil(result) {
			return result
		}

		call := policyMethod(policy, name)
		if call == nil {
			return nil
		}

		return call(ctx, user, arguments...)
	}
}

// Policy says which policy decides for a given model.
//
// The match is on the argument's reflect.Type, never on a name in a string.
// Register the model by value or by pointer; GetPolicyFor finds either from the
// other.
//
// A policy is any value with a method named after the ability, in the shape of
// an Ability:
//
//	func (PostPolicy) Update(ctx context.Context, user auth.Subject, arguments ...any) any
//
// It may also have a before method, which runs first and short-circuits the
// check when it answers anything but nil:
//
//	func (PostPolicy) Before(ctx context.Context, user auth.Subject, ability string, arguments ...any) any
//
// An interface type may be registered too, and any model implementing it is
// then covered by that policy.
func (g *Gate) Policy(model any, policy any) *Gate {
	if t := typeOf(model); t != nil {
		g.policies[t] = policy
	}

	return g
}

// Before registers a callback that runs before every check.
func (g *Gate) Before(callback BeforeCallback) *Gate {
	g.beforeCallbacks = append(g.beforeCallbacks, callback)

	return g
}

// After registers a callback that runs after every check.
func (g *Gate) After(callback AfterCallback) *Gate {
	g.afterCallbacks = append(g.afterCallbacks, callback)

	return g
}

// Allows reports whether the ability is granted.
//
// It answers yes or no and issues nothing. A handler that acts on the answer
// still has to call Authorize to reach a repository, because that is the call
// that produces the Grant -- Allows is for the view, which needs to know whether
// to draw the button.
func (g *Gate) Allows(ctx context.Context, s auth.Subject, ability string, arguments ...any) bool {
	return g.Check(ctx, s, []string{ability}, arguments...)
}

// Denies is the negation of [Gate.Allows].
func (g *Gate) Denies(ctx context.Context, s auth.Subject, ability string, arguments ...any) bool {
	return !g.Allows(ctx, s, ability, arguments...)
}

// Check reports that every one of the abilities is granted.
//
// An empty list is granted.
func (g *Gate) Check(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	for _, ability := range abilities {
		if !g.Inspect(ctx, s, ability, arguments...).Allowed() {
			return false
		}
	}

	return true
}

// Any reports that at least one of the abilities is granted.
//
// An empty list is not.
func (g *Gate) Any(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	for _, ability := range abilities {
		if g.Check(ctx, s, []string{ability}, arguments...) {
			return true
		}
	}

	return false
}

// None reports that not one of the abilities is granted.
func (g *Gate) None(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	return !g.Any(ctx, s, abilities, arguments...)
}

// Authorize runs the check and issues the Grant, and it is the reason this
// package exists.
//
// It answers with an auth.Grant rather than a Response, because a Response
// proves nothing: every repository in the framework demands a Grant, and a Gate
// that answered with anything else would be a second way to authorize -- one
// whose answer no repository can be reached with.
//
// The Grant is not built here. The ability is wrapped as an auth.Policy and
// handed to auth.Authorize, which refuses an anonymous subject before the
// ability is ever consulted and issues the Grant when it is allowed. The
// decision goes through the same door a hand-written policy goes through.
//
// The error is the AuthorizationError the denial produced -- it carries the
// Response, the code and the status the ability asked for, which the sentence
// auth.Authorize builds does not -- with that sentence kept as its cause. Either
// way errors.Is(err, auth.ErrForbidden) holds, so the exception handler answers
// 403 without knowing this package exists.
func (g *Gate) Authorize(ctx context.Context, s auth.Subject, ability string, arguments ...any) (auth.Grant, error) {
	user := g.resolveUser(s)

	var denial *AuthorizationError

	policy := abilityPolicy{fn: func(ctx context.Context, s auth.Subject, args []any) error {
		_, err := g.Inspect(ctx, s, ability, args...).Authorize()
		if err != nil {
			denial, _ = err.(*AuthorizationError)

			return err
		}

		return nil
	}}

	grant, err := auth.Authorize[[]any](ctx, policy, user, auth.Action(ability), arguments)
	if err != nil {
		if denial != nil {
			return auth.Grant{}, denial.withPrevious(err)
		}

		// The ability was never called: auth.Authorize refuses a subject with no
		// id before it consults a policy, because that is a session nobody
		// loaded rather than a decision anybody made.
		return auth.Grant{}, err
	}

	return grant, nil
}

// abilityPolicy adapts a registered ability to the Policy contract, so that a
// Gate decision goes through auth.Authorize like any other and comes back with
// a real Grant. The Gate is sugar over the one authorization path, not a
// second one.
type abilityPolicy struct {
	fn func(ctx context.Context, s auth.Subject, args []any) error
}

func (p abilityPolicy) Can(ctx context.Context, s auth.Subject, a auth.Action, args []any) error {
	return p.fn(ctx, s, args)
}

var _ auth.Policy[[]any] = abilityPolicy{}

// Inspect is the check, as a Response.
//
// An ability that answers with an error is a denial, and an *AuthorizationError
// keeps the status and the code it carried.
func (g *Gate) Inspect(ctx context.Context, s auth.Subject, ability string, arguments ...any) *Response {
	result := g.Raw(ctx, s, ability, arguments...)

	if !isNil(result) {
		switch v := result.(type) {
		case *Response:
			return v
		case *AuthorizationError:
			return v.ToResponse()
		case error:
			return Deny(v.Error(), nil)
		}
	}

	if truthy(result) {
		return Allow("", nil)
	}

	if g.defaultDenialResponse != nil {
		return g.defaultDenialResponse
	}

	return Deny("", nil)
}

// Raw is the untouched answer of the callback, before Inspect reads it as an
// allow or a denial: nil, a bool, a *Response or an error.
//
// It fires events.GateEvaluated when an observer was given, after the answer is
// settled. See [Gate.Observe].
func (g *Gate) Raw(ctx context.Context, s auth.Subject, ability string, arguments ...any) any {
	user := g.resolveUser(s)

	// First we call the "before" callbacks. Any of them answering with anything
	// but nil settles the question, which is what an override is for.
	result := g.callBeforeCallbacks(ctx, user, ability, arguments)

	if isNil(result) {
		result = g.callAuthCallback(ctx, user, ability, arguments)
	}

	// Then the "after" callbacks, which see the result and may fill it in when
	// nobody decided, but may not replace a decision.
	return g.callAfterCallbacks(ctx, user, ability, arguments, result)
}

// callAuthCallback runs whichever callback decides this ability.
func (g *Gate) callAuthCallback(ctx context.Context, user auth.Subject, ability string, arguments []any) any {
	return g.resolveAuthCallback(ability, arguments)(ctx, user, arguments...)
}

// callBeforeCallbacks runs the before callbacks and answers with the first
// non-nil result.
func (g *Gate) callBeforeCallbacks(ctx context.Context, user auth.Subject, ability string, arguments []any) any {
	for _, before := range g.beforeCallbacks {
		if result := before(ctx, user, ability, arguments); !isNil(result) {
			return result
		}
	}

	return nil
}

// callAfterCallbacks runs the after callbacks, lets them fill in a result
// nobody decided, and then notifies the observer.
func (g *Gate) callAfterCallbacks(ctx context.Context, user auth.Subject, ability string, arguments []any, result any) any {
	for _, after := range g.afterCallbacks {
		afterResult := after(ctx, user, ability, result, arguments)

		if isNil(result) {
			result = afterResult
		}
	}

	// After the answer, never before, and its return value is ignored.
	//
	// An observer that could change the result would be a second authorization
	// path, and there is only ever one. What it is for is the record: every
	// decision the application makes passes through here, and this is the only
	// place that sees all of them with the answer attached.
	if g.observer != nil {
		g.observer(events.GateEvaluated{
			Subject:   user,
			Ability:   ability,
			Result:    result,
			Arguments: arguments,
		})
	}
	return result
}

// resolveAuthCallback finds the callback that decides this ability: the policy
// method for the first argument if there is one, then the defined ability, then
// a callback that answers nothing.
func (g *Gate) resolveAuthCallback(ability string, arguments []any) Ability {
	if len(arguments) > 0 && !isNil(arguments[0]) {
		if policy := g.GetPolicyFor(arguments[0]); policy != nil {
			if callback := g.resolvePolicyCallback(ability, policy); callback != nil {
				return callback
			}
		}
	}

	if callback, ok := g.abilities[ability]; ok {
		return callback
	}

	return func(ctx context.Context, user auth.Subject, arguments ...any) any { return nil }
}

// GetPolicyFor is the policy registered for a model, or nil.
//
// It takes the model, a pointer to it, or its reflect.Type. It looks for the
// policy registered against that exact type, then against the type on the other
// side of a pointer, then against any interface the type implements. Nothing is
// resolved from a name in a string.
func (g *Gate) GetPolicyFor(class any) any {
	t := typeOf(class)
	if t == nil {
		return nil
	}

	if policy, ok := g.policies[t]; ok {
		return policy
	}

	if t.Kind() == reflect.Pointer {
		if policy, ok := g.policies[t.Elem()]; ok {
			return policy
		}
	} else if policy, ok := g.policies[reflect.PointerTo(t)]; ok {
		return policy
	}

	for expected, policy := range g.policies {
		if expected.Kind() == reflect.Interface && t.Implements(expected) {
			return policy
		}
	}

	return nil
}

// resolvePolicyCallback is the policy's method for this ability, with the
// policy's before method in front of it, or nil when the policy has no such
// method -- in which case the Gate falls through to the defined abilities.
func (g *Gate) resolvePolicyCallback(ability string, policy any) Ability {
	method := formatAbilityToMethod(ability)

	if policyMethod(policy, method) == nil {
		return nil
	}

	return func(ctx context.Context, user auth.Subject, arguments ...any) any {
		// The policy's before method runs first, and a non-nil answer from it is
		// the final one -- that is how a policy grants an administrator every
		// ability it defines without repeating the test in each method.
		if result := callPolicyBefore(ctx, policy, user, ability, arguments); !isNil(result) {
			return result
		}

		return callPolicyMethod(ctx, policy, method, user, arguments)
	}
}

// callPolicyBefore runs the policy's before method, when it has one.
func callPolicyBefore(ctx context.Context, policy any, user auth.Subject, ability string, arguments []any) any {
	before, ok := policy.(interface {
		Before(ctx context.Context, user auth.Subject, ability string, arguments ...any) any
	})
	if !ok {
		return nil
	}

	return before.Before(ctx, user, ability, arguments...)
}

// callPolicyMethod runs the named method on the policy.
func callPolicyMethod(ctx context.Context, policy any, method string, user auth.Subject, arguments []any) any {
	// A first argument that names the type rather than holding a value is the
	// collection case -- "may this subject create a Post at all" -- and the
	// policy already knows which type it decides for, so it is dropped.
	if len(arguments) > 0 {
		if _, ok := arguments[0].(reflect.Type); ok {
			arguments = arguments[1:]
		}
	}

	call := policyMethod(policy, method)
	if call == nil {
		return nil
	}

	return call(ctx, user, arguments...)
}

// policyMethod finds the method by name and checks it has the Ability shape.
// A missing method and a method with another signature are the same answer:
// nil.
func policyMethod(policy any, name string) Ability {
	if isNil(policy) {
		return nil
	}

	method := reflect.ValueOf(policy).MethodByName(name)
	if !method.IsValid() {
		return nil
	}

	call, ok := method.Interface().(func(context.Context, auth.Subject, ...any) any)
	if !ok {
		return nil
	}

	return call
}

// formatAbilityToMethod turns an ability name into the policy method that
// decides it. The method is exported, so that a policy in another package can
// declare it: "view-any" becomes ViewAny.
func formatAbilityToMethod(ability string) string {
	if strings.Contains(ability, "-") {
		ability = str.Camel(ability)
	}

	return str.Ucfirst(ability)
}

// ForUser returns a Gate that already knows whose abilities it is answering
// about.
//
// The subject is still an argument to every check, so the one given here is
// what a check falls back to when it is passed the zero Subject -- an argument
// naming somebody always wins. The copy is over a snapshot of the abilities,
// the policies and the callbacks.
//
// The default denial response is not carried over.
func (g *Gate) ForUser(s auth.Subject) *Gate {
	return &Gate{
		user:            &s,
		abilities:       maps.Clone(g.abilities),
		policies:        maps.Clone(g.policies),
		beforeCallbacks: slices.Clone(g.beforeCallbacks),
		afterCallbacks:  slices.Clone(g.afterCallbacks),
	}
}

// resolveUser picks the subject a check runs for. The subject the call names
// wins; the one ForUser fixed fills in for a subject nobody named.
//
// A subject with no id is the empty one auth.Authorize refuses, and a guest is
// not empty -- it was declared on purpose -- so neither is quietly replaced by
// somebody else.
func (g *Gate) resolveUser(s auth.Subject) auth.Subject {
	if g.user != nil && s.ID == "" && !s.IsGuest() {
		return *g.user
	}

	return s
}

// Abilities is every ability the Gate has been given, by name.
//
// It is a copy: reading what a Gate knows must not be a way to change it.
func (g *Gate) Abilities() map[string]Ability { return maps.Clone(g.abilities) }

// Policies is every policy the Gate has been given, keyed by the model type.
// It is a copy, for the reason given on Abilities.
func (g *Gate) Policies() map[reflect.Type]any { return maps.Clone(g.policies) }

// DefaultDenialResponse sets the denial [Gate.Inspect] answers with when a
// check simply failed and nobody said why.
func (g *Gate) DefaultDenialResponse(response *Response) *Gate {
	g.defaultDenialResponse = response

	return g
}

// typeOf is the type a policy is keyed by: a value stands for its own type, and
// a reflect.Type stands for itself.
func typeOf(v any) reflect.Type {
	if t, ok := v.(reflect.Type); ok {
		return t
	}

	return reflect.TypeOf(v)
}

// isNil reports that a value holds nothing, including an interface holding a
// typed nil -- which reads as non-nil to == and would otherwise make a callback
// that answered (*Response)(nil) short-circuit a check.
func isNil(v any) bool {
	if v == nil {
		return true
	}

	switch value := reflect.ValueOf(v); value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

// truthy reads what an Ability answered as an allow or a refusal: nil is false,
// a bool is itself, and any other value is something rather than nothing, which
// counts as true.
func truthy(v any) bool {
	if isNil(v) {
		return false
	}

	if b, ok := v.(bool); ok {
		return b
	}

	return true
}

// Observe hands the Gate somewhere to send events.GateEvaluated, and answers a copy.
//
// It answers a copy rather than mutating, so that a Gate handed to two places
// cannot have its audit trail redirected by one of them. It is the same shape
// as ForUser.
//
// The destination is an argument, and a Gate given none does no work and
// allocates nothing.
func (g *Gate) Observe(observer func(events.GateEvaluated)) *Gate {
	copied := *g
	copied.observer = observer
	return &copied
}
