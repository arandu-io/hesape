package fluent

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	hesapetesting "github.com/arandu-io/hesape/testing"
)

// This file answers to Illuminate\Testing\Fluent\Concerns\Matching.
//
// The PHP is a trait mixed into AssertableJson. Go has no traits, so the
// methods are on AssertableJSON itself and the file keeps the trait's name.

// Where answers to Matching::where: the property is the expected value.
//
// expected stands for the PHP's `mixed|Closure`: a value, or a func(any) bool
// where the PHP takes a Closure. The closure form receives what is at the key,
// where the PHP wraps an array in a Collection first.
//
// The PHP calls ensureSorted() on both sides before comparing, so that two
// objects differing only in the order their keys were written compare equal.
// There is nothing to do here: a Go map has no order to sort, and the
// comparison is over decoded JSON.
func (a *AssertableJSON) Where(key string, expected any) *AssertableJSON {
	a.t.Helper()

	a.Has(key)

	actual := a.prop(key)

	if test, ok := expected.(func(any) bool); ok {
		hesapetesting.AssertTrue(a.t, test(actual),
			fmt.Sprintf("Property [%s] was marked as invalid using a closure.", a.dotPath(key)))
		return a
	}

	hesapetesting.AssertSame(a.t, expected, actual,
		fmt.Sprintf("Property [%s] does not match the expected value.", a.dotPath(key)))
	return a
}

// WhereNot answers to Matching::whereNot: the property is anything but that.
func (a *AssertableJSON) WhereNot(key string, expected any) *AssertableJSON {
	a.t.Helper()

	a.Has(key)

	actual := a.prop(key)

	if test, ok := expected.(func(any) bool); ok {
		hesapetesting.AssertFalse(a.t, test(actual),
			fmt.Sprintf("Property [%s] was marked as invalid using a closure.", a.dotPath(key)))
		return a
	}

	hesapetesting.AssertNotSame(a.t, expected, actual,
		fmt.Sprintf("Property [%s] contains a value that should be missing: [%s, %v]",
			a.dotPath(key), key, expected))
	return a
}

// WhereNull answers to Matching::whereNull.
func (a *AssertableJSON) WhereNull(key string) *AssertableJSON {
	a.t.Helper()

	a.Has(key)
	hesapetesting.AssertNull(a.t, a.prop(key),
		fmt.Sprintf("Property [%s] should be null.", a.dotPath(key)))
	return a
}

// WhereNotNull answers to Matching::whereNotNull.
func (a *AssertableJSON) WhereNotNull(key string) *AssertableJSON {
	a.t.Helper()

	a.Has(key)
	hesapetesting.AssertNotNull(a.t, a.prop(key),
		fmt.Sprintf("Property [%s] should not be null.", a.dotPath(key)))
	return a
}

// WhereAll answers to Matching::whereAll.
func (a *AssertableJSON) WhereAll(bindings map[string]any) *AssertableJSON {
	a.t.Helper()

	for _, key := range sortedAnyMapKeys(bindings) {
		a.Where(key, bindings[key])
	}
	return a
}

// WhereType answers to Matching::whereType: the property is of that JSON type.
//
// expected stands for the PHP's `string|array`: one name, several names
// separated by "|", or a []string. The names are PHP's gettype() names, because
// that is what the assertion compares against: boolean, integer, double,
// string, array, object, null.
//
// A JSON number arrives here as a float64 and reaches the PHP as an int when it
// has no fractional part, so an integral number is "integer" and the rest are
// "double" -- otherwise whereType("id", "integer") would never hold.
func (a *AssertableJSON) WhereType(key string, expected any) *AssertableJSON {
	a.t.Helper()

	a.Has(key)

	actual := a.prop(key)

	var names []string
	switch value := expected.(type) {
	case []string:
		names = value
	case string:
		names = strings.Split(value, "|")
	default:
		names = []string{fmt.Sprint(value)}
	}

	hesapetesting.AssertContains(a.t, getType(actual), names,
		fmt.Sprintf("Property [%s] is not of expected type [%s].", a.dotPath(key), strings.Join(names, "|")))
	return a
}

// WhereAllType answers to Matching::whereAllType.
func (a *AssertableJSON) WhereAllType(bindings map[string]any) *AssertableJSON {
	a.t.Helper()

	for _, key := range sortedAnyMapKeys(bindings) {
		a.WhereType(key, bindings[key])
	}
	return a
}

// WhereContains answers to Matching::whereContains: the property carries each
// of the expected values.
//
// It looks in two places, as the PHP does: the entries of the property itself,
// and the value under key in each of its entries. That second one is what makes
// whereContains("name", "Alice") hold for a list of objects that each have a
// name.
//
// expected is one value or a list of them, which is what `new Collection($expected)`
// does to a scalar in the PHP.
func (a *AssertableJSON) WhereContains(key string, expected any) *AssertableJSON {
	a.t.Helper()

	actual := a.prop(key)
	if actual == nil {
		actual = a.prop()
	}

	var missing []any
	for _, search := range wrapValues(expected) {
		if containsStrictBy(actual, key, search) || containsStrict(actual, search) {
			continue
		}
		missing = append(missing, search)
	}

	// The PHP picks its message on whether any of the values that are missing
	// is a closure, because "does not contain [Closure]" says nothing.
	for _, item := range missing {
		if _, isClosure := item.(func(any) bool); isClosure {
			hesapetesting.AssertEmpty(a.t, missing, fmt.Sprintf(
				"Property [%s] does not contain a value that passes the truth test within the given closure.", key))
			return a
		}
	}

	hesapetesting.AssertEmpty(a.t, missing,
		fmt.Sprintf("Property [%s] does not contain [%s].", key, joinValues(missing)))
	return a
}

// containsStrict answers to Collection::containsStrict with one argument.
func containsStrict(haystack any, search any) bool {
	if test, ok := search.(func(any) bool); ok {
		for _, item := range entries(haystack) {
			if test(item) {
				return true
			}
		}
		return false
	}

	for _, item := range entries(haystack) {
		if sameJSON(item, search) {
			return true
		}
	}
	return false
}

// containsStrictBy answers to Collection::containsStrict with a key and a
// value: an entry whose key holds the value.
func containsStrictBy(haystack any, key string, search any) bool {
	for _, item := range entries(haystack) {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, found := object[key]
		if !found {
			continue
		}
		if test, ok := search.(func(any) bool); ok {
			if test(value) {
				return true
			}
			continue
		}
		if sameJSON(value, search) {
			return true
		}
	}
	return false
}

// entries is the values of a decoded array, in either PHP shape.
func entries(v any) []any {
	switch value := v.(type) {
	case []any:
		return value
	case map[string]any:
		out := make([]any, 0, len(value))
		for _, key := range sortedAnyMapKeys(value) {
			out = append(out, value[key])
		}
		return out
	case nil:
		return nil
	default:
		return []any{v}
	}
}

// wrapValues answers to `new Collection($expected)`: a list stays a list,
// anything else becomes a list of one.
func wrapValues(expected any) []any {
	switch value := expected.(type) {
	case []any:
		return value
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	default:
		return []any{expected}
	}
}

// sameJSON is PHP's === for the values json.Unmarshal produces: the same
// document, written the same way.
//
// It is not the assertion -- that is AssertSame -- but the predicate
// containsStrict needs, and encoding both sides is what makes an int written in
// a test equal the float64 the decoder produced for the same number.
func sameJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// getType answers to strtolower(gettype($value)) over a decoded JSON value.
func getType(v any) string {
	switch value := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "array"
	case float64:
		if value == math.Trunc(value) && !math.IsInf(value, 0) {
			return "integer"
		}
		return "double"
	case int:
		return "integer"
	default:
		return "object"
	}
}

func joinValues(values []any) string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = fmt.Sprint(value)
	}
	return strings.Join(out, ", ")
}
