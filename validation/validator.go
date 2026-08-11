package validation

import (
	"context"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Validator answers to Illuminate\Validation\Validator: one submitted request,
// the rules written against it, and the answer.
//
// It is built by Make and it is not safe for concurrent use -- one belongs to
// one request, as the PHP's does. The compiled Set behind it is read-only and
// is shared by all of them.
//
// What is deliberately not here is Laravel's container: the rules that leave
// the process take what they need through an option instead of resolving it out
// of a service locator (ADR 0001 and 0002). That is `unique`, `exists`,
// `current_password` and `active_url`, and each of them fails closed when the
// thing it needs was not given.
type Validator struct {
	set  *Set
	data Data

	messages Errors
	failed   map[string][]string
	excluded []string

	stopOnFirstFailure bool

	// currentRule answers to Validator::$currentRule: the rule being run, which
	// is how ValidateRegex reads the pattern compiled at boot instead of
	// compiling it again on every request.
	currentRule *rule

	ctx       context.Context
	grant     auth.Grant
	presence  PresenceVerifier
	passwords CurrentPasswordChecker
	dns       Resolver
	now       func() time.Time

	// What follows is the state of Concerns\FormatsMessages: everything that
	// decides which sentence a failure carries, and how the field and the value
	// are spelled inside it. The PHP declares most of it public because a rule
	// object reaches for it; here the two a rule reads are CustomMessages and
	// CustomAttributes, and the rest is set through the With options.
	//
	// trans answers to $translator. A nil one is the whole difference between
	// this package's short sentences and Laravel's: see getMessage.
	trans                       Translator
	customMessages              map[string]any
	fallbackMessages            map[string]any
	customAttributes            map[string]string
	customValues                map[string]map[string]string
	replacers                   map[string]ReplacerFunc
	implicitAttributes          map[string][]string
	implicitAttributesFormatter func(string) string

	// after answers to $after: the callbacks Validator::after registers.
	after []func()

	// ensureExponentWithinAllowedRange answers to
	// $ensureExponentWithinAllowedRangeUsing.
	ensureExponentWithinAllowedRange func(scale int, attribute string, value any) bool

	// exception answers to $exception, which is a class name there and the
	// thing that builds one here.
	exception func(*Validator) error
}

var defaultResolver Resolver = net.DefaultResolver

// ValidatorOption is what a request hands a Validator that a boot-time rule set
// cannot hold: the context it runs under, the Grant that authorizes its
// queries, and the collaborators of the rules that leave the process.
type ValidatorOption func(*Validator)

// WithContext gives the Validator the request's context, so that a lookup or a
// count query carries the request's deadline. Without it the context is
// Background, and a rule that leaves the process has no deadline at all.
func WithContext(ctx context.Context) ValidatorOption {
	return func(v *Validator) { v.ctx = ctx }
}

// WithPresence gives `unique` and `exists` the Grant and the verifier they
// need.
//
// The Grant is not optional and there is no way to pass a verifier without one:
// RULE 17 says a read is authorized too, and "the validator only counts rows"
// is how a count of rows becomes a way to ask whether another tenant has a
// user with a given email.
func WithPresence(g auth.Grant, p PresenceVerifier) ValidatorOption {
	return func(v *Validator) { v.grant, v.presence = g, p }
}

// WithCurrentPassword gives `current_password` its checker.
func WithCurrentPassword(c CurrentPasswordChecker) ValidatorOption {
	return func(v *Validator) { v.passwords = c }
}

// WithResolver gives `active_url` and `email:dns` the resolver to ask. The
// default is net.DefaultResolver; a test gives its own and makes no network
// call at all.
func WithResolver(r Resolver) ValidatorOption {
	return func(v *Validator) { v.dns = r }
}

// WithClock sets the clock the relative date keywords read, so that a test of
// `after:today` does not have to be run before midnight.
func WithClock(now func() time.Time) ValidatorOption {
	return func(v *Validator) { v.now = now }
}

// Make answers to Illuminate\Validation\Factory::make: a Validator over the
// data and the rules.
//
// The rules are a compiled Set rather than an array of strings, because the
// strings are parsed and checked at boot -- see MustCompile. The data is
// copied, so that excluding a field does not reach back into the request's own
// map.
func Make(data Data, rules *Set, opts ...ValidatorOption) *Validator {
	v := &Validator{set: rules, data: data.Clone(), failed: map[string][]string{}}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// StopOnFirstFailure answers to stopOnFirstFailure: leave after the first field
// that fails, rather than reporting every field. It returns the Validator, as
// the PHP returns $this.
func (v *Validator) StopOnFirstFailure() *Validator {
	v.stopOnFirstFailure = true
	return v
}

// Passes answers to passes: run every rule and report whether nothing failed.
func (v *Validator) Passes() bool {
	v.messages = Errors{}
	v.failed = map[string][]string{}
	v.excluded = nil

	for _, f := range v.set.fields {
		if v.ShouldBeExcluded(f.name) {
			v.data.Forget(f.name)
			continue
		}
		if v.stopOnFirstFailure && v.messages.IsNotEmpty() {
			break
		}
		for _, r := range f.rules {
			v.validateAttribute(f, r)

			if v.ShouldBeExcluded(f.name) {
				break
			}
			if v.ShouldStopValidating(f.name) {
				break
			}
		}
	}
	for _, f := range v.set.fields {
		if v.ShouldBeExcluded(f.name) {
			v.data.Forget(f.name)
		}
	}

	// Here we will spin through all of the "after" hooks on this validator and
	// fire them off. This gives the callbacks a chance to perform all kinds of
	// other validation that needs to get wrapped up in this operation.
	v.runAfterCallbacks()

	return v.messages.IsEmpty()
}

// Fails answers to fails.
func (v *Validator) Fails() bool { return !v.Passes() }

// validateAttribute answers to validateAttribute.
func (v *Validator) validateAttribute(f *field, r *rule) {
	value := v.GetValue(f.name)
	if !v.isValidatable(f, r, value) {
		return
	}

	v.currentRule = r
	passed := r.spec.eval(v, f.name, value, r.args)
	v.currentRule = nil

	if !passed {
		v.AddFailure(f.name, r.name, r.args)
	}
}

// isValidatable answers to isValidatable.
func (v *Validator) isValidatable(f *field, r *rule, value any) bool {
	if slices.Contains(excludeRules, r.name) {
		return true
	}
	return v.presentOrRuleIsImplicit(r, f.name, value) &&
		v.passesOptionalCheck(f) &&
		v.isNotNullIfMarkedAsNullable(f, r) &&
		v.hasNotFailedPreviousRuleIfPresenceRule(r, f.name)
}

// presentOrRuleIsImplicit answers to presentOrRuleIsImplicit.
//
// This is what keeps `min:12` on an optional box nobody typed in from saying
// "must be at least 12 characters" about a box the person deliberately left
// alone. Dropping it is a real bug, not a simplification.
func (v *Validator) presentOrRuleIsImplicit(r *rule, attribute string, value any) bool {
	if s, ok := asString(value); ok && strings.TrimSpace(s) == "" {
		return r.spec.implicit
	}
	return v.ValidatePresent(attribute, value, nil) || r.spec.implicit
}

// passesOptionalCheck answers to passesOptionalCheck: `sometimes` skips the
// field entirely when its key was not sent at all.
//
// It is why the data can tell absent from present-and-empty, and that
// difference is what a PATCH is made of.
func (v *Validator) passesOptionalCheck(f *field) bool {
	if !f.sometimes {
		return true
	}
	return v.data.Has(f.name)
}

// isNotNullIfMarkedAsNullable answers to isNotNullIfMarkedAsNullable.
//
// `nullable` stops the chain when the value is NULL, not when it is empty:
// those are two different answers, and only one of them is "the client said
// there is nothing here".
func (v *Validator) isNotNullIfMarkedAsNullable(f *field, r *rule) bool {
	if r.spec.implicit || !f.nullable {
		return true
	}
	return v.GetValue(f.name) != nil
}

// hasNotFailedPreviousRuleIfPresenceRule answers to
// hasNotFailedPreviousRuleIfPresenceRule: a field that already failed does not
// go to the database to fail again.
func (v *Validator) hasNotFailedPreviousRuleIfPresenceRule(r *rule, attribute string) bool {
	if r.name == "unique" || r.name == "exists" {
		return !v.messages.Has(attribute)
	}
	return true
}

// ShouldStopValidating answers to shouldStopValidating.
//
// `bail` stops the field at its first failure. A failed implicit rule stops it
// too, without being asked: `required` failing must not also produce "must be
// at least 12 characters" about the same empty box.
func (v *Validator) ShouldStopValidating(attribute string) bool {
	f, declared := v.set.byName[attribute]
	if !declared {
		return false
	}
	if f.bail {
		return v.messages.Has(attribute)
	}
	if !f.hasImplicit {
		return false
	}
	for _, name := range v.failed[attribute] {
		if slices.Contains(implicitRules, name) {
			return true
		}
	}
	return false
}

// AddFailure answers to addFailure.
//
// An exclude rule never puts a message on the field: its failure removes the
// field from the validated data instead, which is the whole of what the five
// exclude rules do.
func (v *Validator) AddFailure(attribute, rule string, parameters []string) {
	if v.messages == nil {
		v.messages = Errors{}
	}
	if slices.Contains(excludeRules, rule) {
		v.ExcludeAttribute(attribute)
		return
	}
	v.messages.Add(attribute, v.MakeReplacements(
		v.getMessage(attribute, rule), attribute, rule, parameters,
	))
	v.failed[attribute] = append(v.failed[attribute], rule)
}

// ExcludeAttribute answers to excludeAttribute.
func (v *Validator) ExcludeAttribute(attribute string) {
	if !slices.Contains(v.excluded, attribute) {
		v.excluded = append(v.excluded, attribute)
	}
}

// ShouldBeExcluded answers to shouldBeExcluded.
func (v *Validator) ShouldBeExcluded(attribute string) bool {
	for _, excluded := range v.excluded {
		if attribute == excluded || strings.HasPrefix(attribute, excluded+".") {
			return true
		}
	}
	return false
}

// messageFor is the sentence one failure puts on the field, after any override
// the set carries.
func (v *Validator) messageFor(attribute, ruleName string) string {
	f, declared := v.set.byName[attribute]
	if !declared {
		return "is not valid"
	}
	r := f.rule(ruleName)
	if r == nil {
		return "is not valid"
	}
	return v.set.message(f, r)
}

// GetValue answers to getValue.
func (v *Validator) GetValue(attribute string) any { return v.data.Get(attribute) }

// SetValue answers to setValue.
func (v *Validator) SetValue(attribute string, value any) { v.data[attribute] = value }

// GetData answers to getData.
func (v *Validator) GetData() Data { return v.data }

// SetData answers to setData. The data is copied, for the reason Make copies
// it.
func (v *Validator) SetData(data Data) *Validator {
	v.data = data.Clone()
	return v
}

// Attributes answers to attributes: the data the rules run against.
func (v *Validator) Attributes() Data { return v.data }

// GetRules answers to getRules: the rule names written against each field.
func (v *Validator) GetRules() map[string][]string {
	out := make(map[string][]string, len(v.set.fields))
	for _, f := range v.set.fields {
		out[f.name] = v.set.RulesFor(f.name)
	}
	return out
}

// SetRules answers to setRules: run against another compiled set.
func (v *Validator) SetRules(rules *Set) *Validator {
	v.set = rules
	return v
}

// HasRule answers to hasRule: the field declares any of the named rules.
func (v *Validator) HasRule(attribute string, rules []string) bool {
	f, declared := v.set.byName[attribute]
	if !declared {
		return false
	}
	for _, name := range rules {
		if f.rule(name) != nil {
			return true
		}
	}
	return false
}

// Errors answers to errors: the message bag, run first if it has not been.
func (v *Validator) Errors() Errors {
	if v.messages == nil {
		v.Passes()
	}
	return v.messages
}

// Messages answers to messages, which the PHP defines as the same bag errors
// returns.
func (v *Validator) Messages() Errors { return v.Errors() }

// Failed answers to failed: the rules that failed, per attribute.
func (v *Validator) Failed() map[string][]string {
	if v.messages == nil {
		v.Passes()
	}
	return v.failed
}

// Validated answers to validated: the values that were declared, sent and not
// rejected. It is the only way to read a submitted value out of a validated
// request, so a field nobody wrote a rule for cannot reach a repository through
// here.
func (v *Validator) Validated() Input {
	if v.messages == nil {
		v.Passes()
	}
	out := make(Data, len(v.set.fields))
	for _, f := range v.set.fields {
		if v.messages.Has(f.name) || v.ShouldBeExcluded(f.name) {
			continue
		}
		if value, sent := lookup(v.data, f.name); sent {
			out[f.name] = value
		}
	}
	return Input{data: out}
}

// Valid answers to valid: the data of every attribute that has no message.
func (v *Validator) Valid() Data {
	if v.messages == nil {
		v.Passes()
	}
	out := make(Data, len(v.data))
	for name, value := range v.data {
		if !v.messages.Has(name) {
			out[name] = value
		}
	}
	return out
}

// Invalid answers to invalid: the data of every attribute that has one.
func (v *Validator) Invalid() Data {
	if v.messages == nil {
		v.Passes()
	}
	out := make(Data, len(v.messages))
	for name, value := range v.data {
		if v.messages.Has(name) {
			out[name] = value
		}
	}
	return out
}
