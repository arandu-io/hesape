package validation

// This file answers to Illuminate\Contracts\Validation -- the interfaces a rule
// object implements -- and to the two wrappers that carry one:
// Illuminate\Validation\ClosureValidationRule and
// Illuminate\Validation\InvokableValidationRule.
//
// A rule object is not a name in the catalogue and cannot be written into a rule
// string, so it is run through After, which is the seam the PHP's
// validateUsingCustomRule is on the other side of:
//
//	v.After(func(v *validation.Validator) {
//		v.ValidateUsingCustomRule("password", v.GetValue("password"), rule)
//	})
//
// PHP's __invoke, jsonSerialize and the rest of its language interfaces are not
// here: Go has no spelling for them, and nothing else in the port does either.

// Rule answers to Illuminate\Contracts\Validation\Rule.
//
// Message returns a slice where the PHP returns string|array, because both
// wrappers below return several: one per call to $fail.
type Rule interface {
	// Passes answers to Rule::passes.
	Passes(attribute string, value any) bool
	// Message answers to Rule::message.
	Message() []string
}

// ValidationRule answers to Illuminate\Contracts\Validation\ValidationRule: the
// shape a rule written today has, where failing is calling $fail rather than
// returning false.
type ValidationRule interface {
	// Validate answers to ValidationRule::validate. fail is $fail.
	Validate(attribute string, value any, fail func(message string))
}

// ImplicitRule answers to Illuminate\Contracts\Validation\ImplicitRule: a rule
// that runs even when the value is blank.
//
// The PHP marks it by implementing an empty interface, which Go cannot do
// without a method, so the marker is the question itself.
type ImplicitRule interface {
	// Implicit reports whether this rule runs on a blank value.
	Implicit() bool
}

// DataAwareRule is implemented by a rule object that has to see the whole
// request and not only the one value it was written against -- a check that
// compares two fields, for instance. Before asking such a rule whether the value
// passes, the validator hands it every value it is validating. A rule that looks
// only at its own value does not implement it and is not given the data.
//
// Answers Illuminate\Contracts\Validation\DataAwareRule.
type DataAwareRule interface {
	// SetData answers to DataAwareRule::setData.
	SetData(data Data)
}

// ValidatorAwareRule is implemented by a rule object that needs the validator
// running it -- to phrase its message through the same translator, to read the
// custom messages and attribute names in force, or to reach a value it was not
// handed. The validator passes itself in before asking whether the value passes,
// and the rule keeps it for the length of that run.
//
// Answers Illuminate\Contracts\Validation\ValidatorAwareRule.
type ValidatorAwareRule interface {
	// SetValidator answers to ValidatorAwareRule::setValidator.
	SetValidator(validator *Validator)
}

// CompilableRules answers to
// Illuminate\Contracts\Validation\CompilableRules: rules decided by looking at
// the value, which is what NestedRules is.
type CompilableRules interface {
	// Compile answers to CompilableRules::compile.
	Compile(attribute string, value any, data Data) *ExplodedRules
}

// ValidateUsingCustomRule answers to Validator::validateUsingCustomRule: run one
// rule object and put whatever it says on the attribute.
//
// The PHP is protected because its only caller is validateAttribute, which
// reaches rule objects through the rules array. A compiled Set holds rule names
// and not objects, so the only caller here is an After hook, and the method is
// exported for it.
func (v *Validator) ValidateUsingCustomRule(attribute string, value any, rule Rule) {
	if aware, ok := rule.(ValidatorAwareRule); ok {
		aware.SetValidator(v)
	}

	if aware, ok := rule.(DataAwareRule); ok {
		aware.SetData(v.data)
	}

	if rule.Passes(attribute, value) {
		return
	}

	if v.messages == nil {
		v.messages = Errors{}
	}

	messages := rule.Message()
	if len(messages) == 0 {
		messages = []string{"is not valid"}
	}

	for _, message := range messages {
		v.messages.Add(attribute, v.MakeReplacements(message, attribute, "", nil))
	}
	v.failed[attribute] = append(v.failed[attribute], "custom")
}

// ---------------------------------------------------------------------------
// Illuminate\Validation\ClosureValidationRule.
// ---------------------------------------------------------------------------

// ClosureValidationRule is a rule written as a function rather than as a type,
// for a check that does not deserve a type of its own. The callback is handed
// the attribute, its value, a fail function and the validator, and reports a
// problem by calling fail with the sentence to show. Each call to fail adds one
// message, and a callback that never calls it passes.
//
// Answers Illuminate\Validation\ClosureValidationRule.
type ClosureValidationRule struct {
	// Callback answers to the public $callback. It is handed the same four
	// arguments the PHP hands its closure.
	Callback func(attribute string, value any, fail func(message string), validator *Validator)

	// Failed answers to the public $failed.
	Failed bool

	// messages answers to the public $messages: one per call to fail.
	messages []string

	validator *Validator
}

// NewClosureValidationRule answers to the ClosureValidationRule constructor.
func NewClosureValidationRule(callback func(attribute string, value any, fail func(message string), validator *Validator)) *ClosureValidationRule {
	return &ClosureValidationRule{Callback: callback}
}

// Passes answers to ClosureValidationRule::passes.
func (r *ClosureValidationRule) Passes(attribute string, value any) bool {
	r.Failed = false
	r.messages = nil

	if r.Callback == nil {
		return true
	}

	r.Callback(attribute, value, func(message string) {
		r.Failed = true
		r.messages = append(r.messages, message)
	}, r.validator)

	return !r.Failed
}

// Message answers to ClosureValidationRule::message.
func (r *ClosureValidationRule) Message() []string { return r.messages }

// SetValidator answers to ClosureValidationRule::setValidator.
func (r *ClosureValidationRule) SetValidator(validator *Validator) { r.validator = validator }

// ---------------------------------------------------------------------------
// Illuminate\Validation\InvokableValidationRule.
// ---------------------------------------------------------------------------

// InvokableValidationRule answers to
// Illuminate\Validation\InvokableValidationRule: a ValidationRule wrapped so
// that it reads as a Rule.
type InvokableValidationRule struct {
	invokable ValidationRule
	failed    bool
	messages  []string
	validator *Validator
	data      Data
	implicit  bool
}

// NewInvokableValidationRule answers to the static
// InvokableValidationRule::make.
//
// The PHP's make returns an anonymous subclass that implements ImplicitRule when
// the invokable declares $implicit; here the wrapper answers Implicit out of the
// same question, so there is no subclass to make. The name is New rather than
// Make because Make is already the Validator's constructor.
func NewInvokableValidationRule(invokable ValidationRule) *InvokableValidationRule {
	rule := &InvokableValidationRule{invokable: invokable}
	if marker, marked := invokable.(ImplicitRule); marked {
		rule.implicit = marker.Implicit()
	}
	return rule
}

// Passes answers to InvokableValidationRule::passes.
func (r *InvokableValidationRule) Passes(attribute string, value any) bool {
	r.failed = false
	r.messages = nil

	if r.invokable == nil {
		return true
	}

	if aware, ok := r.invokable.(DataAwareRule); ok && r.validator != nil {
		aware.SetData(r.validator.GetData())
	}

	if aware, ok := r.invokable.(ValidatorAwareRule); ok && r.validator != nil {
		aware.SetValidator(r.validator)
	}

	r.invokable.Validate(attribute, value, func(message string) {
		r.failed = true
		r.messages = append(r.messages, message)
	})

	return !r.failed
}

// Invokable answers to InvokableValidationRule::invokable.
func (r *InvokableValidationRule) Invokable() ValidationRule { return r.invokable }

// Message answers to InvokableValidationRule::message.
func (r *InvokableValidationRule) Message() []string { return r.messages }

// Implicit answers to the ImplicitRule marker the PHP's make decides by
// subclassing.
func (r *InvokableValidationRule) Implicit() bool { return r.implicit }

// SetData answers to InvokableValidationRule::setData.
func (r *InvokableValidationRule) SetData(data Data) { r.data = data }

// SetValidator answers to InvokableValidationRule::setValidator.
func (r *InvokableValidationRule) SetValidator(validator *Validator) { r.validator = validator }
