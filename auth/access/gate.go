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
// It answers what the PHP callback answers -- bool|Response|null -- as one type,
// which is the third mechanical change of ADR 0044: PHP's mixed is Go's any. The
// four values it may hold are
//
//	nil            the PHP null: no opinion, carry on
//	true / false   the PHP bool
//	*Response      an answer with a sentence, a code and maybe a status
//	error          a refusal; an *AuthorizationError keeps its status and code
//
// The user is a value and never nil, so an ability that has to tell a visitor
// from an account asks user.IsGuest -- which is the framework's declared
// anonymous reader, not an absent one.
type Ability func(ctx context.Context, user auth.Subject, arguments ...any) any

// BeforeCallback is what Before registers: $before($user, $ability, $arguments).
//
// A non-nil answer short-circuits the whole check, which is how an
// administrator override is written. A nil answer lets the check carry on.
type BeforeCallback func(ctx context.Context, user auth.Subject, ability string, arguments []any) any

// AfterCallback is what After registers: $after($user, $ability, $result, $arguments).
//
// It receives the result the check produced and can only replace it when it was
// nil. An after callback cannot overturn a decision -- that is the PHP's
// `$result ??= $afterResult`, and it is the difference between a hook for
// logging and a second authorization path.
type AfterCallback func(ctx context.Context, user auth.Subject, ability string, result any, arguments []any) any

// Gate is Illuminate\Auth\Access\Gate.
//
// It is the Laravel call site -- gate.Allows(ctx, subject, "update", post) --
// over the one authorization path this framework has. Gate.Authorize does not
// decide anything itself: it hands the ability to auth.Authorize as a policy, so
// the Grant that comes back is the same Grant a hand-written auth.Policy
// produces, and no repository is reachable without one. See the package
// documentation for the adapter that does it.
//
// A Gate is built at boot and read afterwards. Define, Policy, Before, After and
// DefaultDenialResponse write to it and are not safe to call while another
// goroutine is checking; the PHP has the same shape, for the same reason -- it
// is configuration, not request state.
type Gate struct {
	HandlesAuthorization

	// user is the subject ForUser fixed, and nil on a Gate nobody called ForUser
	// on. It stands in for the PHP's $userResolver.
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

// NewGate is the Gate constructor.
//
// The PHP takes the container, a user resolver and the seven pieces of state a
// forUser() clone has to carry. None of them survive here: there is no container
// (ADR 0001), and the subject is an argument to every check rather than
// something the Gate resolves.
func NewGate() *Gate {
	return &Gate{
		abilities: map[string]Ability{},
		policies:  map[reflect.Type]any{},
	}
}

// Has is Gate::has: every named ability has been defined.
//
// The PHP takes an array or a list of arguments; the variadic covers both.
func (g *Gate) Has(abilities ...string) bool {
	for _, ability := range abilities {
		if _, ok := g.abilities[ability]; !ok {
			return false
		}
	}

	return true
}

// Condition is the closure form of what AllowIf and DenyIf take.
//
// The PHP accepts Response|Closure|bool there, so the parameter is any and this
// is the shape a closure must have to be called rather than read as a value.
type Condition func(ctx context.Context, user auth.Subject) any

// AllowIf is Gate::allowIf: an on-demand check that fails when the condition is
// false.
//
// The condition is a bool, a *Response or a Condition. The PHP throws, so this
// returns (T, error).
func (g *Gate) AllowIf(ctx context.Context, s auth.Subject, condition any, message string, code any) (*Response, error) {
	return g.authorizeOnDemand(ctx, s, condition, message, code, true)
}

// DenyIf is Gate::denyIf: an on-demand check that fails when the condition is
// true.
func (g *Gate) DenyIf(ctx context.Context, s auth.Subject, condition any, message string, code any) (*Response, error) {
	return g.authorizeOnDemand(ctx, s, condition, message, code, false)
}

// authorizeOnDemand is Gate::authorizeOnDemand.
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

// Define is Gate::define.
//
// The PHP also accepts a [Class, method] array or a 'Class@method' string and
// throws InvalidArgumentException for anything else. Neither form exists here --
// both name a class to be resolved out of the container by string -- and the
// throw goes with them: a callback that is not an Ability does not compile, so
// there is nothing left to report at runtime. Resource is the callback array's
// replacement.
func (g *Gate) Define(ability string, callback Ability) *Gate {
	g.abilities[ability] = callback

	return g
}

// Resource is Gate::resource: it defines "<name>.<ability>" for each ability of
// a policy at once.
//
// The PHP takes the policy's class name and resolves it out of the container per
// check; this takes the policy itself, because there is no container to resolve
// it from (ADR 0001). Everything else is the same, including the policy's own
// before method running first.
//
// A nil abilities map means the PHP default, which is viewAny, view, create,
// update and delete, each mapping to the method of the same name. The keys are
// abilities and the values are the methods they call.
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

// buildAbilityCallback is Gate::buildAbilityCallback: the ability a Resource
// entry stands for, which calls the policy's before method and then the method
// the entry named.
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

// Policy is Gate::policy: it says which policy decides for a given model.
//
// The PHP matches on class name, because that is what a PHP value carries. Here
// the match is on the argument's reflect.Type -- the first porting reason, a
// language feature Go does not have. Register the model by value or by pointer;
// GetPolicyFor finds either from the other.
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
// covered. That is where the PHP walks is_subclass_of.
func (g *Gate) Policy(model any, policy any) *Gate {
	if t := typeOf(model); t != nil {
		g.policies[t] = policy
	}

	return g
}

// Before is Gate::before: a callback that runs before every check.
func (g *Gate) Before(callback BeforeCallback) *Gate {
	g.beforeCallbacks = append(g.beforeCallbacks, callback)

	return g
}

// After is Gate::after: a callback that runs after every check.
func (g *Gate) After(callback AfterCallback) *Gate {
	g.afterCallbacks = append(g.afterCallbacks, callback)

	return g
}

// Allows is Gate::allows.
//
// It answers yes or no and issues nothing. A handler that acts on the answer
// still has to call Authorize to reach a repository, because that is the call
// that produces the Grant -- Allows is for the view, which needs to know whether
// to draw the button.
func (g *Gate) Allows(ctx context.Context, s auth.Subject, ability string, arguments ...any) bool {
	return g.Check(ctx, s, []string{ability}, arguments...)
}

// Denies is Gate::denies.
func (g *Gate) Denies(ctx context.Context, s auth.Subject, ability string, arguments ...any) bool {
	return !g.Allows(ctx, s, ability, arguments...)
}

// Check is Gate::check: every one of the abilities is granted.
//
// An empty list is granted, which is what Collection::every answers for an empty
// collection.
func (g *Gate) Check(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	for _, ability := range abilities {
		if !g.Inspect(ctx, s, ability, arguments...).Allowed() {
			return false
		}
	}

	return true
}

// Any is Gate::any: at least one of the abilities is granted.
//
// An empty list is not, which is what Collection::contains answers for an empty
// collection.
func (g *Gate) Any(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	for _, ability := range abilities {
		if g.Check(ctx, s, []string{ability}, arguments...) {
			return true
		}
	}

	return false
}

// None is Gate::none: not one of the abilities is granted.
func (g *Gate) None(ctx context.Context, s auth.Subject, abilities []string, arguments ...any) bool {
	return !g.Any(ctx, s, abilities, arguments...)
}

// Authorize is Gate::authorize, and it is the reason this package exists.
//
// The PHP returns the Response and throws when the check fails. This returns the
// auth.Grant, because a Response proves nothing: every repository in the
// framework demands a Grant, and a Gate that answered with anything else would
// be a second way to authorize -- one whose answer no repository can be reached
// with (RULE 9).
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

// Inspect is Gate::inspect: the check, as a Response.
//
// The PHP catches AuthorizationException here and turns it into a denial;
// nothing is thrown in Go, so an ability that answers with an error is the same
// case, and an *AuthorizationError keeps the status and the code it carried.
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

// Raw is Gate::raw: the untouched answer of the callback, before Inspect reads
// it as an allow or a denial.
//
// The PHP returns mixed and so does this: nil, a bool, a *Response or an error.
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

// callAuthCallback is Gate::callAuthCallback.
func (g *Gate) callAuthCallback(ctx context.Context, user auth.Subject, ability string, arguments []any) any {
	return g.resolveAuthCallback(ability, arguments)(ctx, user, arguments...)
}

// callBeforeCallbacks is Gate::callBeforeCallbacks.
func (g *Gate) callBeforeCallbacks(ctx context.Context, user auth.Subject, ability string, arguments []any) any {
	for _, before := range g.beforeCallbacks {
		if result := before(ctx, user, ability, arguments); !isNil(result) {
			return result
		}
	}

	return nil
}

// callAfterCallbacks is Gate::callAfterCallbacks.
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
	// path, and RULE 17 allows one. What it is for is the record: every decision
	// the application makes passes through here, and this is the only place that
	// sees all of them with the answer attached.
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

// resolveAuthCallback is Gate::resolveAuthCallback: the policy method for the
// first argument if there is one, then the defined ability, then a callback that
// answers nothing.
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

// GetPolicyFor is Gate::getPolicyFor.
//
// It takes the model, a pointer to it, or its reflect.Type -- the last one being
// what the PHP means by passing a class name. It finds the policy registered for
// that exact type, then for the type on the other side of a pointer, then for
// any interface the type implements, which is where the PHP walks is_subclass_of.
//
// The two lookups between those in the PHP are gone: getPolicyFromAttribute
// reads a #[UsePolicy] attribute off the class and guessPolicyName looks for a
// class named after it. Go has neither attributes nor class lookup by string.
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

// resolvePolicyCallback is Gate::resolvePolicyCallback: the policy's method for
// this ability, with the policy's before method in front of it, or nil when the
// policy has no such method -- in which case the Gate falls through to the
// defined abilities, exactly as the PHP does.
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

// callPolicyBefore is Gate::callPolicyBefore.
func callPolicyBefore(ctx context.Context, policy any, user auth.Subject, ability string, arguments []any) any {
	before, ok := policy.(interface {
		Before(ctx context.Context, user auth.Subject, ability string, arguments ...any) any
	})
	if !ok {
		return nil
	}

	return before.Before(ctx, user, ability, arguments...)
}

// callPolicyMethod is Gate::callPolicyMethod.
func callPolicyMethod(ctx context.Context, policy any, method string, user auth.Subject, arguments []any) any {
	// A first argument that names the type rather than holding a value is the
	// collection case -- "may this subject create a Post at all" -- and the
	// policy already knows which type it decides for, so it is dropped. The PHP
	// tests for a string, which is what it passes a class name as.
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
// A missing method and a method with another signature are the same answer,
// which is what is_callable([$policy, $method]) reports in the PHP.
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

// formatAbilityToMethod is Gate::formatAbilityToMethod, plus the first
// mechanical change of ADR 0044: a Go method a policy in another package can
// declare is an exported one, so "view-any" becomes ViewAny rather than viewAny.
func formatAbilityToMethod(ability string) string {
	if strings.Contains(ability, "-") {
		ability = str.Camel(ability)
	}

	return str.Ucfirst(ability)
}

// ForUser is Gate::forUser: a Gate that already knows whose abilities it is
// answering about.
//
// The PHP swaps the user resolver, because there every check reads the subject
// off the Gate. Here the subject is an argument, so the one given to ForUser is
// what a check falls back to when it is passed the zero Subject -- an argument
// naming somebody always wins. It is the same clone the PHP makes, over a
// snapshot of the abilities, the policies and the callbacks.
//
// The default denial response is not carried over, which is the PHP's own
// behaviour: forUser() does not pass it to the new instance.
func (g *Gate) ForUser(s auth.Subject) *Gate {
	return &Gate{
		user:            &s,
		abilities:       maps.Clone(g.abilities),
		policies:        maps.Clone(g.policies),
		beforeCallbacks: slices.Clone(g.beforeCallbacks),
		afterCallbacks:  slices.Clone(g.afterCallbacks),
	}
}

// resolveUser is Gate::resolveUser. The subject the call names wins; the one
// ForUser fixed fills in for a subject nobody named.
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

// Abilities is Gate::abilities.
//
// The PHP hands back the array, and a PHP array is copied when it is handed
// back. This copies for the same reason: reading what a Gate knows must not be a
// way to change it.
func (g *Gate) Abilities() map[string]Ability { return maps.Clone(g.abilities) }

// Policies is Gate::policies, keyed by the model type rather than by class name.
// It is a copy, for the reason given on Abilities.
func (g *Gate) Policies() map[reflect.Type]any { return maps.Clone(g.policies) }

// DefaultDenialResponse is Gate::defaultDenialResponse: the denial Inspect
// answers with when a check simply failed and nobody said why.
func (g *Gate) DefaultDenialResponse(response *Response) *Gate {
	g.defaultDenialResponse = response

	return g
}

// typeOf is the Go stand-in for the PHP's `is_object($class) ? get_class($class) : $class`:
// a value stands for its type, and a reflect.Type stands for itself, which is
// how the class name is passed.
func typeOf(v any) reflect.Type {
	if t, ok := v.(reflect.Type); ok {
		return t
	}

	return reflect.TypeOf(v)
}

// isNil is the PHP's is_null, including the interface holding a typed nil --
// which reads as non-nil in Go and would otherwise make a callback that answered
// (*Response)(nil) short-circuit a check.
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

// truthy is the PHP's (bool) cast, narrowed to what an Ability may answer: nil
// is false, a bool is itself, and any other value is something rather than
// nothing, which PHP reads as true.
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
// cannot have its audit trail redirected by one of them. It is the same shape as
// ForUser, which the PHP has for the same reason.
//
// Illuminate resolves the dispatcher out of the container and fires
// unconditionally. There is no container (ADR 0001), so the destination is an
// argument, and a Gate given none does no work and allocates nothing.
func (g *Gate) Observe(observer func(events.GateEvaluated)) *Gate {
	copied := *g
	copied.observer = observer
	return &copied
}
