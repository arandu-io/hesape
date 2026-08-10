package arr

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// countError is the InvalidArgumentException Arr::random throws when more items
// are asked for than the array holds.
type countError struct {
	requested int
	available int
}

func (e *countError) Error() string {
	return fmt.Sprintf("you requested %d items, but there are only %d items available", e.requested, e.available)
}

// Query answers to Arr::query: the array as an RFC 3986 query string, nested
// values written as key[sub]=value the way http_build_query writes them.
//
// PHP keeps the array's insertion order; a Go map has none, so the keys are
// sorted and the string is the same on every run.
func Query(array map[string]any) string {
	pairs := queryPairs("", array)
	return strings.Join(pairs, "&")
}

func queryPairs(prefix string, v any) []string {
	switch value := v.(type) {
	case nil:
		return nil
	case map[string]any:
		var pairs []string
		for _, k := range sortedKeys(value) {
			pairs = append(pairs, queryPairs(childKey(prefix, k), value[k])...)
		}
		return pairs
	case []any:
		var pairs []string
		for i, item := range value {
			pairs = append(pairs, queryPairs(childKey(prefix, strconv.Itoa(i)), item)...)
		}
		return pairs
	default:
		if prefix == "" {
			return nil
		}
		return []string{rawURLEncode(prefix) + "=" + rawURLEncode(toKey(value))}
	}
}

func childKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "[" + key + "]"
}

// rawURLEncode is PHP's rawurlencode: everything outside the RFC 3986
// unreserved set becomes a percent-triplet, and a space is %20, never a plus.
func rawURLEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// toKey renders a value the way PHP renders it inside a query string or as an
// array key: true is 1, false is 0, and a float keeps no trailing zeros.
func toKey(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		if value {
			return "1"
		}
		return "0"
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// ToCssClasses answers to Arr::toCssClasses: a class list built out of plain
// classes and conditional ones.
//
// A string element is always kept; a map[string]bool element keeps the keys
// whose condition is true, sorted, since a Go map has no order. That pair of
// element shapes is the PHP array that mixes numeric and string keys.
func ToCssClasses(array any) string {
	return strings.Join(conditionalList(array, ""), " ")
}

// ToCssStyles answers to Arr::toCssStyles: a style list built out of plain
// styles and conditional ones, every entry finished with a semicolon.
func ToCssStyles(array any) string {
	return strings.Join(conditionalList(array, ";"), " ")
}

func conditionalList(array any, suffix string) []string {
	entries := []string{}
	for _, item := range Wrap(array) {
		switch v := item.(type) {
		case string:
			entries = append(entries, finish(v, suffix))
		case map[string]bool:
			keys := make([]string, 0, len(v))
			for k, on := range v {
				if on {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				entries = append(entries, finish(k, suffix))
			}
		}
	}
	return entries
}

// finish is Str::finish, kept private because the string package already owns
// the public spelling.
func finish(s, cap string) string {
	if cap == "" || strings.HasSuffix(s, cap) {
		return s
	}
	return s + cap
}

// compareValues orders two values the way PHP's SORT_REGULAR does: numbers
// against numbers, strings against strings, and anything else by its rendering.
func compareValues(a, b any) int {
	an, aok := asFloat(a)
	bn, bok := asFloat(b)
	if aok && bok {
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(toKey(a), toKey(b))
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
