package concerns

import (
	"regexp"
	"strings"
)

// Wrapper is the grammar method CompilesJsonPaths reaches through $this->wrap.
//
// In PHP the trait is mixed into a grammar and calls its own wrap. Go has no
// mixin, so the one call back into the grammar is a parameter. It is a function
// rather than an interface because it is one method, and an interface of one
// method that every grammar would satisfy anyway is ceremony.
type Wrapper func(value any) string

// WrapJSONFieldAndPath answers CompilesJsonPaths::wrapJsonFieldAndPath: it
// splits `options->language` into the wrapped column and the JSON path, so a
// grammar can put them either side of whatever its engine spells the extraction
// function.
//
// The PHP names it wrapJsonFieldAndPath and returns a two-element array; JSON
// is an initialism and goes up, and a two-value return is what an array of two
// is in Go. The path comes back with the leading ", " the PHP prepends, so a
// grammar concatenating them gets the PHP's string.
func WrapJSONFieldAndPath(wrap Wrapper, column string) (field, path string) {
	parts := strings.SplitN(column, "->", 2)

	field = wrap(parts[0])

	if len(parts) > 1 {
		path = ", " + WrapJSONPath(parts[1], "->")
	}
	return field, path
}

// jsonPathQuote is the PHP's "/([\\\\]+)?\\'/" -- an apostrophe, escaped or
// not, becomes the doubled apostrophe SQL wants inside a literal.
var jsonPathQuote = regexp.MustCompile(`\\*'`)

// WrapJSONPath answers CompilesJsonPaths::wrapJsonPath: it turns
// `language->code` into the quoted '$."language"."code"' a JSON function takes.
func WrapJSONPath(value, delimiter string) string {
	value = jsonPathQuote.ReplaceAllString(value, "''")

	segments := strings.Split(value, delimiter)
	wrapped := make([]string, 0, len(segments))
	for _, segment := range segments {
		wrapped = append(wrapped, WrapJSONPathSegment(segment))
	}
	jsonPath := strings.Join(wrapped, ".")

	dot := "."
	if strings.HasPrefix(jsonPath, "[") {
		// An array subscript attaches to the root directly: '$[0]', never
		// '$.[0]'. The PHP tests the same prefix for the same reason.
		dot = ""
	}
	return "'$" + dot + jsonPath + "'"
}

// jsonPathSubscript is the PHP's '/(\[[^\]]+\])+$/': one or more array
// subscripts at the end of a segment.
var jsonPathSubscript = regexp.MustCompile(`(\[[^\]]+\])+$`)

// WrapJSONPathSegment answers CompilesJsonPaths::wrapJsonPathSegment: it quotes
// the key and leaves the array subscript outside the quotes, because
// '$."tags"[0]' selects an element and '$."tags[0]"' selects a key that does not
// exist.
func WrapJSONPathSegment(segment string) string {
	if parts := jsonPathSubscript.FindString(segment); parts != "" {
		key := strings.TrimSuffix(segment, parts)
		if key != "" {
			return `"` + key + `"` + parts
		}
		return parts
	}
	return `"` + segment + `"`
}
