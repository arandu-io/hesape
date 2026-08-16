package arr

import (
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
)

// First returns the first item passing the truth test, or the default when
// nothing passes. A nil callback takes the first item of all.
func First[T any](array []T, callback func(item T, key int) bool, def T) T {
	if callback == nil {
		if len(array) == 0 {
			return def
		}
		return array[0]
	}
	for i, item := range array {
		if callback(item, i) {
			return item
		}
	}
	return def
}

// Last returns the last item passing the truth test, or the default when
// nothing passes. A nil callback takes the last item of all.
func Last[T any](array []T, callback func(item T, key int) bool, def T) T {
	if callback == nil {
		if len(array) == 0 {
			return def
		}
		return array[len(array)-1]
	}
	for i := len(array) - 1; i >= 0; i-- {
		if callback(array[i], i) {
			return array[i]
		}
	}
	return def
}

// Take returns the first limit items, or the last abs(limit) items when the
// limit is negative. The result is always a fresh slice.
func Take[T any](array []T, limit int) []T {
	if limit < 0 {
		if -limit >= len(array) {
			return append([]T{}, array...)
		}
		return append([]T{}, array[len(array)+limit:]...)
	}
	if limit >= len(array) {
		return append([]T{}, array...)
	}
	return append([]T{}, array[:limit]...)
}

// Flatten squashes a multi-dimensional list into one level. A depth of 1
// flattens one level; a depth of zero or less flattens all the way down.
func Flatten(array []any, depth int) []any {
	result := []any{}
	for _, item := range array {
		nested, ok := item.([]any)
		if !ok {
			result = append(result, item)
			continue
		}
		if depth == 1 {
			result = append(result, nested...)
			continue
		}
		result = append(result, Flatten(nested, depth-1)...)
	}
	return result
}

// Every reports whether every item passes the truth test. An empty list is
// true.
func Every[T any](array []T, callback func(item T, key int) bool) bool {
	for i, item := range array {
		if !callback(item, i) {
			return false
		}
	}
	return true
}

// Some reports whether any item passes the truth test. An empty list is false.
func Some[T any](array []T, callback func(item T, key int) bool) bool {
	for i, item := range array {
		if callback(item, i) {
			return true
		}
	}
	return false
}

// Join returns the items joined by glue, with the last one joined by finalGlue
// instead when finalGlue is not empty.
func Join(array []string, glue, finalGlue string) string {
	if finalGlue == "" {
		return strings.Join(array, glue)
	}
	switch len(array) {
	case 0:
		return ""
	case 1:
		return array[0]
	}
	return strings.Join(array[:len(array)-1], glue) + finalGlue + array[len(array)-1]
}

// Map returns the callback run over every item, in order. The callback is
// given the item and its index.
func Map[T, U any](array []T, callback func(item T, key int) U) []U {
	results := make([]U, 0, len(array))
	for i, item := range array {
		results = append(results, callback(item, i))
	}
	return results
}

// MapWithKeys merges into one map the pairs the callback returns for each
// item. A key returned twice leaves the later value.
func MapWithKeys[T, V any](array []T, callback func(item T, key int) map[string]V) map[string]V {
	result := map[string]V{}
	for i, item := range array {
		for k, v := range callback(item, i) {
			result[k] = v
		}
	}
	return result
}

// MapSpread runs the callback over every nested chunk, passing the chunk's
// items followed by the chunk's index.
func MapSpread[T any](array [][]any, callback func(args ...any) T) []T {
	results := make([]T, 0, len(array))
	for i, chunk := range array {
		args := append(append([]any{}, chunk...), i)
		results = append(results, callback(args...))
	}
	return results
}

// Prepend returns a new list with the value pushed onto the front. To write
// into a map instead, use [Set]: a map has no front.
func Prepend[T any](array []T, v T) []T {
	return append([]T{v}, array...)
}

// Random returns the given number of items, picked at random and without
// repetition. Asking for more than the list holds is an error; asking for none
// or fewer gives an empty list.
func Random[T any](array []T, number int) ([]T, error) {
	if number > len(array) {
		return nil, &countError{requested: number, available: len(array)}
	}
	if len(array) == 0 || number <= 0 {
		return []T{}, nil
	}
	indexes := rand.Perm(len(array))[:number]
	results := make([]T, 0, number)
	for _, i := range indexes {
		results = append(results, array[i])
	}
	return results, nil
}

// Shuffle returns a shuffled copy of the list, leaving the original alone.
func Shuffle[T any](array []T) []T {
	result := append([]T{}, array...)
	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// Sole returns the one item that matches. Nothing matching is
// [ErrItemNotFound]; more than one is [ErrMultipleItemsFound]. A nil callback
// takes the whole list.
func Sole[T any](array []T, callback func(item T, key int) bool) (T, error) {
	var zero T
	matched := array
	if callback != nil {
		matched = Where(array, callback)
	}
	switch len(matched) {
	case 0:
		return zero, ErrItemNotFound
	case 1:
		return matched[0], nil
	default:
		return zero, ErrMultipleItemsFound
	}
}

// Sort returns a sorted copy of the list, leaving the original alone. The
// argument is the comparison itself, and the sort is stable.
func Sort[T any](array []T, compare func(a, b T) int) []T {
	result := append([]T{}, array...)
	sort.SliceStable(result, func(i, j int) bool {
		return compare(result[i], result[j]) < 0
	})
	return result
}

// SortDesc returns a sorted copy of the list, descending.
func SortDesc[T any](array []T, compare func(a, b T) int) []T {
	return Sort(array, func(a, b T) int { return -compare(a, b) })
}

// SortRecursive returns the list sorted by value, with every nested list
// sorted too. Numbers compare as numbers and everything else by its rendering.
// Nested maps are left as they are: a map has no order to sort.
func SortRecursive(array []any, descending bool) []any {
	result := make([]any, 0, len(array))
	for _, v := range array {
		if nested, ok := v.([]any); ok {
			result = append(result, SortRecursive(nested, descending))
			continue
		}
		result = append(result, v)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if descending {
			return compareValues(result[i], result[j]) > 0
		}
		return compareValues(result[i], result[j]) < 0
	})
	return result
}

// SortRecursiveDesc returns the list sorted descending by value, with every
// nested list sorted too.
func SortRecursiveDesc(array []any) []any {
	return SortRecursive(array, true)
}

// Where returns the items passing the truth test.
func Where[T any](array []T, callback func(item T, key int) bool) []T {
	results := []T{}
	for i, item := range array {
		if callback(item, i) {
			results = append(results, item)
		}
	}
	return results
}

// Reject returns the items failing the truth test.
func Reject[T any](array []T, callback func(item T, key int) bool) []T {
	return Where(array, func(item T, key int) bool { return !callback(item, key) })
}

// Partition returns the items that passed and the items that failed, in that
// order.
func Partition[T any](array []T, callback func(item T, key int) bool) (passed, failed []T) {
	passed, failed = []T{}, []T{}
	for i, item := range array {
		if callback(item, i) {
			passed = append(passed, item)
		} else {
			failed = append(failed, item)
		}
	}
	return passed, failed
}

// WhereNotNull returns the items that are not nil.
func WhereNotNull(array []any) []any {
	return Where(array, func(item any, _ int) bool { return item != nil })
}

// Wrap turns a value into a list: nil becomes the empty list, a slice or an
// array becomes a []any of its elements, and anything else is wrapped in a
// list of one.
func Wrap(v any) []any {
	if v == nil {
		return []any{}
	}
	if list, ok := v.([]any); ok {
		return list
	}
	rv := reflect.ValueOf(v)
	if k := rv.Kind(); k == reflect.Slice || k == reflect.Array {
		results := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			results = append(results, rv.Index(i).Interface())
		}
		return results
	}
	return []any{v}
}

// Pluck reads one value out of every item by dotted path. With an empty key
// the result is a []any in the items' order; with a key it is a
// map[string]any keyed by the value found at that path.
func Pluck(array []any, value, key string) any {
	if key == "" {
		results := make([]any, 0, len(array))
		for _, item := range array {
			results = append(results, dataGet(item, value))
		}
		return results
	}
	results := map[string]any{}
	for _, item := range array {
		results[toKey(dataGet(item, key))] = dataGet(item, value)
	}
	return results
}

// dataGet reads a dotted path out of an item, converting it with [From] first
// when it is not already a map. An empty path is the item itself, and a path
// that cannot be reached is nil.
func dataGet(item any, path string) any {
	if path == "" {
		return item
	}
	if m, ok := item.(map[string]any); ok {
		return Get(m, path, nil)
	}
	converted, err := From(item)
	if err != nil {
		return nil
	}
	if m, ok := converted.(map[string]any); ok {
		return Get(m, path, nil)
	}
	return nil
}

func sortedKeys(array map[string]any) []string {
	keys := make([]string, 0, len(array))
	for k := range array {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
