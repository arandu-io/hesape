package str

import (
	"regexp"
	"strings"
)

var (
	// entityReference is an HTML entity, which passes through instead of
	// having its ampersand escaped.
	entityReference = regexp.MustCompile(`^&(#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

	// rawInlineTag is an HTML tag written inside a paragraph, which passes
	// through to the output untouched.
	rawInlineTag = regexp.MustCompile(`^<(/?[A-Za-z][A-Za-z0-9-]*(\s[^<>]*)?/?|!--.*?--)>`)

	// autolink is the <...> form, which needs a scheme or an email address.
	autolink = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\x00-\x20]*|[^\s<>@]+@[^\s<>@]+\.[^\s<>@]+)>`)

	// bareURL is the GitHub flavour's autolinking of a URL nobody wrapped.
	bareURL = regexp.MustCompile(`^(https?://|www\.)[^\s<]*[^\s<.,:;"')\]]`)

	// asciiPunctuation is what a backslash may escape, per CommonMark.
	asciiPunctuation = `!"#$%&'()*+,-./:;<=>?@[\]^_` + "`" + `{|}~`
)

// renderInline renders the inline part of CommonMark: escapes, code spans,
// autolinks, raw HTML, images, links, emphasis, strikethrough and hard breaks.
func renderInline(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)

	// What keeps a run of openers that never close linear rather than
	// quadratic. closers is where each [ closes, found in one pass the first
	// time a [ is met. unclosed records each emphasis delimiter whose search
	// for a closer has run off the end: every later opener of the same
	// delimiter would search a subset of the same positions, under the same
	// conditions, and find nothing either.
	var closers []int
	var unclosed map[[2]byte]bool
	// closeOf is where the [ at open closes, counted from from, or -1.
	closeOf := func(open, from int) int {
		if closers == nil {
			closers = bracketClosers(s)
		}
		if closers[open] < 0 {
			return -1
		}
		return closers[open] - from
	}

	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && strings.IndexByte(asciiPunctuation, s[i+1]) >= 0:
			b.WriteString(escapeHTML(string(s[i+1])))
			i += 2

		case c == '\\' && i+1 < len(s) && s[i+1] == '\n':
			b.WriteString("<br />\n")
			i += 2

		case c == '`':
			if code, width := codeSpan(s[i:]); width > 0 {
				b.WriteString(code)
				i += width
				continue
			}
			b.WriteString("`")
			i++

		case c == '<':
			if m := autolink.FindStringSubmatch(s[i:]); m != nil {
				href := m[1]
				if !strings.Contains(href, ":") {
					href = "mailto:" + href
				}
				b.WriteString(`<a href="` + escapeHTML(href) + `">` + escapeHTML(m[1]) + `</a>`)
				i += len(m[0])
				continue
			}
			if m := rawInlineTag.FindString(s[i:]); m != "" {
				b.WriteString(m)
				i += len(m)
				continue
			}
			b.WriteString("&lt;")
			i++

		case c == '&':
			if m := entityReference.FindString(s[i:]); m != "" {
				b.WriteString(m)
				i += len(m)
				continue
			}
			b.WriteString("&amp;")
			i++

		case c == '>':
			b.WriteString("&gt;")
			i++

		case c == '"':
			b.WriteString("&quot;")
			i++

		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if html, width := linkOrImage(s[i:], true, closeOf(i+1, i)); width > 0 {
				b.WriteString(html)
				i += width
				continue
			}
			b.WriteString("!")
			i++

		case c == '[':
			if html, width := linkOrImage(s[i:], false, closeOf(i, i)); width > 0 {
				b.WriteString(html)
				i += width
				continue
			}
			b.WriteString("[")
			i++

		case c == '*' || c == '_' || c == '~':
			if c == '_' && !startsWord(s, i) {
				b.WriteByte(c)
				i++
				continue
			}
			delimiter := [2]byte{c, 1}
			if i+1 < len(s) && s[i+1] == c {
				delimiter[1] = 2
			}
			if !unclosed[delimiter] {
				html, width, exhausted := emphasis(s[i:], c)
				if width > 0 {
					b.WriteString(html)
					i += width
					continue
				}
				if exhausted {
					if unclosed == nil {
						unclosed = map[[2]byte]bool{}
					}
					unclosed[delimiter] = true
				}
			}
			b.WriteByte(c)
			i++

		case c == ' ':
			// Two spaces or more before a line break make a hard break, and
			// the run is read here, before it is written: nothing else this
			// renders ends in a space, so the source's run is the output's.
			j := i
			for j < len(s) && s[j] == ' ' {
				j++
			}
			if j-i >= 2 && j < len(s) && s[j] == '\n' {
				b.WriteString("<br />\n")
				i = j + 1
				continue
			}
			b.WriteString(s[i:j])
			i = j

		case c == '\n':
			b.WriteString("\n")
			i++

		case c == 'h' || c == 'w':
			if m := bareURL.FindString(s[i:]); m != "" && startsWord(s, i) {
				href := m
				if strings.HasPrefix(m, "www.") {
					href = "http://" + m
				}
				b.WriteString(`<a href="` + escapeHTML(href) + `">` + escapeHTML(m) + `</a>`)
				i += len(m)
				continue
			}
			b.WriteByte(c)
			i++

		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// startsWord reports whether the byte before i ends a word, so that the "www."
// inside "awww.no" is not read as a URL.
func startsWord(s string, i int) bool {
	if i == 0 {
		return true
	}
	c := s[i-1]
	return !isWordByte(c) && c != '/' && c != '.' && c != '@'
}

// isWordByte reports whether c can sit inside a word, which is what decides
// that an underscore between two letters is not an emphasis marker.
func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// codeSpan reads a backtick-delimited code span off the front of s and reports
// how much of it the span took.
func codeSpan(s string) (string, int) {
	open := 0
	for open < len(s) && s[open] == '`' {
		open++
	}
	fence := s[:open]
	rest := s[open:]

	for i := 0; i+open <= len(rest); i++ {
		if !strings.HasPrefix(rest[i:], fence) {
			continue
		}
		if i+open < len(rest) && rest[i+open] == '`' {
			continue
		}
		body := rest[:i]
		// One space on each side is stripped when the content is not all spaces.
		if len(body) >= 2 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.TrimSpace(body) != "" {
			body = body[1 : len(body)-1]
		}
		return "<code>" + escapeHTML(strings.ReplaceAll(body, "\n", " ")) + "</code>", open + i + open
	}
	return "", 0
}

// linkOrImage reads an inline link or image off the front of s and reports how
// much of it it took. labelEnd is the index just past the ] that closes the
// label, or -1 when nothing closes it. Reference links are not read: there is
// no link reference definition parser here.
func linkOrImage(s string, image bool, labelEnd int) (string, int) {
	bracket := 0
	if image {
		bracket = 1
	}
	if labelEnd < 0 || labelEnd >= len(s) || s[labelEnd] != '(' {
		return "", 0
	}
	text := s[bracket+1 : labelEnd-1]
	destination, title, end := linkTarget(s[labelEnd:])
	if end < 0 {
		return "", 0
	}

	attributes := `href="` + escapeHTML(destination) + `"`
	if image {
		attributes = `src="` + escapeHTML(destination) + `" alt="` + escapeHTML(stripInline(text)) + `"`
	}
	if title != "" {
		attributes += ` title="` + escapeHTML(title) + `"`
	}
	if image {
		return "<img " + attributes + " />", labelEnd + end
	}
	return "<a " + attributes + ">" + renderInline(text) + "</a>", labelEnd + end
}

// bracketClosers answers, for every [ in s, the index just past the ] that
// closes it -- counting the pairs in between, and skipping a character a
// backslash escapes -- and -1 for a [ nothing closes and for every other byte.
//
// One pass with a stack gives every [ the answer a scan from it would give,
// and a scan from each would read the rest of the text once per [ that never
// closes.
func bracketClosers(s string) []int {
	closers := make([]int, len(s))
	for i := range closers {
		closers[i] = -1
	}
	var open []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			open = append(open, i)
		case ']':
			if n := len(open); n > 0 {
				closers[open[n-1]] = i + 1
				open = open[:n-1]
			}
		}
	}
	return closers
}

// maxDestinationNesting is how many unescaped parentheses a link destination
// may nest. CommonMark leaves the bound to the implementation and asks for at
// least three; this is the one cmark uses.
const maxDestinationNesting = 32

// linkTarget reads the "(destination "title")" of a link and reports the index
// just past the closing parenthesis.
func linkTarget(s string) (destination, title string, end int) {
	if len(s) == 0 || s[0] != '(' {
		return "", "", -1
	}
	i := 1
	for i < len(s) && (s[i] == ' ' || s[i] == '\n') {
		i++
	}
	if i < len(s) && s[i] == '<' {
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			return "", "", -1
		}
		destination = s[i+1 : i+j]
		i += j + 1
	} else {
		depth, start := 0, i
		for i < len(s) {
			c := s[i]
			// A backslash escapes the character after it, and there is none
			// when it ends the text: skipping two would run past the end.
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == '(' {
				// Every [ of a run that never closes opens one more level
				// for each scan still reading, so the bound is what keeps
				// those scans from each reading the rest of the text.
				if depth++; depth > maxDestinationNesting {
					return "", "", -1
				}
			}
			if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
			if c == ' ' || c == '\n' {
				break
			}
			i++
		}
		destination = unescapeMarkdown(s[start:i])
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\n') {
		i++
	}
	if i < len(s) && (s[i] == '"' || s[i] == '\'') {
		quote := s[i]
		j := strings.IndexByte(s[i+1:], quote)
		if j < 0 {
			return "", "", -1
		}
		title = unescapeMarkdown(s[i+1 : i+1+j])
		i += j + 2
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\n') {
		i++
	}
	if i >= len(s) || s[i] != ')' {
		return "", "", -1
	}
	return destination, title, i + 1
}

// unescapeMarkdown drops the backslashes that were escaping punctuation, which
// a destination and a title carry as their literal text.
func unescapeMarkdown(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte(asciiPunctuation, s[i+1]) >= 0 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// anyTag is every HTML tag, which stripInline drops to reach the text under it.
var anyTag = regexp.MustCompile(`<[^>]*>`)

// htmlUnescaper reads back what escapeHTML wrote.
var htmlUnescaper = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&amp;", "&")

// stripInline is the plain text of a span, which is what an image alt carries.
func stripInline(s string) string {
	return htmlUnescaper.Replace(anyTag.ReplaceAllString(renderInline(s), ""))
}

// emphasis reads a run of emphasis markers and the span it closes, and reports
// how much of the string it took.
//
// Two markers are strong, one is emphasis, and two tildes are the GitHub
// flavour's strikethrough. An underscore inside a word opens nothing, which is
// what keeps snake_case_names whole.
func emphasis(s string, marker byte) (html string, width int, exhausted bool) {
	run := 0
	for run < len(s) && s[run] == marker {
		run++
	}
	if marker == '~' && run != 2 {
		return "", 0, false
	}
	if run > 2 {
		run = 2
	}
	if run+1 > len(s) || s[run] == ' ' || s[run] == '\n' {
		return "", 0, false
	}

	open := s[:run]
	for i := run; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if !strings.HasPrefix(s[i:], open) || s[i-1] == ' ' {
			continue
		}
		if i+run < len(s) && s[i+run] == marker {
			continue
		}
		if marker == '_' && i+run < len(s) && isWordByte(s[i+run]) {
			// An underscore inside a word closes nothing either, which is what
			// keeps snake_case_names whole.
			continue
		}
		inner := s[run:i]
		switch {
		case marker == '~':
			return "<del>" + renderInline(inner) + "</del>", i + run, false
		case run == 2:
			return "<strong>" + renderInline(inner) + "</strong>", i + run, false
		default:
			return "<em>" + renderInline(inner) + "</em>", i + run, false
		}
	}
	return "", 0, true
}
