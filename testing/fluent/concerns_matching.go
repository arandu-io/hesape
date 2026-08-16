package fluent

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	hesapetesting "github.com/arandu-io/hesape/testing"
)

// The value assertions on [AssertableJSON].

// Where asserts the property is the expected value.
//
// An expectation of func(any) bool is handed what is at the key and must
// report true.
//
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

// WhereNot asserts the property is anything but that.
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

// WhereNull asserts the property is there and is null.
func (a *AssertableJSON) WhereNull(key string) *AssertableJSON {
	a.t.Helper()

	a.Has(key)
	hesapetesting.AssertNull(a.t, a.prop(key),
		fmt.Sprintf("Property [%s] should be null.", a.dotPath(key)))
	return a
}

// WhereNotNull asserts the property is there and is not null.
func (a *AssertableJSON) WhereNotNull(key string) *AssertableJSON {
	a.t.Helper()

	a.Has(key)
	hesapetesting.AssertNotNull(a.t, a.prop(key),
		fmt.Sprintf("Property [%s] should not be null.", a.dotPath(key)))
	return a
}

// WhereAll asserts every one of these properties is its expected value. Keys
// are visited in sorted order, so a failure names the same one every run.
func (a *AssertableJSON) WhereAll(bindings map[string]any) *AssertableJSON {
	a.t.Helper()

	for _, key := range sortedAnyMapKeys(bindings) {
		a.Where(key, bindings[key])
	}
	return a
}

// WhereType asserts the property is of that JSON type. Several types may be
// given, and any one of them satisfies it.
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

// WhereAllType asserts every one of these properties is of its expected JSON
// type.
func (a *AssertableJSON) WhereAllType(bindings map[string]any) *AssertableJSON {
	a.t.Helper()

	for _, key := range sortedAnyMapKeys(bindings) {
		a.WhereType(key, bindings[key])
	}
	return a
}

// WhereContains asserts the property carries each of the expected values,
// either as an element or as the value of that key on an element.
//
// That second one is what makes whereContains("name", "Alice") hold for a list
// of objects that each have a name.
//
// expected is one value or a list of them.
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

	// A missing value that is a truth test gets its own message, because
	// printing the function itself would say nothing.
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

// containsStrict reports whether the haystack holds an element equal to the
// search value, or one the search function reports true for.
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

// containsStrictBy reports whether the haystack holds an element whose key
// holds the search value, or one the search function reports true for.
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

// entries returns the values of a decoded payload, in order. A map answers
// with its values in sorted key order.
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

// wrapValues normalises an expectation into a list: a list stays a list,
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

// sameJSON reports whether two values are the same JSON document.
//
// It is not an assertion but the predicate containsStrict needs, and encoding
// both sides is what makes an int written in a test equal the float64 the
// decoder produced for the same number.
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

// getType names the JSON type of a decoded value: null, boolean, string,
// integer, double or array.
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
