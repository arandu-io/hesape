package validation

import (
	"fmt"
	"strings"
	"sync"
)

// This file is how the rules of one field are written down, and how what was
// written becomes the rules a Set runs.
//
// A field's rules are written in one of two forms and they mean the same thing.
// The pipe string is the short one:
//
//	"email": "required|email|max:255"
//
// The list is the one a rule that is not text can stand in:
//
//	"cpf": []any{"required", br.CPF(), func(attribute string, value any, fail func(string)) {
//		if value == "00000000000" {
//			fail("The :attribute field is not a person.")
//		}
//	}}
//
// Each element of a list is one rule -- it is not split at a pipe, which is what
// lets a pattern hold one: []any{"regex:^(a|b)$"}. An element is one of:
//
//   - text, a rule name with its arguments: "max:255";
//   - a [Rule], a check written as a type;
//   - a [ValidationRule], a check that reports by calling fail;
//   - a function with the signature of [Closure];
//   - a builder -- [In], [NotIn], [Unique], [Exists], [Date], [Numeric], [FileRule],
//     [EmailRule], [Dimensions] and the others whose String method spells a rule
//     string -- which stands for the rules it spells;
//   - the [Enum] rule, which reads its cases off a Go type: EnumFrom;
//   - [When], [Unless] or [ForEach], rules chosen by looking at the request.
//
// Nothing else is a rule, and a list holding anything else does not compile.

// Closure is a rule written as a function: it is handed the attribute, its
// value and a fail function, and reports a problem by calling fail with the
// sentence to show. Each call adds one message; a function that never calls it
// passes. The sentence may hold :attribute and the other placeholders.
//
// It is the shape of the closure Illuminate takes as a rule.
type Closure = func(attribute string, value any, fail func(message string))

// FreshRule is implemented by a rule object that keeps state between asking
// whether a value passes and asking for the message, which is what [Rule] does.
//
// A compiled Set is shared by every request, so such an object would be written
// to by several at once. One that implements FreshRule is asked for an
// independent copy each time it runs. One that does not is run under a lock,
// which is correct and serializes the requests that reach it.
type FreshRule interface {
	// Fresh returns a copy of the rule that holds no state from any run.
	Fresh() Rule
}

// declaredRule is one element of a field's list after it was read: either a rule
// written as text, or an object.
type declaredRule struct {
	text   string
	object any
}

// declaredRules reads what was written against a field.
func declaredRules(decl any) ([]declaredRule, error) {
	switch d := decl.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(d) == "" {
			return nil, nil
		}
		segments := splitChain(d)
		out := make([]declaredRule, len(segments))
		for i, segment := range segments {
			out[i] = declaredRule{text: segment}
		}
		return out, nil
	case []string:
		out := make([]declaredRule, len(d))
		for i, rule := range d {
			out[i] = declaredRule{text: rule}
		}
		return out, nil
	case []any:
		var out []declaredRule
		for _, element := range d {
			items, err := declaredElement(element)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
		return out, nil
	}
	return declaredElement(decl)
}

// declaredElement reads one element of a list, or a single rule written where a
// list or a string was expected.
func declaredElement(element any) ([]declaredRule, error) {
	switch e := element.(type) {
	case string:
		return []declaredRule{{text: e}}, nil
	case *ConditionalRules, *NestedRules, Rule, ValidationRule, Closure,
		func(string, any, func(string), *Validator):
		return []declaredRule{{object: e}}, nil
	case fmt.Stringer:
		// A builder spells the rule string it stands for. It is spelled by this
		// package, so its pipes are separators.
		spelled := e.String()
		if strings.TrimSpace(spelled) == "" {
			return nil, nil
		}
		segments := splitChain(spelled)
		out := make([]declaredRule, len(segments))
		for i, segment := range segments {
			out[i] = declaredRule{text: segment}
		}
		return out, nil
	}
	return nil, fmt.Errorf("a rule of type %T is not one: write text, a Rule, a ValidationRule, a "+
		"func(attribute string, value any, fail func(message string)) or a builder", element)
}

// objectRule is a rule that is an object and not a name in the catalogue.
//
// It is built once, at compile, and run by every request that reaches the field.
type objectRule struct {
	name     string
	implicit bool

	// fresh makes the instance one run uses, when the rule can be copied. Without
	// it the one object is shared, and run holds mu around it.
	fresh func() Rule
	// shared is the one object, and mu serializes the runs that use it.
	shared Rule
	mu     sync.Mutex
}

// newObjectRule reads an object written in a field's list.
func newObjectRule(object any) (*objectRule, error) {
	switch o := object.(type) {
	case Closure:
		return &objectRule{
			name: "closure",
			fresh: func() Rule {
				return NewClosureValidationRule(func(a string, v any, fail func(string), _ *Validator) { o(a, v, fail) })
			},
		}, nil
	case func(string, any, func(string), *Validator):
		return &objectRule{
			name:  "closure",
			fresh: func() Rule { return NewClosureValidationRule(o) },
		}, nil
	case Rule:
		rule := &objectRule{name: ruleName(o)}
		if fresh, can := o.(FreshRule); can {
			rule.fresh = fresh.Fresh
		} else {
			rule.shared = o
		}
		if marker, marked := o.(ImplicitRule); marked {
			rule.implicit = marker.Implicit()
		}
		return rule, nil
	case ValidationRule:
		rule := &objectRule{name: ruleName(o)}
		_, wantsData := o.(DataAwareRule)
		_, wantsValidator := o.(ValidatorAwareRule)
		if wantsData || wantsValidator {
			rule.shared = NewInvokableValidationRule(o)
		} else {
			rule.fresh = func() Rule { return NewInvokableValidationRule(o) }
		}
		if marker, marked := o.(ImplicitRule); marked {
			rule.implicit = marker.Implicit()
		}
		return rule, nil
	}
	return nil, fmt.Errorf("a rule of type %T is not one", object)
}

// ruleName is the name a rule object answers to in a message override and in the
// list of what failed: its type, in the spelling a rule is written in.
func ruleName(object any) string {
	name := fmt.Sprintf("%T", object)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimLeft(name, "*")
	if name == "" {
		return "closure"
	}
	return snakeName(name)
}

// snakeName is CamelCase in snake_case, which is how a rule is spelled.
func snakeName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// prototype returns an instance of the rule that can be looked at but not run.
func (o *objectRule) prototype() Rule {
	if o.fresh != nil {
		return o.fresh()
	}
	return o.shared
}

// run asks the object whether the value passes, and returns the sentences it
// says when it does not. A nil result is a pass.
func (o *objectRule) run(v *Validator, attribute string, value any) []string {
	var rule Rule
	if o.fresh != nil {
		rule = o.fresh()
	} else {
		o.mu.Lock()
		defer o.mu.Unlock()
		rule = o.shared
	}

	if aware, ok := rule.(ValidatorAwareRule); ok {
		aware.SetValidator(v)
	}
	if aware, ok := rule.(DataAwareRule); ok {
		aware.SetData(v.data)
	}
	if rule.Passes(attribute, value) {
		return nil
	}
	messages := rule.Message()
	if len(messages) == 0 {
		return []string{translatedOr(v, "validation.invalid", "The :attribute field is invalid.")}
	}
	return append([]string(nil), messages...)
}
