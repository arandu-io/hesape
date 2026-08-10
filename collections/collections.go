package collections

// Number is the set of types Sum can add. It is written out here rather than
// imported, because the core carries no dependency beyond golang.org/x/crypto.
// Complex numbers are absent on purpose: nothing a Sum is asked for in an
// application is complex, and admitting them would make the zero value of an
// empty sum harder to explain than it is worth.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// Map returns the result of applying f to every element of s, in order.
//
// The result has exactly len(s) elements, so mapping an empty slice yields an
// empty slice rather than nil.
func Map[T, U any](s []T, f func(T) U) []U {
	out := make([]U, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

// Filter returns the elements of s for which keep reports true, in order.
//
// The result is empty rather than nil when nothing matches. To drop the
// elements that match instead, negate the predicate, or use Partition when both
// halves are wanted.
func Filter[T any](s []T, keep func(T) bool) []T {
	out := make([]T, 0, len(s))
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// Reduce folds s into a single value, left to right, starting from initial.
//
// It returns initial unchanged when s is empty.
func Reduce[T, A any](s []T, initial A, f func(A, T) A) A {
	acc := initial
	for _, v := range s {
		acc = f(acc, v)
	}
	return acc
}

// First returns the first element of s for which match reports true.
//
// The second result is false when no element matches, and the first is then the
// zero value of T. To read the first element of a slice without a condition,
// check len(s) and index it.
func First[T any](s []T, match func(T) bool) (T, bool) {
	for _, v := range s {
		if match(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// Last returns the last element of s for which match reports true.
//
// The second result is false when no element matches, and the first is then the
// zero value of T.
func Last[T any](s []T, match func(T) bool) (T, bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if match(s[i]) {
			return s[i], true
		}
	}
	var zero T
	return zero, false
}

// GroupBy indexes s by the key that key reports, keeping every element.
//
// Within each group the elements stay in the order they had in s. The map is
// empty rather than nil when s is empty.
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for _, v := range s {
		k := key(v)
		out[k] = append(out[k], v)
	}
	return out
}

// KeyBy indexes s by the key that key reports, keeping one element per key.
//
// When two elements share a key the later one wins, which is what makes KeyBy
// usable to collapse a list onto its most recent row. Use GroupBy to keep them
// all.
func KeyBy[T any, K comparable](s []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(s))
	for _, v := range s {
		out[key(v)] = v
	}
	return out
}

// Partition splits s in two: the elements for which match reports true, and the
// rest. Both keep the order they had in s, and both are empty rather than nil.
func Partition[T any](s []T, match func(T) bool) (matched, rest []T) {
	matched = make([]T, 0)
	rest = make([]T, 0)
	for _, v := range s {
		if match(v) {
			matched = append(matched, v)
		} else {
			rest = append(rest, v)
		}
	}
	return matched, rest
}

// UniqueBy returns the elements of s whose key is seen for the first time, in
// order. The first element carrying a key is the one kept.
//
// It does not sort, so it is not the same as sorting and calling slices.Compact:
// the input order survives, which is the reason to reach for it.
func UniqueBy[T any, K comparable](s []T, key func(T) K) []T {
	seen := make(map[K]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		k := key(v)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Sum adds the number that value reports for every element of s.
//
// It returns the zero of N when s is empty. There is no form without the
// projection: over a slice of numbers, pass a function that returns its
// argument.
func Sum[T any, N Number](s []T, value func(T) N) N {
	var total N
	for _, v := range s {
		total += value(v)
	}
	return total
}
