package str_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/str"
)

func TestMarkdownBlocks(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"# Hello", "<h1>Hello</h1>\n"},
		{"###### Six", "<h6>Six</h6>\n"},
		{"Title\n=====", "<h1>Title</h1>\n"},
		{"Title\n-----", "<h2>Title</h2>\n"},
		{"Hello", "<p>Hello</p>\n"},
		{"Hello\n\nWorld", "<p>Hello</p>\n<p>World</p>\n"},
		{"---", "<hr />\n"},
		{"***", "<hr />\n"},
		{"- a\n- b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
		{"1. a\n2. b", "<ol>\n<li>a</li>\n<li>b</li>\n</ol>\n"},
		{"3. a", "<ol start=\"3\">\n<li>a</li>\n</ol>\n"},
		{"> quoted", "<blockquote>\n<p>quoted</p>\n</blockquote>\n"},
		{"    code", "<pre><code>code</code></pre>\n"[:len("<pre><code>code")] + "\n</code></pre>\n"},
		{"```\nraw\n```", "<pre><code>raw\n</code></pre>\n"},
		{"```go\nx := 1\n```", "<pre><code class=\"language-go\">x := 1\n</code></pre>\n"},
		{"<div>raw</div>", "<div>raw</div>\n"},
	}
	for _, c := range cases {
		if got := str.Markdown(c.in); got != c.want {
			t.Errorf("Markdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownInlines(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"*em*", "<p><em>em</em></p>\n"},
		{"_em_", "<p><em>em</em></p>\n"},
		{"**strong**", "<p><strong>strong</strong></p>\n"},
		{"__strong__", "<p><strong>strong</strong></p>\n"},
		{"~~gone~~", "<p><del>gone</del></p>\n"},
		{"`code`", "<p><code>code</code></p>\n"},
		{"`` a ` b ``", "<p><code>a ` b</code></p>\n"},
		{"[text](https://example.com)", `<p><a href="https://example.com">text</a></p>` + "\n"},
		{`[text](https://example.com "t")`, `<p><a href="https://example.com" title="t">text</a></p>` + "\n"},
		{"![alt](/i.png)", `<p><img src="/i.png" alt="alt" /></p>` + "\n"},
		{"<https://example.com>", `<p><a href="https://example.com">https://example.com</a></p>` + "\n"},
		{"<a@b.com>", `<p><a href="mailto:a@b.com">a@b.com</a></p>` + "\n"},
		{"a & b", "<p>a &amp; b</p>\n"},
		{"&amp;", "<p>&amp;</p>\n"},
		{"5 < 6 > 4", "<p>5 &lt; 6 &gt; 4</p>\n"},
		{`\*not em\*`, "<p>*not em*</p>\n"},
		{"snake_case_name", "<p>snake_case_name</p>\n"},
		{"line  \nbreak", "<p>line<br />\nbreak</p>\n"},
		{"see https://example.com", `<p>see <a href="https://example.com">https://example.com</a></p>` + "\n"},
		{"* not emphasis", "<ul>\n<li>not emphasis</li>\n</ul>\n"},
	}
	for _, c := range cases {
		if got := str.Markdown(c.in); got != c.want {
			t.Errorf("Markdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInlineMarkdown(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"*hello world*", "<em>hello world</em>\n"},
		{"**Hello World**", "<strong>Hello World</strong>\n"},
		{"# Hello", "# Hello\n"},
		{"`code`", "<code>code</code>\n"},
		{"", "\n"},
	}
	for _, c := range cases {
		if got := str.InlineMarkdown(c.in); got != c.want {
			t.Errorf("InlineMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMarkdownPassesRawHTMLThrough records the choice: raw HTML in the source
// is raw HTML in the output, so untrusted Markdown has to be sanitized after
// this.
func TestMarkdownPassesRawHTMLThrough(t *testing.T) {
	if got := str.Markdown("<script>alert(1)</script>"); got != "<script>alert(1)</script>\n" {
		t.Errorf("Markdown of a script tag = %q", got)
	}
	if got := str.Markdown("a <b>bold</b> word"); got != "<p>a <b>bold</b> word</p>\n" {
		t.Errorf("Markdown of an inline tag = %q", got)
	}
}

func TestMarkdownTable(t *testing.T) {
	got := str.Markdown("| a | b |\n| :- | -: |\n| 1 | 2 |")
	want := "<table>\n<thead>\n<tr>\n" +
		"<th align=\"left\">a</th>\n<th align=\"right\">b</th>\n" +
		"</tr>\n</thead>\n<tbody>\n<tr>\n" +
		"<td align=\"left\">1</td>\n<td align=\"right\">2</td>\n" +
		"</tr>\n</tbody>\n</table>\n"
	if got != want {
		t.Errorf("Markdown of a table = %q, want %q", got, want)
	}
}

func TestMarkdownTaskList(t *testing.T) {
	got := str.Markdown("- [ ] todo\n- [x] done")
	want := "<ul>\n" +
		`<li><input disabled="" type="checkbox"> todo</li>` + "\n" +
		`<li><input checked="" disabled="" type="checkbox"> done</li>` + "\n" +
		"</ul>\n"
	if got != want {
		t.Errorf("Markdown of a task list = %q, want %q", got, want)
	}
}

func TestMarkdownLooseList(t *testing.T) {
	got := str.Markdown("- a\n\n- b")
	want := "<ul>\n<li>\n<p>a</p>\n</li>\n<li>\n<p>b</p>\n</li>\n</ul>\n"
	if got != want {
		t.Errorf("Markdown of a loose list = %q, want %q", got, want)
	}
}

// TestMarkdownReadsATrailingBackslashInALinkAsText holds a destination that
// ends on the backslash that would have escaped its next character. There is
// no next character, and reading past the end panicked the whole render.
func TestMarkdownReadsATrailingBackslashInALinkAsText(t *testing.T) {
	for _, src := range []string{"[](\\", "![](\\", "[a](b\\", "[a](b \\", "x [a](\\"} {
		got := str.Markdown(src)
		if strings.Contains(got, "<a ") || strings.Contains(got, "<img ") {
			t.Errorf("Markdown(%q) = %q, want the text with no link", src, got)
		}
	}
}

// FuzzMarkdownNeverPanics runs its seeds on every test run and searches past
// them under `go test -fuzz`: a renderer handed text somebody typed has an
// answer for all of it.
func FuzzMarkdownNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"[](\\", "[a](<b>)", "![a](b \"c\")", "`a", "***a**", "| a |\n|---|\n| b |",
		"- [ ] a\n  - b", "> > a\n>\n> b", "<div>\n\na", "a  \nb\\\nc", "1. a\n\n   b",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_ = str.Markdown(src)
		_ = str.InlineMarkdown(src)
	})
}

// TestMarkdownStaysLinearOnOpenersThatNeverClose holds the shapes that once
// made each opener read the rest of the text: emphasis with no closer, a [
// with no ], a destination that keeps opening parentheses, and a run of hard
// breaks.
//
// It measures the growth rather than the time. Doubling the input doubles the
// work of a linear render and quadruples it when every opener reads the rest
// of the text; a wall-clock bound is instead a guess about the machine, and the
// race detector on a shared runner made a linear render of 60 KB take a second.
// The fastest of a few runs is compared, and a render too quick to measure is
// too quick to be quadratic.
func TestMarkdownStaysLinearOnOpenersThatNeverClose(t *testing.T) {
	for name, unit := range map[string]string{
		"emphasis":      "*a ",
		"strong":        "__a ",
		"strikethrough": "~~a ",
		"brackets":      "[",
		"links":         "[a](",
		"destinations":  "[a](b(",
		"hard breaks":   "a  \n",
	} {
		reps := 16000 / len(unit)
		checkLinearGrowth(t, name, strings.Repeat(unit, reps/2), strings.Repeat(unit, 2*reps))
	}
}

// TestMarkdownBoundsTheNestingOfADestination keeps the CommonMark allowance
// for nested parentheses in a destination, up to the bound.
func TestMarkdownBoundsTheNestingOfADestination(t *testing.T) {
	nested := func(n int) string {
		return "[a](x" + strings.Repeat("(", n) + strings.Repeat(")", n) + ")"
	}
	if got := str.Markdown(nested(32)); !strings.Contains(got, "<a href=") {
		t.Errorf("32 nested parentheses are not a link: %q", got)
	}
	if got := str.Markdown(nested(33)); strings.Contains(got, "<a href=") {
		t.Errorf("33 nested parentheses are a link: %q", got)
	}
	if got := str.Markdown("[a](b(c)d)"); !strings.Contains(got, `<a href="b(c)d">a</a>`) {
		t.Errorf("a balanced pair in a destination = %q", got)
	}
}

// fastestRenders is the fastest of a few renders of small and of large, taken
// in turns. The fastest is the one the rest of the machine disturbed least, and
// taking the two in turns means a slow stretch on a shared machine slows both
// instead of only the one measured during it.
//
// The heap is collected before every render, so that each one starts from the
// same heap and pays the collections its own allocations cause. Without it the
// large render leaves the collector a target sized to its garbage, the small
// render after it runs under that target without one collection, and the
// fastest small render is the one that paid nothing for its allocations: a
// linear render that allocates heavily then looked eight times slower at four
// times the input.
func fastestRenders(small, large string) (time.Duration, time.Duration) {
	bestSmall, bestLarge := time.Duration(1<<62), time.Duration(1<<62)
	for range 5 {
		runtime.GC()
		start := time.Now()
		str.Markdown(small)
		bestSmall = min(bestSmall, time.Since(start))
		runtime.GC()
		start = time.Now()
		str.Markdown(large)
		bestLarge = min(bestLarge, time.Since(start))
	}
	return bestSmall, bestLarge
}

// checkLinearGrowth fails when rendering large, four times the length of small,
// takes more than eight times as long. A linear render takes four times as long
// and a quadratic one sixteen, so eight sits far from both: doubling the length
// left a gap of 2 against 4, narrow enough that a busy shared runner under the
// race detector crossed it with a linear render. A render of large too quick to
// measure is too quick to be quadratic, and is not compared.
func checkLinearGrowth(t *testing.T, name, small, large string) {
	t.Helper()
	smallTime, largeTime := fastestRenders(small, large)
	if largeTime < 10*time.Millisecond {
		return
	}
	if growth := float64(largeTime) / float64(smallTime); growth > 8 {
		t.Errorf("%s: four times the input took %.1f times as long (%s to %s)", name, growth, smallTime, largeTime)
	}
}

// TestMarkdownStaysLinearOnHostileInput holds the bodies an application that
// renders what its users type has to survive, each at its length and at twice
// it.
func TestMarkdownStaysLinearOnHostileInput(t *testing.T) {
	for _, c := range []struct {
		name string
		src  func(n int) string
		n    int
	}{
		{"emphasis", func(n int) string { return strings.Repeat("*a ", n) }, 20000},
		{"strong", func(n int) string { return strings.Repeat("__a ", n) }, 15000},
		{"brackets", func(n int) string { return strings.Repeat("[", 2*n) + strings.Repeat("](", n) }, 10000},
		{"links", func(n int) string { return strings.Repeat("[a](", n) }, 15000},
		{"backticks", func(n int) string { return strings.Repeat("`", n) }, 30001},
		{"quotes", func(n int) string { return strings.Repeat("> ", n) + "x" }, 5000},
		{"list markers", func(n int) string { return strings.Repeat("- ", n) + "x" }, 5000},
		{"hard breaks", func(n int) string { return strings.Repeat("a  \n", n) }, 15000},
	} {
		checkLinearGrowth(t, c.name, c.src(c.n/2), c.src(2*c.n))
	}
}

// TestMarkdownStaysLinearOnNestedContainers holds the shapes where every level
// of nesting once read again everything it held: quotes, lists and spans
// nested to the full length of the text, and the lazy lines of a deep quote,
// which every level carried down to the paragraph at the bottom.
func TestMarkdownStaysLinearOnNestedContainers(t *testing.T) {
	for _, c := range []struct {
		name string
		src  func(n int) string
		n    int
	}{
		{"nested quotes", func(n int) string { return strings.Repeat("> ", n) + "x" }, 64000},
		{"heading in nested quotes", func(n int) string { return strings.Repeat("> ", n) + "# x" }, 64000},
		{"fence in nested quotes", func(n int) string {
			return strings.Repeat("> ", n) + "```\n" + strings.Repeat("> ", n) + "x"
		}, 64000},
		// Every lazy line is carried by each of the hundred levels, so the
		// render is linear with a large constant. The length keeps the small
		// render at several milliseconds, well above what a collection or a
		// descheduled thread adds to one render.
		{"lazy lines under nested quotes", func(n int) string {
			return strings.Repeat("> ", n) + "x\n" + strings.Repeat("y\n", n)
		}, 12000},
		{"nested lists", func(n int) string { return strings.Repeat("- ", n) + "x\ny" }, 2000},
		{"quote in list", func(n int) string { return strings.Repeat("- > ", n) + "x\ny" }, 2000},
		{"list in quote", func(n int) string { return strings.Repeat("> - ", n) + "x\ny" }, 2000},
		{"nested strong", func(n int) string { return strings.Repeat("**", n) + "x" + strings.Repeat("**", n) }, 5000},
		{"nested links", func(n int) string { return strings.Repeat("[", n) + "x" + strings.Repeat("](u)", n) }, 5000},
		{"nested images", func(n int) string { return strings.Repeat("![", n) + "x" + strings.Repeat("](u)", n) }, 3000},
		{"emphasis in links", func(n int) string { return strings.Repeat("[*", n) + "x" + strings.Repeat("*](u)", n) }, 3000},
	} {
		checkLinearGrowth(t, c.name, c.src(c.n/2), c.src(2*c.n))
	}
}

// TestMarkdownBoundsContainerNesting holds the depth past which a marker is
// text: a hundred quotes or list items nest, and the marker after them is
// written escaped, inside the innermost one.
func TestMarkdownBoundsContainerNesting(t *testing.T) {
	quotes := str.Markdown(strings.Repeat("> ", 100) + "x")
	if n := strings.Count(quotes, "<blockquote>"); n != 100 {
		t.Errorf("100 nested quotes rendered %d", n)
	}
	deeper := str.Markdown(strings.Repeat("> ", 101) + "x\n" + strings.Repeat("> ", 101) + "- y")
	if n := strings.Count(deeper, "<blockquote>"); n != 100 {
		t.Errorf("101 nested quotes rendered %d", n)
	}
	if !strings.Contains(deeper, "<blockquote>\n<p>&gt; x\n&gt; - y</p>\n</blockquote>") {
		t.Errorf("the marker past the bound is not text in the innermost quote: %q", deeper[len(deeper)/2-60:len(deeper)/2+60])
	}

	lists := str.Markdown(strings.Repeat("- ", 101) + "x\ny")
	if n := strings.Count(lists, "<ul>"); n != 100 {
		t.Errorf("101 nested list items rendered %d lists", n)
	}
	if !strings.Contains(lists, "<li>\n<p>- x\ny</p>\n</li>") {
		t.Errorf("the marker past the bound is not text in the innermost item")
	}
}

// TestMarkdownBoundsSpanNesting holds the same depth for emphasis and links:
// the span past the hundredth is written as its markers.
func TestMarkdownBoundsSpanNesting(t *testing.T) {
	strong := str.Markdown(strings.Repeat("**", 101) + "x" + strings.Repeat("**", 101))
	if n := strings.Count(strong, "<strong>"); n != 100 {
		t.Errorf("101 nested strong spans rendered %d", n)
	}
	links := str.Markdown(strings.Repeat("[", 101) + "x" + strings.Repeat("](u)", 101))
	if n := strings.Count(links, "<a "); n != 100 {
		t.Errorf("101 nested links rendered %d", n)
	}
	if !strings.Contains(links, `<a href="u">[x](u)</a>`) {
		t.Errorf("the link past the bound is not text in the innermost one")
	}
}

// TestMarkdownRendersShallowNestingAsBefore pins what the bound must not
// touch: quotes, lists and spans nested a few levels deep, as documents
// actually nest them.
func TestMarkdownRendersShallowNestingAsBefore(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"> one\n> > two\n> > > three\n> > > lazy\n>\n> back",
			"<blockquote>\n<p>one</p>\n<blockquote>\n<p>two</p>\n<blockquote>\n<p>three\nlazy</p>\n</blockquote>\n</blockquote>\n<p>back</p>\n</blockquote>\n",
		},
		{
			"> - a\n>   - b\n>\n> c",
			"<blockquote>\n<ul>\n<li>\n<p>a</p>\n<ul>\n<li>b</li>\n</ul>\n</li>\n</ul>\n<p>c</p>\n</blockquote>\n",
		},
		{
			"- a\n  > quoted\n  > - inner\n- b",
			"<ul>\n<li>\n<p>a</p>\n<blockquote>\n<p>quoted</p>\n<ul>\n<li>inner</li>\n</ul>\n</blockquote>\n</li>\n<li>b</li>\n</ul>\n",
		},
		{
			"> # Title\n> ```go\n> x := 1\n> ```",
			"<blockquote>\n<h1>Title</h1>\n<pre><code class=\"language-go\">x := 1\n</code></pre>\n</blockquote>\n",
		},
		{
			"**a *b ~~c~~ b* a** [l *e* ![i](s)](d)",
			"<p><strong>a <em>b <del>c</del> b</em> a</strong> <a href=\"d\">l <em>e</em> <img src=\"s\" alt=\"i\" /></a></p>\n",
		},
	}
	for _, c := range cases {
		if got := str.Markdown(c.in); got != c.want {
			t.Errorf("Markdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMarkdownHoldsNestedLinesOnce bounds what a render allocates for each
// byte of input on the shapes where every level of nesting holds the lines
// beneath it: the lazy lines of a deep quote or a deep list item, and quotes
// and lists nested inside each other. Each level once copied those lines into
// a slice of its own, or joined them into a string and cut it again, so a
// hundred levels held a hundred copies of the document while the render ran:
// a quote or a list with lazy lines allocated 1.2 to 2.7 KB, and kept 0.5 to
// 1 KB live, for every byte of input. Holding a level as a range of the one
// slice of lines leaves at most about 13 bytes per byte, and the bound sits
// well above that and well below the 60 the mildest copying shape allocated.
// The time of the same shapes is held by
// TestMarkdownStaysLinearOnNestedContainers.
func TestMarkdownHoldsNestedLinesOnce(t *testing.T) {
	const bound = 32
	for _, c := range []struct {
		name string
		src  string
	}{
		{"lazy lines under nested quotes", strings.Repeat("> ", 5000) + "x\n" + strings.Repeat("y\n", 5000)},
		{"lazy lines under nested lists", strings.Repeat("- ", 2500) + "x\n" + strings.Repeat("y\n", 2500)},
		{"lazy lines under quotes in lists", strings.Repeat("- > ", 2000) + "x\n" + strings.Repeat("y\n", 2000)},
		{"nested lists", strings.Repeat("- ", 10000) + "x\ny"},
		{"quote in list", strings.Repeat("- > ", 5000) + "x\ny"},
		{"list in quote", strings.Repeat("> - ", 5000) + "x\ny"},
	} {
		if perByte := allocatedPerByte(t, c.src); perByte > bound {
			t.Errorf("%s: a render of %d bytes allocated %.0f bytes for each, want at most %d", c.name, len(c.src), perByte, bound)
		} else {
			t.Logf("%s: %.1f bytes allocated per input byte", c.name, perByte)
		}
	}
}

// allocatedPerByte is what one render of src allocates in package str,
// divided by the length of src, read from a memory profile that records every
// allocation.
//
// The profile is read rather than the heap's running total because of what
// package regexp allocates under the race detector. A matcher keeps its
// backtracking state, 32 KB of it, in a sync.Pool, and with the detector on the
// pool drops a quarter of what is put back, so a render that matches a short
// line a million times allocates gigabytes that a build without the detector
// never does. That is the detector's cost and not the renderer's, and it would
// bury the copies this measures; the allocations made under regexp are left
// out, and what remains is the same with the detector on and off.
func allocatedPerByte(t *testing.T, src string) float64 {
	t.Helper()
	defer func(rate int) { runtime.MemProfileRate = rate }(runtime.MemProfileRate)
	runtime.MemProfileRate = 1
	before := rendererAllocations()
	str.Markdown(src)
	return float64(rendererAllocations()-before) / float64(len(src))
}

// rendererAllocations is the total the memory profile has recorded allocated
// under a function of package str and outside package regexp. The collection
// it runs first is what publishes the allocations made since the last one.
func rendererAllocations() int64 {
	runtime.GC()
	var records []runtime.MemProfileRecord
	n, ok := runtime.MemProfile(nil, true)
	for !ok {
		records = make([]runtime.MemProfileRecord, n+64)
		n, ok = runtime.MemProfile(records, true)
	}
	var total int64
	for _, r := range records[:n] {
		inStr, inRegexp := false, false
		frames := runtime.CallersFrames(r.Stack())
		for {
			frame, more := frames.Next()
			inStr = inStr || strings.HasPrefix(frame.Function, "github.com/arandu-io/hesape/str.")
			inRegexp = inRegexp || strings.HasPrefix(frame.Function, "regexp.")
			if !more {
				break
			}
		}
		if inStr && !inRegexp {
			total += r.AllocBytes
		}
	}
	return total
}
