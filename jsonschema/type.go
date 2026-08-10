package jsonschema

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Type is one node of a schema: the kind a value must have, and the
// constraints on it.
//
// The set of implementations is closed -- ObjectType, StringType, IntegerType,
// NumberType, BooleanType, ArrayType, UnionType and AnyOfType. Validate
// switches on that set, so a ninth shape declared outside this package would
// be a schema Validate cannot check. The unexported method is what closes it.
type Type interface {
	json.Marshaler

	meta() *meta
}

// meta holds the keywords every type carries.
type meta struct {
	title       string
	description string
	enum        []any
	def         any
	hasDefault  bool
	required    bool
	nullable    bool
}

// base carries the modifiers every type shares.
//
// It takes the concrete type as a parameter and keeps a pointer to it so that
// Title, Description, Required and Nullable return the type they were called
// on and stay chainable. Go has no covariant return, and the alternative is
// the same four methods written eight times.
type base[T any] struct {
	m    meta
	self T
}

func (b *base[T]) meta() *meta { return &b.m }

// Title sets the type's title: a short label for the value, for a reader.
func (b *base[T]) Title(v string) T {
	b.m.title = v
	return b.self
}

// Description says what the value is for.
//
// It is worth writing on every property. A description is the cheapest thing
// in the document and it is the only place a producer learns what the program
// meant by a name.
func (b *base[T]) Description(v string) T {
	b.m.description = v
	return b.self
}

// Required marks the property as mandatory. An object missing it is refused
// before anything reads it.
//
// It is declared on the property's own type and hoisted into the parent
// object's required list when the document renders, which is where JSON Schema
// keeps it.
func (b *base[T]) Required() T {
	b.m.required = true
	return b.self
}

// Nullable allows the value to be null. Without it, an explicit null is
// refused like any other wrong kind.
func (b *base[T]) Nullable() T {
	b.m.nullable = true
	return b.self
}

// Property is one named member of an object.
type Property struct {
	// Name is the key the value appears under.
	Name string
	// Type is the schema the value must satisfy.
	Type Type
}

// Prop names a property of an object.
func Prop(name string, t Type) Property {
	return Property{Name: name, Type: t}
}

// ObjectType is a value with named properties.
//
// An object refuses a property it did not declare. See the package comment for
// why that is the default and not a switch.
type ObjectType struct {
	base[*ObjectType]

	properties []Property
}

// Object builds an object from its properties, in the order given.
func Object(properties ...Property) *ObjectType {
	t := &ObjectType{properties: properties}
	t.self = t
	return t
}

// Properties returns the object's properties in declaration order.
//
// It is here so a caller can print or walk a schema without decoding the
// document it just rendered.
func (t *ObjectType) Properties() []Property {
	out := make([]Property, len(t.properties))
	copy(out, t.properties)
	return out
}

// Default sets the value used when the property is absent.
func (t *ObjectType) Default(v map[string]any) *ObjectType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// StringType is a text value.
type StringType struct {
	base[*StringType]

	minLength, maxLength int
	hasMin, hasMax       bool
	pattern              *regexp.Regexp
	format               string
}

// String builds a string type.
func String() *StringType {
	t := &StringType{}
	t.self = t
	return t
}

// Min sets the minimum length in characters, inclusive.
func (t *StringType) Min(v int) *StringType {
	t.minLength, t.hasMin = v, true
	return t
}

// Max sets the maximum length in characters, inclusive.
func (t *StringType) Max(v int) *StringType {
	t.maxLength, t.hasMax = v, true
	return t
}

// Pattern sets a regular expression the value must match.
//
// It compiles here rather than at validation time, so a broken expression is a
// panic where it was written instead of a refusal where it was used.
func (t *StringType) Pattern(expr string) *StringType {
	t.pattern = regexp.MustCompile(expr)
	return t
}

// Format records one of JSON Schema's built-in formats, such as "email" or
// "date-time".
//
// It is written into the document and is not checked by Validate: JSON Schema
// itself defines format as an annotation, and a value refused for a reason the
// published document does not state is worse than one that is not refused.
func (t *StringType) Format(v string) *StringType {
	t.format = v
	return t
}

// Enum limits the value to a set.
//
// It is worth reaching for. A producer given a closed list picks from it, and
// a producer given "the status" invents one.
func (t *StringType) Enum(values ...string) *StringType {
	t.m.enum = make([]any, len(values))
	for i, v := range values {
		t.m.enum[i] = v
	}
	return t
}

// Default sets the value used when the property is absent.
func (t *StringType) Default(v string) *StringType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// IntegerType is a whole number.
type IntegerType struct {
	base[*IntegerType]

	minimum, maximum int
	hasMin, hasMax   bool
	multipleOf       int
	hasMultipleOf    bool
}

// Integer builds an integer type.
func Integer() *IntegerType {
	t := &IntegerType{}
	t.self = t
	return t
}

// Min sets the minimum value, inclusive.
func (t *IntegerType) Min(v int) *IntegerType {
	t.minimum, t.hasMin = v, true
	return t
}

// Max sets the maximum value, inclusive.
func (t *IntegerType) Max(v int) *IntegerType {
	t.maximum, t.hasMax = v, true
	return t
}

// MultipleOf requires the value to be a multiple of v.
func (t *IntegerType) MultipleOf(v int) *IntegerType {
	t.multipleOf, t.hasMultipleOf = v, true
	return t
}

// Enum limits the value to a set.
func (t *IntegerType) Enum(values ...int) *IntegerType {
	t.m.enum = make([]any, len(values))
	for i, v := range values {
		t.m.enum[i] = float64(v)
	}
	return t
}

// Default sets the value used when the property is absent.
func (t *IntegerType) Default(v int) *IntegerType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// NumberType is a number, whole or fractional.
//
// It has no MultipleOf. The check is exact arithmetic on a binary float, and
// 0.1 is not representable, so a rule written for money would refuse amounts
// that are correct.
type NumberType struct {
	base[*NumberType]

	minimum, maximum float64
	hasMin, hasMax   bool
}

// Number builds a number type.
func Number() *NumberType {
	t := &NumberType{}
	t.self = t
	return t
}

// Min sets the minimum value, inclusive.
func (t *NumberType) Min(v float64) *NumberType {
	t.minimum, t.hasMin = v, true
	return t
}

// Max sets the maximum value, inclusive.
func (t *NumberType) Max(v float64) *NumberType {
	t.maximum, t.hasMax = v, true
	return t
}

// Enum limits the value to a set.
func (t *NumberType) Enum(values ...float64) *NumberType {
	t.m.enum = make([]any, len(values))
	for i, v := range values {
		t.m.enum[i] = v
	}
	return t
}

// Default sets the value used when the property is absent.
func (t *NumberType) Default(v float64) *NumberType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// BooleanType is true or false.
type BooleanType struct {
	base[*BooleanType]
}

// Boolean builds a boolean type.
func Boolean() *BooleanType {
	t := &BooleanType{}
	t.self = t
	return t
}

// Default sets the value used when the property is absent.
func (t *BooleanType) Default(v bool) *BooleanType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// ArrayType is an ordered list.
type ArrayType struct {
	base[*ArrayType]

	minItems, maxItems int
	hasMin, hasMax     bool
	items              Type
	unique             bool
}

// Array builds an array type. Call Items to say what the elements are; an
// array with no Items accepts any element.
func Array() *ArrayType {
	t := &ArrayType{}
	t.self = t
	return t
}

// Items sets the schema every element must satisfy.
func (t *ArrayType) Items(item Type) *ArrayType {
	t.items = item
	return t
}

// Min sets the minimum number of elements, inclusive.
func (t *ArrayType) Min(v int) *ArrayType {
	t.minItems, t.hasMin = v, true
	return t
}

// Max sets the maximum number of elements, inclusive.
func (t *ArrayType) Max(v int) *ArrayType {
	t.maxItems, t.hasMax = v, true
	return t
}

// Unique requires the elements to differ from one another.
func (t *ArrayType) Unique() *ArrayType {
	t.unique = true
	return t
}

// Default sets the value used when the property is absent.
func (t *ArrayType) Default(v []any) *ArrayType {
	t.m.def, t.m.hasDefault = v, true
	return t
}

// unionKinds is the set of primitive names a union may be built from.
var unionKinds = []string{"string", "integer", "number", "boolean", "object", "array"}

// UnionType is a value that may be any of several primitive kinds. It renders
// as JSON Schema's type-as-a-list form.
//
// It is the short way to write a value that differs only in kind. When the
// alternatives differ in more than kind -- one has a pattern, the other a
// minimum -- they are separate schemas and the answer is AnyOf.
type UnionType struct {
	base[*UnionType]

	kinds []string
}

// Union builds a union of the named primitive kinds: string, integer, number,
// boolean, object or array. The name "null" is accepted and marks the union
// nullable.
//
// An unsupported name panics. A schema is written once, at start-up, and a
// typo there is a mistake in the program, not in the data.
func Union(kinds ...string) *UnionType {
	t := &UnionType{}
	t.self = t

	seen := map[string]bool{}
	for _, kind := range kinds {
		if kind == "null" {
			t.m.nullable = true
			continue
		}
		if !contains(unionKinds, kind) {
			panic(fmt.Sprintf("jsonschema: %q is not a kind a union may be built from", kind))
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		t.kinds = append(t.kinds, kind)
	}
	return t
}

// Kinds returns the union's member kinds, in the order given.
func (t *UnionType) Kinds() []string {
	out := make([]string, len(t.kinds))
	copy(out, t.kinds)
	return out
}

// AnyOfType is a value that must satisfy at least one of several schemas.
type AnyOfType struct {
	base[*AnyOfType]

	options []Type
}

// AnyOf builds an alternative between whole schemas.
func AnyOf(options ...Type) *AnyOfType {
	t := &AnyOfType{options: options}
	t.self = t
	return t
}

// Options returns the alternatives, in the order given.
func (t *AnyOfType) Options() []Type {
	out := make([]Type, len(t.options))
	copy(out, t.options)
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
