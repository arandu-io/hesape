package collections

// Collection is Illuminate\Support\Collection.
//
// The PHP class wraps a PHP array, which is an ordered map: it carries keys and
// values together, and half of the class exists to keep the two in step. Go has
// no such type, so Collection is a list -- the shape a PHP collection has in
// almost every application -- and the key of an element is its index. Every
// method that the PHP names after a key is documented here with the index it
// reads instead.
//
// It is declared as a slice rather than a struct so that a []T can be handed to
// it and taken back out without a copy, and so that len, range and the slices
// package keep working on it.
type Collection[T any] []T

// Collect is the collect() helper of helpers.php.
//
// It is the constructor an application actually types. The result shares the
// backing array with items, exactly as the PHP shares nothing and everything at
// once: mutate one and the other sees it, up to the point a method reallocates.
func Collect[T any](items []T) Collection[T] { return Collection[T](items) }

// Make is Enumerable::make.
//
// It is Collect under the name the PHP gives the static constructor.
func Make[T any](items []T) Collection[T] { return Collection[T](items) }

// Empty is Enumerable::empty.
//
// It returns an empty collection that is not nil, so that appending to it and
// comparing its length behave the same as on a collection that had elements.
func Empty[T any]() Collection[T] { return Collection[T]{} }

// Times is Enumerable::times.
//
// The callback receives 1, 2, ... number, one-based, as the PHP does. A number
// below one yields an empty collection rather than an error, which is what
// makes Sliding safe to write in terms of it.
func Times[T any](number int, callback func(int) T) Collection[T] {
	if number < 1 {
		return Collection[T]{}
	}
	out := make(Collection[T], 0, number)
	for i := 1; i <= number; i++ {
		out = append(out, callback(i))
	}
	return out
}

// Range is Enumerable::range.
//
// It counts from from to to inclusive. A step of zero is treated as one, and a
// negative step, or a to below from, counts downwards -- the PHP delegates to
// range(), which does the same.
func Range(from, to, step int) Collection[int] {
	if step == 0 {
		step = 1
	}
	if step < 0 {
		step = -step
	}
	out := Collection[int]{}
	if from <= to {
		for v := from; v <= to; v += step {
			out = append(out, v)
		}
		return out
	}
	for v := from; v >= to; v -= step {
		out = append(out, v)
	}
	return out
}

// Wrap is Enumerable::wrap.
//
// The PHP turns null into [], a single value into [value] and an array into
// itself. A variadic parameter says all three in Go: Wrap[int]() is empty,
// Wrap(1) holds one element, and Wrap(s...) holds the slice.
func Wrap[T any](value ...T) Collection[T] {
	if value == nil {
		return Collection[T]{}
	}
	return Collection[T](value)
}

// Unwrap is Enumerable::unwrap.
//
// It gives back the plain slice underneath, which is what the PHP does when the
// argument is a collection.
func Unwrap[T any](value Collection[T]) []T { return []T(value) }

// All is Collection::all.
//
// The result is never nil: an empty collection gives an empty slice, so that a
// caller can append to it without checking.
func (c Collection[T]) All() []T {
	if c == nil {
		return []T{}
	}
	return []T(c)
}

// Count is Collection::count.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty is Collection::isEmpty.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// IsNotEmpty is EnumeratesValues::isNotEmpty.
func (c Collection[T]) IsNotEmpty() bool { return len(c) > 0 }

// Get is Collection::get.
//
// The key of an element in a list is its index. The second result is false
// where the PHP would return the $default, so the caller supplies the default
// at the call site instead of passing it in.
func (c Collection[T]) Get(key int) (T, bool) {
	if key < 0 || key >= len(c) {
		var zero T
		return zero, false
	}
	return c[key], true
}

// Has is Collection::has.
//
// It reports whether every index given is within the collection. With no index
// it reports false, as the PHP does for an empty key list.
func (c Collection[T]) Has(keys ...int) bool {
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if k < 0 || k >= len(c) {
			return false
		}
	}
	return true
}

// HasAny is Collection::hasAny.
//
// It reports whether at least one index given is within the collection.
func (c Collection[T]) HasAny(keys ...int) bool {
	for _, k := range keys {
		if k >= 0 && k < len(c) {
			return true
		}
	}
	return false
}

// Keys is Collection::keys.
//
// The keys of a list are its indices, so this is 0, 1, ... count-1.
func (c Collection[T]) Keys() Collection[int] {
	out := make(Collection[int], len(c))
	for i := range c {
		out[i] = i
	}
	return out
}

// Values is Collection::values.
//
// The PHP renumbers the keys; a list is already numbered, so this is a copy,
// which is still worth having because it detaches the result from the receiver.
func (c Collection[T]) Values() Collection[T] {
	out := make(Collection[T], len(c))
	copy(out, c)
	return out
}

// Collect is Enumerable::collect.
//
// It returns a fresh collection holding the same elements.
func (c Collection[T]) Collect() Collection[T] { return c.Values() }

// Put is Collection::put.
//
// The key of a list is an index. Writing past the end grows the collection with
// zero values up to that index, which is what assigning to a high key does to a
// PHP array. It returns the receiver so that calls chain.
func (c *Collection[T]) Put(key int, value T) *Collection[T] {
	if key < 0 {
		return c
	}
	for len(*c) <= key {
		var zero T
		*c = append(*c, zero)
	}
	(*c)[key] = value
	return c
}

// Push is Collection::push.
//
// It appends every value and returns the receiver.
func (c *Collection[T]) Push(values ...T) *Collection[T] {
	*c = append(*c, values...)
	return c
}

// Add is Collection::add.
//
// It appends one element. The PHP keeps it apart from push because push is
// variadic and add is not; both append.
func (c *Collection[T]) Add(item T) *Collection[T] {
	*c = append(*c, item)
	return c
}

// Unshift is Collection::unshift.
//
// It puts the values at the front, in the order given.
func (c *Collection[T]) Unshift(values ...T) *Collection[T] {
	*c = append(append(make(Collection[T], 0, len(*c)+len(values)), values...), *c...)
	return c
}

// Prepend is Collection::prepend.
//
// The PHP takes an optional key; a list has no key to give, so this is Unshift
// with a single value, which is the form the PHP is called in.
func (c *Collection[T]) Prepend(value T) *Collection[T] { return c.Unshift(value) }

// Pop is Collection::pop.
//
// It removes the last count elements and returns them, most recent first, as
// the PHP does when count is above one. The PHP returns the bare element when
// count is one; a Go function has one return type, so this always returns a
// collection.
func (c *Collection[T]) Pop(count int) Collection[T] {
	if count <= 0 || len(*c) == 0 {
		return Collection[T]{}
	}
	if count > len(*c) {
		count = len(*c)
	}
	out := make(Collection[T], 0, count)
	for i := 0; i < count; i++ {
		last := len(*c) - 1
		out = append(out, (*c)[last])
		*c = (*c)[:last]
	}
	return out
}

// Shift is Collection::shift.
//
// It removes the first count elements and returns them in order. As with Pop,
// the PHP's single-element return collapses to a collection here.
func (c *Collection[T]) Shift(count int) Collection[T] {
	if count <= 0 || len(*c) == 0 {
		return Collection[T]{}
	}
	if count > len(*c) {
		count = len(*c)
	}
	out := make(Collection[T], count)
	copy(out, (*c)[:count])
	*c = (*c)[count:]
	return out
}

// Forget is Collection::forget.
//
// It removes the elements at the given indices. Removing from a list closes the
// gap, where the PHP would leave a hole in the key sequence.
func (c *Collection[T]) Forget(keys ...int) *Collection[T] {
	drop := make(map[int]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}
	out := make(Collection[T], 0, len(*c))
	for i, v := range *c {
		if _, ok := drop[i]; ok {
			continue
		}
		out = append(out, v)
	}
	*c = out
	return c
}

// Pull is Collection::pull.
//
// It reads the element at the index and removes it. The second result is false
// when the index is out of range, where the PHP returns the $default.
func (c *Collection[T]) Pull(key int) (T, bool) {
	v, ok := c.Get(key)
	if !ok {
		return v, false
	}
	c.Forget(key)
	return v, true
}

// GetOrPut is Collection::getOrPut.
//
// It returns the element at the index, or writes the value there and returns it
// when the index is not filled yet.
func (c *Collection[T]) GetOrPut(key int, value T) T {
	if v, ok := c.Get(key); ok {
		return v
	}
	c.Put(key, value)
	return value
}

// Transform is Collection::transform.
//
// It replaces every element with the result of the callback, in place. Unlike
// Map it cannot change the element type, which is exactly the distinction the
// PHP draws between the two.
func (c *Collection[T]) Transform(callback func(T) T) *Collection[T] {
	for i, v := range *c {
		(*c)[i] = callback(v)
	}
	return c
}

// Concat is Collection::concat.
//
// It appends the source to a copy of the collection and leaves the receiver
// alone, which is where it differs from Push.
func (c Collection[T]) Concat(source []T) Collection[T] {
	out := make(Collection[T], 0, len(c)+len(source))
	out = append(out, c...)
	out = append(out, source...)
	return out
}

// Merge is Collection::merge.
//
// On a list the PHP's array_merge appends, because numeric keys are renumbered
// rather than overwritten. That is what this does.
func (c Collection[T]) Merge(items []T) Collection[T] { return c.Concat(items) }

// Union is Collection::union.
//
// The PHP's + operator keeps the element already at a key and takes from the
// argument only the keys the receiver does not have. On a list that means the
// receiver wins for every index it has, and the tail of items past its length
// is appended.
func (c Collection[T]) Union(items []T) Collection[T] {
	out := make(Collection[T], 0, max(len(c), len(items)))
	out = append(out, c...)
	if len(items) > len(c) {
		out = append(out, items[len(c):]...)
	}
	return out
}

// Reverse is Collection::reverse.
func (c Collection[T]) Reverse() Collection[T] {
	out := make(Collection[T], len(c))
	for i, v := range c {
		out[len(c)-1-i] = v
	}
	return out
}

// Pad is Collection::pad.
//
// A positive size pads on the right, a negative size on the left, and a size
// whose magnitude is not larger than the count returns the elements unchanged.
// That is array_pad.
func (c Collection[T]) Pad(size int, value T) Collection[T] {
	n := size
	left := false
	if n < 0 {
		n = -n
		left = true
	}
	if n <= len(c) {
		return c.Values()
	}
	pad := make(Collection[T], n-len(c))
	for i := range pad {
		pad[i] = value
	}
	if left {
		return append(pad, c...)
	}
	return append(c.Values(), pad...)
}
