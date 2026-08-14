package validation

import (
	"errors"
	"maps"
	"slices"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/str"
	"github.com/arandu-io/hesape/support"
)

// This file is the rest of Illuminate\Validation\Validator: the entry points a
// controller calls, the two ways a rule set grows after the validator was built,
// and the extension points Factory registers into.

// Validate answers to Validator::validate: the attributes that were validated,
// or the ValidationException the failures make.
//
// The PHP throws and this returns, which is the change Go forces. The error is
// always a *ValidationException, so errors.As reaches the bag, the status and
// the redirect.
func (v *Validator) Validate() (Input, error) {
	if v.Fails() {
		return Input{}, v.exceptionFor()
	}

	return v.Validated(), nil
}

// ValidateWithBag answers to Validator::validateWithBag: Validate with the
// failures named, so that two forms on one page do not draw each other's errors.
func (v *Validator) ValidateWithBag(errorBag string) (Input, error) {
	validated, err := v.Validate()
	if err != nil {
		var invalid *ValidationException
		if errors.As(err, &invalid) {
			invalid.ErrorBag(errorBag)
		}
		return Input{}, err
	}
	return validated, nil
}

// Safe answers to Validator::safe: the validated attributes in a container that
// reads them one at a time.
//
// The PHP returns a plain array when keys are given and a ValidatedInput when
// they are not; here it is a ValidatedInput either way, so a caller has one type
// to write against, and ValidatedInput.All answers with the map.
func (v *Validator) Safe(keys ...string) (*support.ValidatedInput, error) {
	if v.Fails() {
		return nil, v.exceptionFor()
	}

	input := support.NewValidatedInput(v.Validated().data)
	if len(keys) == 0 {
		return input, nil
	}
	return support.NewValidatedInput(input.All(keys...)), nil
}

// exceptionFor builds what a failure is turned into, which SetException
// replaces.
func (v *Validator) exceptionFor() error {
	if v.exception == nil {
		return NewValidationException(v)
	}
	return v.exception(v)
}

// GetException answers to Validator::getException: what a failure is turned
// into. It is a class name in the PHP and the thing that builds one here,
// because Go has no class to name.
func (v *Validator) GetException() func(*Validator) error {
	if v.exception == nil {
		return func(v *Validator) error { return NewValidationException(v) }
	}
	return v.exception
}

// SetException answers to Validator::setException.
//
// The PHP refuses a class that does not extend ValidationException with an
// InvalidArgumentException; the Go signature refuses it at compile time, and
// only a nil factory is left to reject.
func (v *Validator) SetException(exception func(*Validator) error) *Validator {
	if exception == nil {
		panic("validation: the exception factory must not be nil -- it is what a failure is turned into")
	}
	v.exception = exception

	return v
}

// After answers to Validator::after: a callback that runs once every rule has.
//
// It is where a check that needs the whole form goes, and it is the seam a rule
// object is run through: the callback holds the Validator, so it calls
// AddFailure with the attribute and rule name of its choosing.
func (v *Validator) After(callback func(*Validator)) *Validator {
	v.after = append(v.after, func() { callback(v) })

	return v
}

// runAfterCallbacks fires the After hooks. Passes calls it once every field has
// run, which is where the PHP fires them.
func (v *Validator) runAfterCallbacks() {
	for _, after := range v.after {
		after()
	}
}

// AddRules answers to Validator::addRules: another compiled set merged into this
// validator's, so that a rule decided at request time joins the ones decided at
// boot.
//
// The PHP takes an array of strings and parses it here; the strings are parsed
// and checked by MustCompile instead, so what is merged is a Set. A field both
// sets declare keeps the rules of both, in the order this one wrote them first.
func (v *Validator) AddRules(rules *Set) *Validator {
	if rules == nil {
		return v
	}
	v.explodeRules(mergeSets(v.initialRules, rules))

	return v
}

// Sometimes answers to Validator::sometimes: rules added to an attribute only
// when the callback says so.
//
// The callback is handed the data, as the PHP hands it a Fluent of it, and the
// value of the attribute being decided.
func (v *Validator) Sometimes(attributes []string, rules *Set, callback func(payload *support.Fluent, value any) bool) *Validator {
	if rules == nil {
		return v
	}

	payload := support.NewFluent(v.data)

	for _, attribute := range attributes {
		f, declared := rules.byName[attribute]
		if !declared {
			continue
		}
		if !callback(payload, v.GetValue(attribute)) {
			continue
		}
		v.explodeRules(mergeSets(v.initialRules, &Set{
			fields:   []*field{f},
			byName:   map[string]*field{attribute: f},
			messages: rules.messages,
		}))
	}

	return v
}

// mergeSets is the union of two compiled sets, with neither changed: a Validator
// that grows its rules must not reach back into the Set every other request
// shares.
func mergeSets(base, extra *Set) *Set {
	merged := &Set{
		byName:   make(map[string]*field, len(base.byName)+len(extra.byName)),
		messages: Messages{},
		file:     base.file,
		line:     base.line,
	}
	maps.Copy(merged.messages, base.messages)
	maps.Copy(merged.messages, extra.messages)

	for _, f := range base.fields {
		clone := *f
		clone.rules = slices.Clone(f.rules)
		merged.fields = append(merged.fields, &clone)
		merged.byName[clone.name] = &clone
	}

	for _, f := range extra.fields {
		existing, declared := merged.byName[f.name]
		if !declared {
			clone := *f
			clone.rules = slices.Clone(f.rules)
			merged.fields = append(merged.fields, &clone)
			merged.byName[clone.name] = &clone
			continue
		}
		for _, r := range f.rules {
			if existing.rule(r.name) == nil {
				existing.rules = append(existing.rules, r)
			}
		}
		existing.bail = existing.bail || f.bail
		existing.sometimes = existing.sometimes || f.sometimes
		existing.nullable = existing.nullable || f.nullable
		existing.numeric = existing.numeric || f.numeric
		existing.file = existing.file || f.file
		existing.hasImplicit = existing.hasImplicit || f.hasImplicit
		if existing.layout == "" {
			existing.layout = f.layout
		}
	}

	return merged
}

// GetRule answers to Validator::getRule: the first of the given rules written
// against the attribute, and the parameters it carries.
//
// The PHP returns [$rule, $parameters] or null; the third value is how Go spells
// the null.
func (v *Validator) GetRule(attribute string, rules []string) (string, []string, bool) {
	f, declared := v.set.byName[attribute]
	if !declared {
		return "", nil, false
	}
	for _, name := range rules {
		if r := f.rule(name); r != nil {
			return r.name, r.args, true
		}
	}
	return "", nil, false
}

// GetMessageBag answers to Validator::getMessageBag.
func (v *Validator) GetMessageBag() Errors { return v.Errors() }

// GetPresenceVerifier answers to Validator::getPresenceVerifier, which throws a
// RuntimeException when none was set.
func (v *Validator) GetPresenceVerifier() (PresenceVerifier, error) {
	if v.presence == nil {
		return nil, errors.New("validation: presence verifier has not been set")
	}
	return v.presence, nil
}

// SetPresenceVerifier answers to Validator::setPresenceVerifier.
//
// RULE 17 adds the Grant: a read is authorized too, and there is no way to give
// a verifier without one. WithPresence is the same pair as an option.
func (v *Validator) SetPresenceVerifier(g auth.Grant, presenceVerifier PresenceVerifier) {
	v.grant, v.presence = g, presenceVerifier
}

// ExtensionFunc answers to the Closure Factory::extend registers: a rule written
// by the application, called with the arguments every rule in the table is.
type ExtensionFunc func(v *Validator, attribute string, value any, parameters []string) bool

// AddExtension answers to Validator::addExtension.
//
// The PHP registers on the validator because it parses its rules per request;
// here a rule name has to exist before MustCompile reads it, so an extension
// joins the one catalogue every rule set is checked against. Registering the
// same name twice panics, for the reason two answers to one rule name always do.
//
// It is a package-level table rather than per-validator state, which is what
// Laravel's Factory::extend effectively is: registered once, in a provider,
// before any validator is made.
func (v *Validator) AddExtension(rule string, extension ExtensionFunc) {
	Extend(rule, extension, "")
}

// AddExtensions answers to Validator::addExtensions.
func (v *Validator) AddExtensions(extensions map[string]ExtensionFunc) {
	for rule, extension := range extensions {
		v.AddExtension(rule, extension)
	}
}

// AddImplicitExtension answers to Validator::addImplicitExtension: an extension
// that runs even when the value is blank.
func (v *Validator) AddImplicitExtension(rule string, extension ExtensionFunc) {
	ExtendImplicit(rule, extension, "")
}

// AddImplicitExtensions answers to Validator::addImplicitExtensions.
func (v *Validator) AddImplicitExtensions(extensions map[string]ExtensionFunc) {
	for rule, extension := range extensions {
		v.AddImplicitExtension(rule, extension)
	}
}

// AddDependentExtension answers to Validator::addDependentExtension: an
// extension whose first parameter names another field, which is then checked at
// boot like every other cross-field reference.
func (v *Validator) AddDependentExtension(rule string, extension ExtensionFunc) {
	ExtendDependent(rule, extension, "")
}

// AddDependentExtensions answers to Validator::addDependentExtensions.
func (v *Validator) AddDependentExtensions(extensions map[string]ExtensionFunc) {
	for rule, extension := range extensions {
		v.AddDependentExtension(rule, extension)
	}
}

// Extend answers to Factory::extend at the level a Go program registers one: a
// rule name, the check behind it, and the sentence it says.
//
// It is called from an init or from main, before any rule set is compiled. An
// empty message leaves the rule saying "is not valid", which is what the PHP's
// null message leaves it saying.
func Extend(rule string, extension ExtensionFunc, message string) {
	registerExtension(rule, extension, message, false, nil)
}

// ExtendImplicit answers to Factory::extendImplicit.
func ExtendImplicit(rule string, extension ExtensionFunc, message string) {
	registerExtension(rule, extension, message, true, nil)
}

// ExtendDependent answers to Factory::extendDependent.
func ExtendDependent(rule string, extension ExtensionFunc, message string) {
	registerExtension(rule, extension, message, false, []int{0})
}

func registerExtension(name string, extension ExtensionFunc, message string, implicit bool, refs []int) {
	name = str.Snake(name, "_")

	if extension == nil {
		panic("validation: rule " + name + " was registered with no check")
	}
	if _, taken := specs[name]; taken {
		panic("validation: rule " + name + " is already in the catalogue -- " +
			"two answers to one rule name is what this refuses to let boot")
	}
	if _, deliberate := refused[name]; deliberate {
		panic("validation: rule " + name + " is refused on purpose -- see refused.go")
	}

	if message == "" {
		message = "is not valid"
	}

	specs[name] = &spec{
		maxArgs:  -1,
		implicit: implicit,
		refs:     refs,
		eval:     evaluator(extension),
		message:  func(f *field, r *rule) string { return message },
	}

	if implicit {
		implicitRules = append(implicitRules, name)
	}
}

// ParseData answers to Validator::parseData.
//
// The PHP swaps the dots and asterisks in the KEYS for a random placeholder, so
// that an input genuinely named "a.b" is not read as the path a -> b, and swaps
// them back in validated(). Data answers the same question by construction:
// lookup tries the literal key before it walks the path, so a key with a dot in
// it is found as itself. What is left for this to do is copy, which is what
// keeps a Validator from writing through to the request's own map.
func (v *Validator) ParseData(data Data) Data { return data.Clone() }

// GetRulesWithoutPlaceholders answers to
// Validator::getRulesWithoutPlaceholders.
//
// It is the same map GetRules answers with, for the reason ParseData is a copy:
// there are no placeholders in a key here to take back out.
func (v *Validator) GetRulesWithoutPlaceholders() map[string][]string { return v.GetRules() }

// EnsureExponentWithinAllowedRangeUsing answers to
// Validator::ensureExponentWithinAllowedRangeUsing: the check `numeric` and
// `decimal` make before they read a number written in exponent notation, so that
// "1e100000" is refused rather than turned into a float nobody meant.
func (v *Validator) EnsureExponentWithinAllowedRangeUsing(callback func(scale int, attribute string, value any) bool) *Validator {
	v.ensureExponentWithinAllowedRange = callback

	return v
}

// EnsureExponentWithinAllowedRange asks that check.
//
// With nothing registered the answer is the PHP's own default -- an exponent
// between -1000 and 1000 -- and not "yes". This method answered true for every
// scale, so "1e100000" had the range check the doc comment above promises and
// the range was infinite: getSize never refused an exponent, which is half of
// what an audit found while proving that getSize compared in float64.
func (v *Validator) EnsureExponentWithinAllowedRange(scale int, attribute string, value any) bool {
	if v.ensureExponentWithinAllowedRange == nil {
		return scale <= 1000 && scale >= -1000
	}
	return v.ensureExponentWithinAllowedRange(scale, attribute, value)
}
