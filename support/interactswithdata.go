package support

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/support/arr"
)

// dataSource is Illuminate\Support\Traits\InteractsWithData. The PHP trait
// declares all() and data() abstract and the using class fills them in; Go has
// no traits, so the two accessors are function fields and the type is embedded.
//
// Every method below is promoted onto [Fluent], [UriQueryString] and
// [ValidatedInput] under the name the PHP trait gives it, which is the name a
// Laravel application types.
type dataSource struct {
	allFn  func(keys ...string) map[string]any
	dataFn func(key string, def any) any
}

// Data answers to the protected data() of InteractsWithData: one value out of
// the instance, by dotted key. It is exported because Go has no protected, and
// because [Enum] and [Enums] have to reach it from outside the type.
func (d dataSource) Data(key string, def any) any {
	if d.dataFn == nil {
		return value(def)
	}
	return d.dataFn(key, def)
}

func (d dataSource) allData(keys ...string) map[string]any {
	if d.allFn == nil {
		return map[string]any{}
	}
	return d.allFn(keys...)
}

// Exists answers to InteractsWithData::exists, which is has() under its older
// name.
func (d dataSource) Exists(keys ...string) bool { return d.Has(keys...) }

// Has answers to InteractsWithData::has: every one of the keys is present.
func (d dataSource) Has(keys ...string) bool {
	if len(keys) == 0 {
		return false
	}
	data := d.allData()
	for _, key := range keys {
		if !arr.Has(data, key) {
			return false
		}
	}
	return true
}

// HasAny answers to InteractsWithData::hasAny: at least one key is present.
func (d dataSource) HasAny(keys ...string) bool {
	return arr.HasAny(d.allData(), keys...)
}

// Missing answers to InteractsWithData::missing.
func (d dataSource) Missing(keys ...string) bool { return !d.Has(keys...) }

// Filled answers to InteractsWithData::filled: every key holds something that
// is not an empty string. A bool and a list count as filled even when empty,
// the way the PHP isEmptyString does.
func (d dataSource) Filled(keys ...string) bool {
	for _, key := range keys {
		if d.isEmptyString(key) {
			return false
		}
	}
	return true
}

// IsNotFilled answers to InteractsWithData::isNotFilled: every key is empty.
func (d dataSource) IsNotFilled(keys ...string) bool {
	for _, key := range keys {
		if !d.isEmptyString(key) {
			return false
		}
	}
	return true
}

// AnyFilled answers to InteractsWithData::anyFilled.
func (d dataSource) AnyFilled(keys ...string) bool {
	for _, key := range keys {
		if d.Filled(key) {
			return true
		}
	}
	return false
}

func (d dataSource) isEmptyString(key string) bool {
	v := d.Data(key, nil)
	switch v.(type) {
	case bool:
		return false
	case []any, map[string]any:
		return false
	}
	return strings.TrimSpace(toString(v)) == ""
}

// WhenHas answers to InteractsWithData::whenHas: run the callback with the
// value when the key is present, otherwise the fallback.
//
// The PHP returns the callback's value or $this so the call can be chained; an
// embedded Go struct cannot reach the outer instance, so this returns nothing.
func (d dataSource) WhenHas(key string, callback func(value any), def ...func()) {
	if d.Has(key) {
		callback(arr.Get(d.allData(), key, nil))
		return
	}
	if len(def) > 0 && def[0] != nil {
		def[0]()
	}
}

// WhenFilled answers to InteractsWithData::whenFilled. It returns nothing for
// the reason given on [dataSource.WhenHas].
func (d dataSource) WhenFilled(key string, callback func(value any), def ...func()) {
	if d.Filled(key) {
		callback(arr.Get(d.allData(), key, nil))
		return
	}
	if len(def) > 0 && def[0] != nil {
		def[0]()
	}
}

// WhenMissing answers to InteractsWithData::whenMissing. It returns nothing for
// the reason given on [dataSource.WhenHas].
func (d dataSource) WhenMissing(key string, callback func(value any), def ...func()) {
	if d.Missing(key) {
		callback(arr.Get(d.allData(), key, nil))
		return
	}
	if len(def) > 0 && def[0] != nil {
		def[0]()
	}
}

// Str answers to InteractsWithData::str, the older spelling of [dataSource.String].
//
// The PHP hands back a Stringable; here it is the string itself, because
// Stringable is the str package's type and this one carries no dependency on it.
func (d dataSource) Str(key string, def ...any) string { return d.String(key, def...) }

// String answers to InteractsWithData::string. See [dataSource.Str] on the
// missing Stringable.
func (d dataSource) String(key string, def ...any) string {
	return toString(d.Data(key, firstOr(def, nil)))
}

// Boolean answers to InteractsWithData::boolean, which is PHP's
// FILTER_VALIDATE_BOOLEAN: "1", "true", "on" and "yes" are true, in any case,
// and everything else is false.
func (d dataSource) Boolean(key string, def ...bool) bool {
	var fallback any
	if len(def) > 0 {
		fallback = def[0]
	} else {
		fallback = false
	}
	return toBool(d.Data(key, fallback))
}

// Integer answers to InteractsWithData::integer, which is PHP's intval: the
// leading run of digits, or zero.
func (d dataSource) Integer(key string, def ...int) int {
	var fallback any
	if len(def) > 0 {
		fallback = def[0]
	} else {
		fallback = 0
	}
	return toInt(d.Data(key, fallback))
}

// Float answers to InteractsWithData::float, which is PHP's floatval: the
// leading numeric run, or zero.
func (d dataSource) Float(key string, def ...float64) float64 {
	var fallback any
	if len(def) > 0 {
		fallback = def[0]
	} else {
		fallback = 0.0
	}
	return toFloat(d.Data(key, fallback))
}

// Date answers to InteractsWithData::date. The variadic argument stands for the
// PHP $format and $tz, in that order.
//
// A key that is not filled gives the zero time and no error, which is the PHP
// null. A value that cannot be read gives the error the PHP throws as
// InvalidFormatException. The format is a Go layout, not a PHP one.
func (d dataSource) Date(key string, formatAndLocation ...string) (time.Time, error) {
	if d.IsNotFilled(key) {
		return time.Time{}, nil
	}
	raw := toString(d.Data(key, nil))
	format := firstOr(formatAndLocation, "")
	loc := time.Local
	if len(formatAndLocation) > 1 && formatAndLocation[1] != "" {
		parsed, err := time.LoadLocation(formatAndLocation[1])
		if err != nil {
			return time.Time{}, err
		}
		loc = parsed
	}
	if format == "" {
		parsed, err := Parse(raw)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.In(loc), nil
	}
	return time.ParseInLocation(format, raw, loc)
}

// Array answers to InteractsWithData::array: the value at the key as a list.
//
// The PHP also accepts an array of keys, in which case it is only(); in Go that
// call is [dataSource.Only], because the two shapes cannot share a return type.
func (d dataSource) Array(key string) []any {
	return arr.Wrap(d.Data(key, nil))
}

// Collect answers to InteractsWithData::collect: the value at the key as a
// list. A Collection in the collections package is a slice, so this is it.
func (d dataSource) Collect(key string) []any { return d.Array(key) }

// Only answers to InteractsWithData::only: the subset under the given dotted
// keys, with the keys that are absent left out.
func (d dataSource) Only(keys ...string) map[string]any {
	results := map[string]any{}
	data := d.allData()
	missing := &struct{}{}
	for _, key := range keys {
		v := arr.Get(data, key, missing)
		if v != any(missing) {
			arr.Set(results, key, v)
		}
	}
	return results
}

// Except answers to InteractsWithData::except: everything but the given dotted
// keys.
//
// The PHP copies the array on write; a Go map is a reference, so the copy is
// made here and the instance is left alone.
func (d dataSource) Except(keys ...string) map[string]any {
	results := arr.Except(d.allData())
	arr.Forget(results, keys...)
	return results
}

// Enum answers to InteractsWithData::enum: the value at the key as one of the
// enum's cases, or the zero value and false when it is not one of them.
//
// The PHP reads the cases off the enum class and calls tryFrom. Go has no enum
// type and no way to list the cases of a named string or int type, so the cases
// are the third argument. It is a function and not a method because a Go method
// cannot take a type parameter.
func Enum[T ~string | ~int](d dataSource, key string, cases []T) (T, bool) {
	var zero T
	if d.IsNotFilled(key) {
		return zero, false
	}
	raw := d.Data(key, nil)
	for _, c := range cases {
		if matchesEnumCase(c, raw) {
			return c, true
		}
	}
	return zero, false
}

// Enums answers to InteractsWithData::enums: every value under the key that is
// one of the enum's cases. See [Enum] on why the cases are an argument.
func Enums[T ~string | ~int](d dataSource, key string, cases []T) []T {
	results := []T{}
	if d.IsNotFilled(key) {
		return results
	}
	for _, raw := range d.Array(key) {
		for _, c := range cases {
			if matchesEnumCase(c, raw) {
				results = append(results, c)
				break
			}
		}
	}
	return results
}

// matchesEnumCase is what BackedEnum::tryFrom does: compare the raw value
// against the case's backing value, string against string and int against int.
// The kind is read with reflect because a named type over string does not
// satisfy a type switch on string.
func matchesEnumCase[T ~string | ~int](c T, raw any) bool {
	rv := reflect.ValueOf(c)
	if rv.Kind() == reflect.String {
		return toString(raw) == rv.String()
	}
	return int64(toInt(raw)) == rv.Int()
}

// toString is PHP's (string) cast, narrowed to the shapes a Go value takes.
func toString(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "1"
		}
		return ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case fmtStringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

type fmtStringer interface{ String() string }

// toBool is PHP's FILTER_VALIDATE_BOOLEAN.
func toBool(v any) bool {
	switch typed := v.(type) {
	case nil:
		return false
	case bool:
		return typed
	case int:
		return typed == 1
	case int64:
		return typed == 1
	case float64:
		return typed == 1
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "on", "yes":
			return true
		}
		return false
	default:
		return false
	}
}

// toInt is PHP's intval: the leading run of digits, zero when there is none.
func toInt(v any) int { return int(toFloat(v)) }

// toFloat is PHP's floatval.
func toFloat(v any) float64 {
	switch typed := v.(type) {
	case nil:
		return 0
	case bool:
		if typed {
			return 1
		}
		return 0
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float32:
		return float64(typed)
	case float64:
		return typed
	case string:
		return leadingFloat(typed)
	default:
		return leadingFloat(toString(v))
	}
}

func leadingFloat(s string) float64 {
	s = strings.TrimLeft(s, " \t\n\r")
	end := 0
	seenDigit, seenDot, seenExp := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
		case (c == '+' || c == '-') && (i == 0 || s[i-1] == 'e' || s[i-1] == 'E'):
		case c == '.' && !seenDot && !seenExp:
			seenDot = true
		case (c == 'e' || c == 'E') && seenDigit && !seenExp:
			seenExp = true
		default:
			i = len(s)
			continue
		}
		end = i + 1
	}
	if !seenDigit {
		return 0
	}
	parsed, err := strconv.ParseFloat(strings.TrimRight(s[:end], "eE+-"), 64)
	if err != nil {
		return 0
	}
	return parsed
}

// value is PHP's value() helper: a callable is invoked, everything else is
// itself.
func value(v any) any {
	if fn, ok := v.(func() any); ok {
		return fn()
	}
	return v
}

func firstOr[T any](values []T, def T) T {
	if len(values) > 0 {
		return values[0]
	}
	return def
}
