package testing

import (
	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/testing/exceptions"
)

// The bare assertions. Every one of them takes the [T] first, and an empty
// message means "use the assertion's own wording".

// AssertTrue fails unless the condition is true.
func AssertTrue(t T, condition bool, message string) {
	t.Helper()
	assertTrue(t, condition, message)
}

// AssertFalse fails unless the condition is false.
func AssertFalse(t T, condition bool, message string) {
	t.Helper()
	assertFalse(t, condition, message)
}

// AssertNotTrue fails unless the value is anything other than true.
func AssertNotTrue(t T, condition bool, message string) {
	t.Helper()
	assertNotTrue(t, condition, message)
}

// AssertSame fails unless the two values are identical. It is identity, not
// equality.
func AssertSame(t T, expected, actual any, message string) {
	t.Helper()
	assertSame(t, expected, actual, message)
}

// AssertNotSame fails when the two values are identical.
func AssertNotSame(t T, expected, actual any, message string) {
	t.Helper()
	assertNotSame(t, expected, actual, message)
}

// AssertEquals fails unless the two values compare loosely equal, which holds
// a string and the number it spells to be equal.
func AssertEquals(t T, expected, actual any, message string) {
	t.Helper()
	assertEquals(t, expected, actual, message)
}

// AssertNotEquals fails when the two values compare loosely equal.
func AssertNotEquals(t T, expected, actual any, message string) {
	t.Helper()
	assertNotEquals(t, expected, actual, message)
}

// AssertEqualsCanonicalizing fails unless the two values are equal once both
// sides are sorted, so the order of a list does not count.
func AssertEqualsCanonicalizing(t T, expected, actual any, message string) {
	t.Helper()
	assertEqualsCanonicalizing(t, expected, actual, message)
}

// AssertNull fails unless the value is nil.
func AssertNull(t T, actual any, message string) {
	t.Helper()
	assertNull(t, actual, message)
}

// AssertNotNull fails when the value is nil.
func AssertNotNull(t T, actual any, message string) {
	t.Helper()
	assertNotNull(t, actual, message)
}

// AssertEmpty fails unless the value is empty: nil, false, zero, the empty
// string, "0", and the empty list or map.
func AssertEmpty(t T, actual any, message string) {
	t.Helper()
	assertEmpty(t, actual, message)
}

// AssertNotEmpty fails when the value is empty.
func AssertNotEmpty(t T, actual any, message string) {
	t.Helper()
	assertNotEmpty(t, actual, message)
}

// AssertCount fails unless the value is countable and its length is the one
// expected.
func AssertCount(t T, expected int, actual any, message string) {
	t.Helper()
	assertCount(t, expected, actual, message)
}

// AssertContains fails unless one element of the haystack compares loosely
// equal to the needle.
func AssertContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	assertContains(t, needle, haystack, message)
}

// AssertNotContains fails when an element of the haystack compares loosely
// equal to the needle.
func AssertNotContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	assertNotContains(t, needle, haystack, message)
}

// AssertStringContainsString fails unless the haystack contains the needle.
func AssertStringContainsString(t T, needle, haystack, message string) {
	t.Helper()
	assertStringContainsString(t, needle, haystack, message)
}

// AssertStringNotContainsString fails when the haystack contains the needle.
func AssertStringNotContainsString(t T, needle, haystack, message string) {
	t.Helper()
	assertStringNotContainsString(t, needle, haystack, message)
}

// AssertArrayHasKey fails unless the map has the key. It looks at one level,
// not at a dot path.
func AssertArrayHasKey(t T, key string, array any, message string) {
	t.Helper()
	assertArrayHasKey(t, key, array, message)
}

// AssertIsArray fails unless the value is a list, an array or a map.
func AssertIsArray(t T, actual any, message string) {
	t.Helper()
	assertIsArray(t, actual, message)
}

// AssertGreaterThanOrEqual fails unless actual is at least expected.
func AssertGreaterThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	assertGreaterThanOrEqual(t, expected, actual, message)
}

// AssertLessThanOrEqual fails unless actual is at most expected.
func AssertLessThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	assertLessThanOrEqual(t, expected, actual, message)
}

// AssertThat evaluates a constraint and reports what it says when it does not
// hold.
func AssertThat(t T, value any, c constraints.Constraint, message string) {
	t.Helper()
	assertThat(t, value, c, message)
}

// Fail reports a failure with the given message and stops the test.
func Fail(t T, message string) {
	t.Helper()
	fail(t, "%s", message)
}

// AssertArraySubset fails unless the value carries everything the subset
// names. It may carry more.
//
// checkForIdentity picks the comparison: identity when true, the relaxed one
// when false. The error result reports an argument that is neither a list nor
// a map, and no assertion is made in that case.
func AssertArraySubset(t T, subset, array any, checkForIdentity bool, msg string) error {
	t.Helper()

	if !isArrayish(subset) {
		return exceptions.Create(1, "array or ArrayAccess")
	}
	if !isArrayish(array) {
		return exceptions.Create(2, "array or ArrayAccess")
	}

	constraint := constraints.NewArraySubset(canonical(subset), checkForIdentity)

	// The constraint takes the comparison rather than hard-coding one, so the
	// same matcher serves both modes over the values json.Unmarshal produces.
	constraint.Equals = identical
	if !checkForIdentity {
		constraint.Equals = loosely
	}

	assertThat(t, canonical(array), constraint, msg)
	return nil
}

// isArrayish reports whether a value is something with a length: a list, an
// array or a map.
func isArrayish(v any) bool {
	if v == nil {
		return false
	}
	if _, ok := count(v); ok {
		return true
	}
	return false
}
