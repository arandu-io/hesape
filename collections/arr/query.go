package arr

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Query answers to Arr::query: the array rendered as a query string.
//
// The PHP calls http_build_query with PHP_QUERY_RFC3986, so the separator is
// "&", a nested array becomes name[key] and both sides are raw-url-encoded --
// a space is %20 and never a plus, and "-_.~" are left alone. A nil value is
// dropped, as http_build_query drops null. A bool is "1" or "0", which is what
// it casts one to.
//
// The pairs come out in ascending key order. PHP emits them in insertion order,
// which a Go map does not have.
func Query(array map[string]any) string {
	return strings.Join(queryPairs("", array), "&")
}

func queryPairs(prefix string, node any) []string {
	out := []string{}
	for _, key := range keys(node) {
		value, ok := index(node, key)
		if !ok || value == nil {
			continue
		}
		name := key
		if prefix != "" {
			name = prefix + "[" + key + "]"
		}
		if _, nested := container(value); nested {
			out = append(out, queryPairs(name, value)...)
			continue
		}
		out = append(out, rawURLEncode(name)+"="+rawURLEncode(queryValue(value)))
	}
	return out
}

// queryValue renders a scalar the way http_build_query casts one to a string.
func queryValue(value any) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "1"
		}
		return "0"
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	default:
		return keyString(value)
	}
}

// rawURLEncode is PHP's rawurlencode: percent-encode everything but the
// unreserved set of RFC 3986. It is written out because net/url's escapers
// answer to different rules -- QueryEscape turns a space into a plus.
func rawURLEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// ToCssClasses answers to Arr::toCssClasses: a class list built from entries
// that are either unconditional or conditional.
//
// The PHP reads one array where a numeric key means "always" and a string key
// means "when the value is truthy". A Go map cannot hold both shapes at once,
// and it has no order for the classes to come out in, so each argument is one
// entry and it is either:
//
//	a string          the class, always
//	map[string]bool   the keys whose value is true, in ascending key order
//
// A bare string argument is a single class, which is what the PHP's
// Arr::wrap($array) makes of one.
func ToCssClasses(array ...any) string {
	return strings.Join(conditionalList(array, ""), " ")
}

// ToCssStyles answers to Arr::toCssStyles.
//
// It is ToCssClasses with every entry finished with a semicolon, which is what
// the PHP's Str::finish($class, ';') does -- an entry that already ends in one
// does not get a second.
func ToCssStyles(array ...any) string {
	return strings.Join(conditionalList(array, ";"), " ")
}

func conditionalList(entries []any, suffix string) []string {
	out := []string{}
	for _, entry := range entries {
		switch typed := entry.(type) {
		case string:
			out = append(out, finish(typed, suffix))
		case map[string]bool:
			names := make([]string, 0, len(typed))
			for name := range typed {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if typed[name] {
					out = append(out, finish(name, suffix))
				}
			}
		case nil:
			continue
		default:
			out = append(out, finish(keyString(entry), suffix))
		}
	}
	return out
}

// finish is Str::finish: end the string with cap, unless it already does.
func finish(s, cap string) string {
	if cap == "" || strings.HasSuffix(s, cap) {
		return s
	}
	return s + cap
}

// SortRecursive answers to Arr::sortRecursive: every nested list sorted, all
// the way down.
//
// The PHP sorts a list with sort() and an associative array with ksort(), then
// recurses. A Go map holds no order for ksort to establish, so a map is walked
// and rebuilt with its values sorted and its keys left as they are -- which is
// the whole of what "sorted" can mean for a map. Lists are sorted.
//
// The result is built fresh, so the argument is untouched, as it is in PHP
// where the array arrives by value. It is a map[string]any or a []any whatever
// the concrete types going in were, because the recursion has to hold both.
//
// The optional descending is the PHP's third argument. The PHP's second, the
// SORT_REGULAR flag set, is a PHP sort() flag with nothing to name in Go;
// comparison here is numeric between numbers, lexicographic between strings,
// false before true between bools, and otherwise by that same order of kinds.
func SortRecursive(array any, descending ...bool) any {
	desc := false
	if len(descending) > 0 {
		desc = descending[0]
	}
	return sortRecursive(array, desc)
}

// SortRecursiveDesc answers to Arr::sortRecursiveDesc: SortRecursive with the
// order reversed.
func SortRecursiveDesc(array any) any { return SortRecursive(array, true) }

func sortRecursive(value any, descending bool) any {
	rv, ok := container(value)
	if !ok {
		return value
	}
	if rv.Kind() == reflect.Map {
		out := make(map[string]any, rv.Len())
		for _, key := range keys(value) {
			inner, _ := index(value, key)
			out[key] = sortRecursive(inner, descending)
		}
		return out
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = sortRecursive(rv.Index(i).Interface(), descending)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if descending {
			return compareValues(out[i], out[j]) > 0
		}
		return compareValues(out[i], out[j]) < 0
	})
	return out
}

// compareValues orders two values the way PHP's sort() with SORT_REGULAR
// orders them, as far as Go can: numbers numerically, strings
// lexicographically, false before true, and values of different kinds by the
// order nil, bool, number, string, array.
func compareValues(a, b any) int {
	ra, rb := valueRank(a), valueRank(b)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	switch ra {
	case rankBool:
		left, right := a.(bool), b.(bool)
		switch {
		case left == right:
			return 0
		case !left:
			return -1
		default:
			return 1
		}
	case rankNumber:
		left, right := asFloat(a), asFloat(b)
		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		default:
			return 0
		}
	case rankString:
		return strings.Compare(keyString(a), keyString(b))
	default:
		return 0
	}
}

const (
	rankNil = iota
	rankBool
	rankNumber
	rankString
	rankArray
)

func valueRank(value any) int {
	if value == nil {
		return rankNil
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Bool:
		return rankBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return rankNumber
	case reflect.String:
		return rankString
	case reflect.Map, reflect.Slice, reflect.Array:
		return rankArray
	default:
		return rankString
	}
}

func asFloat(value any) float64 {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	default:
		return 0
	}
}
