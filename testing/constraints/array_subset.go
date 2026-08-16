package constraints

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ArraySubset matches a value that contains everything the subset names, and
// may contain more.
//
// The test is to overlay the subset onto the value and compare: nothing
// changed means everything the subset asked for was already there.
type ArraySubset struct {
	// Subset is what the value has to contain.
	Subset any

	// Strict asks for identity rather than a relaxed comparison. It records the
	// caller's choice; Equals is what actually performs it.
	Strict bool

	// Equals is the comparison, and defaults to reflect.DeepEqual when nil.
	//
	// It is a field because Go's == panics on maps and slices, and because it
	// holds int(1) and float64(1) apart -- which is the same JSON number
	// written twice.
	Equals func(a, b any) bool
}

// NewArraySubset returns a constraint over the given subset. Set Equals on the
// result to choose the comparison.
func NewArraySubset(subset any, strict bool) *ArraySubset {
	return &ArraySubset{Subset: subset, Strict: strict}
}

// Matches reports whether the value carries everything the subset names.
//
// It overlays the subset onto the value and compares the two with Equals, or
// with reflect.DeepEqual when Equals is nil.
func (c *ArraySubset) Matches(other any) bool {
	actual := toArray(other)
	patched := replaceRecursive(actual, toArray(c.Subset))

	equals := c.Equals
	if equals == nil {
		equals = reflect.DeepEqual
	}
	return equals(actual, patched)
}

// FailureDescription names the subset that was not carried.
func (c *ArraySubset) FailureDescription(other any) string {
	return "an array " + c.String()
}

// String names the constraint and the subset it looks for.
func (c *ArraySubset) String() string {
	return "has the subset " + export(c.Subset)
}

// toArray returns the value the comparison runs over.
func toArray(v any) any { return v }

// replaceRecursive overlays subset onto other and returns the result.
//
// Keys in the subset win, except where both sides hold a map or a list: there
// it descends, which is what makes the constraint a subset check on every
// level rather than only the top one.
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
		// Lists are matched by position, keeping whatever the longer of the
		// two has past the end of the other.
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

// export renders a value the way a reader of the failure message can read it.
func export(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(encoded)
}
