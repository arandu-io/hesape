package str_test

import (
	"testing"

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
		{"see https://laravel.com", `<p>see <a href="https://laravel.com">https://laravel.com</a></p>` + "\n"},
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
