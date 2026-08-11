package arr

import (
	"cmp"
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/collections"
)

// Collapse answers to Arr::collapse: one level of nesting removed.
//
// The PHP keeps only the elements that are arrays or Collections and drops
// everything else before merging, and so does this. The typed form, over a
// slice of slices, is collections.Collapse.
func Collapse(array []any) []any {
	out := []any{}
	for _, values := range array {
		nested, ok := elements(values)
		if !ok {
			continue
		}
		out = append(out, nested...)
	}
	return out
}

// CrossJoin answers to Arr::crossJoin: the cartesian product of the lists, one
// tuple per combination.
//
// The PHP folds from [[]], so crossing nothing gives one empty tuple, crossing
// a single list wraps each of its elements on its own, and crossing an empty
// list yields nothing at all. All three hold here.
func CrossJoin[T any](arrays ...[]T) [][]T {
	results := [][]T{{}}
	for _, array := range arrays {
		next := make([][]T, 0, len(results)*len(array))
		for _, product := range results {
			for _, item := range array {
				combined := make([]T, len(product), len(product)+1)
				copy(combined, product)
				next = append(next, append(combined, item))
			}
		}
		results = next
	}
	return results
}

// First answers to Arr::first: the first element passing the test.
//
// A nil callback returns the first element, which is what the PHP does with no
// argument. The second result is false where the PHP returns its $default: the
// slice is empty, or nothing matched.
//
// helpers.php's head() is this with a nil callback.
func First[T any](array []T, callback func(value T, key int) bool) (T, bool) {
	for i, v := range array {
		if callback == nil || callback(v, i) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// Last answers to Arr::last: the last element passing the test.
//
// The PHP reverses the array and calls first, so the callback still sees the
// original keys; this walks backwards, which is the same thing without the
// copy.
//
// helpers.php's last() is this with a nil callback.
func Last[T any](array []T, callback func(value T, key int) bool) (T, bool) {
	for i := len(array) - 1; i >= 0; i-- {
		if callback == nil || callback(array[i], i) {
			return array[i], true
		}
	}
	var zero T
	return zero, false
}

// Take answers to Arr::take: the first limit elements, or the last ones when
// limit is negative.
//
// A limit of zero gives nothing, and a limit larger than the slice gives the
// whole slice, which is what array_slice does.
func Take[T any](array []T, limit int) []T {
	if limit < 0 {
		if -limit >= len(array) {
			return append([]T{}, array...)
		}
		return append([]T{}, array[len(array)+limit:]...)
	}
	if limit > len(array) {
		limit = len(array)
	}
	return append([]T{}, array[:limit]...)
}

// Flatten answers to Arr::flatten: a nested structure squashed into one level.
//
// The depth is optional and unlimited when omitted, as the PHP's INF default
// is. A depth of 1 removes one level of nesting. Only the first depth is used;
// the variadic is how Go spells a PHP default argument.
//
// A nested map contributes its values in ascending key order, because
// array_values on an associative array takes the values in the order the array
// holds them and a Go map holds none.
func Flatten(array []any, depth ...int) []any {
	limit := -1
	if len(depth) > 0 {
		limit = depth[0]
	}
	return flattenInto([]any{}, array, limit)
}

func flattenInto(out []any, array []any, depth int) []any {
	for _, item := range array {
		nested, ok := elements(item)
		if !ok {
			out = append(out, item)
			continue
		}
		if depth == 1 {
			out = append(out, nested...)
			continue
		}
		out = flattenInto(out, nested, depth-1)
	}
	return out
}

// Every answers to Arr::every: every element passes the test.
//
// An empty slice passes, as PHP's array_all does: there is no element to fail.
func Every[T any](array []T, callback func(value T, key int) bool) bool {
	for i, v := range array {
		if !callback(v, i) {
			return false
		}
	}
	return true
}

// Some answers to Arr::some: at least one element passes the test.
func Some[T any](array []T, callback func(value T, key int) bool) bool {
	for i, v := range array {
		if callback(v, i) {
			return true
		}
	}
	return false
}

// Join answers to Arr::join: the elements glued together, the last one attached
// with finalGlue.
//
// An empty finalGlue is a plain implode, one element is that element and no
// element is the empty string -- the three cases the PHP spells out. The PHP's
// finalGlue default of "" is passed explicitly here.
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

// KeyBy answers to Arr::keyBy: the elements keyed by what the callback reads
// off each one.
//
// The PHP takes a field name or a callback and routes both through
// Collection::keyBy; Go names the field with an accessor. When two elements
// share a key the later one wins, as it does in PHP.
func KeyBy[T any](array []T, keyBy func(value T, key int) string) map[string]T {
	out := make(map[string]T, len(array))
	for i, v := range array {
		out[keyBy(v, i)] = v
	}
	return out
}

// Map answers to Arr::map.
//
// The PHP passes ($value, $key) and falls back to ($value) when the callback
// takes one argument; a Go callback declares what it wants, so both are the
// same signature here.
func Map[T, U any](array []T, callback func(value T, key int) U) []U {
	out := make([]U, len(array))
	for i, v := range array {
		out[i] = callback(v, i)
	}
	return out
}

// MapWithKeys answers to Arr::mapWithKeys.
//
// The PHP callback returns an associative array and every pair in it is folded
// into the result; a Go callback returns the one pair, which is the shape every
// call in Illuminate itself uses.
func MapWithKeys[T, V any](array []T, callback func(value T, key int) (string, V)) map[string]V {
	out := make(map[string]V, len(array))
	for i, v := range array {
		key, value := callback(v, i)
		out[key] = value
	}
	return out
}

// MapSpread answers to Arr::mapSpread: each nested chunk spread across the
// callback's arguments.
//
// The PHP appends the key to the chunk before spreading it, so the callback
// receives the chunk's elements and then the key. That is easy to miss reading
// the signature and it is why this takes [][]any rather than a typed chunk: the
// trailing key is an int and the elements are not.
func MapSpread[U any](array [][]any, callback func(values ...any) U) []U {
	out := make([]U, len(array))
	for i, chunk := range array {
		spread := make([]any, 0, len(chunk)+1)
		spread = append(spread, chunk...)
		spread = append(spread, i)
		out[i] = callback(spread...)
	}
	return out
}

// Prepend answers to Arr::prepend: the value put at the front.
//
// The PHP has a three-argument form, prepend($array, $value, $key), which
// writes the pair in front of an associative array. A Go map has no order for
// that to be visible in, so m[key] = value is the whole of it; the list form is
// this one.
func Prepend[T any](array []T, value T) []T {
	return append(append(make([]T, 0, len(array)+1), value), array...)
}

// Pluck answers to Arr::pluck: one field read off every element.
//
// The PHP returns a list when no $key is given and an array keyed by that field
// when one is, so its return type is mixed. Go says mixed with any: the result
// is a []any with no key, and a map[string]any with one. Both paths resolve
// their argument with DataGet, exactly as the PHP resolves it with data_get.
func Pluck(array []any, value string, key ...string) any {
	if len(key) == 0 {
		out := make([]any, 0, len(array))
		for _, item := range array {
			resolved, _ := DataGet(item, value)
			out = append(out, resolved)
		}
		return out
	}
	out := make(map[string]any, len(array))
	for _, item := range array {
		resolved, _ := DataGet(item, value)
		itemKey, _ := DataGet(item, key[0])
		out[keyString(itemKey)] = resolved
	}
	return out
}

// Select answers to Arr::select: each element reduced to the named keys.
//
// The PHP reads a key off an array element and a property off an object one.
// The Go counterpart of the property is an exported struct field of the same
// name, which is what From uses too. A key an element does not have is left
// out rather than filled with nil.
func Select(array []any, keys ...string) []map[string]any {
	out := make([]map[string]any, len(array))
	for i, item := range array {
		row := make(map[string]any, len(keys))
		for _, key := range keys {
			if value, ok := index(item, key); ok {
				row[key] = value
				continue
			}
			if value, ok := field(item, key); ok {
				row[key] = value
			}
		}
		out[i] = row
	}
	return out
}

// field reads an exported struct field by name, following a pointer. It stands
// in for PHP's isset($item->{$key}).
func field(item any, name string) (any, bool) {
	if item == nil {
		return nil, false
	}
	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, false
	}
	found := rv.FieldByName(name)
	if !found.IsValid() || !found.CanInterface() {
		return nil, false
	}
	return found.Interface(), true
}

// Random answers to Arr::random: number elements picked at random.
//
// It returns ErrInvalidArgument when more elements are asked for than there
// are, which is what the PHP throws. A number of zero or less gives an empty
// slice rather than an error, because the PHP returns [] there without looking
// at the count.
//
// The draw is from crypto/rand: the core carries no other source, and a shuffle
// seeded from the clock is a shuffle an attacker can replay.
func Random[T any](array []T, number int) ([]T, error) {
	if number > len(array) {
		return nil, fmt.Errorf("%w: you requested %d items, but there are only %d items available", ErrInvalidArgument, number, len(array))
	}
	if number <= 0 || len(array) == 0 {
		return []T{}, nil
	}
	return Shuffle(array)[:number], nil
}

// Shuffle answers to Arr::shuffle: the elements in a random order.
//
// The PHP renumbers the keys, so the result is a list; the Go result is a new
// slice and the receiver is untouched.
func Shuffle[T any](array []T) []T {
	out := append(make([]T, 0, len(array)), array...)
	for i := len(out) - 1; i > 0; i-- {
		drawn, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return out
		}
		j := int(drawn.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Sole answers to Arr::sole: the one element passing the test.
//
// It returns ErrItemNotFound where the PHP throws ItemNotFoundException and a
// collections.MultipleItemsFoundError, which carries the count and unwraps to
// ErrMultipleItemsFound, where it throws MultipleItemsFoundException.
func Sole[T any](array []T, callback func(value T, key int) bool) (T, error) {
	matched := array
	if callback != nil {
		matched = Where(array, callback)
	}
	var zero T
	switch len(matched) {
	case 0:
		return zero, ErrItemNotFound
	case 1:
		return matched[0], nil
	default:
		return zero, &collections.MultipleItemsFoundError{Count: len(matched)}
	}
}

// Sort answers to Arr::sort.
//
// The PHP routes to Collection::sortBy, so its callback reads the value to
// order by and is not a comparator -- Collection::sort is the one that takes a
// comparator. The projection is required here because the PHP's null callback
// orders by the values themselves and Go cannot compare an arbitrary T.
//
// The PHP keeps the original keys attached to the sorted values, so sorting the
// list [3,1,2] leaves an array keyed 1,2,0. A Go slice renumbers.
//
// The sort is stable, as PHP's is since 8.0.
func Sort[T any, V cmp.Ordered](array []T, callback func(value T, key int) V) []T {
	type keyed struct {
		item T
		key  V
	}
	pairs := make([]keyed, len(array))
	for i, v := range array {
		pairs[i] = keyed{item: v, key: callback(v, i)}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return cmp.Less(pairs[i].key, pairs[j].key) })
	out := make([]T, len(pairs))
	for i, pair := range pairs {
		out[i] = pair.item
	}
	return out
}

// SortDesc answers to Arr::sortDesc: Sort with the order reversed.
func SortDesc[T any, V cmp.Ordered](array []T, callback func(value T, key int) V) []T {
	sorted := Sort(array, callback)
	for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}
	return sorted
}

// Where answers to Arr::where: the elements passing the test.
//
// The PHP's array_filter keeps the keys, which on a list leaves gaps; a Go
// slice closes them.
func Where[T any](array []T, callback func(value T, key int) bool) []T {
	out := make([]T, 0, len(array))
	for i, v := range array {
		if callback(v, i) {
			out = append(out, v)
		}
	}
	return out
}

// Reject answers to Arr::reject: Where with the test negated.
func Reject[T any](array []T, callback func(value T, key int) bool) []T {
	return Where(array, func(value T, key int) bool { return !callback(value, key) })
}

// Partition answers to Arr::partition: the elements passing the test, then the
// ones failing it.
//
// The PHP returns one array holding two; Go returns them as two results, which
// is the same pair without the indexing. Neither half is nil.
func Partition[T any](array []T, callback func(value T, key int) bool) (passed, failed []T) {
	passed = make([]T, 0, len(array))
	failed = make([]T, 0, len(array))
	for i, v := range array {
		if callback(v, i) {
			passed = append(passed, v)
			continue
		}
		failed = append(failed, v)
	}
	return passed, failed
}

// WhereNotNull answers to Arr::whereNotNull: the elements that are not nil.
func WhereNotNull(array []any) []any {
	return Where(array, func(value any, _ int) bool { return value != nil })
}

// ExceptValues answers to Arr::exceptValues: everything but the values given.
//
// The PHP has a $strict flag choosing between == and ===; Go's == is already
// ===, so there is no flag. Its array_filter keeps the keys and leaves gaps in
// a list; a Go slice closes them.
func ExceptValues[T comparable](array []T, values ...T) []T {
	unwanted := make(map[T]struct{}, len(values))
	for _, value := range values {
		unwanted[value] = struct{}{}
	}
	return Where(array, func(value T, _ int) bool {
		_, found := unwanted[value]
		return !found
	})
}

// OnlyValues answers to Arr::onlyValues: only the values given.
func OnlyValues[T comparable](array []T, values ...T) []T {
	wanted := make(map[T]struct{}, len(values))
	for _, value := range values {
		wanted[value] = struct{}{}
	}
	return Where(array, func(value T, _ int) bool {
		_, found := wanted[value]
		return found
	})
}

// Wrap answers to Arr::wrap: nil becomes an empty list, an array stays as it
// is, and anything else is put in a list on its own.
//
// A slice of another element type is an array in PHP's sense too, so it is
// converted rather than wrapped. A map is not: PHP would hand it back
// unchanged, and a []any cannot carry its keys, so it is wrapped whole, which
// keeps it reachable.
func Wrap(value any) []any {
	if value == nil {
		return []any{}
	}
	if list, ok := value.([]any); ok {
		return list
	}
	if rv, ok := container(value); ok && rv.Kind() != reflect.Map {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	}
	return []any{value}
}
