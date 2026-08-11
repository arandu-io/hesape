package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/collections"
)

// Repository mirrors Illuminate\Config\Repository: the dotted-key store behind
// config('app.name').
//
// It sits beside [App] rather than replacing it. [App] is what the framework
// reads -- a wrong field is a compile error there -- and this is what an
// application reads when the key is only known at runtime, which is what a
// Laravel developer types. A first-party package that reaches for a string key
// instead of a struct field is doing it wrong; that is the only rule about
// which of the two to use.
//
// Every method is safe for concurrent use. Illuminate has no lock because PHP
// has no shared process; a long-lived Go binary does, and a Set racing a Get is
// a crash rather than a stale read.
//
// Values are copied in and out. In PHP an array is a value, so
// config()->get('database') hands back a copy and mutating it changes nothing;
// a Go map is a reference, and returning the live one would hand a caller the
// ability to edit configuration by accident -- and to race the lock while doing
// it. [Repository.Get], [Repository.All] and the constructor deep-copy any
// map[string]any and []any they cross.
type Repository struct {
	mu    sync.RWMutex
	items map[string]any
}

// NewRepository answers to __construct: it creates a configuration repository
// over the given items.
//
// A nil map is the empty repository, as an omitted $items is in PHP. The items
// are deep-copied, so the caller's map is not the repository's map.
func NewRepository(items map[string]any) *Repository {
	copied, _ := clone(items).(map[string]any)
	if copied == nil {
		copied = map[string]any{}
	}
	return &Repository{items: copied}
}

// Has answers to has: it reports whether the given configuration value exists.
//
// It mirrors Arr::has. A key present with a nil value exists -- the question is
// about the key, not the value. An empty repository answers false to every key,
// including the empty one, which is what PHP's `if (! $array)` does.
func (r *Repository) Has(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.items) == 0 {
		return false
	}
	_, ok := lookup(r.items, key)
	return ok
}

// Get answers to get: it returns the specified configuration value.
//
// At most one default may be given, mirroring PHP's optional $default; with
// none, a missing key reads as nil. A default of func() any is called rather
// than returned, which is what value() does to a closure default in PHP.
//
// The array form of the PHP argument -- get(['a', 'b' => 1]) -- is
// [Repository.GetMany]. Go has no overload, and PHP dispatches to getMany for
// it anyway.
func (r *Repository) Get(key string, def ...any) any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if v, ok := lookup(r.items, key); ok {
		return clone(v)
	}
	return value(first(def))
}

// GetMany answers to getMany: it returns many configuration values at once.
//
// Each entry maps a key to the default used when that key is missing. PHP
// accepts the two shapes in one array -- a numeric entry is a bare key with a
// null default, a string entry is key => default -- so ['a', 'b' => 1] is
// written here as {"a": nil, "b": 1}. A nil default is called if it is a
// func() any, as everywhere else.
//
// The result is never nil, and it holds one entry per requested key.
func (r *Repository) GetMany(keys map[string]any) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]any, len(keys))
	for key, def := range keys {
		if v, ok := lookup(r.items, key); ok {
			out[key] = clone(v)
			continue
		}
		out[key] = value(def)
	}
	return out
}

// String answers to string: it returns the specified string configuration
// value.
//
// The InvalidArgumentException PHP throws when the value is not a string is the
// error. The check is is_string, so it is the type and not a conversion: an int
// is an error, not "42".
func (r *Repository) String(key string, def ...any) (string, error) {
	v := r.Get(key, def...)
	s, ok := v.(string)
	if !ok {
		return "", typeError(key, "a string", v)
	}
	return s, nil
}

// Integer answers to integer: it returns the specified integer configuration
// value.
//
// The InvalidArgumentException PHP throws when the value is not an integer is
// the error. PHP has one integer type and Go has ten, so every integer kind
// answers here; a float does not, because is_int rejects it in PHP too. A value
// too large for int is an error rather than a truncation.
func (r *Repository) Integer(key string, def ...any) (int, error) {
	v := r.Get(key, def...)
	switch t := v.(type) {
	case int:
		return t, nil
	case int8:
		return int(t), nil
	case int16:
		return int(t), nil
	case int32:
		return int(t), nil
	case int64:
		if t < math.MinInt || t > math.MaxInt {
			return 0, rangeError(key, v)
		}
		return int(t), nil
	case uint:
		if uint64(t) > math.MaxInt {
			return 0, rangeError(key, v)
		}
		return int(t), nil
	case uint8:
		return int(t), nil
	case uint16:
		return int(t), nil
	case uint32:
		if uint64(t) > math.MaxInt {
			return 0, rangeError(key, v)
		}
		return int(t), nil
	case uint64:
		if t > math.MaxInt {
			return 0, rangeError(key, v)
		}
		return int(t), nil
	default:
		return 0, typeError(key, "an integer", v)
	}
}

// Float answers to float: it returns the specified float configuration value.
//
// The InvalidArgumentException PHP throws when the value is not a float is the
// error. An integer is not a float, here as in is_float: config values are
// written by hand, and 0 where 0.0 was meant is worth a message.
func (r *Repository) Float(key string, def ...any) (float64, error) {
	v := r.Get(key, def...)
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	default:
		return 0, typeError(key, "a float", v)
	}
}

// Boolean answers to boolean: it returns the specified boolean configuration
// value.
//
// The InvalidArgumentException PHP throws when the value is not a boolean is
// the error. It is is_bool and not a cast, so "true" and 1 are errors.
func (r *Repository) Boolean(key string, def ...any) (bool, error) {
	v := r.Get(key, def...)
	b, ok := v.(bool)
	if !ok {
		return false, typeError(key, "a boolean", v)
	}
	return b, nil
}

// Array answers to array: it returns the specified array configuration value.
//
// The InvalidArgumentException PHP throws when the value is not an array is the
// error.
//
// A PHP array is both the list and the map, and a Go method returns one type.
// This is the list -- config('app.providers'), the shape array() and
// collection() were added for. A section with string keys is a map[string]any
// and is read with [Repository.Get] or by its sub-keys; asking for it here is
// an error that says so, rather than a silent conversion that would drop the
// keys.
func (r *Repository) Array(key string, def ...any) ([]any, error) {
	v := r.Get(key, def...)
	l, ok := v.([]any)
	if !ok {
		if _, assoc := v.(map[string]any); assoc {
			return nil, fmt.Errorf("configuration value for key [%s] is an array with string keys; read it with Get or by its sub-keys, because a Go slice has no string keys", key)
		}
		return nil, typeError(key, "an array", v)
	}
	return l, nil
}

// Collection answers to collection: it returns the specified array
// configuration value as a collection.
//
// Illuminate wraps the array in Illuminate\Support\Collection, and this wraps
// it in the mirror of that class, [collections.Collection]. It is
// [Repository.Array] plus the wrap, exactly as the PHP is: the error is the one
// array() raises, because collection() is `new Collection($this->array(...))`
// and a value that is not an array never reaches the constructor.
//
// collections.Collection[any] has []any as its underlying type, so a caller
// that only ranges over the result reads it as a list; a caller that wants
// Map, Filter or First has them without converting.
func (r *Repository) Collection(key string, def ...any) (collections.Collection[any], error) {
	list, err := r.Array(key, def...)
	if err != nil {
		return nil, err
	}
	return collections.Collection[any](list), nil
}

// Set answers to set: it sets a given configuration value.
//
// The key is a dotted path and the intermediate levels are created as needed,
// as Arr::set does. A level that exists but is not a map is replaced by one,
// which is PHP's `if (! is_array($array[$key])) $array[$key] = []`.
//
// The array form -- set(['a' => 1, 'b' => 2]) -- is one call per key. Go has no
// overload, and PHP's version is a loop over exactly this.
func (r *Repository) Set(key string, val any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	assign(r.items, key, clone(val))
}

// Prepend answers to prepend: it prepends a value onto an array configuration
// value.
//
// A missing key starts an empty list, as `$this->get($key, [])` does. A key
// that holds something other than a list is the error, because the
// array_unshift PHP reaches for next is a TypeError on anything else -- nil
// included, since a key present with a null value returns null and not the
// default.
func (r *Repository) Prepend(key string, val any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	list, err := listAt(r.items, key)
	if err != nil {
		return err
	}
	assign(r.items, key, append([]any{clone(val)}, list...))
	return nil
}

// Push answers to push: it pushes a value onto an array configuration value.
//
// It is [Repository.Prepend] at the other end, with the same rule about a key
// that is not a list.
func (r *Repository) Push(key string, val any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	list, err := listAt(r.items, key)
	if err != nil {
		return err
	}
	assign(r.items, key, append(list, clone(val)))
	return nil
}

// All answers to all: it returns all of the configuration items for the
// application.
//
// The result is a deep copy. PHP hands back a copy because an array is a value
// there; editing what this returns changes nothing, which is the same promise.
func (r *Repository) All() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all, _ := clone(r.items).(map[string]any)
	return all
}

// lookup resolves a dotted key against items, mirroring Arr::get and the
// existence half of Arr::has in one pass -- they walk the same path, and two
// copies of this walk would be two answers to "does app.name exist".
//
// The literal key wins over the path, as in PHP: a top-level item actually
// named "a.b" is found before "a" is descended into.
func lookup(items map[string]any, key string) (any, bool) {
	if v, ok := items[key]; ok {
		return v, true
	}
	if !strings.Contains(key, ".") {
		return nil, false
	}

	var current any = items
	for _, segment := range strings.Split(key, ".") {
		next, ok := index(current, segment)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// index reads one segment out of one level. A map is read by key and a list by
// position, because a PHP list is an array whose keys are 0, 1, 2 and
// config('app.providers.0') resolves there.
func index(container any, segment string) (any, bool) {
	switch t := container.(type) {
	case map[string]any:
		v, ok := t[segment]
		return v, ok
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(t) {
			return nil, false
		}
		return t[i], true
	default:
		return nil, false
	}
}

// assign writes val at a dotted key, mirroring Arr::set. Unlike lookup it does
// not try the literal key first: PHP explodes on "." unconditionally here, so
// Set("a.b", 1) always nests and never creates a top-level "a.b".
func assign(items map[string]any, key string, val any) {
	segments := strings.Split(key, ".")
	level := items
	for _, segment := range segments[:len(segments)-1] {
		next, ok := level[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			level[segment] = next
		}
		level = next
	}
	level[segments[len(segments)-1]] = val
}

// listAt returns the list stored at key for Prepend and Push, or the error they
// share. The result is a fresh slice: appending to the stored one could write
// into an array a caller still holds.
func listAt(items map[string]any, key string) ([]any, error) {
	v, ok := lookup(items, key)
	if !ok {
		return []any{}, nil
	}
	l, ok := v.([]any)
	if !ok {
		return nil, typeError(key, "an array", v)
	}
	return append(make([]any, 0, len(l)+1), l...), nil
}

// first returns the single optional default. More than one is a call PHP could
// not have written, and the rest are ignored rather than made into a panic on a
// read path.
func first(def []any) any {
	if len(def) == 0 {
		return nil
	}
	return def[0]
}

// value mirrors the global value(): a default that is a function is the result
// of calling it, and anything else is itself. It is how PHP lets
// string($key, fn () => expensive()) skip the work when the key is present.
func value(v any) any {
	if f, ok := v.(func() any); ok {
		return f()
	}
	return v
}

// clone deep-copies the containers a configuration tree is made of and returns
// anything else as it stands. Scalars are values in Go already; maps and slices
// are not, and handing out a live one is how a read turns into a write.
func clone(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = clone(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = clone(item)
		}
		return out
	default:
		return v
	}
}

// typeError is the InvalidArgumentException the typed readers throw, with the
// same sentence and the same gettype name for the value.
func typeError(key, want string, v any) error {
	return fmt.Errorf("configuration value for key [%s] must be %s, %s given", key, want, phpType(v))
}

// rangeError reports a value that is an integer but does not fit in an int.
// PHP has no such case, having one integer width; Go does, and truncating
// silently would be a port number that is not the one in the file.
func rangeError(key string, v any) error {
	return fmt.Errorf("configuration value for key [%s] is out of range for int: %v", key, v)
}

// phpType names a value the way gettype does, so the message a Go developer
// reads is the message the Laravel documentation quotes.
func phpType(v any) string {
	switch v.(type) {
	case nil:
		return "NULL"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float32, float64:
		return "double"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr:
		return "integer"
	case []any, map[string]any:
		return "array"
	default:
		return "object"
	}
}
