package validation

import (
	"slices"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/jsonschema"
)

// This file reads a compiled Set as a JSON Schema, so that the rules a request
// declares are also the schema its documentation shows. One declaration, two
// readers: nothing is written a second time to describe what the rules already
// say.
//
// Only a rule that means something to a schema is carried over. The rest -- a
// rule about another field, one that asks the database, one that runs code -- is
// left out of the document rather than approximated, because a schema that says
// less than the validator is honest and one that says something different is
// not.
//
//	required, filled-by-type            the property is required
//	string, integer, numeric, decimal,
//	boolean, array, list, file          the type; a field with none is text
//	min, max, size, between             length, value or item count, by type
//	in, enum                            an enum
//	email, url, uuid, ipv4, ipv6,
//	date_format (date only)             a format
//	regex, digits, digits_between       a pattern
//	multiple_of                         multipleOf
//	distinct                            uniqueItems
//	nullable                            nullable
//
// A name with a wildcard is a list of objects, and a name with a dot is an
// object: items.*.name is {"items": [{"name": ...}]}.

// JSONSchema returns the set as an object schema, with one property for each
// top-level name the set declares.
//
// The object is closed, as every object in the jsonschema package is: a field
// no rule names is not part of what the set accepts into its result.
func (s *Set) JSONSchema() *jsonschema.ObjectType {
	root := &schemaNode{}
	for _, f := range s.fields {
		root.place(strings.Split(f.name, "."), f)
	}
	return root.object()
}

// schemaNode is one name in the tree of names a set declares.
type schemaNode struct {
	field    *field
	children map[string]*schemaNode
	order    []string
	star     *schemaNode
}

// place puts the field at the path, creating the nodes above it.
func (n *schemaNode) place(path []string, f *field) {
	if len(path) == 0 {
		n.field = f
		return
	}
	if path[0] == "*" {
		if n.star == nil {
			n.star = &schemaNode{}
		}
		n.star.place(path[1:], f)
		return
	}
	if n.children == nil {
		n.children = map[string]*schemaNode{}
	}
	child, held := n.children[path[0]]
	if !held {
		child = &schemaNode{}
		n.children[path[0]] = child
		n.order = append(n.order, path[0])
	}
	child.place(path[1:], f)
}

// object renders the node's children as the properties of an object.
func (n *schemaNode) object() *jsonschema.ObjectType {
	properties := make([]jsonschema.Property, 0, len(n.order))
	for _, name := range n.order {
		properties = append(properties, jsonschema.Prop(name, n.children[name].schema()))
	}
	object := jsonschema.Object(properties...)
	if n.field != nil {
		applyCommon(n.field, object)
	}
	return object
}

// schema renders the node as the type its rules and its children describe.
func (n *schemaNode) schema() jsonschema.Type {
	switch {
	case len(n.children) > 0:
		return n.object()
	case n.star != nil:
		array := jsonschema.Array().Items(n.star.schema())
		if n.field != nil {
			applyArray(n.field, array)
			applyCommon(n.field, array)
		}
		return array
	case n.field != nil:
		return scalarSchema(n.field)
	}
	return jsonschema.String()
}

// kinds a schema can have.
const (
	kindString  = "string"
	kindInteger = "integer"
	kindNumber  = "number"
	kindBoolean = "boolean"
	kindArray   = "array"
)

// kindOf reads the type a field's rules declare. A field that declares none is
// text, which is what a form sends.
func kindOf(f *field) string {
	switch {
	case f.rule("integer") != nil:
		return kindInteger
	case f.rule("numeric") != nil || f.rule("decimal") != nil:
		return kindNumber
	case f.rule("boolean") != nil:
		return kindBoolean
	case f.rule("array") != nil || f.rule("list") != nil:
		return kindArray
	}
	return kindString
}

// scalarSchema renders a field that has no children.
func scalarSchema(f *field) jsonschema.Type {
	switch kindOf(f) {
	case kindInteger:
		t := jsonschema.Integer()
		if lo, hi, ok := bounds(f); ok {
			if lo != nil {
				t.Min(int(*lo))
			}
			if hi != nil {
				t.Max(int(*hi))
			}
		}
		if r := f.rule("multiple_of"); r != nil && len(r.nums) == 1 && r.nums[0] == float64(int(r.nums[0])) {
			t.MultipleOf(int(r.nums[0]))
		}
		if cases := enumCases(f); len(cases) > 0 {
			var ints []int
			for _, c := range cases {
				if n, err := strconv.Atoi(c); err == nil {
					ints = append(ints, n)
				}
			}
			if len(ints) == len(cases) {
				t.Enum(ints...)
			}
		}
		applyCommon(f, t)
		return t
	case kindNumber:
		t := jsonschema.Number()
		if lo, hi, ok := bounds(f); ok {
			if lo != nil {
				t.Min(*lo)
			}
			if hi != nil {
				t.Max(*hi)
			}
		}
		if r := f.rule("multiple_of"); r != nil && len(r.nums) == 1 {
			t.MultipleOf(r.nums[0])
		}
		applyCommon(f, t)
		return t
	case kindBoolean:
		t := jsonschema.Boolean()
		applyCommon(f, t)
		return t
	case kindArray:
		t := jsonschema.Array()
		applyArray(f, t)
		applyCommon(f, t)
		return t
	}

	t := jsonschema.String()
	if f.file {
		t.Format("binary")
	}
	if lo, hi, ok := bounds(f); ok && !f.file {
		if lo != nil {
			t.Min(int(*lo))
		}
		if hi != nil {
			t.Max(int(*hi))
		}
	}
	if cases := enumCases(f); len(cases) > 0 {
		t.Enum(cases...)
	}
	if format := formatOf(f); format != "" {
		t.Format(format)
	}
	if pattern := patternOf(f); pattern != "" {
		t.Pattern(pattern)
	}
	applyCommon(f, t)
	return t
}

// commonTyped is what every type the builder returns can do.
type commonTyped interface {
	jsonschema.Type
}

// applyCommon sets what any type carries: that it is required, and that it may
// be null.
func applyCommon(f *field, t commonTyped) {
	required := f.rule("required") != nil || f.rule("present") != nil
	nullable := f.rule("nullable") != nil
	switch typed := t.(type) {
	case *jsonschema.StringType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	case *jsonschema.IntegerType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	case *jsonschema.NumberType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	case *jsonschema.BooleanType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	case *jsonschema.ArrayType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	case *jsonschema.ObjectType:
		if required {
			typed.Required()
		}
		if nullable {
			typed.Nullable()
		}
	}
}

// applyArray sets the item count and the uniqueness of an array.
func applyArray(f *field, t *jsonschema.ArrayType) {
	if lo, hi, ok := bounds(f); ok {
		if lo != nil {
			t.Min(int(*lo))
		}
		if hi != nil {
			t.Max(int(*hi))
		}
	}
	if f.rule("distinct") != nil {
		t.Unique()
	}
}

// bounds reads the inclusive limits a field's size rules declare. A bound is nil
// when the rules leave that side open.
func bounds(f *field) (lo, hi *float64, found bool) {
	set := func(p **float64, v float64) {
		if *p == nil || (p == &lo && v > **p) || (p == &hi && v < **p) {
			x := v
			*p = &x
		}
		found = true
	}
	if r := f.rule("min"); r != nil && len(r.nums) == 1 {
		set(&lo, r.nums[0])
	}
	if r := f.rule("max"); r != nil && len(r.nums) == 1 {
		set(&hi, r.nums[0])
	}
	if r := f.rule("size"); r != nil && len(r.nums) == 1 {
		set(&lo, r.nums[0])
		set(&hi, r.nums[0])
	}
	if r := f.rule("between"); r != nil && len(r.nums) == 2 {
		set(&lo, r.nums[0])
		set(&hi, r.nums[1])
	}
	return lo, hi, found
}

// enumCases reads the values an in rule or an enum rule allows.
func enumCases(f *field) []string {
	if r := f.rule("in"); r != nil {
		return slices.Clone(r.args)
	}
	for _, r := range f.rules {
		if r.name == "enum" && r.object == nil && len(r.args) > 0 {
			return slices.Clone(r.args)
		}
		if r.object != nil && r.name == "enum" {
			if e, ok := r.object.prototype().(*Enum); ok {
				return e.allowed()
			}
		}
	}
	return nil
}

// formatOf reads the format a string rule implies.
func formatOf(f *field) string {
	switch {
	case f.rule("email") != nil:
		return "email"
	case f.rule("url") != nil || f.rule("active_url") != nil:
		return "uri"
	case f.rule("uuid") != nil:
		return "uuid"
	case f.rule("ipv4") != nil:
		return "ipv4"
	case f.rule("ipv6") != nil:
		return "ipv6"
	case f.rule("date_format") != nil && f.layout == "2006-01-02":
		return "date"
	case f.rule("date_format") != nil && f.layout == "2006-01-02T15:04:05Z07:00":
		return "date-time"
	}
	return ""
}

// patternOf reads the pattern a rule implies: the one written, or the digits.
func patternOf(f *field) string {
	if r := f.rule("regex"); r != nil && len(r.args) == 1 {
		return r.args[0]
	}
	if r := f.rule("digits"); r != nil && len(r.args) == 1 {
		return "^[0-9]{" + r.args[0] + "}$"
	}
	if r := f.rule("digits_between"); r != nil && len(r.args) == 2 {
		return "^[0-9]{" + r.args[0] + "," + r.args[1] + "}$"
	}
	return ""
}
