package testing

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/testing/constraints"
)

// T is what an assertion in this package needs from the test it runs inside.
// The standard library's *testing.T satisfies it.
//
// Continuing after a failed assertion would let the assertions that follow
// read a response the first one already proved wrong. Logf is here for the
// dump methods rather than for the assertions: dump() and dumpHeaders() have
// to write somewhere, and a test's own log is where output belongs -- go test
// shows it beside the test that produced it and hides it when the test passes.
type T interface {
	Helper()
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// fail reports a failure and stops the test.
func fail(t T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}

// assertTrue fails unless the condition is true.
func assertTrue(t T, condition bool, message string) {
	t.Helper()
	if !condition {
		fail(t, "%s", or(message, "Failed asserting that false is true."))
	}
}

// assertFalse fails unless the condition is false.
func assertFalse(t T, condition bool, message string) {
	t.Helper()
	if condition {
		fail(t, "%s", or(message, "Failed asserting that true is false."))
	}
}

// assertNotTrue fails unless the value is anything other than true.
func assertNotTrue(t T, condition bool, message string) {
	t.Helper()
	if condition {
		fail(t, "%s", or(message, "Failed asserting that true is not true."))
	}
}

// assertSame fails unless the two values are identical. It is identity, not
// equality.
func assertSame(t T, expected, actual any, message string) {
	t.Helper()
	if !identical(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s is identical to %s.", export(actual), export(expected))))
	}
}

// assertNotSame fails when the two values are identical.
func assertNotSame(t T, expected, actual any, message string) {
	t.Helper()
	if identical(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s is not identical to %s.", export(actual), export(expected))))
	}
}

// assertEquals fails unless the two values compare loosely equal, which holds
// a string and the number it spells to be equal.
func assertEquals(t T, expected, actual any, message string) {
	t.Helper()
	if !loosely(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s matches expected %s.", export(actual), export(expected))))
	}
}

// assertNotEquals fails when the two values compare loosely equal.
func assertNotEquals(t T, expected, actual any, message string) {
	t.Helper()
	if loosely(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s is not equal to %s.", export(actual), export(expected))))
	}
}

// assertNull fails unless the value is nil.
func assertNull(t T, actual any, message string) {
	t.Helper()
	if !isNil(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is null.", export(actual))))
	}
}

// assertNotNull fails when the value is nil.
func assertNotNull(t T, actual any, message string) {
	t.Helper()
	if isNil(actual) {
		fail(t, "%s", or(message, "Failed asserting that null is not null."))
	}
}

// assertEmpty fails unless the value is empty: nil, false, zero, the empty
// string, "0", and the empty list or map.
func assertEmpty(t T, actual any, message string) {
	t.Helper()
	if !empty(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is empty.", export(actual))))
	}
}

// assertNotEmpty fails when the value is empty.
func assertNotEmpty(t T, actual any, message string) {
	t.Helper()
	if empty(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is not empty.", export(actual))))
	}
}

// assertCount fails unless the value is countable and its length is the one
// expected.
func assertCount(t T, expected int, actual any, message string) {
	t.Helper()
	n, ok := count(actual)
	if !ok {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is countable.", export(actual))))
		return
	}
	if n != expected {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that actual size %d matches expected size %d.", n, expected)))
	}
}

// assertStringContainsString fails unless the haystack contains the needle.
func assertStringContainsString(t T, needle, haystack, message string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %q contains %q.", haystack, needle)))
	}
}

// assertStringNotContainsString fails when the haystack contains the needle.
func assertStringNotContainsString(t T, needle, haystack, message string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %q does not contain %q.", haystack, needle)))
	}
}

// assertArrayHasKey fails unless the map has the key. It looks at one level,
// not at a dot path.
func assertArrayHasKey(t T, key string, array any, message string) {
	t.Helper()
	m, ok := array.(map[string]any)
	if !ok || !mapHas(m, key) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that an array has the key '%s'.", key)))
	}
}

// assertContains fails unless one element of the haystack compares loosely
// equal to the needle.
func assertContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	for _, item := range haystack {
		if loosely(needle, item) {
			return
		}
	}
	fail(t, "%s", or(message, fmt.Sprintf(
		"Failed asserting that %s contains %s.", export(haystack), export(needle))))
}

// assertNotContains fails when an element of the haystack compares loosely
// equal to the needle.
func assertNotContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	for _, item := range haystack {
		if loosely(needle, item) {
			fail(t, "%s", or(message, fmt.Sprintf(
				"Failed asserting that %s does not contain %s.", export(haystack), export(needle))))
			return
		}
	}
}

// assertEqualsCanonicalizing fails unless the two values are equal once both
// sides are sorted, so the order of a list does not count.
func assertEqualsCanonicalizing(t T, expected, actual any, message string) {
	t.Helper()
	if !loosely(sortRecursive(expected), sortRecursive(actual)) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s matches expected %s, in any order.",
			export(actual), export(expected))))
	}
}

// assertIsArray fails unless the value is a list, an array or a map.
func assertIsArray(t T, actual any, message string) {
	t.Helper()
	switch actual.(type) {
	case map[string]any, []any:
		return
	}
	if actual != nil {
		k := reflect.ValueOf(actual).Kind()
		if k == reflect.Slice || k == reflect.Map || k == reflect.Array {
			return
		}
	}
	fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is of type array.", export(actual))))
}

// assertGreaterThanOrEqual fails unless actual is at least expected.
func assertGreaterThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	if actual < expected {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %d is equal to or greater than %d.", actual, expected)))
	}
}

// assertLessThanOrEqual fails unless actual is at most expected.
func assertLessThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	if actual > expected {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %d is equal to or less than %d.", actual, expected)))
	}
}

// assertThat evaluates a constraint and reports what it says when it does not
// hold.
func assertThat(t T, value any, c constraints.Constraint, message string) {
	t.Helper()
	if !c.Matches(value) {
		fail(t, "%s", or(message, "Failed asserting that "+c.FailureDescription(value)))
	}
}

func or(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

// identical reports whether two values are the same value, over the shapes
// json.Unmarshal produces.
//
// Numbers are the one place where a literal DeepEqual would be wrong rather
// than strict: json.Unmarshal gives every number as a float64, so an expected
// int 1 and an actual float64 1 are the same JSON value written twice.
func identical(a, b any) bool {
	return reflect.DeepEqual(canonical(a), canonical(b))
}

// loosely reports whether two values are equal under a relaxed comparison:
// identical, or the same number written as a string on one side and a number
// on the other.
func loosely(a, b any) bool {
	if identical(a, b) {
		return true
	}
	an, aok := number(a)
	bn, bok := number(b)
	if aok && bok {
		return an == bn
	}
	as, aIsStr := a.(string)
	bs, bIsStr := b.(string)
	if aIsStr && bok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(as), 64); err == nil {
			return f == bn
		}
	}
	if bIsStr && aok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(bs), 64); err == nil {
			return f == an
		}
	}
	if aIsStr && bIsStr {
		return as == bs
	}
	return false
}

// canonical rewrites a value into the shape json.Unmarshal would have
// produced for it, so that a literal written in a test and a value decoded
// from a response can be compared at all.
func canonical(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case string, bool:
		return value
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, item := range value {
			out[k] = canonical(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = canonical(item)
		}
		return out
	case json.Number:
		f, err := value.Float64()
		if err != nil {
			return value.String()
		}
		return f
	}
	if n, ok := number(v); ok {
		return n
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = canonical(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = canonical(iter.Value().Interface())
		}
		return out
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return canonical(rv.Elem().Interface())
	default:
		return v
	}
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

// empty reports whether a value counts as empty: nil, false, 0, "", "0" and
// the empty list or map.
func empty(v any) bool {
	if isNil(v) {
		return true
	}
	switch value := v.(type) {
	case bool:
		return !value
	case string:
		return value == "" || value == "0"
	}
	if n, ok := number(v); ok {
		return n == 0
	}
	if n, ok := count(v); ok {
		return n == 0
	}
	return false
}

func count(v any) (int, bool) {
	if v == nil {
		return 0, false
	}
	switch value := v.(type) {
	case map[string]any:
		return len(value), true
	case []any:
		return len(value), true
	case string:
		return len(value), false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len(), true
	default:
		return 0, false
	}
}

func mapHas(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// export renders a value the way a test reader can read it in a failure
// message.
func export(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	encoded, err := encode(v, false)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return encoded
}

// encode renders a value as JSON, leaving slashes and non-ASCII unescaped,
// optionally indented.
func encode(v any, pretty bool) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "    ")
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func mustEncode(v any) string {
	encoded, err := encode(v, false)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return encoded
}

func mustEncodePretty(v any) string {
	encoded, err := encode(v, true)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return encoded
}

// sortRecursive sorts every list by its encoded form, all the way down, so
// that two payloads that differ only in order come out the same. Object keys need no sorting -- encoding/json already writes
// them in order.
func sortRecursive(v any) any {
	switch value := canonical(v).(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, item := range value {
			out[k] = sortRecursive(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = sortRecursive(item)
		}
		sort.SliceStable(out, func(i, j int) bool {
			return mustEncode(out[i]) < mustEncode(out[j])
		})
		return out
	default:
		return value
	}
}
