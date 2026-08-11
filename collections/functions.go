package collections

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

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

// Map answers to Illuminate\Support\Collection::map.
//
// It is a function and not a method because the callback changes the element
// type, and a Go method cannot declare a type parameter.
func Map[T, U any](c Collection[T], callback func(value T, key int) U) Collection[U] {
	out := make([]U, len(c))
	for i, v := range c {
		out[i] = callback(v, i)
	}
	return Collection[U](out)
}

// MapSpread answers to
// Illuminate\Support\Traits\EnumeratesValues::mapSpread: each nested chunk
// spread across the callback's arguments.
//
// The PHP appends the key to the chunk before spreading it -- $chunk[] = $key
// -- so the callback receives the chunk's elements and then the position. That
// is why the element type is []any and not []T: the trailing key is an int and
// the elements need not be.
func MapSpread[U any](c Collection[[]any], callback func(values ...any) U) Collection[U] {
	out := make([]U, len(c))
	for i, chunk := range c {
		out[i] = callback(spreadWithKey(chunk, i)...)
	}
	return Collection[U](out)
}

// EachSpread answers to
// Illuminate\Support\Traits\EnumeratesValues::eachSpread.
//
// As in MapSpread the position is appended to the chunk before it is spread.
// Returning false from the callback stops the walk, as it does in PHP.
func EachSpread(c Collection[[]any], callback func(values ...any) bool) Collection[[]any] {
	for i, chunk := range c {
		if !callback(spreadWithKey(chunk, i)...) {
			break
		}
	}
	return c
}

// spreadWithKey is the PHP's $chunk[] = $key, done on a copy so that the
// collection's own chunk is not grown by being spread.
func spreadWithKey(chunk []any, key int) []any {
	out := make([]any, 0, len(chunk)+1)
	out = append(out, chunk...)
	return append(out, key)
}

// MapInto answers to Illuminate\Support\Traits\EnumeratesValues::mapInto.
//
// PHP names a class and calls its constructor with the item; Go has no class
// name to pass, so the constructor itself is the argument.
func MapInto[T, U any](c Collection[T], into func(value T) U) Collection[U] {
	out := make([]U, len(c))
	for i, v := range c {
		out[i] = into(v)
	}
	return Collection[U](out)
}

// FlatMap answers to Illuminate\Support\Traits\EnumeratesValues::flatMap.
func FlatMap[T, U any](c Collection[T], callback func(value T, key int) []U) Collection[U] {
	out := make([]U, 0, len(c))
	for i, v := range c {
		out = append(out, callback(v, i)...)
	}
	return Collection[U](out)
}

// Pluck answers to Illuminate\Support\Collection::pluck.
//
// PHP names a field with a string; Go names it with an accessor. The optional
// $key argument of pluck($value, $key) is MapWithKeys here, because Go cannot
// infer the type of a key that was not passed.
func Pluck[T, V any](c Collection[T], value func(item T) V) Collection[V] {
	out := make([]V, len(c))
	for i, v := range c {
		out[i] = value(v)
	}
	return Collection[V](out)
}

// Value answers to Illuminate\Support\Traits\EnumeratesValues::value: the
// field of the first item. The second result is false on an empty collection,
// where PHP returns the default.
func Value[T, V any](c Collection[T], key func(item T) V) (V, bool) {
	if len(c) == 0 {
		var zero V
		return zero, false
	}
	return key(c[0]), true
}

// Reduce answers to Illuminate\Support\Traits\EnumeratesValues::reduce.
func Reduce[T, A any](c Collection[T], callback func(carry A, value T, key int) A, initial A) A {
	carry := initial
	for i, v := range c {
		carry = callback(carry, v, i)
	}
	return carry
}

// ReduceWithKeys answers to
// Illuminate\Support\Traits\EnumeratesValues::reduceWithKeys, which Illuminate
// defines as reduce under a name that says the key is passed.
func ReduceWithKeys[T, A any](c Collection[T], callback func(carry A, value T, key int) A, initial A) A {
	return Reduce(c, callback, initial)
}

// ReduceSpread answers to
// Illuminate\Support\Traits\EnumeratesValues::reduceSpread: a reduction that
// carries several accumulators at once.
//
// PHP throws UnexpectedValueException when the reducer returns something other
// than an array; the Go signature makes that unrepresentable except by
// returning a nil slice, which is reported as ErrUnexpectedValue.
func ReduceSpread[T, A any](c Collection[T], callback func(carry []A, value T, key int) []A, initial ...A) ([]A, error) {
	carry := append(make([]A, 0, len(initial)), initial...)
	for i, v := range c {
		carry = callback(carry, v, i)
		if carry == nil {
			return nil, fmt.Errorf("%w: reduceSpread expects the reducer to return a slice", ErrUnexpectedValue)
		}
	}
	return carry, nil
}

// Pipe answers to Illuminate\Support\Traits\EnumeratesValues::pipe.
func Pipe[T, U any](c Collection[T], callback func(collection Collection[T]) U) U {
	return callback(c)
}

// PipeInto answers to Illuminate\Support\Traits\EnumeratesValues::pipeInto.
//
// PHP names a class and calls its constructor with the collection; Go passes
// the constructor.
func PipeInto[T, U any](c Collection[T], into func(collection Collection[T]) U) U {
	return into(c)
}

// GroupBy answers to Illuminate\Support\Collection::groupBy.
//
// Within each group the items keep the order they had in the collection. PHP's
// $preserveKeys argument has no counterpart, because the keys of a
// Collection[T] are positions and a group renumbers them.
func GroupBy[T any, K comparable](c Collection[T], groupBy func(value T, key int) K) map[K]Collection[T] {
	out := make(map[K]Collection[T])
	for i, v := range c {
		k := groupBy(v, i)
		out[k] = append(out[k], v)
	}
	return out
}

// KeyBy answers to Illuminate\Support\Collection::keyBy.
//
// When two items share a key the later one wins, as it does in PHP.
func KeyBy[T any, K comparable](c Collection[T], keyBy func(value T, key int) K) map[K]T {
	out := make(map[K]T, len(c))
	for i, v := range c {
		out[keyBy(v, i)] = v
	}
	return out
}

// CountBy answers to Illuminate\Support\Collection::countBy.
func CountBy[T any, K comparable](c Collection[T], countBy func(value T, key int) K) map[K]int {
	out := make(map[K]int)
	for i, v := range c {
		out[countBy(v, i)]++
	}
	return out
}

// MapWithKeys answers to Illuminate\Support\Collection::mapWithKeys.
//
// PHP returns a single-entry array from the callback; Go returns the pair.
func MapWithKeys[T any, K comparable, V any](c Collection[T], callback func(value T, key int) (K, V)) map[K]V {
	out := make(map[K]V, len(c))
	for i, v := range c {
		k, val := callback(v, i)
		out[k] = val
	}
	return out
}

// MapToDictionary answers to
// Illuminate\Support\Collection::mapToDictionary: every key keeps all the
// values mapped onto it, in order.
func MapToDictionary[T any, K comparable, V any](c Collection[T], callback func(value T, key int) (K, V)) map[K][]V {
	out := make(map[K][]V)
	for i, v := range c {
		k, val := callback(v, i)
		out[k] = append(out[k], val)
	}
	return out
}

// MapToGroups answers to
// Illuminate\Support\Traits\EnumeratesValues::mapToGroups, which Illuminate
// defines as mapToDictionary with each bucket wrapped in a collection.
func MapToGroups[T any, K comparable, V any](c Collection[T], callback func(value T, key int) (K, V)) map[K]Collection[V] {
	out := make(map[K]Collection[V])
	for k, v := range MapToDictionary(c, callback) {
		out[k] = Collection[V](v)
	}
	return out
}

// Flip answers to Illuminate\Support\Collection::flip.
//
// The keys of a Collection[T] are positions, so flipping gives value to
// position. When a value repeats, the last position wins, as in PHP.
func Flip[T comparable](c Collection[T]) map[T]int {
	out := make(map[T]int, len(c))
	for i, v := range c {
		out[v] = i
	}
	return out
}

// Combine answers to Illuminate\Support\Collection::combine: this collection
// supplies the keys and values supplies the values.
//
// Pairing stops at the shorter of the two, where PHP raises a warning.
func Combine[K comparable, V any](keys Collection[K], values []V) map[K]V {
	n := min(len(keys), len(values))
	out := make(map[K]V, n)
	for i := 0; i < n; i++ {
		out[keys[i]] = values[i]
	}
	return out
}

// Unique answers to Illuminate\Support\Traits\EnumeratesValues::unique.
//
// The first item carrying a key is the one kept, and the order of the
// collection survives.
func Unique[T any, K comparable](c Collection[T], key func(value T, k int) K) Collection[T] {
	seen := make(map[K]struct{}, len(c))
	out := make([]T, 0, len(c))
	for i, v := range c {
		k := key(v, i)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	return Collection[T](out)
}

// UniqueStrict answers to
// Illuminate\Support\Traits\EnumeratesValues::uniqueStrict.
//
// Go's == is already PHP's ===, so this is Unique under the second name
// Illuminate gives it.
func UniqueStrict[T any, K comparable](c Collection[T], key func(value T, k int) K) Collection[T] {
	return Unique(c, key)
}

// Duplicates answers to Illuminate\Support\Collection::duplicates: the keys
// that appear more than once, in the order their repeats appear.
func Duplicates[T any, K comparable](c Collection[T], key func(value T, k int) K) Collection[K] {
	seen := make(map[K]struct{}, len(c))
	out := make([]K, 0)
	for i, v := range c {
		k := key(v, i)
		if _, ok := seen[k]; ok {
			out = append(out, k)
			continue
		}
		seen[k] = struct{}{}
	}
	return Collection[K](out)
}

// DuplicatesStrict answers to
// Illuminate\Support\Collection::duplicatesStrict. Go's == is already PHP's
// ===, so this is Duplicates under Illuminate's second name.
func DuplicatesStrict[T any, K comparable](c Collection[T], key func(value T, k int) K) Collection[K] {
	return Duplicates(c, key)
}

// Sum answers to Illuminate\Support\Traits\EnumeratesValues::sum.
//
// It returns the zero of N on an empty collection, as PHP returns 0. There is
// no form without the projection: over a collection of numbers, pass a function
// that returns its argument.
func Sum[T any, N Number](c Collection[T], value func(item T) N) N {
	var total N
	for _, v := range c {
		total += value(v)
	}
	return total
}

// Avg answers to Illuminate\Support\Traits\EnumeratesValues::avg.
//
// The second result is false on an empty collection, where PHP returns null.
func Avg[T any, N Number](c Collection[T], value func(item T) N) (float64, bool) {
	if len(c) == 0 {
		return 0, false
	}
	return float64(Sum(c, value)) / float64(len(c)), true
}

// Average answers to Illuminate\Support\Traits\EnumeratesValues::average,
// which Illuminate defines as an alias of avg.
func Average[T any, N Number](c Collection[T], value func(item T) N) (float64, bool) {
	return Avg(c, value)
}

// Median answers to Illuminate\Support\Collection::median.
//
// With an even count it averages the two middle values, as PHP does. The second
// result is false on an empty collection, where PHP returns null.
func Median[T any, N Number](c Collection[T], value func(item T) N) (float64, bool) {
	if len(c) == 0 {
		return 0, false
	}
	values := make([]float64, len(c))
	for i, v := range c {
		values[i] = float64(value(v))
	}
	stableSort(values, cmp.Compare)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle], true
	}
	return (values[middle-1] + values[middle]) / 2, true
}

// Mode answers to Illuminate\Support\Collection::mode: the values that occur
// most often, sorted ascending.
//
// The result is nil on an empty collection, where PHP returns null.
func Mode[T any, N cmp.Ordered](c Collection[T], value func(item T) N) []N {
	if len(c) == 0 {
		return nil
	}
	counts := make(map[N]int, len(c))
	for _, v := range c {
		counts[value(v)]++
	}
	highest := 0
	for _, n := range counts {
		highest = max(highest, n)
	}
	out := make([]N, 0, len(counts))
	for k, n := range counts {
		if n == highest {
			out = append(out, k)
		}
	}
	stableSort(out, cmp.Compare)
	return out
}

// Min answers to Illuminate\Support\Traits\EnumeratesValues::min.
//
// The second result is false on an empty collection, where PHP returns null.
func Min[T any, V cmp.Ordered](c Collection[T], value func(item T) V) (V, bool) {
	var lowest V
	if len(c) == 0 {
		return lowest, false
	}
	lowest = value(c[0])
	for _, v := range c[1:] {
		lowest = min(lowest, value(v))
	}
	return lowest, true
}

// Max answers to Illuminate\Support\Traits\EnumeratesValues::max.
func Max[T any, V cmp.Ordered](c Collection[T], value func(item T) V) (V, bool) {
	var highest V
	if len(c) == 0 {
		return highest, false
	}
	highest = value(c[0])
	for _, v := range c[1:] {
		highest = max(highest, value(v))
	}
	return highest, true
}

// SortBy answers to Illuminate\Support\Collection::sortBy. The sort is stable,
// as PHP's is.
func SortBy[T any, V cmp.Ordered](c Collection[T], callback func(value T, key int) V) Collection[T] {
	type keyed struct {
		item T
		key  V
	}
	pairs := make([]keyed, len(c))
	for i, v := range c {
		pairs[i] = keyed{item: v, key: callback(v, i)}
	}
	stableSort(pairs, func(a, b keyed) int { return cmp.Compare(a.key, b.key) })
	out := make([]T, len(pairs))
	for i, p := range pairs {
		out[i] = p.item
	}
	return Collection[T](out)
}

// SortByDesc answers to Illuminate\Support\Collection::sortByDesc.
func SortByDesc[T any, V cmp.Ordered](c Collection[T], callback func(value T, key int) V) Collection[T] {
	return SortBy(c, callback).Reverse()
}

// Where answers to Illuminate\Support\Traits\EnumeratesValues::where.
//
// The operator is one of "=", "==", "===", "!=", "<>", "!==", ">", ">=", "<",
// "<=", the same set PHP accepts. PHP's two-argument form where($key, $value)
// is this with "=". An unknown operator matches nothing, as in PHP.
func Where[T any, V cmp.Ordered](c Collection[T], key func(item T) V, operator string, value V) Collection[T] {
	return c.Filter(func(item T, _ int) bool {
		return compareOperator(key(item), operator, value)
	})
}

// FirstWhere answers to
// Illuminate\Support\Traits\EnumeratesValues::firstWhere.
func FirstWhere[T any, V cmp.Ordered](c Collection[T], key func(item T) V, operator string, value V) (T, bool) {
	return Where(c, key, operator, value).First(nil)
}

// WhereStrict answers to
// Illuminate\Support\Traits\EnumeratesValues::whereStrict.
//
// Go's == is already PHP's ===, so no operator is taken.
func WhereStrict[T any, V comparable](c Collection[T], key func(item T) V, value V) Collection[T] {
	return c.Filter(func(item T, _ int) bool { return key(item) == value })
}

// WhereIn answers to Illuminate\Support\Traits\EnumeratesValues::whereIn.
func WhereIn[T any, V comparable](c Collection[T], key func(item T) V, values []V) Collection[T] {
	want := make(map[V]struct{}, len(values))
	for _, v := range values {
		want[v] = struct{}{}
	}
	return c.Filter(func(item T, _ int) bool {
		_, ok := want[key(item)]
		return ok
	})
}

// WhereInStrict answers to
// Illuminate\Support\Traits\EnumeratesValues::whereInStrict. Go's == is
// already PHP's ===, so this is WhereIn under Illuminate's second name.
func WhereInStrict[T any, V comparable](c Collection[T], key func(item T) V, values []V) Collection[T] {
	return WhereIn(c, key, values)
}

// WhereNotIn answers to
// Illuminate\Support\Traits\EnumeratesValues::whereNotIn.
func WhereNotIn[T any, V comparable](c Collection[T], key func(item T) V, values []V) Collection[T] {
	want := make(map[V]struct{}, len(values))
	for _, v := range values {
		want[v] = struct{}{}
	}
	return c.Filter(func(item T, _ int) bool {
		_, ok := want[key(item)]
		return !ok
	})
}

// WhereNotInStrict answers to
// Illuminate\Support\Traits\EnumeratesValues::whereNotInStrict.
func WhereNotInStrict[T any, V comparable](c Collection[T], key func(item T) V, values []V) Collection[T] {
	return WhereNotIn(c, key, values)
}

// WhereBetween answers to
// Illuminate\Support\Traits\EnumeratesValues::whereBetween. Both ends are
// included, as in PHP.
func WhereBetween[T any, V cmp.Ordered](c Collection[T], key func(item T) V, from, to V) Collection[T] {
	return c.Filter(func(item T, _ int) bool {
		v := key(item)
		return v >= from && v <= to
	})
}

// WhereNotBetween answers to
// Illuminate\Support\Traits\EnumeratesValues::whereNotBetween.
func WhereNotBetween[T any, V cmp.Ordered](c Collection[T], key func(item T) V, from, to V) Collection[T] {
	return c.Filter(func(item T, _ int) bool {
		v := key(item)
		return v < from || v > to
	})
}

// WhereNull answers to
// Illuminate\Support\Traits\EnumeratesValues::whereNull.
//
// PHP's null on a field is Go's nil pointer, so the accessor returns a pointer.
func WhereNull[T any, V any](c Collection[T], key func(item T) *V) Collection[T] {
	return c.Filter(func(item T, _ int) bool { return key(item) == nil })
}

// WhereNotNull answers to
// Illuminate\Support\Traits\EnumeratesValues::whereNotNull.
func WhereNotNull[T any, V any](c Collection[T], key func(item T) *V) Collection[T] {
	return c.Filter(func(item T, _ int) bool { return key(item) != nil })
}

// WhereInstanceOf answers to
// Illuminate\Support\Traits\EnumeratesValues::whereInstanceOf: the items whose
// dynamic type is U.
func WhereInstanceOf[T, U any](c Collection[T]) Collection[T] {
	return c.Filter(func(item T, _ int) bool {
		_, ok := any(item).(U)
		return ok
	})
}

// Ensure answers to Illuminate\Support\Traits\EnumeratesValues::ensure: it
// returns the collection unchanged when every item is a U, and an error naming
// the first item that is not, where PHP throws UnexpectedValueException.
func Ensure[T, U any](c Collection[T]) (Collection[T], error) {
	for i, v := range c {
		if _, ok := any(v).(U); !ok {
			var want U
			return c, fmt.Errorf("%w: item at %d is %T, expected %T", ErrUnexpectedValue, i, v, want)
		}
	}
	return c, nil
}

// ContainsStrict answers to
// Illuminate\Support\Collection::containsStrict, and to the form of contains
// that takes a bare value: Go's == is PHP's ===.
func ContainsStrict[T comparable](c Collection[T], value T) bool {
	for _, v := range c {
		if v == value {
			return true
		}
	}
	return false
}

// DoesntContainStrict answers to
// Illuminate\Support\Collection::doesntContainStrict.
func DoesntContainStrict[T comparable](c Collection[T], value T) bool {
	return !ContainsStrict(c, value)
}

// Search answers to Illuminate\Support\Collection::search: the key of the
// first item equal to value. The second result is false where PHP returns
// false.
func Search[T comparable](c Collection[T], value T) (int, bool) {
	for i, v := range c {
		if v == value {
			return i, true
		}
	}
	return 0, false
}

// Before answers to Illuminate\Support\Collection::before.
//
// The second result is false when the value is absent or is the first item,
// where PHP returns null.
func Before[T comparable](c Collection[T], value T) (T, bool) {
	i, ok := Search(c, value)
	if !ok || i == 0 {
		var zero T
		return zero, false
	}
	return c[i-1], true
}

// After answers to Illuminate\Support\Collection::after.
func After[T comparable](c Collection[T], value T) (T, bool) {
	i, ok := Search(c, value)
	if !ok || i == len(c)-1 {
		var zero T
		return zero, false
	}
	return c[i+1], true
}

// Diff answers to Illuminate\Support\Collection::diff: the items not present
// in items.
func Diff[T comparable](c Collection[T], items []T) Collection[T] {
	drop := make(map[T]struct{}, len(items))
	for _, v := range items {
		drop[v] = struct{}{}
	}
	return c.Filter(func(v T, _ int) bool {
		_, ok := drop[v]
		return !ok
	})
}

// DiffUsing answers to Illuminate\Support\Collection::diffUsing, which takes
// the comparison rather than relying on the default one.
func DiffUsing[T any](c Collection[T], items []T, compare func(a, b T) int) Collection[T] {
	return c.Filter(func(v T, _ int) bool {
		for _, other := range items {
			if compare(v, other) == 0 {
				return false
			}
		}
		return true
	})
}

// Intersect answers to Illuminate\Support\Collection::intersect: the items
// also present in items.
func Intersect[T comparable](c Collection[T], items []T) Collection[T] {
	keep := make(map[T]struct{}, len(items))
	for _, v := range items {
		keep[v] = struct{}{}
	}
	return c.Filter(func(v T, _ int) bool {
		_, ok := keep[v]
		return ok
	})
}

// IntersectUsing answers to
// Illuminate\Support\Collection::intersectUsing.
func IntersectUsing[T any](c Collection[T], items []T, compare func(a, b T) int) Collection[T] {
	return c.Filter(func(v T, _ int) bool {
		for _, other := range items {
			if compare(v, other) == 0 {
				return true
			}
		}
		return false
	})
}

// Collapse answers to Illuminate\Support\Collection::collapse.
func Collapse[T any](c Collection[Collection[T]]) Collection[T] {
	out := make([]T, 0, len(c))
	for _, inner := range c {
		out = append(out, inner...)
	}
	return Collection[T](out)
}

// Flatten answers to Illuminate\Support\Collection::flatten.
//
// The depth is optional and unlimited when omitted, as in PHP. A nested value
// is a []any or a Collection[any]; anything else is a leaf.
func Flatten(c Collection[any], depth ...int) Collection[any] {
	limit := -1
	if len(depth) > 0 {
		limit = depth[0]
	}
	return Collection[any](flattenInto(make([]any, 0, len(c)), c, limit))
}

func flattenInto(out []any, items []any, depth int) []any {
	for _, item := range items {
		var nested []any
		switch v := item.(type) {
		case []any:
			nested = v
		case Collection[any]:
			nested = v
		default:
			out = append(out, item)
			continue
		}
		if depth == 0 {
			out = append(out, nested...)
			continue
		}
		out = flattenInto(out, nested, depth-1)
	}
	return out
}

// Select answers to Illuminate\Support\Collection::select: each item reduced
// to the named keys.
func Select[T any](c Collection[map[string]T], keys ...string) Collection[map[string]T] {
	out := make([]map[string]T, len(c))
	for i, item := range c {
		row := make(map[string]T, len(keys))
		for _, k := range keys {
			if v, ok := item[k]; ok {
				row[k] = v
			}
		}
		out[i] = row
	}
	return Collection[map[string]T](out)
}

// Dot answers to Illuminate\Support\Collection::dot: the collection flattened
// into single-level "dot" keys, the keys being the positions.
func Dot(c Collection[any]) map[string]any {
	out := make(map[string]any, len(c))
	for i, v := range c {
		dotInto(out, strconv.Itoa(i), v)
	}
	return out
}

func dotInto(out map[string]any, prefix string, value any) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			out[prefix] = value
			return
		}
		for k, inner := range v {
			dotInto(out, prefix+"."+k, inner)
		}
	case []any:
		if len(v) == 0 {
			out[prefix] = value
			return
		}
		for i, inner := range v {
			dotInto(out, prefix+"."+strconv.Itoa(i), inner)
		}
	case Collection[any]:
		dotInto(out, prefix, v)
	default:
		out[prefix] = value
	}
}

// Undot answers to Illuminate\Support\Collection::undot: "dot" keys expanded
// back into nested maps.
func Undot(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		setDotted(out, k, v)
	}
	return out
}

func setDotted(m map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[part] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = value
}

// compareOperator applies one of PHP's comparison operators. An operator the
// list does not name matches nothing, which is what operatorForWhere does.
func compareOperator[V cmp.Ordered](left V, operator string, right V) bool {
	switch operator {
	case "=", "==", "===":
		return left == right
	case "!=", "<>", "!==":
		return left != right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	default:
		return false
	}
}
