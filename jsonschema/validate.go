package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Problem is one thing wrong with a value.
type Problem struct {
	// Path is where the problem is, in dotted form: "filters.tag",
	// "tags[2]". It is empty for the value handed to Validate.
	Path string

	// Message is what is wrong with it, as a predicate: "is required",
	// "must be a string".
	Message string
}

// String renders the problem as a sentence.
func (p Problem) String() string {
	if p.Path == "" {
		return "the value " + p.Message
	}
	return p.Path + " " + p.Message
}

// Invalid is the error Validate returns. It carries every problem, so a caller
// that wants to act on one field can, and a caller that only prints gets the
// whole answer from Error.
type Invalid struct {
	Problems []Problem
}

// Error joins every problem into one sentence.
func (e *Invalid) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return strings.Join(parts, "; ")
}

// Validate checks a decoded JSON value against a schema.
//
// The value is what [encoding/json] produces for an any: map[string]any,
// []any, string, float64, bool or nil. It is checked before a handler runs,
// which is what lets that handler read a declared field without checking it,
// and what stops a value nobody declared from reaching application code.
//
// Every problem is reported at once, in the order the schema declares them. A
// producer told one mistake per attempt spends three attempts on a form it
// could have filled in on the second.
func Validate(schema Type, value any) error {
	if schema == nil {
		return errors.New("jsonschema: validate against a nil schema")
	}
	var problems []Problem
	check(schema, value, "", &problems)
	if len(problems) == 0 {
		return nil
	}
	return &Invalid{Problems: problems}
}

func check(t Type, v any, path string, problems *[]Problem) {
	m := t.meta()

	if v == nil {
		if !m.nullable {
			report(problems, path, "must not be null")
		}
		return
	}

	matched := false
	switch s := t.(type) {
	case *ObjectType:
		matched = checkObject(s, v, path, problems)
	case *StringType:
		matched = checkString(s, v, path, problems)
	case *IntegerType:
		matched = checkInteger(s, v, path, problems)
	case *NumberType:
		matched = checkNumber(s, v, path, problems)
	case *BooleanType:
		if _, ok := v.(bool); !ok {
			report(problems, path, "must be true or false")
			break
		}
		matched = true
	case *ArrayType:
		matched = checkArray(s, v, path, problems)
	case *UnionType:
		matched = checkUnion(s, v, path, problems)
	case *AnyOfType:
		matched = checkAnyOf(s, v, path, problems)
	default:
		report(problems, path, "has a schema this package cannot check")
	}

	if matched && len(m.enum) > 0 && !containsValue(m.enum, v) {
		report(problems, path, "must be one of "+list(m.enum))
	}
}

func checkObject(t *ObjectType, v any, path string, problems *[]Problem) bool {
	object, ok := v.(map[string]any)
	if !ok {
		report(problems, path, "must be an object")
		return false
	}

	declared := make(map[string]bool, len(t.properties))
	for _, p := range t.properties {
		declared[p.Name] = true

		child, present := object[p.Name]
		if !present {
			if p.Type.meta().required {
				report(problems, join(path, p.Name), "is required")
			}
			continue
		}
		check(p.Type, child, join(path, p.Name), problems)
	}

	// A property nobody declared is reported rather than ignored, and the
	// names are sorted because a map is not ordered and an error that reads
	// differently on two runs is an error nobody can test.
	//
	// An open object skips the check: additionalProperties is absent or true,
	// which JSON Schema reads as "anything else is allowed". Only Deserialize
	// produces one -- everything built here is closed.
	if !t.closed() {
		return true
	}
	var undeclared []string
	for name := range object {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(undeclared)
	for _, name := range undeclared {
		report(problems, join(path, name), "is not a declared property")
	}
	return true
}

func checkString(t *StringType, v any, path string, problems *[]Problem) bool {
	text, ok := v.(string)
	if !ok {
		report(problems, path, "must be a string")
		return false
	}
	if n := utf8.RuneCountInString(text); t.hasMin && n < t.minLength {
		report(problems, path, plural("must be at least %d character", t.minLength))
	} else if t.hasMax && n > t.maxLength {
		report(problems, path, plural("must be at most %d character", t.maxLength))
	}
	if t.pattern != nil && !t.pattern.MatchString(text) {
		report(problems, path, "must match "+t.pattern.String())
	}
	return true
}

func checkInteger(t *IntegerType, v any, path string, problems *[]Problem) bool {
	n, ok := asNumber(v)
	if !ok {
		report(problems, path, "must be a number")
		return false
	}
	if n != math.Trunc(n) {
		report(problems, path, "must be a whole number")
		return false
	}
	if t.hasMin && n < float64(t.minimum) {
		report(problems, path, fmt.Sprintf("must be at least %d", t.minimum))
	}
	if t.hasMax && n > float64(t.maximum) {
		report(problems, path, fmt.Sprintf("must be at most %d", t.maximum))
	}
	if t.hasMultipleOf && t.multipleOf != 0 && math.Mod(n, float64(t.multipleOf)) != 0 {
		report(problems, path, fmt.Sprintf("must be a multiple of %d", t.multipleOf))
	}
	return true
}

func checkNumber(t *NumberType, v any, path string, problems *[]Problem) bool {
	n, ok := asNumber(v)
	if !ok {
		report(problems, path, "must be a number")
		return false
	}
	if t.hasMin && n < t.minimum {
		report(problems, path, "must be at least "+number(t.minimum))
	}
	if t.hasMax && n > t.maximum {
		report(problems, path, "must be at most "+number(t.maximum))
	}
	if t.hasMultipleOf && t.multipleOf != 0 && math.Mod(n, t.multipleOf) != 0 {
		report(problems, path, "must be a multiple of "+number(t.multipleOf))
	}
	return true
}

func checkArray(t *ArrayType, v any, path string, problems *[]Problem) bool {
	items, ok := v.([]any)
	if !ok {
		report(problems, path, "must be a list")
		return false
	}
	if t.hasMin && len(items) < t.minItems {
		report(problems, path, plural("must have at least %d item", t.minItems))
	}
	if t.hasMax && len(items) > t.maxItems {
		report(problems, path, plural("must have at most %d item", t.maxItems))
	}
	if t.items != nil {
		for i, item := range items {
			check(t.items, item, fmt.Sprintf("%s[%d]", path, i), problems)
		}
	}
	if t.unique {
		seen := make(map[string]bool, len(items))
		for i, item := range items {
			key, err := json.Marshal(item)
			if err != nil {
				continue
			}
			if seen[string(key)] {
				report(problems, fmt.Sprintf("%s[%d]", path, i), "repeats an earlier item")
				continue
			}
			seen[string(key)] = true
		}
	}
	return true
}

func checkUnion(t *UnionType, v any, path string, problems *[]Problem) bool {
	for _, name := range t.types {
		if isKind(name, v) {
			return true
		}
	}
	report(problems, path, "must be "+article(t.types))
	return false
}

func checkAnyOf(t *AnyOfType, v any, path string, problems *[]Problem) bool {
	for _, option := range t.options {
		var ignored []Problem
		check(option, v, path, &ignored)
		if len(ignored) == 0 {
			return true
		}
	}
	report(problems, path, "must match one of the allowed shapes")
	return false
}

func report(problems *[]Problem, path, message string) {
	*problems = append(*problems, Problem{Path: path, Message: message})
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// isKind answers whether a decoded value is of a JSON Schema primitive kind.
func isKind(kind string, v any) bool {
	switch kind {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := asNumber(v)
		return ok
	case "integer":
		n, ok := asNumber(v)
		return ok && n == math.Trunc(n)
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return false
}

// asNumber accepts what a JSON decoder produces for a number, and the Go
// numeric types a caller building a value by hand reaches for.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func containsValue(values []any, v any) bool {
	for _, want := range values {
		if equalValue(want, v) {
			return true
		}
	}
	return false
}

func equalValue(a, b any) bool {
	if na, ok := asNumber(a); ok {
		nb, ok := asNumber(b)
		return ok && na == nb
	}
	return a == b
}

func list(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = display(v)
	}
	return strings.Join(parts, ", ")
}

func display(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	if n, ok := asNumber(v); ok {
		return number(n)
	}
	return fmt.Sprint(v)
}

func number(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func plural(format string, n int) string {
	out := fmt.Sprintf(format, n)
	if n != 1 {
		out += "s"
	}
	return out
}

func article(kinds []string) string {
	parts := make([]string, len(kinds))
	for i, kind := range kinds {
		switch kind {
		case "integer", "object", "array":
			parts[i] = "an " + kind
		default:
			parts[i] = "a " + kind
		}
	}
	switch len(parts) {
	case 0:
		return "of a kind this union allows"
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}
