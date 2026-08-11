package translation

import (
	"strconv"
	"strings"
)

// Selector is what [Translator.SetSelector] takes: the thing that picks one
// segment of a pluralised line.
//
// Illuminate has no such interface -- setSelector() is typed to the
// MessageSelector class and an application overrides the rules by extending it.
// Go has no inheritance, so the extension point is an interface and
// [MessageSelector] is the implementation that ships.
type Selector interface {
	// Choose answers MessageSelector::choose().
	Choose(line string, number int, locale string) string

	// GetPluralIndex answers MessageSelector::getPluralIndex().
	GetPluralIndex(locale string, number int) int
}

// MessageSelector is Illuminate's Translation\MessageSelector: the arithmetic
// that turns one line holding every plural form into the form a count needs.
//
// The zero value is ready to use and holds nothing, so one instance serves
// every request.
type MessageSelector struct{}

// Choose answers MessageSelector::choose(). It selects the segment of line that
// number calls for, out of the segments divided by "|".
//
// There are two syntaxes and both are read, in this order:
//
//   - An explicit condition opens a segment: "{0}" matches one count, and
//     "[1,19]" or "[20,*]" match a range, with "*" for an open end. The first
//     segment whose condition matches wins, and only it is trimmed.
//   - With no condition matching, every condition is stripped and the plural
//     rule of the locale indexes what is left: "one apple|:count apples" is two
//     forms in English and "яблоко|яблока|яблок" is three in Russian.
//
// Only the first path trims, which is what PHP does: trim() is applied to the
// value extracted by a condition and nowhere else, so "{1} one | :count many"
// answers "one" for one and " :count many", with its leading space, for four. A
// line whose segments are spaced out either side of the bar renders that space
// on the page.
//
// A line with fewer segments than the rule has forms falls back to the first,
// which is what a catalogue translated only for the singular needs.
func (s MessageSelector) Choose(line string, number int, locale string) string {
	segments := strings.Split(line, "|")

	if value, ok := extract(segments, number); ok {
		return phpTrim(value)
	}

	segments = stripConditions(segments)

	pluralIndex := s.GetPluralIndex(locale, number)

	if len(segments) == 1 || pluralIndex < 0 || pluralIndex >= len(segments) {
		return segments[0]
	}
	return segments[pluralIndex]
}

// extract answers MessageSelector::extract(). It returns the first segment
// whose inline condition matches the number.
func extract(segments []string, number int) (string, bool) {
	for _, part := range segments {
		if value, ok := extractFromString(part, number); ok {
			return value, true
		}
	}
	return "", false
}

// extractFromString answers MessageSelector::extractFromString(). It reports
// the text of one segment when the condition that opens it matches the number,
// and reports false when the segment opens with no condition or with one the
// number does not satisfy.
//
// PHP matches ^[\{\[]([^\[\]\{\}]*)[\}\]](.*)/s: an opening brace or bracket,
// a body holding neither, a closing brace or bracket, and the rest of the
// segment. Reading it by hand rather than with a regexp keeps the two spellings
// of the condition in one place -- and a segment that merely begins with a
// bracket, "[draft] not published", is text, not a malformed rule to swallow.
func extractFromString(part string, number int) (string, bool) {
	if part == "" || (part[0] != '{' && part[0] != '[') {
		return "", false
	}
	end := strings.IndexAny(part, "}]")
	if end < 0 || strings.ContainsAny(part[1:end], "{[") {
		return "", false
	}
	condition, value := part[1:end], part[end+1:]

	from, to, ranged := strings.Cut(condition, ",")
	if !ranged {
		n, err := strconv.Atoi(strings.TrimSpace(condition))
		if err != nil {
			return "", false
		}
		return value, n == number
	}

	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	low, lowErr := strconv.Atoi(from)
	high, highErr := strconv.Atoi(to)

	switch {
	case to == "*" && lowErr == nil:
		return value, number >= low
	case from == "*" && highErr == nil:
		return value, number <= high
	case lowErr == nil && highErr == nil:
		return value, number >= low && number <= high
	default:
		return "", false
	}
}

// stripConditions answers MessageSelector::stripConditions(). It removes the
// leading condition of every segment, leaving the text -- and leaving whatever
// space followed the condition, exactly as the PHP preg_replace does.
func stripConditions(segments []string) []string {
	out := make([]string, len(segments))
	for i, part := range segments {
		out[i] = part
		if part == "" || (part[0] != '{' && part[0] != '[') {
			continue
		}
		end := strings.IndexAny(part, "}]")
		if end < 0 || strings.ContainsAny(part[1:end], "{[") {
			continue
		}
		out[i] = part[end+1:]
	}
	return out
}

// phpTrim trims what PHP's trim() trims by default: space, tab, newline,
// carriage return, NUL and vertical tab. strings.TrimSpace would also take
// U+0085 and U+00A0, which PHP leaves alone.
func phpTrim(value string) string {
	return strings.Trim(value, " \t\n\r\x00\x0B")
}
