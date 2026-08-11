package str

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Replace answers for Str::replace. It replaces every occurrence of each search
// with the replacement standing at the same index, running the searches in
// order over the whole subject, and matches without regard to case when
// caseSensitive is false.
//
// This is PHP's str_replace with two arrays: a search with no replacement
// opposite it is replaced with the empty string, which is why
// Replace([]string{"a", "b"}, []string{"x"}, "ab", true) is "x" and not "xx".
// An empty search matches nothing and is skipped, the way str_replace skips it.
func Replace(search, replace []string, subject string, caseSensitive bool) string {
	for i, s := range search {
		with := ""
		if i < len(replace) {
			with = replace[i]
		}
		subject = replaceAll(subject, s, with, caseSensitive)
	}
	return subject
}

// Remove answers for Str::remove. It deletes every occurrence of each search
// from the subject, matching without regard to case when caseSensitive is
// false.
func Remove(search []string, subject string, caseSensitive bool) string {
	for _, s := range search {
		subject = replaceAll(subject, s, "", caseSensitive)
	}
	return subject
}

// replaceAll is str_replace and str_ireplace for one search at a time. The
// insensitive branch is a quoted pattern with Go's (?i) flag, because folding
// the subject to compare it would move the byte offsets the replacement needs.
func replaceAll(subject, search, with string, caseSensitive bool) string {
	if search == "" {
		return subject
	}
	if caseSensitive {
		return strings.ReplaceAll(subject, search, with)
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(search))
	if err != nil {
		return subject
	}
	return re.ReplaceAllLiteralString(subject, with)
}

// ReplaceArray answers for Str::replaceArray. It walks the occurrences of
// search from left to right and spends one replacement on each:
// ReplaceArray("?", []string{"8:30", "9:00"}, "from ? to ?") is "from 8:30 to
// 9:00".
//
// When the replacements run out the search is left where it stands, which is
// what PHP's `array_shift($replace) ?? $search` does. An empty search returns
// the subject; PHP raises a ValueError for it, because explode refuses an empty
// delimiter.
func ReplaceArray(search string, replace []string, subject string) string {
	if search == "" {
		return subject
	}
	segments := strings.Split(subject, search)
	var b strings.Builder
	b.WriteString(segments[0])
	for i, segment := range segments[1:] {
		if i < len(replace) {
			b.WriteString(replace[i])
		} else {
			b.WriteString(search)
		}
		b.WriteString(segment)
	}
	return b.String()
}

// ReplaceFirst answers for Str::replaceFirst. It replaces the first occurrence
// of search, and returns the subject untouched when search is empty or absent.
func ReplaceFirst(search, replace, subject string) string {
	if search == "" {
		return subject
	}
	i := strings.Index(subject, search)
	if i < 0 {
		return subject
	}
	return subject[:i] + replace + subject[i+len(search):]
}

// ReplaceLast answers for Str::replaceLast. It replaces the last occurrence of
// search, and returns the subject untouched when search is empty or absent.
func ReplaceLast(search, replace, subject string) string {
	if search == "" {
		return subject
	}
	i := strings.LastIndex(subject, search)
	if i < 0 {
		return subject
	}
	return subject[:i] + replace + subject[i+len(search):]
}

// ReplaceStart answers for Str::replaceStart. It replaces search only where it
// begins the subject.
func ReplaceStart(search, replace, subject string) string {
	if search == "" {
		return subject
	}
	if StartsWith(subject, search) {
		return ReplaceFirst(search, replace, subject)
	}
	return subject
}

// ReplaceEnd answers for Str::replaceEnd. It replaces search only where it ends
// the subject.
func ReplaceEnd(search, replace, subject string) string {
	if search == "" {
		return subject
	}
	if EndsWith(subject, search) {
		return ReplaceLast(search, replace, subject)
	}
	return subject
}

// Swap answers for Str::swap. It applies a whole map of replacements in one
// left-to-right pass, so nothing a replacement produces is replaced again:
// Swap(map[string]string{"a": "b", "b": "a"}, "ab") is "ba".
//
// It is PHP's strtr, which means the longest key that fits at a position wins
// and an empty key is ignored.
func Swap(swap map[string]string, subject string) string {
	keys := make([]string, 0, len(swap))
	for k := range swap {
		if k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return subject
	}
	// strings.Replacer settles a tie at one position by argument order, and
	// strtr settles it by length, so the longest key is offered first. Equal
	// lengths are ordered by the key itself to keep the answer stable.
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	pairs := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		pairs = append(pairs, k, swap[k])
	}
	return strings.NewReplacer(pairs...).Replace(subject)
}

// Reverse answers for Str::reverse. It reverses the string character by
// character, so a multi-byte rune survives the trip.
func Reverse(value string) string {
	rs := []rune(value)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// Repeat answers for Str::repeat. It is the string written times over.
//
// PHP raises a ValueError for a negative count; this returns the empty string,
// as it does for a count of zero, because there is no shorter answer to give.
func Repeat(value string, times int) string {
	if times <= 0 {
		return ""
	}
	return strings.Repeat(value, times)
}

// Numbers answers for Str::numbers. It keeps the digits and drops everything
// else: Numbers("(11) 98765-4321") is "11987654321".
//
// Digits means the ten ASCII ones, which is what the PHP's [^0-9] class means.
func Numbers(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for i := range len(value) {
		if value[i] >= '0' && value[i] <= '9' {
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

// ParseCallback answers for Str::parseCallback. It splits a "Class@method"
// string into its two halves, and answers the whole string with the fallback
// when there is no at sign in it.
//
// PHP returns null for the second half when no fallback is given; pass the
// empty string for that. Its branch for "@anonymous\0" is not here: that string
// is what PHP names an anonymous class with, and Go has no such name.
func ParseCallback(callback, fallback string) (string, string) {
	if before, after, found := strings.Cut(callback, "@"); found {
		return before, after
	}
	return callback, fallback
}

// ToBase64 answers for Str::toBase64. It is standard base64 with padding.
func ToBase64(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// FromBase64 answers for Str::fromBase64. It decodes standard base64.
//
// PHP returns false when the decoding fails; the second result is that false.
// Strict decoding refuses any character outside the alphabet, as base64_decode
// does, and both modes skip whitespace and stop at the first padding character.
func FromBase64(value string, strict bool) (string, bool) {
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '=':
			i = len(value)
		case c == '+' || c == '/' ||
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			b.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			// Whitespace is skipped in both modes, the way base64_decode skips it.
		case strict:
			return "", false
		}
	}
	decoded, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(b.String())
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

// Initials answers for Str::initials. It keeps the first character of every
// whitespace-separated part: Initials("Paulo Ricardo Lima", false) is "PRL".
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func Initials(value string, capitalize bool) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(value, unicode.IsSpace) {
		b.WriteString(Substr(part, 0, 1))
	}
	if capitalize {
		return Upper(b.String())
	}
	return b.String()
}

// Ucwords answers for Str::ucwords. It uppercases the first letter of the
// string and every lowercase letter that follows one of the separators;
// passing no separators uses PHP's default of space, tab and the line breaks.
//
// Only a lowercase letter is touched, which is the PHP's \p{Ll} class: a part
// that already begins with a capital or a digit is left as it stands.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func Ucwords(value string, separators ...string) string {
	seps := " \t\r\n\f\v"
	if len(separators) > 0 {
		seps = separators[0]
	}
	var b strings.Builder
	b.Grow(len(value))
	boundary := true
	for _, r := range value {
		if boundary && unicode.IsLower(r) {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
		boundary = strings.ContainsRune(seps, r)
	}
	return b.String()
}
