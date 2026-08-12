package constraints

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ArraySubset answers to Illuminate\Testing\Constraints\ArraySubset: the value
// contains everything the subset says, and may contain more.
//
// It is the constraint behind Assert::assertArraySubset, which is what
// TestResponse::assertJson is built on. The mechanism is the PHP's:
// array_replace_recursive the subset over the actual value, and see whether the
// actual value changed. Nothing changed means everything the subset asked for
// was already there.
type ArraySubset struct {
	// Subset answers to $subset: what the value has to contain.
	Subset any

	// Strict answers to $strict: === rather than ==. It is what
	// assertJson($value, true) turns on, and it is the difference between a
	// response that carries the number 1 and one that carries the string "1".
	Strict bool

	// Equals stands for the PHP operators the comparison is written with.
	//
	// evaluate() is one line -- `$other === $patched` or `$other == $patched`
	// -- because PHP resolves both at the language level, over values that are
	// always arrays, strings, numbers, booleans or null. Go has ==, but it
	// panics on maps and slices and it holds int(1) and float64(1) apart, which
	// is the same JSON number written twice. So the comparison is passed in:
	// Assert::assertArraySubset hands over the one Illuminate\Testing uses for
	// === and ==, and a constraint built by hand falls back to
	// reflect.DeepEqual.
	Equals func(a, b any) bool
}

// NewArraySubset answers to ArraySubset::__construct.
func NewArraySubset(subset any, strict bool) *ArraySubset {
	return &ArraySubset{Subset: subset, Strict: strict}
}

// Matches answers to ArraySubset::evaluate with $returnResult true.
//
// The failing half of evaluate() -- building a ComparisonFailure and calling
// fail() -- is FailureDescription here, because a Go assertion reports by
// returning a message rather than by throwing an object that carries one.
func (c *ArraySubset) Matches(other any) bool {
	actual := toArray(other)
	patched := replaceRecursive(actual, toArray(c.Subset))

	equals := c.Equals
	if equals == nil {
		equals = reflect.DeepEqual
	}
	return equals(actual, patched)
}

// FailureDescription answers to ArraySubset::failureDescription.
func (c *ArraySubset) FailureDescription(other any) string {
	return "an array " + c.String()
}

// String answers to ArraySubset::toString.
func (c *ArraySubset) String() string {
	return "has the subset " + export(c.Subset)
}

// toArray answers to ArraySubset::toArray: whatever came in, seen as the array
// the comparison runs over.
//
// A PHP array is a list and a map at once, so both Go shapes pass through
// unchanged and anything else is left alone -- a scalar compared against a
// scalar is still a comparison, and turning it into a one-element array here
// would make it a different one.
func toArray(v any) any { return v }

// replaceRecursive answers to array_replace_recursive.
//
// Keys in the subset win, except where both sides hold an array: there it
// descends, which is what makes the constraint a subset check on every level
// rather than only the top one.
func replaceRecursive(other, subset any) any {
	switch s := subset.(type) {
	case map[string]any:
		o, ok := other.(map[string]any)
		if !ok {
			return subset
		}
		out := make(map[string]any, len(o)+len(s))
		for k, v := range o {
			out[k] = v
		}
		for k, v := range s {
			if have, found := out[k]; found {
				out[k] = replaceRecursive(have, v)
				continue
			}
			out[k] = v
		}
		return out

	case []any:
		o, ok := other.([]any)
		if !ok {
			return subset
		}
		// array_replace_recursive matches lists by position and keeps whatever
		// the longer of the two has past the end of the other.
		out := make([]any, max(len(o), len(s)))
		copy(out, o)
		for i, v := range s {
			if i < len(o) {
				out[i] = replaceRecursive(o[i], v)
				continue
			}
			out[i] = v
		}
		return out

	default:
		return subset
	}
}

// export answers to the Exporter toString() calls: the subset as a reader of
// the failure can read it.
func export(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(encoded)
}
