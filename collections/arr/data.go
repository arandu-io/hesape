package arr

import (
	"reflect"
	"strconv"
	"strings"
)

// The five global helpers of helpers.php that walk a "dot" path: data_get,
// data_has, data_set, data_fill and data_forget. They are here rather than in
// the parent package because each is Arr::get, Arr::set or Arr::forget with
// wildcard segments added, and they read a structure with the same primitives.
//
// A PHP global function is lower_snake_case and a Go exported identifier is
// PascalCase, so data_get is DataGet. That is the same "capitalise to export"
// the rest of this package applies, carried across the underscore.
//
// One behaviour differs from Arr::get and is easy to trip over: data_get always
// splits on dots. Arr::get tries the whole key first, so a key that itself
// contains a dot is found; data_get never looks for one.

// DataGet answers to helpers.php's data_get: read a value out of nested arrays
// and structs with a "dot" path, wildcards included.
//
// The segments the PHP gives a meaning to are all here. A segment of "*" reads
// every element at this level and gathers them into a []any; "{first}" and
// "{last}" resolve to the first and the last key at this level; and a backslash
// in front of any of the three -- "\*", "\{first}", "\{last}" -- names that
// literal key instead.
//
// Two wildcards in one path collapse one level, as the PHP's
// in_array('*', $key) check does: "users.*.roles.*.name" is a flat list of
// names, not a list of lists.
//
// The second result is false where the PHP returns its $default. Inside a
// wildcard a segment that misses contributes nil to the result rather than
// failing the whole read, which is what the PHP's inner data_get($item, $key)
// with a null default does.
//
// A struct field of the matching name answers for the PHP's object property.
// The first and last key of a Go map are its keys in ascending order, since a
// map has no insertion order for array_key_first to read.
func DataGet(target any, key string) (any, bool) {
	return dataGet(target, strings.Split(key, "."))
}

func dataGet(target any, segments []string) (any, bool) {
	for i, segment := range segments {
		remaining := segments[i+1:]

		if segment == "*" {
			items, ok := elements(target)
			if !ok {
				return nil, false
			}
			result := make([]any, 0, len(items))
			for _, item := range items {
				value, _ := dataGet(item, remaining)
				result = append(result, value)
			}
			for _, rest := range remaining {
				if rest == "*" {
					return Collapse(result), true
				}
			}
			return result, true
		}

		resolved := segment
		switch segment {
		case `\*`:
			resolved = "*"
		case `\{first}`:
			resolved = "{first}"
		case `\{last}`:
			resolved = "{last}"
		case "{first}", "{last}":
			from, err := From(target)
			if err != nil {
				return nil, false
			}
			names := keys(from)
			if len(names) == 0 {
				return nil, false
			}
			resolved = names[0]
			if segment == "{last}" {
				resolved = names[len(names)-1]
			}
		}

		if value, ok := index(target, resolved); ok {
			target = value
			continue
		}
		if value, ok := field(target, resolved); ok {
			target = value
			continue
		}
		return nil, false
	}
	return target, true
}

// DataHas answers to helpers.php's data_has: the "dot" path leads somewhere.
//
// The PHP gives no meaning to a wildcard here -- data_has walks plain segments
// only -- and neither does this. An empty key reports false, as the PHP's
// `is_null($key) || $key === []` guard does.
func DataHas(target any, key string) bool {
	if key == "" {
		return false
	}
	for _, segment := range strings.Split(key, ".") {
		if value, ok := index(target, segment); ok {
			target = value
			continue
		}
		if value, ok := field(target, segment); ok {
			target = value
			continue
		}
		return false
	}
	return true
}

// DataSet answers to helpers.php's data_set: write a value at a "dot" path,
// creating the maps along the way, with "*" writing to every element at that
// level.
//
// The PHP takes the target by reference. A Go map or slice is already a
// reference and is written through, but a target that was not an array has to
// be created, and the result carries that back -- so the return value is the
// one to keep.
//
// The optional overwrite is the PHP's fourth argument, true by default. Only
// the first is used; the variadic is how Go spells a PHP default argument.
// DataFill is this with it set to false.
//
// Two divergences, both from a Go slice being fixed in length. A segment naming
// a position past the end of a []any turns that list into a keyed array in PHP;
// here the write is dropped. And PHP writes into an object's properties, which
// Go can only do through an addressable struct, so a struct on the path is left
// alone rather than half-written.
func DataSet(target any, key string, value any, overwrite ...bool) any {
	over := true
	if len(overwrite) > 0 {
		over = overwrite[0]
	}
	return dataSet(target, strings.Split(key, "."), value, over)
}

func dataSet(target any, segments []string, value any, overwrite bool) any {
	segment, rest := segments[0], segments[1:]

	if segment == "*" {
		if !Accessible(target) {
			target = map[string]any{}
		}
		for _, name := range keys(target) {
			inner, _ := index(target, name)
			switch {
			case len(rest) > 0:
				writeChild(target, name, dataSet(inner, rest, value, overwrite))
			case overwrite:
				writeChild(target, name, value)
			}
		}
		return target
	}

	if Accessible(target) {
		switch {
		case len(rest) > 0:
			inner, ok := index(target, segment)
			if !ok {
				inner = map[string]any{}
			}
			writeChild(target, segment, dataSet(inner, rest, value, overwrite))
		case overwrite || !Exists(target, segment):
			writeChild(target, segment, value)
		}
		return target
	}

	fresh := map[string]any{}
	switch {
	case len(rest) > 0:
		fresh[segment] = dataSet(map[string]any{}, rest, value, overwrite)
	case overwrite:
		fresh[segment] = value
	}
	return fresh
}

// DataFill answers to helpers.php's data_fill: DataSet that does not overwrite
// what is already there.
func DataFill(target any, key string, value any) any {
	return DataSet(target, key, value, false)
}

// DataForget answers to helpers.php's data_forget: remove what sits at a "dot"
// path, with "*" removing it from every element at that level.
//
// The PHP takes the target by reference; the result is the one to keep, for the
// reason DataSet's is. Removing a position from a []any closes the gap, where
// PHP's unset leaves the other positions under the keys they had.
func DataForget(target any, key string) any {
	return dataForget(target, strings.Split(key, "."))
}

func dataForget(target any, segments []string) any {
	segment, rest := segments[0], segments[1:]

	if segment == "*" && Accessible(target) {
		if len(rest) > 0 {
			for _, name := range keys(target) {
				inner, _ := index(target, name)
				writeChild(target, name, dataForget(inner, rest))
			}
		}
		return target
	}

	if Accessible(target) {
		if len(rest) > 0 {
			if inner, ok := index(target, segment); ok {
				writeChild(target, segment, dataForget(inner, rest))
			}
			return target
		}
		return removeChild(target, segment)
	}
	return target
}

// removeChild deletes one key from one container and hands back the container.
// A map is written through; a slice cannot be shortened in place, so a new one
// is returned and the caller writes it into its parent.
func removeChild(node any, segment string) any {
	if mapped, ok := node.(map[string]any); ok {
		delete(mapped, segment)
		return node
	}
	rv, ok := container(node)
	if !ok {
		return node
	}
	if rv.Kind() == reflect.Map {
		if key, ok := mapKey(segment, rv.Type().Key()); ok {
			rv.SetMapIndex(key, reflect.Value{})
		}
		return node
	}
	i, err := strconv.Atoi(segment)
	if err != nil || i < 0 || i >= rv.Len() {
		return node
	}
	shortened := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), 0, rv.Len()-1)
	for position := 0; position < rv.Len(); position++ {
		if position != i {
			shortened = reflect.Append(shortened, rv.Index(position))
		}
	}
	return shortened.Interface()
}
