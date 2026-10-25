package validation

import (
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Set is a compiled, checked rule set.
//
// Build one in a package-level variable with MustCompile: the rules are then
// parsed once, at boot, and a set that boots is a set whose names are all real.
// A Set is read-only once compiled, so one is shared by every request; the
// per-request state lives in the Validator that Make builds over it.
type Set struct {
	fields   []*field // sorted by name, so failures come out in a stable order
	byName   map[string]*field
	messages Messages
	file     string
	line     int

	// registry is the registry the set was compiled against. A run reads the
	// replacers of the rules it declares from it.
	registry *Registry

	// source is the rules the set was compiled from, kept so that the set can be
	// compiled again against another registry.
	source Rules
	opts   []Option
}

// field is one input and the rules written against it, in the order written.
type field struct {
	name  string
	rules []*rule

	// primary is the wildcard name this field was expanded from --
	// "items.*.price" for "items.0.price" -- and empty for a field written
	// without one. It is what lets a message override still be keyed on the
	// name somebody wrote.
	primary string

	// bail stops the field at its first failure.
	bail bool
	// sometimes skips the field entirely when its key was not sent at all --
	// which is why the data can tell "absent" from "present and empty", and
	// that is the difference a PATCH is made of.
	sometimes bool
	// nullable stops the field when the value is NULL rather than when it is
	// empty.
	nullable bool
	// numeric is set by numeric, integer or decimal, and makes every size rule
	// on this field measure the value rather than the characters.
	numeric bool
	// file is set by file, image, mimes, mimetypes, extensions or dimensions,
	// and makes a size limit on this field mean KILOBYTES.
	file bool
	// hasImplicit records that the field declares at least one implicit rule,
	// which is what shouldStopValidating asks before it stops.
	hasImplicit bool
	// layout is the date_format layout, which the date comparisons read.
	layout string
}

// rule is one rule of one field, with whatever its boot check parsed for it.
type rule struct {
	name string
	args []string
	spec *spec

	// object is set for a rule that is not a name in the catalogue: a Rule, a
	// ValidationRule or a function. Its spec holds only what the compiler asks of
	// every rule.
	object *objectRule

	// dynamic is set for a rule decided by looking at the request.
	dynamic *dynamicRule

	// re is the compiled pattern of regex/not_regex, and nums the parsed
	// numeric arguments. Both are filled at boot, so a request parses nothing.
	re   *regexp.Regexp
	nums []float64
}

func (f *field) rule(name string) *rule {
	for _, r := range f.rules {
		if r.name == name {
			return r
		}
	}
	return nil
}

// Fields returns the input names this set declares, sorted.
func (s *Set) Fields() []string {
	names := make([]string, len(s.fields))
	for i, f := range s.fields {
		names[i] = f.name
	}
	return names
}

// RulesFor returns the rule names written against one field, in the order they
// were written. It is nil for a field the set does not declare.
func (s *Set) RulesFor(field string) []string {
	f, ok := s.byName[field]
	if !ok {
		return nil
	}
	names := make([]string, len(f.rules))
	for i, r := range f.rules {
		names[i] = r.name
	}
	return names
}

// Source returns where the set was compiled, which is what a boot failure
// names.
func (s *Set) Source() (file string, line int) { return s.file, s.line }

// Validate runs the set over a submitted form and returns what passed and what
// failed.
//
// It is Make plus Passes for the common case: an HTML form, no upload, no rule
// that leaves the process. A set with `unique`, `exists`, `current_password` or
// `active_url` in it goes through Make instead, which is where the Grant, the
// verifier and the context are given -- those four fail closed here, on purpose:
// a read is authorized like any other, and a rule set carries no Grant.
//
// The input is url.Values rather than Data because that is what a form arrives
// as; DataFrom is the conversion, and a name sent twice becomes a list.
//
// The returned Errors is nil when nothing failed. Assigning it to an error
// interface makes that interface non-nil even so, because the type is not nil
// -- callers ask Any(), and http.Context.Validate returns a plain nil for
// exactly this reason.
func (s *Set) Validate(values url.Values) (Input, Errors) {
	v := Make(DataFrom(values), s)
	if v.Passes() {
		return v.Validated(), nil
	}
	return v.Validated(), v.Errors()
}

// message is the sentence one failure puts on the field, after any override.
//
// An expanded field answers to the name it was written under as well: the
// override is keyed "items.*.price.required" because that is what the rule set
// declares, and the failure lands on "items.0.price".
func (s *Set) message(f *field, r *rule) string {
	if custom, ok := s.messages[f.name+"."+r.name]; ok {
		return custom
	}
	if f.primary != "" {
		if custom, ok := s.messages[f.primary+"."+r.name]; ok {
			return custom
		}
	}
	return r.spec.message(f, r)
}

// Input is what passed the rules. It is the only way to read a submitted value
// out of a validated request: a field the set does not declare is not in here,
// so a value nobody wrote a rule for cannot reach a repository by accident.
//
// It is read by the name a rule declared it under, dotted for a nested one --
// "address.city", "items.0.name" -- or all at once as a tree with Tree.
type Input struct {
	// data is what passed, one entry per declared name, flat.
	data Data
	// tree is the same values nested the way their names declare.
	tree Data
}

// newInput wraps the values that passed.
func newInput(flat Data) Input {
	return Input{data: flat, tree: buildTree(flat)}
}

// value reads a field: by the exact name it was declared under, and then by
// walking the tree, so "tags" finds the list that "tags.*" declared.
func (in Input) value(field string) (any, bool) {
	if v, ok := lookup(in.data, field); ok {
		return v, true
	}
	return lookup(in.tree, field)
}

// Has reports whether the field was sent and passed.
func (in Input) Has(field string) bool {
	_, ok := in.value(field)
	return ok
}

// Get returns the value of the field in the shape it was sent, and nil when it
// was not sent. A nested field is a Data and a list is a []any.
func (in Input) Get(field string) any {
	v, _ := in.value(field)
	return v
}

// String returns the value, or empty when the field was not sent. A field sent
// more than once is a list, and this is its first value -- what url.Values.Get
// answers.
func (in Input) String(field string) string {
	value := in.Get(field)
	if list, ok := asList(value); ok {
		if len(list) == 0 {
			return ""
		}
		return stringOf(list[0])
	}
	return stringOf(value)
}

// Strings returns every value of a field, which is what a multi-select sends. A
// field sent once is a list of one.
func (in Input) Strings(field string) []string {
	value, sent := in.value(field)
	if !sent {
		return nil
	}
	if list, ok := asList(value); ok {
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = stringOf(item)
		}
		return out
	}
	return []string{stringOf(value)}
}

// Int returns the value as a whole number. It is zero when the field was not
// sent or does not hold one -- which is a rule's job to have proven, with
// `integer`.
func (in Input) Int(field string) int64 {
	n, _ := whole(in.String(field))
	return n
}

// Float returns the value as a number, zero when there is none. `numeric` or
// `decimal` is what proves there is.
func (in Input) Float(field string) float64 {
	n, _ := number(in.String(field))
	return n
}

// Bool reads a checkbox.
//
// It accepts every value `accepted` does -- "1", "on", "yes", "true" -- and not
// only the "0" and "1" that the `boolean` rule allows, because an unticked
// checkbox sends nothing and a ticked one sends "on". Anything else is false: a
// checkbox has no third answer.
func (in Input) Bool(field string) bool { return isAccepted(in.String(field)) }

// Time reads a value with the layout its rule declared. It is the zero time
// when the field was not sent or does not parse, which `date_format` is what
// prevents.
func (in Input) Time(field, layout string) time.Time {
	t, err := time.Parse(layout, in.String(field))
	if err != nil {
		return time.Time{}
	}
	return t
}

// File returns the upload that passed, and false when the field holds none.
func (in Input) File(field string) (File, bool) { return asFile(in.Get(field)) }

// Tree returns everything that passed, nested the way the names declare it: the
// rules of items.*.name give {"items": [{"name": ...}]}, and the rules of
// address.city give {"address": {"city": ...}}. A level whose keys are 0, 1, 2...
// is a list.
//
// It holds the fields that were declared and passed and nothing else the request
// sent. It is a copy, so that a caller cannot reach back into the request's own
// values through it.
func (in Input) Tree() Data {
	copied, _ := deepClone(in.tree).(Data)
	if copied == nil {
		return Data{}
	}
	return copied
}

// Data returns a copy of everything that passed, flat: one entry per declared
// name, with a nested field under its dotted name. Tree is the nested one.
func (in Input) Data() Data { return in.data.Clone() }

// Values returns a copy of everything that passed, as a submitted form, for a
// caller that hands the whole thing on. A nested value is written under its
// dotted name, and a list of plain values is the several values of one name.
func (in Input) Values() url.Values {
	out := url.Values{}
	for name, value := range in.tree {
		valuesInto(out, name, value)
	}
	return out
}

// valuesInto writes a value as a form would have sent it. An upload has no
// spelling in url.Values and is left out.
func valuesInto(out url.Values, name string, value any) {
	switch v := value.(type) {
	case nil:
		out[name] = []string{""}
	case File:
	case []any:
		plain := true
		for _, item := range v {
			if isArray(item) {
				plain = false
			}
			if _, isFile := item.(File); isFile {
				plain = false
			}
		}
		if plain {
			list := make([]string, len(v))
			for i, item := range v {
				list[i] = stringOf(item)
			}
			out[name] = list
			return
		}
		for i, item := range v {
			valuesInto(out, name+"."+itoa(i), item)
		}
	case Data:
		for key, item := range v {
			valuesInto(out, name+"."+key, item)
		}
	case map[string]any:
		for key, item := range v {
			valuesInto(out, name+"."+key, item)
		}
	default:
		out[name] = []string{stringOf(v)}
	}
}

// buildTree nests the dotted names of the values that passed.
func buildTree(flat Data) Data {
	tree := Data{}
	for _, name := range slices.Sorted(maps.Keys(flat)) {
		next, _ := dataSet(tree, strings.Split(name, "."), deepClone(flat[name])).(Data)
		tree = next
	}
	return listsOf(tree).(Data)
}

// listsOf turns every level whose keys are 0, 1, 2... into a list.
func listsOf(node any) any {
	switch n := node.(type) {
	case Data:
		converted := make(Data, len(n))
		for key, item := range n {
			converted[key] = listsOf(item)
		}
		if len(converted) > 0 {
			list := make([]any, len(converted))
			for key, item := range converted {
				i, err := strconv.Atoi(key)
				if err != nil || i < 0 || i >= len(list) || strconv.Itoa(i) != key {
					return converted
				}
				list[i] = item
			}
			return list
		}
		return converted
	case map[string]any:
		return listsOf(Data(n))
	case []any:
		out := make([]any, len(n))
		for i, item := range n {
			out[i] = listsOf(item)
		}
		return out
	}
	return node
}

// anyAffix reports whether the value carries any of the affixes, with the given
// match -- the shape starts_with, ends_with and their two refusals share.
func anyAffix(list []string, match func(string, string) bool, v string) bool {
	return slices.ContainsFunc(list, func(affix string) bool { return match(v, affix) })
}
