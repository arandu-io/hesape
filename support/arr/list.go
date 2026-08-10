package arr

import (
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
)

// First answers to Arr::first: the first item passing the truth test, or the
// default when nothing passes. A nil callback is PHP's null callback and takes
// the first item of all.
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

// Last answers to Arr::last: the last item passing the truth test, or the
// default when nothing passes. A nil callback takes the last item of all.
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

// Take answers to Arr::take: the first limit items, or the last abs(limit)
// items when the limit is negative.
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

// Flatten answers to Arr::flatten: a multi-dimensional list squashed into one
// level. A depth of 1 flattens one level; a depth of zero or less is PHP's
// INF default and flattens all the way down.
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

// Every answers to Arr::every: whether every item passes the truth test.
func Every[T any](array []T, callback func(item T, key int) bool) bool {
	for i, item := range array {
		if !callback(item, i) {
			return false
		}
	}
	return true
}

// Some answers to Arr::some: whether any item passes the truth test.
func Some[T any](array []T, callback func(item T, key int) bool) bool {
	for i, item := range array {
		if callback(item, i) {
			return true
		}
	}
	return false
}

// Join answers to Arr::join: the items joined by glue, with the last one
// joined by finalGlue when finalGlue is not empty.
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

// Map answers to Arr::map: the callback run over every item, keys kept.
func Map[T, U any](array []T, callback func(item T, key int) U) []U {
	results := make([]U, 0, len(array))
	for i, item := range array {
		results = append(results, callback(item, i))
	}
	return results
}

// MapWithKeys answers to Arr::mapWithKeys: the callback returns the key and
// the value of each item, and the pairs are merged into one map.
func MapWithKeys[T, V any](array []T, callback func(item T, key int) map[string]V) map[string]V {
	result := map[string]V{}
	for i, item := range array {
		for k, v := range callback(item, i) {
			result[k] = v
		}
	}
	return result
}

// MapSpread answers to Arr::mapSpread: the callback run over every nested
// chunk, receiving the chunk's items followed by the chunk's key, as the PHP
// spread does.
func MapSpread[T any](array [][]any, callback func(args ...any) T) []T {
	results := make([]T, 0, len(array))
	for i, chunk := range array {
		args := append(append([]any{}, chunk...), i)
		results = append(results, callback(args...))
	}
	return results
}

// Prepend answers to Arr::prepend: the value pushed onto the front.
//
// The PHP third argument prepends under a key instead, which only means
// anything because a PHP array remembers its order. A Go map does not, so
// prepending to one is [Set].
func Prepend[T any](array []T, v T) []T {
	return append([]T{v}, array...)
}

// Random answers to Arr::random: the given number of items, picked at random.
// Asking for more than the array holds is the error the PHP throws as
// InvalidArgumentException.
//
// The PHP null count returns a bare item rather than an array of one; in Go
// that is Random(array, 1) and its single element.
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

// Shuffle answers to Arr::shuffle: a shuffled copy of the list.
func Shuffle[T any](array []T) []T {
	result := append([]T{}, array...)
	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// Sole answers to Arr::sole: the one item that matches. Nothing matching is
// [ErrItemNotFound]; more than one is [ErrMultipleItemsFound]. A nil callback
// takes the whole list, as PHP's null callback does.
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

// Sort answers to Arr::sort: a sorted copy of the list.
//
// The PHP argument is a sort-key callback or a dotted field name resolved
// through data_get; the Go argument is the comparison itself, which is the one
// spelling a typed language needs.
func Sort[T any](array []T, compare func(a, b T) int) []T {
	result := append([]T{}, array...)
	sort.SliceStable(result, func(i, j int) bool {
		return compare(result[i], result[j]) < 0
	})
	return result
}

// SortDesc answers to Arr::sortDesc: a sorted copy of the list, descending.
func SortDesc[T any](array []T, compare func(a, b T) int) []T {
	return Sort(array, func(a, b T) int { return -compare(a, b) })
}

// SortRecursive answers to Arr::sortRecursive: the list sorted by value, and
// every nested list sorted too.
//
// The PHP sort flags choose a comparison mode; Go compares numbers as numbers
// and strings as strings, which is what SORT_REGULAR asks for, so there is no
// flag to pass. Nested maps keep their contents: a Go map has no order to sort.
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

// SortRecursiveDesc answers to Arr::sortRecursiveDesc.
func SortRecursiveDesc(array []any) []any {
	return SortRecursive(array, true)
}

// Where answers to Arr::where: the items passing the truth test.
func Where[T any](array []T, callback func(item T, key int) bool) []T {
	results := []T{}
	for i, item := range array {
		if callback(item, i) {
			results = append(results, item)
		}
	}
	return results
}

// Reject answers to Arr::reject: the items failing the truth test.
func Reject[T any](array []T, callback func(item T, key int) bool) []T {
	return Where(array, func(item T, key int) bool { return !callback(item, key) })
}

// Partition answers to Arr::partition: the items that passed and the items
// that failed, in that order.
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

// WhereNotNull answers to Arr::whereNotNull: the items that are not nil.
func WhereNotNull(array []any) []any {
	return Where(array, func(item any, _ int) bool { return item != nil })
}

// Wrap answers to Arr::wrap: nil becomes the empty list, a list stays as it is,
// and anything else is wrapped in a list of one.
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

// Pluck answers to Arr::pluck: one value read out of every item by dotted
// path. With an empty key the result is a []any in the items' order; with a
// key it is a map[string]any keyed by that path, which is the PHP's own pair
// of return shapes.
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

// dataGet is the data_get() the PHP body leans on, narrowed to the shapes a Go
// value actually takes here.
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
