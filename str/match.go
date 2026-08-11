package str

import (
	"regexp"
	"strings"
)

// Is answers for Str::is. It reports whether the value matches any one of the
// patterns, where * stands for any run of characters, including none. Every
// other rune is literal -- the PHP quotes the pattern before it translates the
// asterisks -- so a pattern is safe to build from configuration without
// escaping anything.
//
// Is([]string{"admin/*"}, "admin/users", false) is true, and so is
// Is([]string{"*"}, anything, false).
//
// It is not path.Match: * crosses a slash, because what this matches is a route
// name, an ability or a host, not a file name. PHP takes a pattern that is
// either a string or an array; here it is a slice, because the value and the
// case flag follow it.
func Is(patterns []string, value string, ignoreCase bool) bool {
	if ignoreCase {
		value = Lower(value)
	}
	for _, pattern := range patterns {
		if pattern == "*" {
			return true
		}
		if ignoreCase {
			pattern = Lower(pattern)
		}
		if pattern == value {
			return true
		}
		if globMatch(pattern, value) {
			return true
		}
	}
	return false
}

// globMatch is the pattern with its asterisks read as ".*" and everything else
// read literally, anchored at both ends.
func globMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return false
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	rest := value[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}

// Match answers for Str::match. It returns the first capture group of the first
// match, or the whole match when the pattern captures nothing, or the empty
// string when the pattern does not match at all.
//
// PHP takes a delimited PCRE string. Go's regexp has neither delimiters nor
// trailing modifiers, so this takes a compiled pattern: "/name/i" is
// regexp.MustCompile(`(?i)name`).
func Match(pattern *regexp.Regexp, subject string) string {
	m := pattern.FindStringSubmatch(subject)
	if m == nil {
		return ""
	}
	if len(m) > 1 {
		return m[1]
	}
	return m[0]
}

// MatchAll answers for Str::matchAll. It returns the first capture group of
// every match, or every whole match when the pattern captures nothing.
//
// PHP returns a Collection; this returns a slice, and it is empty rather than
// nil when nothing matched.
func MatchAll(pattern *regexp.Regexp, subject string) []string {
	ms := pattern.FindAllStringSubmatch(subject, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if len(m) > 1 {
			out = append(out, m[1])
			continue
		}
		out = append(out, m[0])
	}
	return out
}

// IsMatch answers for Str::isMatch. It reports whether the value matches any
// one of the patterns.
//
// PHP takes a pattern that is either a string or an array; here it is a slice,
// because the value follows it.
func IsMatch(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

// ReplaceMatches answers for Str::replaceMatches. It replaces at most limit
// matches of the pattern, and every one of them when limit is negative, which
// is the PHP's default.
//
// The replacement expands a capture group as $1 or ${name}, which is Go's
// spelling; PHP writes $1 or \1 for the same thing. A limit of zero replaces
// nothing.
func ReplaceMatches(pattern *regexp.Regexp, replace, subject string, limit int) string {
	if limit < 0 {
		return pattern.ReplaceAllString(subject, replace)
	}
	if limit == 0 {
		return subject
	}
	done := 0
	return pattern.ReplaceAllStringFunc(subject, func(match string) string {
		if done >= limit {
			return match
		}
		done++
		idx := pattern.FindStringSubmatchIndex(match)
		return string(pattern.ExpandString(nil, replace, match, idx))
	})
}
