package testing

import (
	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/testing/exceptions"
)

// This file answers to Illuminate\Testing\Assert.
//
// The PHP is `abstract class Assert extends PHPUnit\Framework\Assert`: one
// method of its own, assertArraySubset, and every assertion the component makes
// inherited from PHPUnit and reachable as Assert::assertTrue and the rest.
// PHPUnit is not a dependency this collection carries -- go test is the runner
// -- so the inherited half is written out below, over the T that stands in for
// PHPUnit's ambient test case.
//
// They are exported because Fluent uses them: Fluent\Concerns\Has, Matching and
// Interaction call PHPUnit\Framework\Assert directly, which in Go is one
// package importing another.
//
// Every one of them takes the T first, where the PHP takes none, and a message
// last, where the PHP takes an optional one. The empty message means the same
// as the omitted argument: use the assertion's own wording.

// AssertTrue answers to Assert::assertTrue.
func AssertTrue(t T, condition bool, message string) {
	t.Helper()
	assertTrue(t, condition, message)
}

// AssertFalse answers to Assert::assertFalse.
func AssertFalse(t T, condition bool, message string) {
	t.Helper()
	assertFalse(t, condition, message)
}

// AssertNotTrue answers to Assert::assertNotTrue.
func AssertNotTrue(t T, condition bool, message string) {
	t.Helper()
	assertNotTrue(t, condition, message)
}

// AssertSame answers to Assert::assertSame.
func AssertSame(t T, expected, actual any, message string) {
	t.Helper()
	assertSame(t, expected, actual, message)
}

// AssertNotSame answers to Assert::assertNotSame.
func AssertNotSame(t T, expected, actual any, message string) {
	t.Helper()
	assertNotSame(t, expected, actual, message)
}

// AssertEquals answers to Assert::assertEquals.
func AssertEquals(t T, expected, actual any, message string) {
	t.Helper()
	assertEquals(t, expected, actual, message)
}

// AssertNotEquals answers to Assert::assertNotEquals.
func AssertNotEquals(t T, expected, actual any, message string) {
	t.Helper()
	assertNotEquals(t, expected, actual, message)
}

// AssertEqualsCanonicalizing answers to Assert::assertEqualsCanonicalizing.
func AssertEqualsCanonicalizing(t T, expected, actual any, message string) {
	t.Helper()
	assertEqualsCanonicalizing(t, expected, actual, message)
}

// AssertNull answers to Assert::assertNull.
func AssertNull(t T, actual any, message string) {
	t.Helper()
	assertNull(t, actual, message)
}

// AssertNotNull answers to Assert::assertNotNull.
func AssertNotNull(t T, actual any, message string) {
	t.Helper()
	assertNotNull(t, actual, message)
}

// AssertEmpty answers to Assert::assertEmpty.
func AssertEmpty(t T, actual any, message string) {
	t.Helper()
	assertEmpty(t, actual, message)
}

// AssertNotEmpty answers to Assert::assertNotEmpty.
func AssertNotEmpty(t T, actual any, message string) {
	t.Helper()
	assertNotEmpty(t, actual, message)
}

// AssertCount answers to Assert::assertCount.
func AssertCount(t T, expected int, actual any, message string) {
	t.Helper()
	assertCount(t, expected, actual, message)
}

// AssertContains answers to Assert::assertContains.
func AssertContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	assertContains(t, needle, haystack, message)
}

// AssertNotContains answers to Assert::assertNotContains.
func AssertNotContains(t T, needle any, haystack []string, message string) {
	t.Helper()
	assertNotContains(t, needle, haystack, message)
}

// AssertStringContainsString answers to Assert::assertStringContainsString.
func AssertStringContainsString(t T, needle, haystack, message string) {
	t.Helper()
	assertStringContainsString(t, needle, haystack, message)
}

// AssertStringNotContainsString answers to
// Assert::assertStringNotContainsString.
func AssertStringNotContainsString(t T, needle, haystack, message string) {
	t.Helper()
	assertStringNotContainsString(t, needle, haystack, message)
}

// AssertArrayHasKey answers to Assert::assertArrayHasKey.
func AssertArrayHasKey(t T, key string, array any, message string) {
	t.Helper()
	assertArrayHasKey(t, key, array, message)
}

// AssertIsArray answers to Assert::assertIsArray.
func AssertIsArray(t T, actual any, message string) {
	t.Helper()
	assertIsArray(t, actual, message)
}

// AssertGreaterThanOrEqual answers to Assert::assertGreaterThanOrEqual.
func AssertGreaterThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	assertGreaterThanOrEqual(t, expected, actual, message)
}

// AssertLessThanOrEqual answers to Assert::assertLessThanOrEqual.
func AssertLessThanOrEqual(t T, expected, actual int, message string) {
	t.Helper()
	assertLessThanOrEqual(t, expected, actual, message)
}

// AssertThat answers to Assert::assertThat.
func AssertThat(t T, value any, c constraints.Constraint, message string) {
	t.Helper()
	assertThat(t, value, c, message)
}

// Fail answers to Assert::fail.
func Fail(t T, message string) {
	t.Helper()
	fail(t, "%s", message)
}

// AssertArraySubset answers to Assert::assertArraySubset: the value carries
// everything the subset names, and may carry more.
//
// It returns an error where the PHP throws InvalidArgumentException, which is
// the (T, error) change: an argument of the wrong shape is the caller's
// mistake, not a failed expectation, and reporting it as a failure would file
// it under the thing being tested.
//
// checkForIdentity is the PHP's third argument: === instead of ==.
func AssertArraySubset(t T, subset, array any, checkForIdentity bool, msg string) error {
	t.Helper()

	if !isArrayish(subset) {
		return exceptions.Create(1, "array or ArrayAccess")
	}
	if !isArrayish(array) {
		return exceptions.Create(2, "array or ArrayAccess")
	}

	constraint := constraints.NewArraySubset(canonical(subset), checkForIdentity)

	// PHP compares with === or == at the language level; the constraint takes
	// the comparison instead. These two are the same operators, written for the
	// values json.Unmarshal produces.
	constraint.Equals = identical
	if !checkForIdentity {
		constraint.Equals = loosely
	}

	assertThat(t, canonical(array), constraint, msg)
	return nil
}

// isArrayish answers to `is_array($x) || $x instanceof ArrayAccess`.
func isArrayish(v any) bool {
	if v == nil {
		return false
	}
	if _, ok := count(v); ok {
		return true
	}
	return false
}
