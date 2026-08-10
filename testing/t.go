package testing

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// T is what an assertion in this package needs from the test it runs inside.
// The standard library's *testing.T satisfies it.
//
// It answers to PHPUnit's Assert class, which Illuminate\Testing calls into
// statically. PHP has one ambient test context; Go passes it, so every
// constructor here takes a T where the PHP constructor takes none. That is the
// only structural difference between this package and Illuminate\Testing.
//
// Fatalf rather than Errorf: a PHPUnit assertion throws
// ExpectationFailedException, which stops the test method. Continuing after a
// failed assertion would let the assertions that follow read a response the
// first one already proved wrong.
type T interface {
	Helper()
	Fatalf(format string, args ...any)
}

// fail answers to PHPUnit::fail.
func fail(t T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}

// assertTrue answers to PHPUnit::assertTrue.
func assertTrue(t T, condition bool, message string) {
	t.Helper()
	if !condition {
		fail(t, "%s", or(message, "Failed asserting that false is true."))
	}
}

// assertFalse answers to PHPUnit::assertFalse.
func assertFalse(t T, condition bool, message string) {
	t.Helper()
	if condition {
		fail(t, "%s", or(message, "Failed asserting that true is false."))
	}
}

// assertSame answers to PHPUnit::assertSame: identity, not equality.
func assertSame(t T, expected, actual any, message string) {
	t.Helper()
	if !identical(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s is identical to %s.", export(actual), export(expected))))
	}
}

// assertNotSame answers to PHPUnit::assertNotSame.
func assertNotSame(t T, expected, actual any, message string) {
	t.Helper()
	if identical(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s is not identical to %s.", export(actual), export(expected))))
	}
}

// assertEquals answers to PHPUnit::assertEquals: PHP's loose comparison, which
// holds a string and the number it spells to be equal.
func assertEquals(t T, expected, actual any, message string) {
	t.Helper()
	if !loosely(expected, actual) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s matches expected %s.", export(actual), export(expected))))
	}
}

// assertNull answers to PHPUnit::assertNull.
func assertNull(t T, actual any, message string) {
	t.Helper()
	if !isNil(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is null.", export(actual))))
	}
}

// assertNotNull answers to PHPUnit::assertNotNull.
func assertNotNull(t T, actual any, message string) {
	t.Helper()
	if isNil(actual) {
		fail(t, "%s", or(message, "Failed asserting that null is not null."))
	}
}

// assertEmpty answers to PHPUnit::assertEmpty, with PHP's notion of empty:
// null, false, zero, the empty string and the empty array.
func assertEmpty(t T, actual any, message string) {
	t.Helper()
	if !empty(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is empty.", export(actual))))
	}
}

// assertNotEmpty answers to PHPUnit::assertNotEmpty.
func assertNotEmpty(t T, actual any, message string) {
	t.Helper()
	if empty(actual) {
		fail(t, "%s", or(message, fmt.Sprintf("Failed asserting that %s is not empty.", export(actual))))
	}
}

// assertCount answers to PHPUnit::assertCount.
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

// assertStringContainsString answers to PHPUnit::assertStringContainsString.
func assertStringContainsString(t T, needle, haystack, message string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %q contains %q.", haystack, needle)))
	}
}

// assertStringNotContainsString answers to
// PHPUnit::assertStringNotContainsString.
func assertStringNotContainsString(t T, needle, haystack, message string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %q does not contain %q.", haystack, needle)))
	}
}

// assertArrayHasKey answers to PHPUnit::assertArrayHasKey. It looks at one
// level, not at a dot path -- Arr::has is the one that walks.
func assertArrayHasKey(t T, key string, array any, message string) {
	t.Helper()
	m, ok := array.(map[string]any)
	if !ok || !mapHas(m, key) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that an array has the key '%s'.", key)))
	}
}

// assertContains answers to PHPUnit::assertContains.
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

// assertNotContains answers to PHPUnit::assertNotContains.
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

// assertEqualsCanonicalizing answers to
// PHPUnit::assertEqualsCanonicalizing: equal once both sides are sorted.
func assertEqualsCanonicalizing(t T, expected, actual any, message string) {
	t.Helper()
	if !loosely(sortRecursive(expected), sortRecursive(actual)) {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %s matches expected %s, in any order.",
			export(actual), export(expected))))
	}
}

// assertIsArray answers to PHPUnit::assertIsArray. A PHP array is either a
// list or a string-keyed map, so both Go shapes pass.
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

// assertGreaterThanOrEqual answers to PHPUnit::assertGreaterThanOrEqual.
func assertGreaterThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	if actual < expected {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %d is equal to or greater than %d.", actual, expected)))
	}
}

// assertLessThanOrEqual answers to PHPUnit::assertLessThanOrEqual.
func assertLessThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	if actual > expected {
		fail(t, "%s", or(message, fmt.Sprintf(
			"Failed asserting that %d is equal to or less than %d.", actual, expected)))
	}
}

func or(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

// identical answers to PHP's === for the values json.Unmarshal produces.
//
// Numbers are the one place where a literal DeepEqual would be wrong rather
// than strict: json.Unmarshal gives every number as a float64, so an expected
// int 1 and an actual float64 1 are the same JSON value written twice.
func identical(a, b any) bool {
	return reflect.DeepEqual(canonical(a), canonical(b))
}

// loosely answers to PHP's ==: identical, or the same number written as a
// string on one side and a number on the other.
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

// empty answers to PHP's empty(): null, false, 0, "", "0" and the empty array.
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

// export answers to PHPUnit's exporter: the value as a test reader can read
// it in a failure message.
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

// encode answers to json_encode with JSON_UNESCAPED_SLASHES and
// JSON_UNESCAPED_UNICODE, optionally JSON_PRETTY_PRINT.
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

// sortRecursive answers to Arr::sortRecursive: sort every list by its encoded
// form, all the way down, so that two payloads that differ only in order come
// out the same. Object keys need no sorting -- encoding/json already writes
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
