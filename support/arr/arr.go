// Package arr holds the helpers for reading and reshaping maps and slices:
// dotted-key access ([Get], [Set], [Has], [Forget]), subsetting ([Only],
// [Except], [Where]), flattening ([Dot], [Undot], [Collapse]) and query-string
// rendering ([Query]).
//
// # Two shapes
//
// Each function takes the shape it walks: map[string]any where it reads keys,
// and a slice where it walks a list. The doc comment on each one says which.
//
// A key holding dots is a path. [Get] and [Has] descend through nested maps,
// and a numeric segment indexes into a nested slice, so one path can cross
// both shapes; [Set] creates the levels that are missing.
//
// A map carries no order of its own, so wherever the output would otherwise
// depend on one -- [Divide], [Query], [Dot] over nested values, [Undot] -- the
// keys are sorted first and the result is stable across runs.
package arr

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ErrItemNotFound is returned by [Sole] when nothing in the list matched: the
// caller asked for exactly one item and got none. It reports the shape of the
// data, not a mistake in the call, so match it with errors.Is and decide what
// an empty result means here -- a missing record, a default, an empty page.
var ErrItemNotFound = errors.New("arr: item not found")

// ErrMultipleItemsFound is returned by [Sole] when more than one item matched:
// the caller asked for exactly one and the filter was not selective enough.
// Only the fact is reported, never how many matched or which -- reaching it
// means either the callback is too loose or the list holds a duplicate that
// should not be there.
var ErrMultipleItemsFound = errors.New("arr: multiple items found")

// Arrayer is a value that knows how to present itself as a keyed map.
type Arrayer interface {
	ToArray() map[string]any
}

// value invokes a func() any and returns its result; every other value is
// returned unchanged.
func value(v any) any {
	if fn, ok := v.(func() any); ok {
		return fn()
	}
	return v
}

// Accessible reports whether the value can be indexed at all: a map, a slice
// or an array.
func Accessible(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

// Arrayable reports whether the value can be turned into an array: a map, a
// slice, an array, an [Arrayer] or a json.Marshaler.
func Arrayable(v any) bool {
	if v == nil {
		return false
	}
	if _, ok := v.(Arrayer); ok {
		return true
	}
	if _, ok := v.(json.Marshaler); ok {
		return true
	}
	return Accessible(v)
}

// Add writes the value under the dotted key only when nothing is there yet,
// and returns the same map.
func Add(array map[string]any, key string, v any) map[string]any {
	if Get(array, key, nil) == nil {
		return Set(array, key, v)
	}
	return array
}

// Array reads a dotted key and requires the value to be a []any, returning an
// error naming the key and the type found when it is not.
//
// A nil default means no default, which fails the type check unless the key is
// present.
func Array(array map[string]any, key string, def []any) ([]any, error) {
	var raw any
	if def == nil {
		raw = Get(array, key, nil)
	} else {
		raw = Get(array, key, def)
	}
	v, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("array value for key [%s] must be an array, %s found", key, typeName(raw))
	}
	return v, nil
}

// Boolean reads a dotted key and requires the value to be a bool, returning an
// error naming the key and the type found when it is not.
//
// The default is a pointer so that nil can mean no default, which fails the
// type check unless the key is present.
func Boolean(array map[string]any, key string, def *bool) (bool, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("array value for key [%s] must be a boolean, %s found", key, typeName(raw))
	}
	return v, nil
}

// Float reads a dotted key and requires the value to be a float64, returning
// an error naming the key and the type found when it is not. A nil default
// means no default.
func Float(array map[string]any, key string, def *float64) (float64, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("array value for key [%s] must be a float, %s found", key, typeName(raw))
	}
	return v, nil
}

// Integer reads a dotted key and requires the value to be an int, returning an
// error naming the key and the type found when it is not. A nil default means
// no default.
func Integer(array map[string]any, key string, def *int) (int, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(int)
	if !ok {
		return 0, fmt.Errorf("array value for key [%s] must be an integer, %s found", key, typeName(raw))
	}
	return v, nil
}

// String reads a dotted key and requires the value to be a string, returning
// an error naming the key and the type found when it is not. A nil default
// means no default.
func String(array map[string]any, key string, def *string) (string, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("array value for key [%s] must be a string, %s found", key, typeName(raw))
	}
	return v, nil
}

func getWithPointerDefault[T any](array map[string]any, key string, def *T) any {
	if def == nil {
		return Get(array, key, nil)
	}
	return Get(array, key, *def)
}

// typeName names a value's type for the type-check messages. A nil value is
// reported as NULL.
func typeName(v any) string {
	if v == nil {
		return "NULL"
	}
	return reflect.TypeOf(v).String()
}

// Collapse returns one flat list out of a list of lists.
func Collapse[T any](array [][]T) []T {
	results := []T{}
	for _, values := range array {
		results = append(results, values...)
	}
	return results
}

// CrossJoin returns every combination of the given lists, one list per
// combination, with the last list varying fastest.
func CrossJoin[T any](arrays ...[]T) [][]T {
	results := [][]T{{}}
	for _, array := range arrays {
		var appended [][]T
		for _, product := range results {
			for _, item := range array {
				next := make([]T, len(product), len(product)+1)
				copy(next, product)
				appended = append(appended, append(next, item))
			}
		}
		results = appended
	}
	return results
}

// Divide returns the keys in one list and the values in another, aligned by
// position. The keys are sorted, because a map has no order of its own.
func Divide(array map[string]any) ([]string, []any) {
	keys := sortedKeys(array)
	values := make([]any, 0, len(keys))
	for _, k := range keys {
		values = append(values, array[k])
	}
	return keys, values
}

// Dot returns a nested map flattened into one level, its keys joined with dots
// and carrying the given prefix. Nested lists are walked too, keyed by their
// index. An empty nested map or list is kept as a value, not descended into.
func Dot(array map[string]any, prepend string) map[string]any {
	results := map[string]any{}
	dotInto(results, array, prepend)
	return results
}

func dotInto(results map[string]any, data any, prefix string) {
	switch d := data.(type) {
	case map[string]any:
		if len(d) == 0 {
			return
		}
		for _, key := range sortedKeys(d) {
			dotEntry(results, prefix+key, d[key])
		}
	case []any:
		if len(d) == 0 {
			return
		}
		for i, v := range d {
			dotEntry(results, prefix+strconv.Itoa(i), v)
		}
	}
}

func dotEntry(results map[string]any, newKey string, v any) {
	switch inner := v.(type) {
	case map[string]any:
		if len(inner) == 0 {
			results[newKey] = v
			return
		}
		dotInto(results, inner, newKey+".")
	case []any:
		if len(inner) == 0 {
			results[newKey] = v
			return
		}
		dotInto(results, inner, newKey+".")
	default:
		results[newKey] = v
	}
}

// Undot expands a dotted, flattened map back out into nested maps.
func Undot(array map[string]any) map[string]any {
	results := map[string]any{}
	for _, key := range sortedKeys(array) {
		Set(results, key, array[key])
	}
	return results
}

// Except returns everything but the given dotted keys. The map is copied
// first, so the original is left alone.
func Except(array map[string]any, keys ...string) map[string]any {
	result := cloneMap(array)
	Forget(result, keys...)
	return result
}

// ExceptValues returns everything but the given values, compared with ==.
func ExceptValues[T comparable](array []T, values ...T) []T {
	return rejectValues(array, values, false)
}

// OnlyValues returns only the given values, compared with ==.
func OnlyValues[T comparable](array []T, values ...T) []T {
	return rejectValues(array, values, true)
}

func rejectValues[T comparable](array []T, values []T, keep bool) []T {
	set := make(map[T]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	results := []T{}
	for _, v := range array {
		if _, found := set[v]; found == keep {
			results = append(results, v)
		}
	}
	return results
}

// Exists reports whether the exact key is present. It does not read dots;
// [Has] does.
func Exists(array map[string]any, key string) bool {
	if array == nil {
		return false
	}
	_, ok := array[key]
	return ok
}

// Forget removes one or many dotted keys. A map is a reference, so this
// mutates in place and returns nothing.
func Forget(array map[string]any, keys ...string) {
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		if Exists(array, key) {
			delete(array, key)
			continue
		}
		parts := strings.Split(key, ".")
		current := array
		descended := true
		for len(parts) > 1 {
			part := parts[0]
			parts = parts[1:]
			next, ok := current[part].(map[string]any)
			if !ok {
				descended = false
				break
			}
			current = next
		}
		if descended {
			delete(current, parts[0])
		}
	}
}

// From returns the underlying array of the given value. A map, a slice, an
// [Arrayer], a json.Marshaler or a struct all convert; a scalar, and nil, are
// an error.
//
// The result is map[string]any for keyed values and []any for lists, so the
// caller type-asserts on the shape it expects.
func From(items any) (any, error) {
	switch v := items.(type) {
	case nil:
		return nil, errors.New("arr: items cannot be represented by a scalar value")
	case map[string]any:
		return v, nil
	case []any:
		return v, nil
	case Arrayer:
		return v.ToArray(), nil
	}

	if m, ok := items.(json.Marshaler); ok {
		raw, err := m.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	rv := reflect.ValueOf(items)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, errors.New("arr: items cannot be represented by a scalar value")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		raw, err := json.Marshal(rv.Interface())
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, errors.New("arr: items cannot be represented by a scalar value")
	}
}

// Get reads a value by dotted key, falling back to the default. A default that
// is a func() any is invoked and its result returned.
//
// The empty key returns the whole map. A numeric segment indexes into a nested
// []any, so one path can cross both shapes.
func Get(array map[string]any, key string, def any) any {
	if array == nil {
		return value(def)
	}
	if key == "" {
		return array
	}
	if v, ok := array[key]; ok {
		return v
	}
	if !strings.Contains(key, ".") {
		return value(def)
	}

	var current any = array
	for _, segment := range strings.Split(key, ".") {
		next, ok := descend(current, segment)
		if !ok {
			return value(def)
		}
		current = next
	}
	return current
}

func descend(current any, segment string) (any, bool) {
	switch c := current.(type) {
	case map[string]any:
		v, ok := c[segment]
		return v, ok
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(c) {
			return nil, false
		}
		return c[i], true
	default:
		return nil, false
	}
}

// Has reports whether every one of the dotted keys is present. An empty map,
// or an empty key list, is false.
func Has(array map[string]any, keys ...string) bool {
	if len(array) == 0 || len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if Exists(array, key) {
			continue
		}
		var current any = array
		for _, segment := range strings.Split(key, ".") {
			next, ok := descend(current, segment)
			if !ok {
				return false
			}
			current = next
		}
	}
	return true
}

// HasAll reports whether every one of the dotted keys is present, which is
// what [Has] reports under a name that says so.
func HasAll(array map[string]any, keys ...string) bool {
	if len(array) == 0 || len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if !Has(array, key) {
			return false
		}
	}
	return true
}

// HasAny reports whether any one of the dotted keys is present. An empty map,
// or an empty key list, is false.
func HasAny(array map[string]any, keys ...string) bool {
	if len(array) == 0 || len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if Has(array, key) {
			return true
		}
	}
	return false
}

// IsAssoc reports whether the value is a map rather than a list.
func IsAssoc(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Map
}

// IsList reports whether the value is a slice or an array.
func IsList(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

// KeyBy keys a list by the string the callback returns for each item. Two
// items yielding the same key leave the later one.
func KeyBy[T any](array []T, keyBy func(T) string) map[string]T {
	results := make(map[string]T, len(array))
	for _, item := range array {
		results[keyBy(item)] = item
	}
	return results
}

// Only returns the subset of the map under the given exact keys. A key the map
// does not hold is skipped.
func Only(array map[string]any, keys ...string) map[string]any {
	results := map[string]any{}
	for _, key := range keys {
		if v, ok := array[key]; ok {
			results[key] = v
		}
	}
	return results
}

// PrependKeysWith returns a new map with every key given the prefix.
func PrependKeysWith(array map[string]any, prependWith string) map[string]any {
	results := make(map[string]any, len(array))
	for k, v := range array {
		results[prependWith+k] = v
	}
	return results
}

// Pull reads a dotted key and removes it, returning the value or the default.
// A map is a reference, so the removal is seen by every holder of it.
func Pull(array map[string]any, key string, def any) any {
	v := Get(array, key, def)
	Forget(array, key)
	return v
}

// Push appends values to the list living under a dotted key and returns the
// same map. A key holding something other than a []any is an error.
func Push(array map[string]any, key string, values ...any) (map[string]any, error) {
	target, err := Array(array, key, []any{})
	if err != nil {
		return nil, err
	}
	target = append(append([]any{}, target...), values...)
	return Set(array, key, target), nil
}

// Select keeps only the given keys of every item in the list.
func Select(array []map[string]any, keys ...string) []map[string]any {
	results := make([]map[string]any, 0, len(array))
	for _, item := range array {
		result := map[string]any{}
		for _, key := range keys {
			if v, ok := item[key]; ok {
				result[key] = v
			}
		}
		results = append(results, result)
	}
	return results
}

// Set writes a value under a dotted key, creating the levels that are missing,
// and returns the same map.
//
// A map cannot be replaced through its own reference, so an empty key is a
// no-op and the map comes back unchanged. A nil map is a no-op too.
func Set(array map[string]any, key string, v any) map[string]any {
	if array == nil || key == "" {
		return array
	}
	keys := strings.Split(key, ".")
	current := array
	for len(keys) > 1 {
		segment := keys[0]
		keys = keys[1:]
		next, ok := current[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[segment] = next
		}
		current = next
	}
	current[keys[0]] = v
	return array
}

func cloneMap(array map[string]any) map[string]any {
	result := make(map[string]any, len(array))
	for k, v := range array {
		result[k] = v
	}
	return result
}
