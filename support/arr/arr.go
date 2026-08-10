// Package arr is Illuminate\Support\Arr.
//
// Every exported function answers to the static method of the same name on the
// PHP class: Arr::get is [Get], Arr::sortRecursiveDesc is [SortRecursiveDesc].
// The names are the ecosystem's, not ours, and they are not translated.
//
// # The shape of a PHP array
//
// A PHP array is an ordered map that doubles as a list. Go splits the two:
// map[string]any is the keyed form, []T the list form. Each function takes the
// shape its PHP body actually walks -- Get, Set and Forget take the map, First,
// Where and Partition take the list -- and every doc comment says which.
//
// Go maps carry no insertion order. Where the PHP body produces order-dependent
// output from a keyed array ([Divide], [Query], [Dot] over nested lists) the Go
// body sorts the keys, so the result is stable across runs.
package arr

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ErrItemNotFound answers to Illuminate\Support\ItemNotFoundException, which
// Arr::sole throws when nothing matched.
var ErrItemNotFound = errors.New("arr: item not found")

// ErrMultipleItemsFound answers to Illuminate\Support\MultipleItemsFoundException,
// which Arr::sole throws when more than one item matched.
var ErrMultipleItemsFound = errors.New("arr: multiple items found")

// Arrayer is the Go form of Illuminate\Contracts\Support\Arrayable: a value
// that knows how to present itself as a keyed array.
type Arrayer interface {
	ToArray() map[string]any
}

// value mirrors the PHP value() helper: a closure default is invoked, every
// other default is returned unchanged.
func value(v any) any {
	if fn, ok := v.(func() any); ok {
		return fn()
	}
	return v
}

// Accessible answers to Arr::accessible. It reports whether the value can be
// indexed at all -- in PHP an array or an ArrayAccess, in Go a map or a slice.
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

// Arrayable answers to Arr::arrayable. It reports whether the value can be
// turned into an array: a map, a slice, an [Arrayer] or a json.Marshaler,
// which are the Go counterparts of PHP's Arrayable, Traversable, Jsonable and
// JsonSerializable.
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

// Add answers to Arr::add. It writes the value under the dotted key only when
// nothing is there yet, and returns the same map.
func Add(array map[string]any, key string, v any) map[string]any {
	if Get(array, key, nil) == nil {
		return Set(array, key, v)
	}
	return array
}

// Array answers to Arr::array: read a dotted key and require it to be a list.
// The PHP throws InvalidArgumentException; the Go returns the error.
//
// A nil default is PHP's null default, which fails the type check unless the
// key is present.
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

// Boolean answers to Arr::boolean: read a dotted key and require it to be a
// bool. The PHP throws InvalidArgumentException; the Go returns the error.
//
// The default is a pointer so that nil can be PHP's null default, which fails
// the type check unless the key is present.
func Boolean(array map[string]any, key string, def *bool) (bool, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("array value for key [%s] must be a boolean, %s found", key, typeName(raw))
	}
	return v, nil
}

// Float answers to Arr::float: read a dotted key and require it to be a float.
// The PHP throws InvalidArgumentException; the Go returns the error.
func Float(array map[string]any, key string, def *float64) (float64, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("array value for key [%s] must be a float, %s found", key, typeName(raw))
	}
	return v, nil
}

// Integer answers to Arr::integer: read a dotted key and require it to be an
// int. The PHP throws InvalidArgumentException; the Go returns the error.
func Integer(array map[string]any, key string, def *int) (int, error) {
	raw := getWithPointerDefault(array, key, def)
	v, ok := raw.(int)
	if !ok {
		return 0, fmt.Errorf("array value for key [%s] must be an integer, %s found", key, typeName(raw))
	}
	return v, nil
}

// String answers to Arr::string: read a dotted key and require it to be a
// string. The PHP throws InvalidArgumentException; the Go returns the error.
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

// typeName is the Go answer to PHP's gettype() inside the type-check messages.
func typeName(v any) string {
	if v == nil {
		return "NULL"
	}
	return reflect.TypeOf(v).String()
}

// Collapse answers to Arr::collapse: one flat list out of a list of lists.
func Collapse[T any](array [][]T) []T {
	results := []T{}
	for _, values := range array {
		results = append(results, values...)
	}
	return results
}

// CrossJoin answers to Arr::crossJoin: every permutation of the given lists,
// one list per permutation, in the order the PHP produces them.
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

// Divide answers to Arr::divide: the keys in one list, the values in another.
// The keys are sorted, because a Go map has no order of its own.
func Divide(array map[string]any) ([]string, []any) {
	keys := sortedKeys(array)
	values := make([]any, 0, len(keys))
	for _, k := range keys {
		values = append(values, array[k])
	}
	return keys, values
}

// Dot answers to Arr::dot: a nested array flattened into one level whose keys
// are joined with dots. Nested lists are walked too, keyed by their index, as
// the PHP does. An empty nested array is kept as a value, not descended into.
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

// Undot answers to Arr::undot: a dotted, flattened array expanded back out.
func Undot(array map[string]any) map[string]any {
	results := map[string]any{}
	for _, key := range sortedKeys(array) {
		Set(results, key, array[key])
	}
	return results
}

// Except answers to Arr::except: everything but the given dotted keys. The
// PHP copies the array before forgetting; so does this.
func Except(array map[string]any, keys ...string) map[string]any {
	result := cloneMap(array)
	Forget(result, keys...)
	return result
}

// ExceptValues answers to Arr::exceptValues: everything but the given values.
//
// The PHP $strict flag chooses between loose and strict comparison; Go compares
// only strictly, so there is no flag to pass.
func ExceptValues[T comparable](array []T, values ...T) []T {
	return rejectValues(array, values, false)
}

// OnlyValues answers to Arr::onlyValues: only the given values.
//
// The PHP $strict flag chooses between loose and strict comparison; Go compares
// only strictly, so there is no flag to pass.
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

// Exists answers to Arr::exists: whether the exact key is present. It does not
// read dots; [Has] does.
func Exists(array map[string]any, key string) bool {
	if array == nil {
		return false
	}
	_, ok := array[key]
	return ok
}

// Forget answers to Arr::forget: remove one or many dotted keys. The PHP takes
// the array by reference and returns nothing; a Go map is already a reference,
// so this mutates in place and returns nothing too.
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

// From answers to Arr::from: the underlying array of the given value. A map, a
// slice, an [Arrayer], a json.Marshaler or a struct all convert; a scalar is
// the error the PHP throws as InvalidArgumentException.
//
// The result is map[string]any for keyed values and []any for lists, which is
// the one place a PHP array's double life survives into Go.
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

// Get answers to Arr::get: read a value by dotted key, falling back to the
// default. A default that is a func() any is invoked, as PHP's value() does.
//
// The empty key is PHP's null key and returns the whole array. A numeric
// segment indexes into a nested list, because a PHP array is both shapes.
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

// Has answers to Arr::has: whether every one of the dotted keys is present.
// An empty array or an empty key list is false, as it is in PHP.
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

// HasAll answers to Arr::hasAll: whether every one of the dotted keys is
// present. The PHP keeps it beside has() as the explicit spelling.
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

// HasAny answers to Arr::hasAny: whether any one of the dotted keys is present.
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

// IsAssoc answers to Arr::isAssoc: whether the value is a keyed array rather
// than a list. In Go that is the question of whether it is a map.
func IsAssoc(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Map
}

// IsList answers to Arr::isList: whether the value is a list. In Go that is
// the question of whether it is a slice or an array.
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

// KeyBy answers to Arr::keyBy: key a list by the value the callback returns.
// The PHP also accepts a field name, which reaches the field through data_get;
// in Go the callback is the one spelling.
func KeyBy[T any](array []T, keyBy func(T) string) map[string]T {
	results := make(map[string]T, len(array))
	for _, item := range array {
		results[keyBy(item)] = item
	}
	return results
}

// Only answers to Arr::only: the subset of the array under the given keys.
func Only(array map[string]any, keys ...string) map[string]any {
	results := map[string]any{}
	for _, key := range keys {
		if v, ok := array[key]; ok {
			results[key] = v
		}
	}
	return results
}

// PrependKeysWith answers to Arr::prependKeysWith: every key given a prefix.
func PrependKeysWith(array map[string]any, prependWith string) map[string]any {
	results := make(map[string]any, len(array))
	for k, v := range array {
		results[prependWith+k] = v
	}
	return results
}

// Pull answers to Arr::pull: read a dotted key and remove it. The PHP takes
// the array by reference; a Go map is already a reference.
func Pull(array map[string]any, key string, def any) any {
	v := Get(array, key, def)
	Forget(array, key)
	return v
}

// Push answers to Arr::push: append values to the list living under a dotted
// key. The PHP throws when the key does not hold an array; the Go returns the
// error.
func Push(array map[string]any, key string, values ...any) (map[string]any, error) {
	target, err := Array(array, key, []any{})
	if err != nil {
		return nil, err
	}
	target = append(append([]any{}, target...), values...)
	return Set(array, key, target), nil
}

// Select answers to Arr::select: keep only the given keys of every item.
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

// Set answers to Arr::set: write a value under a dotted key, creating the
// levels that are missing. The PHP takes the array by reference and returns it;
// a Go map is already a reference, and the same map comes back.
//
// The empty key is PHP's null key, which replaces the whole array; a Go map
// cannot be replaced through its own reference, so the empty key is a no-op and
// the map is returned unchanged.
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
