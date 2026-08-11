package arr

import (
	"fmt"
	"reflect"
)

// The five readers of Arr.php that assert a type: array, boolean, float,
// integer and string. Each one reads a "dot" path through Arr::get and throws
// InvalidArgumentException when what it finds is of the wrong type. Here they
// return that as ErrInvalidArgument, and the message keeps the PHP's shape --
// the PHP prints gettype($value), Go prints the Go type, which is the same
// sentence with a more precise noun.
//
// Each takes the PHP's optional $default as a variadic, which is how Go spells
// a default argument. Only the first is used. With no default and no value at
// the key, the PHP asks is_string(null) and throws; so does this.

// Array answers to Arr::array: the list at the "dot" path.
//
// One divergence, stated because it is the only place a Go slice cannot stand
// in for a PHP array: where the value is an associative array, is_array() is
// true in PHP and the map is handed back, but a []any cannot carry keys. That
// case reports ErrInvalidArgument; Get returns the map itself.
func Array(array any, key string, def ...[]any) ([]any, error) {
	value, ok := Get(array, key)
	if !ok {
		if len(def) > 0 {
			return def[0], nil
		}
		return nil, typeError(key, "an array", nil)
	}
	if rv, ok := container(value); ok && rv.Kind() != reflect.Map {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	}
	return nil, typeError(key, "an array", value)
}

// Boolean answers to Arr::boolean: the bool at the "dot" path.
func Boolean(array any, key string, def ...bool) (bool, error) {
	value, ok := Get(array, key)
	if !ok {
		if len(def) > 0 {
			return def[0], nil
		}
		return false, typeError(key, "a boolean", nil)
	}
	if b, ok := value.(bool); ok {
		return b, nil
	}
	return false, typeError(key, "a boolean", value)
}

// Float answers to Arr::float: the float at the "dot" path.
//
// The PHP asks is_float, which is false for an integer, and so is this: a value
// stored as an int is not a float. Both Go float widths answer to the one PHP
// float.
func Float(array any, key string, def ...float64) (float64, error) {
	value, ok := Get(array, key)
	if !ok {
		if len(def) > 0 {
			return def[0], nil
		}
		return 0, typeError(key, "a float", nil)
	}
	switch number := value.(type) {
	case float64:
		return number, nil
	case float32:
		return float64(number), nil
	}
	return 0, typeError(key, "a float", value)
}

// Integer answers to Arr::integer: the int at the "dot" path.
//
// The PHP asks is_int, which is false for a float. Go's several integer widths
// all answer to the one PHP integer, so any of them is accepted and returned as
// an int.
func Integer(array any, key string, def ...int) (int, error) {
	value, ok := Get(array, key)
	if !ok {
		if len(def) > 0 {
			return def[0], nil
		}
		return 0, typeError(key, "an integer", nil)
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(rv.Uint()), nil
	}
	return 0, typeError(key, "an integer", value)
}

// String answers to Arr::string: the string at the "dot" path.
func String(array any, key string, def ...string) (string, error) {
	value, ok := Get(array, key)
	if !ok {
		if len(def) > 0 {
			return def[0], nil
		}
		return "", typeError(key, "a string", nil)
	}
	if s, ok := value.(string); ok {
		return s, nil
	}
	return "", typeError(key, "a string", value)
}

// typeError builds the sentence Arr.php builds: "Array value for key [%s] must
// be %s, %s found." The PHP names the type with gettype; Go names it with the
// type itself, and calls an absent value nil where the PHP calls it NULL.
func typeError(key, want string, got any) error {
	if got == nil {
		return fmt.Errorf("%w: array value for key [%s] must be %s, nil found", ErrInvalidArgument, key, want)
	}
	return fmt.Errorf("%w: array value for key [%s] must be %s, %T found", ErrInvalidArgument, key, want, got)
}
