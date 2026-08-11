package concerns

import (
	"regexp"
	"strings"
)

// searchPathToken is the PHP's '/[^\s,"\']+/': anything that is not
// whitespace, a comma or a quote.
var searchPathToken = regexp.MustCompile(`[^\s,"']+`)

// ParseSearchPath answers Concerns\ParsesSearchPath::parseSearchPath: it reads
// Postgres's search_path option, which arrives as either a string or a list.
//
//	"public,laravel"        ->  ["public", "laravel"]
//	`"public", 'laravel'`   ->  ["public", "laravel"]
//	[]string{"public"}      ->  ["public"]
//
// The PHP takes string|array|null and this takes any for the same reason: the
// value comes out of a configuration file where either shape is legal. A value
// of any other type answers an empty list, which is what the PHP's `?? []` does
// with null.
func ParseSearchPath(searchPath any) []string {
	var parts []string

	switch v := searchPath.(type) {
	case nil:
		return []string{}
	case string:
		parts = searchPathToken.FindAllString(v, -1)
	case []string:
		parts = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
	default:
		return []string{}
	}

	out := make([]string, 0, len(parts))
	for _, schema := range parts {
		// The PHP trims quotes off each entry after the split, because the list
		// form arrives with them still attached.
		out = append(out, strings.Trim(schema, `'"`))
	}
	return out
}
