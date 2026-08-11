package arr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// container reports the reflect.Value of value when value is something PHP's
// is_array() would accept: a map, a slice or an array. A named slice type such
// as collections.Collection[T] is a slice, so it passes here without being
// named, which is how every function in this file accepts one.
func container(value any) (reflect.Value, bool) {
	if value == nil {
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return rv, true
	default:
		return reflect.Value{}, false
	}
}

// mapKey converts a segment of a dotted key to the key type of a Go map. PHP
// array keys are strings or integers and it casts between them freely, so a
// map[int]T is reachable with the segment "3".
func mapKey(segment string, keyType reflect.Type) (reflect.Value, bool) {
	switch keyType.Kind() {
	case reflect.String:
		return reflect.ValueOf(segment).Convert(keyType), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(segment, 10, 64)
		if err != nil || reflect.ValueOf(n).OverflowInt(n) {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(n).Convert(keyType), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(segment, 10, 64)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(n).Convert(keyType), true
	case reflect.Interface:
		if keyType.NumMethod() == 0 {
			return reflect.ValueOf(segment), true
		}
		return reflect.Value{}, false
	default:
		return reflect.Value{}, false
	}
}

// index reads one key out of one container, reporting whether the key is
// there. It is PHP's static::exists() and $array[$key] in a single step, so
// that a key holding null is told apart from a key that is absent.
func index(value any, segment string) (any, bool) {
	rv, ok := container(value)
	if !ok {
		return nil, false
	}
	switch rv.Kind() {
	case reflect.Map:
		key, ok := mapKey(segment, rv.Type().Key())
		if !ok {
			return nil, false
		}
		found := rv.MapIndex(key)
		if !found.IsValid() {
			return nil, false
		}
		return found.Interface(), true
	default:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= rv.Len() {
			return nil, false
		}
		return rv.Index(i).Interface(), true
	}
}

// keys returns the keys of a container as strings, ascending. For a list they
// are "0", "1", ... which is what PHP's array_keys gives for a list.
func keys(value any) []string {
	rv, ok := container(value)
	if !ok {
		return nil
	}
	if rv.Kind() != reflect.Map {
		out := make([]string, rv.Len())
		for i := range out {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	out := make([]string, 0, rv.Len())
	for _, k := range rv.MapKeys() {
		out = append(out, keyString(k.Interface()))
	}
	sort.Strings(out)
	return out
}

// keyString renders a map key the way PHP would print it as an array key.
func keyString(key any) string {
	switch v := key.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(key)
	}
}

// elements returns the values of a container in key order: the order of a list,
// and ascending key order for a map.
func elements(value any) ([]any, bool) {
	rv, ok := container(value)
	if !ok {
		return nil, false
	}
	if rv.Kind() != reflect.Map {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	}
	names := keys(value)
	out := make([]any, 0, len(names))
	for _, name := range names {
		if v, ok := index(value, name); ok {
			out = append(out, v)
		}
	}
	return out, true
}

// Accessible answers to Arr::accessible.
//
// The PHP asks is_array($value) || $value instanceof ArrayAccess. Go has no
// ArrayAccess, so the question is whether the value is a map, a slice or an
// array -- which covers collections.Collection, whose underlying type is a
// slice.
func Accessible(value any) bool {
	_, ok := container(value)
	return ok
}

// Arrayable answers to Arr::arrayable.
//
// The PHP asks whether the value is an array or implements Arrayable,
// Traversable, Jsonable or JsonSerializable. The Go counterparts are: a map,
// slice or array; a value with a ToArray method returning []any or
// map[string]any; and a json.Marshaler, which is what JsonSerializable and
// Jsonable both amount to once the value has to become JSON.
func Arrayable(value any) bool {
	if Accessible(value) {
		return true
	}
	switch value.(type) {
	case interface{ ToArray() []any }, interface{ ToArray() map[string]any }, json.Marshaler:
		return true
	default:
		return false
	}
}

// Exists answers to Arr::exists: the key is present, whatever it holds.
//
// A key holding nil exists, exactly as a PHP key holding null does for
// array_key_exists.
func Exists(array any, key string) bool {
	_, ok := index(array, key)
	return ok
}

// Get answers to Arr::get: read a value out of a nested structure with a "dot"
// path.
//
// The exact key is tried first and only then split on dots, so a key that
// itself contains a dot -- "user.name" written as one key -- is found rather
// than treated as a path. That is the order the PHP checks in, and it is the
// reason the check is not simply a split.
//
// The second result is false where the PHP returns its $default: the value is
// missing, a segment ran off the end of a list, or something along the path was
// not an array. A key present but holding nil returns (nil, true).
//
// The PHP also accepts a null key and hands back the whole array. Go has no
// null string and the caller already holds the array, so there is nothing to
// spell.
func Get(array any, key string) (any, bool) {
	if !Accessible(array) {
		return nil, false
	}
	if value, ok := index(array, key); ok {
		return value, true
	}
	if !strings.Contains(key, ".") {
		return nil, false
	}
	current := any(array)
	for _, segment := range strings.Split(key, ".") {
		value, ok := index(current, segment)
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// Has answers to Arr::has: every key given is present, by "dot" path.
//
// With no key it reports false, and over a value that is not an array it
// reports false, which is what the PHP's `! $array || $keys === []` guard does.
func Has(array any, keys ...string) bool {
	if len(keys) == 0 || !Accessible(array) || isEmptyContainer(array) {
		return false
	}
	for _, key := range keys {
		if _, ok := Get(array, key); !ok {
			return false
		}
	}
	return true
}

// HasAll answers to Arr::hasAll.
//
// The PHP defines it as Arr::has applied to each key in turn, and Arr::has
// already requires all of them, so the two agree on every input. It is here
// because Illuminate has it, and Illuminate has it because hasAny reads better
// against a name that says "all".
func HasAll(array any, keys ...string) bool { return Has(array, keys...) }

// HasAny answers to Arr::hasAny: at least one key is present, by "dot" path.
func HasAny(array any, keys ...string) bool {
	if len(keys) == 0 || !Accessible(array) || isEmptyContainer(array) {
		return false
	}
	for _, key := range keys {
		if _, ok := Get(array, key); ok {
			return true
		}
	}
	return false
}

// isEmptyContainer is PHP's `! $array` for something already known to be an
// array: an empty array is falsy there, and has() and hasAny() both return
// false on one before they look at the keys at all.
func isEmptyContainer(array any) bool {
	rv, ok := container(array)
	return ok && rv.Len() == 0
}

// Set answers to Arr::set: write a value into a nested map with a "dot" path,
// creating the maps along the way.
//
// The PHP takes the array by reference and mutates it; a Go map is already a
// reference, so this writes through and returns the same map for chaining. A
// segment whose current value is not an array is replaced by a fresh map, as
// the PHP replaces it with a fresh array.
//
// A segment naming a position inside a []any along the path is followed and
// written through, because a Go slice element is addressable. A position past
// the end of that slice is not: PHP would turn the list into a map keyed by
// that position, and a []any cannot hold a key. Set stops there and returns the
// map unchanged.
//
// The PHP's null key, which replaces the whole array, has no Go spelling. A nil
// map has nowhere to write and is handed back unchanged rather than panicking.
func Set(array map[string]any, key string, value any) map[string]any {
	if array == nil {
		return array
	}
	segments := strings.Split(key, ".")
	current := any(array)
	for _, segment := range segments[:len(segments)-1] {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[segment]
			if !ok || !Accessible(next) {
				next = map[string]any{}
				node[segment] = next
			}
			current = next
		default:
			next, ok := index(current, segment)
			if !ok {
				return array
			}
			if !Accessible(next) {
				fresh := map[string]any{}
				if !writeChild(current, segment, fresh) {
					return array
				}
				next = fresh
			}
			current = next
		}
	}
	last := segments[len(segments)-1]
	if node, ok := current.(map[string]any); ok {
		node[last] = value
		return array
	}
	writeChild(current, last, value)
	return array
}

// writeChild stores value under segment in an already existing container,
// reporting whether it could. It is the half of Set that has to go through
// reflection, because a map of another type or a slice position is not a
// map[string]any.
func writeChild(node any, segment string, value any) bool {
	rv, ok := container(node)
	if !ok {
		return false
	}
	if rv.Kind() == reflect.Map {
		key, ok := mapKey(segment, rv.Type().Key())
		if !ok {
			return false
		}
		element := rv.Type().Elem()
		stored, ok := assignable(value, element)
		if !ok {
			return false
		}
		rv.SetMapIndex(key, stored)
		return true
	}
	i, err := strconv.Atoi(segment)
	if err != nil || i < 0 || i >= rv.Len() {
		return false
	}
	stored, ok := assignable(value, rv.Type().Elem())
	if !ok || !rv.Index(i).CanSet() {
		return false
	}
	rv.Index(i).Set(stored)
	return true
}

// assignable converts value to the element type of the container it is about
// to be stored in, reporting false when no such conversion exists.
func assignable(value any, target reflect.Type) (reflect.Value, bool) {
	if value == nil {
		if target.Kind() == reflect.Interface || target.Kind() == reflect.Ptr ||
			target.Kind() == reflect.Map || target.Kind() == reflect.Slice {
			return reflect.Zero(target), true
		}
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(value)
	if rv.Type().AssignableTo(target) {
		return rv, true
	}
	if rv.Type().ConvertibleTo(target) {
		return rv.Convert(target), true
	}
	return reflect.Value{}, false
}

// Add answers to Arr::add: write the value only where the "dot" path holds
// nothing yet.
//
// The PHP takes the array by value, so the caller's array is untouched and the
// result is the new one. This copies every map along the path it writes to and
// shares the rest, which is what PHP's copy-on-write does with the same code.
func Add(array map[string]any, key string, value any) map[string]any {
	if _, ok := Get(array, key); ok {
		return array
	}
	return Set(clone(array), key, value)
}

// clone is the shallow copy that stands in for PHP taking an array by value.
func clone(array map[string]any) map[string]any {
	out := make(map[string]any, len(array))
	for k, v := range array {
		if nested, ok := v.(map[string]any); ok {
			out[k] = clone(nested)
			continue
		}
		out[k] = v
	}
	return out
}

// Forget answers to Arr::forget: remove the keys, by "dot" path.
//
// The PHP takes the array by reference and returns nothing; so does this. As in
// Get, an exact key wins over a path, so a key containing a dot is removed
// rather than walked.
//
// One divergence, stated because it is visible. Where the last segment names a
// position inside a []any, PHP's unset leaves the other positions under the
// keys they had, turning the list into a map with a hole. A Go slice cannot
// hold a hole, so the element is removed and the ones after it move down.
func Forget(array map[string]any, keys ...string) {
	for _, key := range keys {
		forgetOne(array, key)
	}
}

func forgetOne(array map[string]any, key string) {
	if _, ok := array[key]; ok {
		delete(array, key)
		return
	}
	segments := strings.Split(key, ".")
	if len(segments) == 1 {
		return
	}
	var (
		parent  any
		current = any(array)
	)
	parentSegment := ""
	for _, segment := range segments[:len(segments)-1] {
		next, ok := index(current, segment)
		if !ok || !Accessible(next) {
			return
		}
		parent, parentSegment = current, segment
		current = next
	}
	last := segments[len(segments)-1]
	if node, ok := current.(map[string]any); ok {
		delete(node, last)
		return
	}
	rv, ok := container(current)
	if !ok {
		return
	}
	if rv.Kind() == reflect.Map {
		if mapped, ok := mapKey(last, rv.Type().Key()); ok {
			rv.SetMapIndex(mapped, reflect.Value{})
		}
		return
	}
	i, err := strconv.Atoi(last)
	if err != nil || i < 0 || i >= rv.Len() || parent == nil {
		return
	}
	shortened := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), 0, rv.Len()-1)
	for position := 0; position < rv.Len(); position++ {
		if position == i {
			continue
		}
		shortened = reflect.Append(shortened, rv.Index(position))
	}
	writeChild(parent, parentSegment, shortened.Interface())
}

// Pull answers to Arr::pull: read the value at the "dot" path and remove it.
//
// The PHP takes the array by reference and returns its $default when the key is
// absent; this mutates the map and reports false there instead.
func Pull(array map[string]any, key string) (any, bool) {
	value, ok := Get(array, key)
	Forget(array, key)
	return value, ok
}

// Push answers to Arr::push: append the values to the list held at the "dot"
// path, creating it when nothing is there.
//
// It returns the error Array returns, where the PHP lets Arr::array throw:
// something is already at that key and it is not a list.
func Push(array map[string]any, key string, values ...any) (map[string]any, error) {
	target, err := Array(array, key, []any{})
	if err != nil {
		return array, err
	}
	return Set(array, key, append(target, values...)), nil
}

// Except answers to Arr::except: everything but the keys given, by "dot" path.
//
// The PHP takes the array by value, so the receiver survives; this copies
// first for the same reason.
func Except(array map[string]any, keys ...string) map[string]any {
	out := clone(array)
	Forget(out, keys...)
	return out
}

// Only answers to Arr::only: the entries under the keys given.
//
// The PHP uses array_intersect_key, which is a top-level lookup and not a "dot"
// path, and so is this. A key that is not there is skipped.
func Only(array map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := array[key]; ok {
			out[key] = value
		}
	}
	return out
}

// PrependKeysWith answers to Arr::prependKeysWith: every key with the prefix
// glued in front of it.
func PrependKeysWith(array map[string]any, prependWith string) map[string]any {
	out := make(map[string]any, len(array))
	for key, value := range array {
		out[prependWith+key] = value
	}
	return out
}

// Dot answers to Arr::dot: a nested structure flattened into single-level "dot"
// keys.
//
// An empty array stays as a value under its own key rather than disappearing,
// which is what the PHP's `! empty($value)` guard produces. A list contributes
// its positions as key segments, so [1,2] under "a" becomes "a.0" and "a.1".
//
// The optional prepend is the PHP's second argument. Only the first is used;
// the variadic is how Go spells a PHP default argument.
//
// The clone this package is written against has no $depth argument. The current
// Laravel added one; when the clone is refreshed, it belongs here.
func Dot(array map[string]any, prepend ...string) map[string]any {
	prefix := ""
	if len(prepend) > 0 {
		prefix = prepend[0]
	}
	out := map[string]any{}
	dotInto(out, array, prefix)
	return out
}

func dotInto(out map[string]any, data any, prefix string) {
	for _, key := range keys(data) {
		value, ok := index(data, key)
		if !ok {
			continue
		}
		newKey := prefix + key
		if nested, ok := container(value); ok && nested.Len() > 0 {
			dotInto(out, value, newKey+".")
			continue
		}
		out[newKey] = value
	}
}

// Undot answers to Arr::undot: "dot" keys expanded back into nested maps.
//
// The PHP folds the entries through Arr::set, and so does this, which means a
// later key wins over an earlier one that claimed the same path. A Go map has
// no insertion order, so the keys are folded in ascending order and the outcome
// is reproducible; PHP's depends on the order the array was built in.
func Undot(array map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range keys(array) {
		value, _ := index(array, key)
		Set(out, key, value)
	}
	return out
}

// IsAssoc answers to Arr::isAssoc: the array is not a list.
//
// The PHP asks `! array_is_list($array)`. A Go map is keyed and is therefore
// associative; a slice or an array is a list. Anything that is not an array at
// all is not associative either.
func IsAssoc(array any) bool {
	rv, ok := container(array)
	return ok && rv.Kind() == reflect.Map
}

// IsList answers to Arr::isList: the keys are 0, 1, ... with no gap.
//
// A Go slice or array is always exactly that; a map never is.
func IsList(array any) bool {
	rv, ok := container(array)
	return ok && rv.Kind() != reflect.Map
}

// Divide answers to Arr::divide: the keys and the values, as two slices.
//
// The PHP pairs array_keys with array_values, which walk in insertion order. A
// Go map has none, so the keys come out ascending and the values follow them,
// which keeps the two slices paired position by position.
func Divide(array map[string]any) ([]string, []any) {
	names := keys(array)
	values := make([]any, len(names))
	for i, name := range names {
		values[i] = array[name]
	}
	return names, values
}

// From answers to Arr::from: the underlying array of whatever was handed in.
//
// The PHP walks a match() over Enumerable, Arrayable, WeakMap, Traversable,
// Jsonable, JsonSerializable and finally a plain object cast. The Go
// counterparts, in the same order of preference:
//
//	a map, slice or array          -> itself, as map[string]any or []any
//	ToArray() []any                -> that slice
//	ToArray() map[string]any       -> that map
//	json.Marshaler                 -> the JSON decoded into a Go value
//	a struct, or a pointer to one  -> its exported fields, by name
//
// Anything else -- a number, a string, nil -- returns ErrInvalidArgument, where
// the PHP throws InvalidArgumentException saying items cannot be represented by
// a scalar value.
func From(items any) (any, error) {
	switch value := items.(type) {
	case nil:
		return nil, fmt.Errorf("%w: items cannot be represented by a scalar value", ErrInvalidArgument)
	case map[string]any:
		return value, nil
	case []any:
		return value, nil
	case interface{ ToArray() []any }:
		return value.ToArray(), nil
	case interface{ ToArray() map[string]any }:
		return value.ToArray(), nil
	}
	if marshaler, ok := items.(json.Marshaler); ok {
		raw, err := marshaler.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	}
	rv := reflect.ValueOf(items)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, fmt.Errorf("%w: items cannot be represented by a scalar value", ErrInvalidArgument)
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, key := range rv.MapKeys() {
			out[keyString(key.Interface())] = rv.MapIndex(key).Interface()
		}
		return out, nil
	case reflect.Struct:
		out := map[string]any{}
		fields := rv.Type()
		for i := 0; i < fields.NumField(); i++ {
			if field := fields.Field(i); field.IsExported() {
				out[field.Name] = rv.Field(i).Interface()
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: items cannot be represented by a scalar value", ErrInvalidArgument)
	}
}
