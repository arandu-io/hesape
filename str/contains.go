package str

import (
	"regexp"
	"strings"
)

// Contains answers for Str::contains. It reports whether the haystack holds any
// one of the needles, comparing without regard to case when ignoreCase is set.
//
// An empty needle never matches, which is what PHP's `$needle !== ”` guard
// buys: Contains("anything", []string{""}, false) is false, not true.
//
// PHP takes a needle that is either a string or an array. Where that argument is
// not last in the Go signature it is a slice, and where it is last it is
// variadic; contains carries a trailing flag, so this one is a slice.
func Contains(haystack string, needles []string, ignoreCase bool) bool {
	if ignoreCase {
		haystack = Lower(haystack)
	}
	for _, needle := range needles {
		if ignoreCase {
			needle = Lower(needle)
		}
		if needle != "" && strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// ContainsAll answers for Str::containsAll. It reports whether the haystack
// holds every one of the needles.
//
// An empty list of needles is true, because there is nothing that fails. A
// needle that is the empty string is false, because Contains refuses it.
func ContainsAll(haystack string, needles []string, ignoreCase bool) bool {
	for _, needle := range needles {
		if !Contains(haystack, []string{needle}, ignoreCase) {
			return false
		}
	}
	return true
}

// DoesntContain answers for Str::doesntContain. It is Contains negated.
func DoesntContain(haystack string, needles []string, ignoreCase bool) bool {
	return !Contains(haystack, needles, ignoreCase)
}

// StartsWith answers for Str::startsWith. It reports whether the haystack
// begins with any one of the needles. An empty needle never matches.
func StartsWith(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.HasPrefix(haystack, needle) {
			return true
		}
	}
	return false
}

// DoesntStartWith answers for Str::doesntStartWith. It is StartsWith negated.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func DoesntStartWith(haystack string, needles ...string) bool {
	return !StartsWith(haystack, needles...)
}

// EndsWith answers for Str::endsWith. It reports whether the haystack ends with
// any one of the needles. An empty needle never matches.
func EndsWith(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.HasSuffix(haystack, needle) {
			return true
		}
	}
	return false
}

// DoesntEndWith answers for Str::doesntEndWith. It is EndsWith negated.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func DoesntEndWith(haystack string, needles ...string) bool {
	return !EndsWith(haystack, needles...)
}

// Wrap answers for Str::wrap. It puts before in front of the value and after
// behind it; passing no after repeats before, so Wrap("name", `"`) is `"name"`.
func Wrap(value, before string, after ...string) string {
	return before + value + wrapAfter(before, after)
}

// Unwrap answers for Str::unwrap. It removes before from the front of the value
// and after from the back, each only if it is there; passing no after repeats
// before.
func Unwrap(value, before string, after ...string) string {
	tail := wrapAfter(before, after)
	if StartsWith(value, before) {
		value = Substr(value, Length(before))
	}
	if EndsWith(value, tail) {
		value = Substr(value, 0, -Length(tail))
	}
	return value
}

// wrapAfter is PHP's `$after ?? $before`.
func wrapAfter(before string, after []string) string {
	if len(after) > 0 {
		return after[0]
	}
	return before
}

// Deduplicate answers for Str::deduplicate. It collapses a run of the given
// character to one occurrence of it; passing no character deduplicates spaces.
//
// The PHP builds the pattern as the quoted character followed by a plus, so a
// multi-character argument repeats only its last character:
// Deduplicate("a---b", "--") is "a--b". That is reproduced here, quirk and all.
// A character that is the empty string builds a pattern PHP cannot compile, and
// the string comes back untouched.
func Deduplicate(value string, character ...string) string {
	char := " "
	if len(character) > 0 {
		char = character[0]
	}
	if char == "" {
		return value
	}
	re, err := regexp.Compile(regexp.QuoteMeta(char) + "+")
	if err != nil {
		return value
	}
	return re.ReplaceAllLiteralString(value, char)
}
