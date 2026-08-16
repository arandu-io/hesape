package arr

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// countError is returned by [Random] when more items are asked for than the
// list holds.
type countError struct {
	requested int
	available int
}

func (e *countError) Error() string {
	return fmt.Sprintf("you requested %d items, but there are only %d items available", e.requested, e.available)
}

// Query renders the map as an RFC 3986 query string, writing nested values as
// key[sub]=value and nested lists by index. A nil value is dropped.
//
// A map has no order of its own, so the keys are sorted and the string is the
// same on every run.
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

// rawURLEncode percent-encodes everything outside the RFC 3986 unreserved set.
// A space becomes %20, never a plus.
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

// toKey renders a value for use inside a query string, or as a map key: nil is
// the empty string, true is 1, false is 0, and a float keeps no trailing
// zeros.
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

// ToCssClasses builds a class list out of plain classes and conditional ones.
//
// A string element is always kept; a map[string]bool element keeps the keys
// whose condition is true, sorted, since a map has no order of its own. Any
// other element is dropped.
func ToCssClasses(array any) string {
	return strings.Join(conditionalList(array, ""), " ")
}

// ToCssStyles builds a style list out of plain styles and conditional ones,
// every entry finished with a semicolon. The elements are read as
// [ToCssClasses] reads them.
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

// finish appends cap to s unless s already ends with it. It is kept private
// because the string package owns the exported spelling.
func finish(s, cap string) string {
	if cap == "" || strings.HasSuffix(s, cap) {
		return s
	}
	return s + cap
}

// compareValues orders two values: numbers against numbers, and anything else
// by its rendering.
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
