package fluent

import (
	"strings"

	hesapetesting "github.com/arandu-io/hesape/testing"
)

// The accounting on [AssertableJSON]: which properties the test named, and
// what happens to the ones it did not.

// interactsWith records that the test said something about this property.
//
// A dotted key counts as an interaction with its first segment: a test that
// asserted about data.name has accounted for data, and what is inside it is the
// scope's business, not this one's.
func (a *AssertableJSON) interactsWith(key string) {
	name := key
	if at := strings.IndexByte(name, '.'); at >= 0 {
		name = name[:at]
	}

	for _, seen := range a.interacted {
		if seen == name {
			return
		}
	}
	a.interacted = append(a.interacted, name)
}

// Interacted asserts that every property in this scope
// was accounted for.
//
// It is what the whole class is for. A response that grew a field nobody meant
// to publish fails here, and it fails in the test that was already asserting
// about that endpoint rather than in one somebody has to think to write.
func (a *AssertableJSON) Interacted() {
	a.t.Helper()

	var unexpected []string
	for _, key := range a.keys {
		accounted := false
		for _, seen := range a.interacted {
			if seen == key {
				accounted = true
				break
			}
		}
		if !accounted {
			unexpected = append(unexpected, key)
		}
	}

	message := "Unexpected properties were found on the root level."
	if a.path != "" {
		message = "Unexpected properties were found in scope [" + a.path + "]."
	}

	hesapetesting.AssertSame(a.t, []string{}, orEmptyStrings(unexpected), message)
}

// Etc stops accounting for this scope, so [AssertableJSON.Interacted] passes
// whatever else the payload carries here.
//
// It is the escape hatch, and it is worth using sparingly: a scope with Etc on
// it asserts about what the test named and nothing about what it did not, which
// is the assertion the class exists to be stronger than.
func (a *AssertableJSON) Etc() *AssertableJSON {
	a.interacted = append([]string{}, a.keys...)
	return a
}

// orEmptyStrings keeps a nil slice from being compared against an empty one:
// array_diff() returns an empty array, never null.
func orEmptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
