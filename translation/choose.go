package translation

import (
	"strconv"
	"strings"
)

// choose picks the segment of line that count selects.
//
// An explicit condition wins over the plural rule, and the first segment whose
// condition matches wins over the ones after it. When no condition matches, the
// conditions are stripped and the rule of the locale indexes what is left; a
// line with fewer segments than the rule has forms falls back to the first,
// which is what a catalogue translated only for the singular needs.
//
// Every segment is trimmed. Illuminate trims only the segment it selected by
// condition, so "{1} one|[2,*] many" keeps a leading space in one path and
// loses it in the other -- one behaviour, here, in both.
func choose(line string, count int, locale string, plural Plural) string {
	segments := strings.Split(line, "|")

	for _, segment := range segments {
		if c, text, ok := split(segment); ok && c.covers(count) {
			return strings.TrimSpace(text)
		}
	}

	for i, segment := range segments {
		if _, text, ok := split(segment); ok {
			segment = text
		}
		segments[i] = strings.TrimSpace(segment)
	}

	i := plural(locale, count)
	if i < 0 || i >= len(segments) {
		return segments[0]
	}
	return segments[i]
}

// condition is a segment's leading "{n}" or "[from,to]", parsed. An open bound
// is spelled "*" and read as unbounded.
type condition struct {
	from, to  int
	low, high bool
}

func (c condition) covers(n int) bool {
	return (!c.low || n >= c.from) && (!c.high || n <= c.to)
}

// split separates a well-formed condition from the text after it.
//
// It reports false for a segment that opens with no bracket and for one whose
// brackets hold something that is not a condition, so a line that begins with
// "[draft] " keeps its first word instead of losing it to a rule it was never
// written as.
func split(segment string) (condition, string, bool) {
	if segment == "" || (segment[0] != '{' && segment[0] != '[') {
		return condition{}, segment, false
	}

	end := strings.IndexAny(segment, "}]")
	if end < 0 || strings.ContainsAny(segment[1:end], "{[") {
		return condition{}, segment, false
	}
	inside, text := segment[1:end], segment[end+1:]

	from, to, ranged := strings.Cut(inside, ",")
	if !ranged {
		n, err := strconv.Atoi(strings.TrimSpace(inside))
		if err != nil {
			return condition{}, segment, false
		}
		return condition{from: n, to: n, low: true, high: true}, text, true
	}

	c := condition{}
	if from = strings.TrimSpace(from); from != "*" {
		n, err := strconv.Atoi(from)
		if err != nil {
			return condition{}, segment, false
		}
		c.from, c.low = n, true
	}
	if to = strings.TrimSpace(to); to != "*" {
		n, err := strconv.Atoi(to)
		if err != nil {
			return condition{}, segment, false
		}
		c.to, c.high = n, true
	}
	return c, text, true
}
