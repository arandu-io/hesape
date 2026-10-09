package str

import (
	"regexp"
	"strconv"
	"strings"
)

// Markdown renders CommonMark, with the GitHub flavour's strikethrough, task
// lists, tables and bare-URL autolinking, as HTML.
//
//	Markdown("# Hello")   // "<h1>Hello</h1>\n"
//	Markdown("*hello*")   // "<p><em>hello</em></p>\n"
//
// Raw HTML passes through untouched. Rendering untrusted Markdown therefore
// renders untrusted HTML: sanitize it after this, before it reaches a page.
//
// Block quotes and list items nest at most 100 deep, counted together, and
// emphasis, strikethrough, links and images nest at most 100 deep inside a
// block. A marker past that depth is text, escaped like any other text. Every
// level reads again the text it contains, so the bound is what keeps the time
// of a render proportional to the length of the input when the input is
// nothing but markers nested inside each other.
//
// There is nothing to configure and there will not be: the renderer is this
// file, and one way to spell a document is enough.
func Markdown(s string) string {
	var b strings.Builder
	renderBlocks(splitLines(s), 0, &b)
	return b.String()
}

// maxNesting is how deep block quotes and list items may nest inside each
// other, and how deep emphasis, links and images may nest inside a block.
// Each level reads again the lines or the text it holds, so without a bound a
// document of markers nested to its full length costs the square of its
// length.
const maxNesting = 100

// InlineMarkdown renders only the inline part of CommonMark -- emphasis, code
// spans, links, images -- with no block element around it.
//
//	InlineMarkdown("**Hello World**") // "<strong>Hello World</strong>\n"
//
// A heading marker or a list marker is left standing as text, because there is
// no block parser to read it.
//
// The notes on raw HTML and on the depth of nesting in the comment on Markdown
// hold here too.
func InlineMarkdown(s string) string {
	return renderInline(strings.TrimRight(s, "\n")) + "\n"
}

// splitLines cuts the document into lines, taking either line ending and
// dropping the one at the end so that a trailing break makes no empty block.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

var (
	atxHeading     = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*#*[ \t]*$`)
	setextHeading  = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	fenceOpen      = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*(.*)$")
	bulletItem     = regexp.MustCompile(`^( {0,3})([-*+])([ \t]+)`)
	orderedItem    = regexp.MustCompile(`^( {0,3})(\d{1,9})([.)])([ \t]+)`)
	taskMarker     = regexp.MustCompile(`^\[([ xX])\][ \t]+`)
	tableDelimiter = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	// htmlBlockStart wants a name that is followed by what a tag is followed
	// by, so that an autolink such as <https://example.com> is not read as one.
	htmlBlockStart = regexp.MustCompile(`^ {0,3}<(/?[A-Za-z][A-Za-z0-9-]*(\s|/?>)|!--)`)
)

// renderBlocks reads block elements off the front of the lines until they run
// out, writing the HTML for each. depth is the number of block quotes and list
// items the lines sit inside; at maxNesting a quote or list marker opens
// nothing and is read as paragraph text.
//
// The lines are the renderer's own, cut once from the document, and a block
// quote or a list item rewrites the ones it has read in place -- the marker or
// the indent taken off -- and renders that same range one level deeper. Every
// level reads its lines front to back and never returns to one it has passed,
// and a rewrite touches only lines the block has already claimed, so nothing
// sees it but the level below. A level holds its lines as a range of the one
// slice, not as a copy, because a hundred nested levels each copying every
// line beneath them hold a hundred copies of the document.
func renderBlocks(lines []string, depth int, b *strings.Builder) {
	container := depth < maxNesting
	for i := 0; i < len(lines); {
		line := lines[i]

		switch {
		case strings.TrimSpace(line) == "":
			i++

		case isThematicBreak(line):
			b.WriteString("<hr />\n")
			i++

		case atxHeading.MatchString(line):
			m := atxHeading.FindStringSubmatch(line)
			level := strconv.Itoa(len(m[1]))
			b.WriteString("<h" + level + ">" + renderInline(strings.TrimSpace(m[2])) + "</h" + level + ">\n")
			i++

		case fenceOpen.MatchString(line):
			i = renderFencedCode(lines, i, b)

		case container && quoteMarker(line) >= 0:
			i = renderBlockQuote(lines, i, depth, b)

		case container && (bulletItem.MatchString(line) || orderedItem.MatchString(line)):
			i = renderList(lines, i, depth, b)

		case isIndentedCode(line):
			i = renderIndentedCode(lines, i, b)

		case htmlBlockStart.MatchString(line):
			i = renderHTMLBlock(lines, i, b)

		default:
			i = renderParagraphOrTable(lines, i, depth, b)
		}
	}
}

func isIndentedCode(line string) bool {
	return strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
}

// isThematicBreak reports whether line is a thematic break: up to three spaces,
// then three or more of one of *, - or _, with only spaces and tabs between and
// after them. It reads the line once, by hand, because a line of nested list
// markers is read by it once per level.
func isThematicBreak(line string) bool {
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	if i == len(line) || (line[i] != '*' && line[i] != '-' && line[i] != '_') {
		return false
	}
	marker, count := line[i], 0
	for ; i < len(line); i++ {
		switch line[i] {
		case marker:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

// renderFencedCode reads a fenced code block and writes it out with the info
// string as a language class.
func renderFencedCode(lines []string, i int, b *strings.Builder) int {
	m := fenceOpen.FindStringSubmatch(lines[i])
	fence, info := m[1], strings.TrimSpace(m[2])
	indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))

	var body []string
	i++
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, fence[:1]) && len(trimmed) >= len(fence) && strings.Trim(trimmed, fence[:1]) == "" {
			i++
			break
		}
		body = append(body, trimLeadingSpaces(lines[i], indent))
		i++
	}

	b.WriteString("<pre><code")
	if info != "" {
		language, _, _ := strings.Cut(info, " ")
		b.WriteString(` class="language-` + escapeHTML(language) + `"`)
	}
	b.WriteString(">")
	for _, l := range body {
		b.WriteString(escapeHTML(l) + "\n")
	}
	b.WriteString("</code></pre>\n")
	return i
}

// renderIndentedCode reads the run of four-space-indented lines as a code
// block, keeping the blank lines that sit inside it.
func renderIndentedCode(lines []string, i int, b *strings.Builder) int {
	var body []string
	for i < len(lines) {
		if isIndentedCode(lines[i]) {
			body = append(body, trimLeadingSpaces(strings.Replace(lines[i], "\t", "    ", 1), 4))
			i++
			continue
		}
		if strings.TrimSpace(lines[i]) == "" && hasMoreIndentedCode(lines, i+1) {
			body = append(body, "")
			i++
			continue
		}
		break
	}
	b.WriteString("<pre><code>")
	for _, l := range body {
		b.WriteString(escapeHTML(l) + "\n")
	}
	b.WriteString("</code></pre>\n")
	return i
}

// hasMoreIndentedCode looks past a run of blank lines for another indented one,
// which is what keeps a blank line inside a code block instead of ending it.
func hasMoreIndentedCode(lines []string, i int) bool {
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return isIndentedCode(lines[i])
		}
	}
	return false
}

// lastIndex is the index renderIndentedCode stopped at, which it signals by
// running the cursor off the end.
func lastIndex(lines []string, i int) int {
	if i > len(lines) {
		return len(lines)
	}
	return i
}

// renderBlockQuote reads the run of quoted lines, strips the markers and
// renders what is left as blocks of its own, one level deeper. The markers are
// stripped in place and the quote renders its range of lines, as the comment
// on renderBlocks explains.
func renderBlockQuote(lines []string, i, depth int, b *strings.Builder) int {
	start := i
	for i < len(lines) {
		if width := quoteMarker(lines[i]); width >= 0 {
			// The rest of the line is sliced, not copied: a line of nested
			// markers would otherwise be copied once per level.
			lines[i] = lines[i][width:]
			i++
			continue
		}
		if strings.TrimSpace(lines[i]) == "" || i == start {
			break
		}
		// A lazy continuation line belongs to the paragraph inside the quote,
		// and is left as it is.
		i++
	}
	b.WriteString("<blockquote>\n")
	renderBlocks(lines[start:i], depth+1, b)
	b.WriteString("</blockquote>\n")
	return i
}

// quoteMarker is the width of the block quote marker that opens line -- up to
// three spaces, a >, and one space or tab after it -- or -1 when the line does
// not open with one.
func quoteMarker(line string) int {
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	if i == len(line) || line[i] != '>' {
		return -1
	}
	i++
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}

// listItem is one item of a list, with the lines that belong to it and whether
// a blank line sat inside or in front of it. The lines are the item's range of
// the list's own lines, rewritten in place, as the comment on renderBlocks
// explains.
type listItem struct {
	lines []string
	loose bool
}

// renderList reads a run of items sharing one marker type and writes the list.
func renderList(lines []string, i, depth int, b *strings.Builder) int {
	ordered := orderedItem.MatchString(lines[i])
	start := ""
	if ordered {
		start = orderedItem.FindStringSubmatch(lines[i])[2]
	}
	marker := itemMarker(lines[i])

	var items []listItem
	loose := false
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "" {
			// A blank line makes the list loose only if an item follows it.
			j := i
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) && itemMarker(lines[j]) == marker {
				loose = true
				i = j
				continue
			}
			break
		}
		if itemMarker(lines[i]) != marker {
			break
		}

		first := i
		content, indent := itemContent(lines[i])
		lines[i] = content
		item := listItem{}
		i++
		for i < len(lines) {
			switch {
			case strings.TrimSpace(lines[i]) == "":
				if j := i + 1; j < len(lines) && countIndent(lines[j]) >= indent {
					lines[i] = ""
					item.loose = true
					i++
					continue
				}
			case countIndent(lines[i]) >= indent:
				lines[i] = trimLeadingSpaces(lines[i], indent)
				i++
				continue
			case itemMarker(lines[i]) == "" && !startsBlock(lines[i]):
				// A lazy continuation line is left as it is.
				i++
				continue
			}
			break
		}
		item.lines = lines[first:i]
		items = append(items, item)
		loose = loose || item.loose
	}

	tag := "ul"
	open := "<ul>"
	if ordered {
		tag = "ol"
		open = "<ol>"
		if start != "" && start != "1" {
			open = `<ol start="` + start + `">`
		}
	}

	b.WriteString(open + "\n")
	for _, item := range items {
		writeListItem(item, loose, depth, b)
	}
	b.WriteString("</" + tag + ">\n")
	return i
}

// writeListItem writes one item, wrapping its text in a paragraph when the list
// is loose and leaving it bare when it is tight.
//
// It reads the item's lines where they are rather than joining them into one
// string and cutting that again, which would copy the text of every item once
// per level of nesting. The task marker sits on the first line, because what
// follows the brackets is spaces and tabs and never a line break.
func writeListItem(item listItem, loose bool, depth int, b *strings.Builder) {
	lines := item.lines
	checkbox := ""
	if m := taskMarker.FindStringSubmatch(lines[0]); m != nil {
		checked := ""
		if m[1] != " " {
			checked = ` checked=""`
		}
		checkbox = `<input` + checked + ` disabled="" type="checkbox"> `
		lines[0] = lines[0][len(m[0]):]
	}

	if !loose {
		if text, single := onlyText(lines); single {
			b.WriteString("<li>" + checkbox + renderInline(text) + "</li>\n")
			return
		}
	}
	b.WriteString("<li>\n")
	if checkbox != "" {
		b.WriteString(checkbox)
	}
	// A blank last line makes no empty block, as splitLines drops the break at
	// the end of a document.
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	renderBlocks(lines, depth+1, b)
	b.WriteString("</li>\n")
}

// onlyText is the trimmed text of the one line that is not blank, and whether
// there is at most one such line: the lines joined and trimmed hold no line
// break exactly when every line but one is blank.
func onlyText(lines []string) (string, bool) {
	text, seen := "", false
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			if seen {
				return "", false
			}
			text, seen = t, true
		}
	}
	return text, true
}

// itemMarker names the kind of list a line opens, so that a run of items with
// one marker is one list.
func itemMarker(line string) string {
	if m := bulletItem.FindStringSubmatch(line); m != nil {
		return "bullet" + m[2]
	}
	if m := orderedItem.FindStringSubmatch(line); m != nil {
		return "ordered" + m[3]
	}
	return ""
}

// itemContent is what an item line says and how far its continuation lines have
// to be indented to belong to it.
func itemContent(line string) (string, int) {
	if m := bulletItem.FindStringSubmatch(line); m != nil {
		return line[len(m[0]):], len(m[1]) + 1 + len(m[3])
	}
	m := orderedItem.FindStringSubmatch(line)
	return line[len(m[0]):], len(m[1]) + len(m[2]) + 1 + len(m[4])
}

// startsBlock reports whether a line opens a block of its own, which is what
// stops it from being read as the continuation of a paragraph.
func startsBlock(line string) bool {
	return isThematicBreak(line) || atxHeading.MatchString(line) ||
		fenceOpen.MatchString(line) || quoteMarker(line) >= 0 ||
		htmlBlockStart.MatchString(line)
}

// interruptsParagraph reports whether a line ends the paragraph above it. Past
// the nesting bound a quote or list marker opens nothing, so it is text and
// carries the paragraph on.
func interruptsParagraph(line string, depth int) bool {
	if depth >= maxNesting {
		return isThematicBreak(line) || atxHeading.MatchString(line) ||
			fenceOpen.MatchString(line) || htmlBlockStart.MatchString(line)
	}
	return startsBlock(line) || itemMarker(line) != ""
}

// renderHTMLBlock passes a run of raw HTML through to the output untouched.
func renderHTMLBlock(lines []string, i int, b *strings.Builder) int {
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		b.WriteString(lines[i] + "\n")
		i++
	}
	return i
}

// renderParagraphOrTable reads the run of lines up to the next blank line or
// block start, and writes it as a table, a setext heading or a paragraph.
func renderParagraphOrTable(lines []string, i, depth int, b *strings.Builder) int {
	start := i
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		// The setext underline is read before the block starters, because a run
		// of dashes is also a thematic break and under a paragraph it is not.
		if i > start && setextHeading.MatchString(line) {
			level := "2"
			if strings.HasPrefix(strings.TrimSpace(line), "=") {
				level = "1"
			}
			b.WriteString("<h" + level + ">" + renderInline(strings.TrimSpace(strings.Join(lines[start:i], "\n"))) + "</h" + level + ">\n")
			return i + 1
		}
		if i > start && interruptsParagraph(line, depth) {
			break
		}
		i++
	}
	// The paragraph is its range of the lines, read where they are.
	block := lines[start:i]

	if len(block) >= 2 && strings.Contains(block[0], "|") && tableDelimiter.MatchString(block[1]) {
		writeTable(block, b)
		return i
	}
	b.WriteString("<p>" + renderInline(strings.TrimSpace(strings.Join(block, "\n"))) + "</p>\n")
	return i
}

// writeTable writes the GitHub table whose header and delimiter row have
// already been recognised.
func writeTable(block []string, b *strings.Builder) {
	alignments := tableAlignments(block[1])
	b.WriteString("<table>\n<thead>\n<tr>\n")
	for i, cell := range tableCells(block[0]) {
		b.WriteString("<th" + alignmentAt(alignments, i) + ">" + renderInline(cell) + "</th>\n")
	}
	b.WriteString("</tr>\n</thead>\n")
	if len(block) > 2 {
		b.WriteString("<tbody>\n")
		for _, row := range block[2:] {
			b.WriteString("<tr>\n")
			for i, cell := range tableCells(row) {
				b.WriteString("<td" + alignmentAt(alignments, i) + ">" + renderInline(cell) + "</td>\n")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n")
	}
	b.WriteString("</table>\n")
}

func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	cells := strings.Split(row, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func tableAlignments(delimiter string) []string {
	var out []string
	for _, cell := range tableCells(delimiter) {
		left, right := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			out = append(out, "center")
		case right:
			out = append(out, "right")
		case left:
			out = append(out, "left")
		default:
			out = append(out, "")
		}
	}
	return out
}

func alignmentAt(alignments []string, i int) string {
	if i < len(alignments) && alignments[i] != "" {
		return ` align="` + alignments[i] + `"`
	}
	return ""
}

// trimLeadingSpaces removes up to n leading spaces, which is how a nested block
// is brought back to column zero before it is parsed again.
func trimLeadingSpaces(line string, n int) string {
	i := 0
	for i < n && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

// countIndent is the number of leading spaces on a line, with a tab counting
// as four.
func countIndent(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// htmlEscaper escapes the four characters that cannot stand for themselves in
// HTML text.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escapeHTML(s string) string { return htmlEscaper.Replace(s) }
