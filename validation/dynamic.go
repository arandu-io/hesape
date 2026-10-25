package validation

import (
	"fmt"
	"slices"
	"strings"
)

// This file is the rules that are decided by looking at the request: the rules a
// field gets when a condition holds and the ones it gets when it does not, and the
// rules each member of a list gets from a function that is shown the member.
//
// They are not decided at boot, because the request is not there. Everything that
// can be checked at boot still is: both branches of a condition are compiled and
// checked when the set is, so a typo in the branch nobody has exercised is still a
// boot failure. What waits for the request is only which branch runs.

// ---------------------------------------------------------------------------
// Rules chosen by a condition.
// ---------------------------------------------------------------------------

// ConditionalRules is the rules an attribute gets when a condition holds, and
// the ones it gets when it does not. Build one with [When] or [Unless].
type ConditionalRules struct {
	// condition is asked of the whole request. A caller holding a bool wraps it
	// in a func that ignores its argument.
	condition func(Data) bool

	rules        []declaredRule
	defaultRules []declaredRule

	// err is what was wrong with the rules, reported when the set is compiled.
	err error
}

// NewConditionalRules returns rules chosen by the condition: rules when it
// holds, and defaultRules when it does not. Each is written in either form a
// field's rules may be.
func NewConditionalRules(condition func(Data) bool, rules any, defaultRules ...any) *ConditionalRules {
	c := &ConditionalRules{condition: condition}

	var err error
	c.rules, err = declaredRules(rules)
	if err != nil {
		c.err = err
	}
	if len(defaultRules) > 0 {
		c.defaultRules, err = declaredRules(defaultRules[0])
		if err != nil && c.err == nil {
			c.err = err
		}
	}
	return c
}

// Passes reports whether the condition holds for this request. A nil condition
// does not hold.
func (c *ConditionalRules) Passes(data Data) bool {
	if c.condition == nil {
		return false
	}
	return c.condition(data)
}

// Rules returns the rules written as text that are used when the condition holds.
func (c *ConditionalRules) Rules(data Data) []string { return texts(c.rules) }

// DefaultRules returns the rules written as text that are used when the condition
// does not hold.
func (c *ConditionalRules) DefaultRules(data Data) []string { return texts(c.defaultRules) }

func texts(items []declaredRule) []string {
	var out []string
	for _, item := range items {
		if item.object == nil {
			out = append(out, item.text)
		}
	}
	return out
}

// FilterConditionalRules replaces every ConditionalRules in the rules with the
// ones its condition chose for this request.
func FilterConditionalRules(rules Rules, data Data) Rules {
	out := Rules{}

	for attribute, written := range rules {
		conditional, isConditional := written.(*ConditionalRules)
		if !isConditional {
			out[attribute] = written
			continue
		}
		chosen := conditional.defaultRules
		if conditional.Passes(data) {
			chosen = conditional.rules
		}
		out[attribute] = writtenAs(chosen)
	}

	return out
}

// dynamicRule is a rule that is decided when the request arrives.
type dynamicRule struct {
	conditional *ConditionalRules
	then        []*rule
	otherwise   []*rule

	nested *NestedRules
}

// addConditional compiles both branches of a condition into the field.
func (c *compiler) addConditional(f *field, conditional *ConditionalRules) {
	if conditional.err != nil {
		c.fail(f, "", "%s", conditional.err)
		return
	}
	dynamic := &dynamicRule{conditional: conditional}
	dynamic.then = c.branch(f, conditional.rules)
	dynamic.otherwise = c.branch(f, conditional.defaultRules)

	f.rules = append(f.rules, &rule{name: "when", spec: objectSpec(false), dynamic: dynamic})
}

// branch compiles one side of a condition.
func (c *compiler) branch(f *field, items []declaredRule) []*rule {
	seen := map[string]bool{}
	var out []*rule
	for _, item := range items {
		if item.object != nil {
			built, err := newObjectRule(item.object)
			if err != nil {
				c.fail(f, "", "%s", err)
				continue
			}
			out = append(out, &rule{name: built.name, spec: objectSpec(built.implicit), object: built})
			continue
		}
		if r, ok := c.textRule(f, item.text, seen); ok {
			out = append(out, r)
		}
	}
	return out
}

// settle returns the set with every condition answered for this request. A set
// that holds none is returned as it is: it is shared by every request, and a set
// nobody has to rewrite must not be copied per request either.
func (s *Set) settle(data Data) *Set {
	if !s.hasDynamic() {
		return s
	}
	settled := &Set{
		byName:   make(map[string]*field, len(s.byName)),
		messages: s.messages,
		file:     s.file,
		line:     s.line,
		registry: s.registry,
		source:   s.source,
	}
	for _, f := range s.fields {
		member := f
		if slices.ContainsFunc(f.rules, func(r *rule) bool { return r.dynamic != nil && r.dynamic.conditional != nil }) {
			clone := *f
			clone.rules = nil
			for _, r := range f.rules {
				if r.dynamic == nil || r.dynamic.conditional == nil {
					clone.rules = append(clone.rules, r)
					continue
				}
				if r.dynamic.conditional.Passes(data) {
					clone.rules = append(clone.rules, r.dynamic.then...)
				} else {
					clone.rules = append(clone.rules, r.dynamic.otherwise...)
				}
			}
			computeFlags(&clone)
			member = &clone
		}
		settled.fields = append(settled.fields, member)
		settled.byName[member.name] = member
	}
	return settled
}

// hasDynamic reports whether any field of the set holds a rule decided by the
// request.
func (s *Set) hasDynamic() bool {
	for _, f := range s.fields {
		for _, r := range f.rules {
			if r.dynamic != nil {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rules decided per member of a list.
// ---------------------------------------------------------------------------

// NestedRules is the rules for one member of a list, decided by looking at that
// member. Build one with [ForEach].
type NestedRules struct {
	callback func(value any, attribute string, data Data) any
}

// NewNestedRules returns rules the callback decides, member by member.
//
// The callback is handed the member's value, the attribute that names it -- the
// concrete one, "items.0" -- and the whole request. It answers either the rules
// of the member itself, in either form a field's rules may be written, or [Rules]
// keyed by the names of fields inside the member, which are then taken relative
// to it.
func NewNestedRules(callback func(value any, attribute string, data Data) any) *NestedRules {
	return &NestedRules{callback: callback}
}

// Compile asks the callback what this member's rules are, and expands them. A
// nil callback compiles to nothing.
func (n *NestedRules) Compile(attribute string, value any, data Data) *ExplodedRules {
	if n.callback == nil {
		return &ExplodedRules{Rules: map[string][]string{}, ImplicitAttributes: map[string][]string{}}
	}
	return NewValidationRuleParser(data).Explode(n.relative(attribute, n.callback(value, attribute, data)))
}

// relative turns what the callback answered into rules keyed by the full name of
// the field they belong to.
func (n *NestedRules) relative(attribute string, answer any) Rules {
	if keyed, isKeyed := answer.(Rules); isKeyed {
		out := make(Rules, len(keyed))
		for key, written := range keyed {
			out[attribute+"."+key] = written
		}
		return out
	}
	return Rules{attribute: answer}
}

// addNested records that the field's members get their rules from a callback.
func (c *compiler) addNested(f *field, nested *NestedRules) {
	if nested.callback == nil {
		c.fail(f, "", "the function that decides the rules of each member is nil")
		return
	}
	f.rules = append(f.rules, &rule{name: "for_each", spec: objectSpec(false), dynamic: &dynamicRule{nested: nested}})
}

// settleNested resolves the rules each member of a list gets from its function.
// It runs after the wildcards were expanded, because the function is shown one
// member at a time.
//
// The rules a function answers are compiled here, when the request arrives, with
// the same compiler a set is compiled with: a name nobody recognises is a
// failure, and here it is a panic on the request that reached it. A function whose
// answer varies in kind is the reason this form exists; one whose answer is
// literal belongs in the set.
func (v *Validator) settleNested(set *Set) *Set {
	nested := false
	for _, f := range set.fields {
		for _, r := range f.rules {
			if r.dynamic != nil && r.dynamic.nested != nil {
				nested = true
			}
		}
	}
	if !nested {
		return set
	}

	out := &Set{
		byName:   make(map[string]*field, len(set.byName)),
		messages: set.messages,
		file:     set.file,
		line:     set.line,
		registry: set.registry,
		source:   set.source,
	}
	add := func(f *field) {
		if _, taken := out.byName[f.name]; taken {
			return
		}
		out.fields = append(out.fields, f)
		out.byName[f.name] = f
	}

	for _, f := range set.fields {
		var dynamic []*dynamicRule
		for _, r := range f.rules {
			if r.dynamic != nil && r.dynamic.nested != nil {
				dynamic = append(dynamic, r.dynamic)
			}
		}
		if len(dynamic) == 0 {
			add(f)
			continue
		}

		clone := *f
		clone.rules = nil
		for _, r := range f.rules {
			if r.dynamic == nil || r.dynamic.nested == nil {
				clone.rules = append(clone.rules, r)
			}
		}
		for _, d := range dynamic {
			answer := d.nested.callback(v.GetValue(f.name), f.name, v.data)
			for name, written := range d.nested.relative(f.name, answer) {
				compiled, err := compileField(set, name, written)
				if err != nil {
					panic(err)
				}
				if name == f.name {
					clone.rules = append(clone.rules, compiled.rules...)
					continue
				}
				add(compiled)
			}
		}
		computeFlags(&clone)
		add(&clone)
	}
	slices.SortStableFunc(out.fields, func(a, b *field) int { return strings.Compare(a.name, b.name) })
	return out
}

// compileField compiles the rules written against one field the way the set it
// belongs to was compiled, and reports what is wrong with them.
func compileField(set *Set, name string, written any) (*field, error) {
	c := &compiler{set: &Set{byName: map[string]*field{}}, file: set.file, line: set.line, registry: set.registry}
	f := c.parse(name, written)
	c.set.byName[name] = f
	for sibling, other := range set.byName {
		c.set.byName[sibling] = other
	}
	c.set.byName[name] = f
	computeFlags(f)
	c.checkField(f)
	if len(c.errs) > 0 {
		return nil, fmt.Errorf("validation: the rules a ForEach function answered for %q do not compile: %w", name, c.errs)
	}
	return f, nil
}

// writtenAs writes declared rules back: as a pipe string when every one is text,
// which is the form a rule set is usually written in, and as a list when one is
// an object.
func writtenAs(items []declaredRule) any {
	list := make([]any, 0, len(items))
	allText := true
	for _, item := range items {
		if item.object != nil {
			allText = false
			list = append(list, item.object)
			continue
		}
		list = append(list, item.text)
	}
	if !allText {
		return list
	}
	joined := make([]string, len(items))
	for i, item := range items {
		joined[i] = item.text
	}
	return strings.Join(joined, "|")
}
