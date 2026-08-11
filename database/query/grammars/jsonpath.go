package grammars

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/database/query"
)

// This file answers Illuminate\Database\Concerns\CompilesJsonPaths, the trait
// the query grammars and the schema grammars share, plus the small value
// helpers PHP gets from the language.
//
// A trait is a set of methods mixed into a class; embedding is Go's version of
// that, and these are methods on Grammar for the same reason the trait is used
// on Grammar there.

// wrapJSONFieldAndPath answers CompilesJsonPaths::wrapJsonFieldAndPath: it
// splits "options->language->code" into the column and the path into it, and
// wraps each separately.
func (g *Grammar) wrapJSONFieldAndPath(column any) (field, path string) {
	parts := strings.SplitN(text(column), "->", 2)

	field = g.self.Wrap(parts[0])

	if len(parts) > 1 {
		path = ", " + g.wrapJSONPath(parts[1], "->")
	}

	return field, path
}

// jsonPathQuote is the PHP's /([\\]+)?\'/ : a quote inside a path, with or
// without the backslashes somebody escaped it with.
var jsonPathQuote = regexp.MustCompile(`(\\+)?'`)

// wrapJSONPath answers CompilesJsonPaths::wrapJsonPath.
func (g *Grammar) wrapJSONPath(value, delimiter string) string {
	value = jsonPathQuote.ReplaceAllString(value, "''")

	segments := strings.Split(value, delimiter)
	wrapped := make([]string, 0, len(segments))
	for _, segment := range segments {
		wrapped = append(wrapped, wrapJSONPathSegment(segment))
	}

	jsonPath := strings.Join(wrapped, ".")

	prefix := "."
	if strings.HasPrefix(jsonPath, "[") {
		prefix = ""
	}

	return "'$" + prefix + jsonPath + "'"
}

// jsonPathArrayKeys is the PHP's /(\[[^\]]+\])+$/ : the trailing [0][1] of a
// path segment that indexes into an array.
var jsonPathArrayKeys = regexp.MustCompile(`(\[[^\]]+\])+$`)

// wrapJSONPathSegment answers CompilesJsonPaths::wrapJsonPathSegment.
func wrapJSONPathSegment(segment string) string {
	if parts := jsonPathArrayKeys.FindString(segment); parts != "" {
		key := strings.TrimSuffix(segment, parts)
		if key != "" {
			return `"` + key + `"` + parts
		}
		return parts
	}
	return `"` + segment + `"`
}

// isJSONSelector answers Grammar::isJsonSelector. The PHP spells it Json; Go
// initialisms are upper case throughout.
func isJSONSelector(value any) bool {
	return strings.Contains(text(value), "->")
}

// WrapJSONSelector answers Grammar::wrapJsonSelector.
//
// The PHP throws for an engine with no JSON support. Wrap returns a string, so
// the refusal here is a name the engine cannot resolve -- the arrow stays
// inside the quoted identifier and the statement fails with "no such column",
// which is true and is the same non-answer the exception gives. Every grammar
// in this package overrides it.
func (g *Grammar) WrapJSONSelector(value string) string {
	return g.self.WrapValue(value)
}

// WrapJSONBooleanSelector answers Grammar::wrapJsonBooleanSelector.
func (g *Grammar) WrapJSONBooleanSelector(value string) string {
	return g.self.WrapJSONSelector(value)
}

// WrapJSONBooleanValue answers Grammar::wrapJsonBooleanValue.
func (g *Grammar) WrapJSONBooleanValue(value string) string { return value }

// text renders a value the way PHP's string coercion does when the grammar
// concatenates it into a statement.
//
// It is not a substitute for a placeholder: nothing that reaches it is a bound
// value. Columns, table names, operators and expressions pass through here;
// values go through Parameter.
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case query.Expression:
		return text(t.Value())
	case *query.Expression:
		if t == nil {
			return ""
		}
		return text(t.Value())
	case fmt.Stringer:
		return t.String()
	case bool:
		if t {
			return "1"
		}
		return "0"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(v)
	}
}

// isBool answers is_bool.
func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

// truthy answers PHP's truth test for an option a caller may not have set.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != "" && v != "0"
	case int:
		return v != 0
	default:
		return true
	}
}

// isStructured answers is_array for the values a Go caller passes: a slice or a
// map, which is what has to become JSON text before it can be bound.
//
// A byte slice is not one of them. It is a binary value on its way to a blob,
// and encoding it as JSON would store the base64 of the bytes instead.
func isStructured(value any) bool {
	if value == nil {
		return false
	}
	if _, ok := value.([]byte); ok {
		return false
	}

	switch reflect.TypeOf(value).Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}

// encodeJSON answers json_encode($value, JSON_UNESCAPED_UNICODE).
//
// Go escapes <, > and & by default and PHP does not, so HTML escaping is turned
// off; a JSON path holding a "<" would otherwise stop matching the value stored
// in the column.
func encodeJSON(value any) (any, error) {
	var buffer bytes.Buffer

	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("query/grammars: encoding a JSON binding: %w", err)
	}

	return strings.TrimRight(buffer.String(), "\n"), nil
}
