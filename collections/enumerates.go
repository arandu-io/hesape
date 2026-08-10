package collections

import (
	"cmp"
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
)

// stableSort sorts s in place with compare, keeping equal elements in the order
// they had. Illuminate sorts with uasort, which is stable since PHP 8.0, so a
// sort that reordered ties would not answer to the same method.
func stableSort[T any](s []T, compare func(a, b T) int) {
	sort.SliceStable(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
}

// Filter answers to Illuminate\Support\Collection::filter.
//
// The PHP takes an optional callback and drops the falsy values when it is
// absent; Go has no falsy, so the callback is required. Pass a nil callback to
// get the collection back unchanged, which is the closest thing to "keep
// everything" a typed language can offer.
//
// The result is empty rather than nil when nothing passes.
func (c Collection[T]) Filter(callback func(value T, key int) bool) Collection[T] {
	if callback == nil {
		return c.Values()
	}
	out := make(Collection[T], 0, len(c))
	for i, v := range c {
		if callback(v, i) {
			out = append(out, v)
		}
	}
	return out
}

// Reject answers to
// Illuminate\Support\Traits\EnumeratesValues::reject: Filter with the
// predicate negated.
func (c Collection[T]) Reject(callback func(value T, key int) bool) Collection[T] {
	if callback == nil {
		return c.Values()
	}
	return c.Filter(func(value T, key int) bool { return !callback(value, key) })
}

// First answers to Illuminate\Support\Collection::first.
//
// A nil callback returns the first element, which is what the PHP does with no
// argument. The second result is false when the collection is empty or nothing
// matches, and the first is then the zero value -- the PHP returns its $default
// there, and a caller who wants one writes the fallback at the call site.
func (c Collection[T]) First(callback func(value T, key int) bool) (T, bool) {
	for i, v := range c {
		if callback == nil || callback(v, i) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// FirstOrFail answers to Illuminate\Support\Collection::firstOrFail.
//
// It returns ErrItemNotFound where the PHP throws ItemNotFoundException.
func (c Collection[T]) FirstOrFail(callback func(value T, key int) bool) (T, error) {
	if value, ok := c.First(callback); ok {
		return value, nil
	}
	var zero T
	return zero, ErrItemNotFound
}

// Last answers to Illuminate\Support\Collection::last.
//
// A nil callback returns the last element. The second result is false when
// nothing matches, as in First.
func (c Collection[T]) Last(callback func(value T, key int) bool) (T, bool) {
	for i := len(c) - 1; i >= 0; i-- {
		if callback == nil || callback(c[i], i) {
			return c[i], true
		}
	}
	var zero T
	return zero, false
}

// Sole answers to Illuminate\Support\Collection::sole.
//
// It returns ErrItemNotFound where the PHP throws ItemNotFoundException and
// ErrMultipleItemsFound where it throws MultipleItemsFoundException. The count
// the PHP exception carries is wrapped into the message.
func (c Collection[T]) Sole(callback func(value T, key int) bool) (T, error) {
	matched := c.Filter(callback)
	var zero T
	switch len(matched) {
	case 0:
		return zero, ErrItemNotFound
	case 1:
		return matched[0], nil
	default:
		return zero, fmt.Errorf("%w: %d items matched", ErrMultipleItemsFound, len(matched))
	}
}

// Each answers to
// Illuminate\Support\Traits\EnumeratesValues::each.
//
// The callback stops the walk by returning false, exactly as the PHP breaks
// when the callback returns false. It returns the collection so the call can be
// chained.
func (c Collection[T]) Each(callback func(value T, key int) bool) Collection[T] {
	for i, v := range c {
		if !callback(v, i) {
			break
		}
	}
	return c
}

// Every answers to
// Illuminate\Support\Traits\EnumeratesValues::every.
//
// An empty collection returns true, as the PHP does: there is no element to
// fail the test.
func (c Collection[T]) Every(callback func(value T, key int) bool) bool {
	for i, v := range c {
		if !callback(v, i) {
			return false
		}
	}
	return true
}

// Contains answers to Illuminate\Support\Collection::contains taking a truth
// test. For the value form, which needs comparison, use ContainsStrict.
func (c Collection[T]) Contains(callback func(value T, key int) bool) bool {
	_, ok := c.First(callback)
	return ok
}

// Some answers to
// Illuminate\Support\Traits\EnumeratesValues::some, which the PHP defines as an
// alias of contains.
func (c Collection[T]) Some(callback func(value T, key int) bool) bool {
	return c.Contains(callback)
}

// DoesntContain answers to Illuminate\Support\Collection::doesntContain.
func (c Collection[T]) DoesntContain(callback func(value T, key int) bool) bool {
	return !c.Contains(callback)
}

// ContainsOneItem answers to
// Illuminate\Support\Collection::containsOneItem.
func (c Collection[T]) ContainsOneItem(callback func(value T, key int) bool) bool {
	if callback == nil {
		return len(c) == 1
	}
	return len(c.Filter(callback)) == 1
}

// Percentage answers to
// Illuminate\Support\Traits\EnumeratesValues::percentage: the share of elements
// passing the test, from 0 to 100, rounded to precision decimal places.
//
// The second result is false on an empty collection, where the PHP returns
// null.
func (c Collection[T]) Percentage(callback func(value T, key int) bool, precision int) (float64, bool) {
	if len(c) == 0 {
		return 0, false
	}
	raw := float64(len(c.Filter(callback))) / float64(len(c)) * 100
	factor := math.Pow(10, float64(precision))
	return math.Round(raw*factor) / factor, true
}

// Partition answers to
// Illuminate\Support\Traits\EnumeratesValues::partition: the elements passing
// the test, then the ones failing it.
//
// The PHP returns a collection holding two collections; Go returns them as two
// results, which is the same pair without the indexing. Neither half is nil.
func (c Collection[T]) Partition(callback func(value T, key int) bool) (passed, failed Collection[T]) {
	passed = make(Collection[T], 0, len(c))
	failed = make(Collection[T], 0, len(c))
	for i, v := range c {
		if callback(v, i) {
			passed = append(passed, v)
		} else {
			failed = append(failed, v)
		}
	}
	return passed, failed
}

// Slice answers to Illuminate\Support\Collection::slice.
//
// A negative offset counts from the end, and a negative length stops that many
// elements before the end, exactly as array_slice does. Omit length to read to
// the end. Only the first length is used; the variadic is how Go spells PHP's
// default argument.
func (c Collection[T]) Slice(offset int, length ...int) Collection[T] {
	start := offset
	if start < 0 {
		start = len(c) + start
		if start < 0 {
			start = 0
		}
	}
	if start > len(c) {
		return Collection[T]{}
	}

	end := len(c)
	if len(length) > 0 {
		n := length[0]
		if n < 0 {
			end = len(c) + n
		} else {
			end = start + n
		}
	}
	if end > len(c) {
		end = len(c)
	}
	if end < start {
		end = start
	}

	out := make(Collection[T], end-start)
	copy(out, c[start:end])
	return out
}

// Take answers to Illuminate\Support\Collection::take.
//
// A negative limit takes that many from the end, as the PHP does by slicing
// with a negative offset.
func (c Collection[T]) Take(limit int) Collection[T] {
	if limit < 0 {
		return c.Slice(limit, -limit)
	}
	return c.Slice(0, limit)
}

// Skip answers to Illuminate\Support\Collection::skip.
func (c Collection[T]) Skip(count int) Collection[T] { return c.Slice(count) }

// TakeWhile answers to Illuminate\Support\Collection::takeWhile: the leading
// run of elements passing the test.
func (c Collection[T]) TakeWhile(callback func(value T, key int) bool) Collection[T] {
	for i, v := range c {
		if !callback(v, i) {
			return c.Slice(0, i)
		}
	}
	return c.Values()
}

// TakeUntil answers to Illuminate\Support\Collection::takeUntil: the elements
// before the first one passing the test.
func (c Collection[T]) TakeUntil(callback func(value T, key int) bool) Collection[T] {
	return c.TakeWhile(func(value T, key int) bool { return !callback(value, key) })
}

// SkipWhile answers to Illuminate\Support\Collection::skipWhile: everything
// from the first element failing the test onwards.
func (c Collection[T]) SkipWhile(callback func(value T, key int) bool) Collection[T] {
	for i, v := range c {
		if !callback(v, i) {
			return c.Slice(i)
		}
	}
	return Collection[T]{}
}

// SkipUntil answers to Illuminate\Support\Collection::skipUntil: everything
// from the first element passing the test onwards.
func (c Collection[T]) SkipUntil(callback func(value T, key int) bool) Collection[T] {
	return c.SkipWhile(func(value T, key int) bool { return !callback(value, key) })
}

// Chunk answers to Illuminate\Support\Collection::chunk.
//
// A size below one yields an empty collection, which is what the PHP's
// array_chunk guard produces rather than an endless loop. The last chunk is
// short when the length does not divide evenly.
func Chunk[T any](c Collection[T], size int) Collection[Collection[T]] {
	if size < 1 {
		return Collection[Collection[T]]{}
	}
	out := make(Collection[Collection[T]], 0, (len(c)+size-1)/size)
	for start := 0; start < len(c); start += size {
		out = append(out, c.Slice(start, size))
	}
	return out
}

// ChunkWhile answers to Illuminate\Support\Collection::chunkWhile: a new chunk
// starts whenever the callback reports false for the element against the chunk
// built so far.
func ChunkWhile[T any](c Collection[T], callback func(value T, key int, chunk Collection[T]) bool) Collection[Collection[T]] {
	out := Collection[Collection[T]]{}
	var chunk Collection[T]
	for i, v := range c {
		if len(chunk) == 0 || callback(v, i, chunk) {
			chunk = append(chunk, v)
			continue
		}
		out = append(out, chunk)
		chunk = Collection[T]{v}
	}
	if len(chunk) > 0 {
		out = append(out, chunk)
	}
	return out
}

// Split answers to Illuminate\Support\Collection::split: the elements dealt
// into numberOfGroups groups, the earlier groups taking the remainder.
//
// It returns ErrInvalidArgument where the PHP throws InvalidArgumentException.
func Split[T any](c Collection[T], numberOfGroups int) (Collection[Collection[T]], error) {
	if numberOfGroups < 1 {
		return nil, fmt.Errorf("%w: number of groups must be at least 1, got %d", ErrInvalidArgument, numberOfGroups)
	}
	if len(c) == 0 {
		return Collection[Collection[T]]{}, nil
	}

	groups := make(Collection[Collection[T]], 0, numberOfGroups)
	groupSize := len(c) / numberOfGroups
	remain := len(c) % numberOfGroups
	start := 0
	for i := 0; i < numberOfGroups; i++ {
		size := groupSize
		if i < remain {
			size++
		}
		if size == 0 {
			continue
		}
		groups = append(groups, c.Slice(start, size))
		start += size
	}
	return groups, nil
}

// SplitIn answers to Illuminate\Support\Collection::splitIn: chunks of the size
// that fits numberOfGroups groups, the last one short.
//
// It returns ErrInvalidArgument where the PHP throws InvalidArgumentException.
func SplitIn[T any](c Collection[T], numberOfGroups int) (Collection[Collection[T]], error) {
	if numberOfGroups < 1 {
		return nil, fmt.Errorf("%w: number of groups must be at least 1, got %d", ErrInvalidArgument, numberOfGroups)
	}
	size := int(math.Ceil(float64(len(c)) / float64(numberOfGroups)))
	return Chunk(c, size), nil
}

// Sliding answers to Illuminate\Support\Collection::sliding: a sliding window
// of size elements, advancing step at a time.
//
// It returns ErrInvalidArgument where the PHP throws InvalidArgumentException.
// A collection shorter than the window yields no chunk.
func Sliding[T any](c Collection[T], size, step int) (Collection[Collection[T]], error) {
	switch {
	case size < 1:
		return nil, fmt.Errorf("%w: size value must be at least 1, got %d", ErrInvalidArgument, size)
	case step < 1:
		return nil, fmt.Errorf("%w: step value must be at least 1, got %d", ErrInvalidArgument, step)
	}
	chunks := (len(c)-size)/step + 1
	if len(c) < size {
		chunks = 0
	}
	out := make(Collection[Collection[T]], 0, chunks)
	for i := 0; i < chunks; i++ {
		out = append(out, c.Slice(i*step, size))
	}
	return out, nil
}

// Nth answers to Illuminate\Support\Collection::nth: every step-th element,
// starting at offset.
//
// It returns ErrInvalidArgument where the PHP throws InvalidArgumentException.
func (c Collection[T]) Nth(step, offset int) (Collection[T], error) {
	if step < 1 {
		return nil, fmt.Errorf("%w: step value must be at least 1, got %d", ErrInvalidArgument, step)
	}
	rest := c.Slice(offset)
	out := make(Collection[T], 0, (len(rest)+step-1)/step)
	for i, v := range rest {
		if i%step == 0 {
			out = append(out, v)
		}
	}
	return out, nil
}

// Splice answers to Illuminate\Support\Collection::splice.
//
// The PHP mutates the receiver and returns what it removed; so does this, which
// is why it takes a pointer. Omit length to cut to the end.
func (c *Collection[T]) Splice(offset int, length *int, replacement ...T) Collection[T] {
	items := *c
	start := offset
	if start < 0 {
		start = len(items) + start
	}
	if start < 0 {
		start = 0
	}
	if start > len(items) {
		start = len(items)
	}

	end := len(items)
	if length != nil {
		if *length < 0 {
			end = len(items) + *length
		} else {
			end = start + *length
		}
	}
	if end > len(items) {
		end = len(items)
	}
	if end < start {
		end = start
	}

	removed := make(Collection[T], end-start)
	copy(removed, items[start:end])

	kept := make(Collection[T], 0, len(items)-(end-start)+len(replacement))
	kept = append(kept, items[:start]...)
	kept = append(kept, replacement...)
	kept = append(kept, items[end:]...)
	*c = kept

	return removed
}

// Sort answers to Illuminate\Support\Collection::sort.
//
// The PHP sorts by the values themselves when given no callback; Go cannot
// compare an arbitrary T, so the comparison is required. It is stable, as
// uasort is.
func (c Collection[T]) Sort(compare func(a, b T) int) Collection[T] {
	out := c.Values()
	if compare != nil {
		stableSort(out, compare)
	}
	return out
}

// SortDesc answers to Illuminate\Support\Collection::sortDesc: Sort with the
// comparison reversed.
func (c Collection[T]) SortDesc(compare func(a, b T) int) Collection[T] {
	if compare == nil {
		return c.Values()
	}
	return c.Sort(func(a, b T) int { return -compare(a, b) })
}

// Shuffle answers to Illuminate\Support\Collection::shuffle.
//
// It draws from crypto/rand, because the core carries no other source and a
// shuffle seeded from the clock is a shuffle an attacker can replay.
func (c Collection[T]) Shuffle() Collection[T] {
	out := c.Values()
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return out
		}
		k := int(j.Int64())
		out[i], out[k] = out[k], out[i]
	}
	return out
}

// Random answers to Illuminate\Support\Collection::random.
//
// It returns ErrInvalidArgument when number exceeds the length, which is what
// Arr::random throws there.
func (c Collection[T]) Random(number int) (Collection[T], error) {
	if number < 0 || number > len(c) {
		return nil, fmt.Errorf("%w: cannot take %d items from a collection of %d", ErrInvalidArgument, number, len(c))
	}
	return c.Shuffle().Slice(0, number), nil
}

// Implode answers to Illuminate\Support\Collection::implode taking the glue
// only: the elements rendered with format and joined.
//
// The PHP reads a property name off each element when given one; Go has the
// stringer instead, so the rendering is the callback.
func (c Collection[T]) Implode(glue string, value func(item T) string) string {
	parts := make([]string, len(c))
	for i, v := range c {
		if value == nil {
			parts[i] = fmt.Sprint(v)
			continue
		}
		parts[i] = value(v)
	}
	return strings.Join(parts, glue)
}

// Join answers to Illuminate\Support\Collection::join: Implode, except that the
// last element is attached with finalGlue.
//
// An empty finalGlue is Implode, one element is that element, and no element is
// the empty string -- the three cases the PHP spells out.
func (c Collection[T]) Join(glue, finalGlue string, value func(item T) string) string {
	if finalGlue == "" {
		return c.Implode(glue, value)
	}
	switch len(c) {
	case 0:
		return ""
	case 1:
		return c.Implode(glue, value)
	}
	return c.Slice(0, len(c)-1).Implode(glue, value) + finalGlue + c.Slice(len(c)-1).Implode(glue, value)
}

// Tap answers to
// Illuminate\Support\Traits\EnumeratesValues::tap: hand the collection to the
// callback and return it unchanged.
func (c Collection[T]) Tap(callback func(collection Collection[T])) Collection[T] {
	callback(c)
	return c
}

// When answers to Illuminate\Support\Traits\Conditionable::when.
//
// The PHP takes a truthy value and a default branch; both are here, the default
// being nil for "do nothing".
func (c Collection[T]) When(condition bool, callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	if condition {
		if callback == nil {
			return c
		}
		return callback(c)
	}
	if otherwise == nil {
		return c
	}
	return otherwise(c)
}

// Unless answers to Illuminate\Support\Traits\Conditionable::unless: When with
// the condition negated.
func (c Collection[T]) Unless(condition bool, callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	return c.When(!condition, callback, otherwise)
}

// WhenEmpty answers to
// Illuminate\Support\Traits\EnumeratesValues::whenEmpty.
func (c Collection[T]) WhenEmpty(callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	return c.When(len(c) == 0, callback, otherwise)
}

// WhenNotEmpty answers to
// Illuminate\Support\Traits\EnumeratesValues::whenNotEmpty.
func (c Collection[T]) WhenNotEmpty(callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	return c.When(len(c) > 0, callback, otherwise)
}

// UnlessEmpty answers to
// Illuminate\Support\Traits\EnumeratesValues::unlessEmpty, which the PHP
// defines as an alias of whenNotEmpty.
func (c Collection[T]) UnlessEmpty(callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	return c.WhenNotEmpty(callback, otherwise)
}

// UnlessNotEmpty answers to
// Illuminate\Support\Traits\EnumeratesValues::unlessNotEmpty, which the PHP
// defines as an alias of whenEmpty.
func (c Collection[T]) UnlessNotEmpty(callback, otherwise func(collection Collection[T]) Collection[T]) Collection[T] {
	return c.WhenEmpty(callback, otherwise)
}

// Only answers to Illuminate\Support\Collection::only: the elements at the
// given indices, in the collection's order.
//
// An index outside the collection is skipped, as a missing key is in PHP.
func (c Collection[T]) Only(keys ...int) Collection[T] {
	wanted := make(map[int]struct{}, len(keys))
	for _, k := range keys {
		wanted[k] = struct{}{}
	}
	out := make(Collection[T], 0, len(keys))
	for i, v := range c {
		if _, ok := wanted[i]; ok {
			out = append(out, v)
		}
	}
	return out
}

// Except answers to Illuminate\Support\Collection::except: everything but the
// elements at the given indices.
func (c Collection[T]) Except(keys ...int) Collection[T] {
	unwanted := make(map[int]struct{}, len(keys))
	for _, k := range keys {
		unwanted[k] = struct{}{}
	}
	out := make(Collection[T], 0, len(c))
	for i, v := range c {
		if _, ok := unwanted[i]; !ok {
			out = append(out, v)
		}
	}
	return out
}

// Replace answers to Illuminate\Support\Collection::replace: the elements of
// items overwrite the ones at the same index, and the ones past the end are
// appended.
func (c Collection[T]) Replace(items map[int]T) Collection[T] {
	out := c.Values()
	extra := make([]int, 0, len(items))
	for k := range items {
		if k >= 0 && k < len(out) {
			out[k] = items[k]
			continue
		}
		if k >= 0 {
			extra = append(extra, k)
		}
	}
	stableSort(extra, cmp.Compare)
	for _, k := range extra {
		out = append(out, items[k])
	}
	return out
}

// Sort is stable and so is SortKeys; the PHP's ksort has no meaning on a list,
// whose keys are already its order.

// CountBy, GroupBy, KeyBy and the rest that change the element type are
// functions in functions.go, because a Go method cannot declare a type
// parameter.
