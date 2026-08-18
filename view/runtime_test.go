package view_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/view"
)

// TestAMethodInterpolatedInsteadOfItsResultIsRefused.
//
// {{ .Greeting }} and {{ .Greeting() }} both compile: without the parentheses
// the expression is the method value, and fmt's %v prints a function as its
// address. The showcase shipped a verification e-mail whose plain-text part read
// "Hello 0x10273d6d0, and welcome." to everybody who registered, and it stayed
// invisible because the HTML part beside it was written correctly -- only a
// reader whose client prefers text ever saw it, which is exactly the audience a
// text part exists for.
func TestAMethodInterpolatedInsteadOfItsResultIsRefused(t *testing.T) {
	for name, value := range map[string]any{
		"func() string":         func() string { return "Ada" },
		"func() int":            func() int { return 1 },
		"func() (string, bool)": func() (string, bool) { return "Ada", true },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("a %s was rendered instead of refused, which is how an address reaches somebody's inbox", name)
				}
				if !strings.Contains(fmt.Sprint(recovered), "{{ .Name() }}") {
					t.Errorf("the panic does not say how to fix it: %v", recovered)
				}
			}()
			_ = view.Text(value)
		})
	}
}

// And the values a view actually interpolates still render.
func TestTheOrdinaryValuesStillRender(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{"Ada", "Ada"}, {42, "42"}, {int64(42), "42"}, {true, "true"},
		{1.5, "1.5"}, {nil, ""}, {struct{ A int }{1}, "{1}"},
	} {
		if got := view.Text(c.in); got != c.want {
			t.Errorf("Text(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAValueCannotEndTheAttributeItSitsIn.
//
// Every case is asserted whole rather than searched for a fragment: a test that
// looks for "&#34;" in the output passes on markup that also carries the quote
// it failed to escape somewhere else in the same string.
func TestAValueCannotEndTheAttributeItSitsIn(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{`a" onfocus="alert(1)`, `a&#34; onfocus=&#34;alert(1)`},
		{`a' onfocus='alert(1)`, `a&#39; onfocus=&#39;alert(1)`},
		{`<script>alert(1)</script>`, `&lt;script&gt;alert(1)&lt;/script&gt;`},
		{`Tom & Jerry`, `Tom &amp; Jerry`},
		{`/posts/1`, `/posts/1`},

		// The payload that ends an attribute written without quotes, and it comes
		// back unchanged, which is the contract and not an oversight: none of its
		// characters mean anything inside quotes, and the space that ends the
		// value in the other position ends nothing here. What answers it is the
		// quote, which is why an attribute value without quotes is a position this
		// is not for.
		{`x onfocus=alert(1) autofocus`, `x onfocus=alert(1) autofocus`},

		// The inline-handler payload, likewise unchanged in meaning by the time a
		// script engine sees it: the entity is decoded back into the quote before
		// the handler is parsed, so this output closes the JavaScript string it
		// was meant to sit inside. An attribute whose value is code is a
		// JavaScript position, not an attribute-value one.
		{`');alert(1);//`, `&#39;);alert(1);//`},
	} {
		if got := view.TextAttr(c.in); got != c.want {
			t.Errorf("TextAttr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAURLSchemeABrowserRunsIsRefused.
//
// Escaping has nothing to do here -- none of these carries a character that
// escaping touches -- so the refusal is the whole defence, and what is asserted
// is that nothing at all comes back with it.
func TestAURLSchemeABrowserRunsIsRefused(t *testing.T) {
	for _, in := range []string{
		`javascript:alert(1)`,
		`JaVaScRiPt:alert(1)`,
		` javascript:alert(1)`,
		"java\tscript:alert(1)",
		"javascript\n:alert(1)",
		`vbscript:msgbox(1)`,
		`data:text/html,<script>alert(1)</script>`,
		`//evil.example/steal`,
		`/\evil.example/steal`,
		`\\evil.example/steal`,
		`\/evil.example/steal`,
		"/\t/evil.example/steal",
	} {
		got, err := view.TextURL(in)
		if err == nil {
			t.Errorf("TextURL(%q) = %q, want a refusal", in, got)
			continue
		}
		if got != "" {
			t.Errorf("TextURL(%q) refused and still returned %q", in, got)
		}
	}
}

// TestAURLThatIsAllowedKeepsItsMeaning.
//
// The other half of the refusal: an escaper that answers javascript: by
// refusing every URL would be a page with no links on it.
func TestAURLThatIsAllowedKeepsItsMeaning(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{`/posts/1?page=2&sort=asc`, `/posts/1?page=2&amp;sort=asc`},
		{`https://example.com/a`, `https://example.com/a`},
		{`HTTPS://example.com/a`, `HTTPS://example.com/a`},
		{`http://example.com/a`, `http://example.com/a`},
		{`mailto:ada@example.com`, `mailto:ada@example.com`},
		{`tel:+5511999999999`, `tel:+5511999999999`},
		{`#top`, `#top`},
		{`/`, `/`},
		{``, ``},

		// A colon after the first slash is a path segment and not a scheme, which
		// is the reading a browser gives it too.
		{`/a:b/c`, `/a:b/c`},

		// Allowed, and still escaped: the scheme check answers what the URL means
		// and the escape answers how it is written, and a URL needs both.
		{`/a" onmouseover="alert(1)`, `/a&#34; onmouseover=&#34;alert(1)`},
	} {
		got, err := view.TextURL(c.in)
		if err != nil {
			t.Errorf("TextURL(%q) was refused: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("TextURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAValueInAScriptComesOutAsALiteral.
//
// The payload here carries none of the characters an HTML escape replaces, so
// the escape that answers every other position writes it into the page
// untouched and the browser runs it. What answers this position is the quoting,
// and the assertion is on the whole literal because the quotes are half of it.
func TestAValueInAScriptComesOutAsALiteral(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{`1;alert(document.cookie)`, `"1;alert(document.cookie)"`},
		{`${alert(1)}`, `"${alert(1)}"`},
		{`</script><img src=x onerror=alert(1)>`, `"\u003c/script\u003e\u003cimg src=x onerror=alert(1)\u003e"`},
		{`he said "hi"`, `"he said \"hi\""`},
		{`abc\`, `"abc\\"`},
		{`a&b`, `"a\u0026b"`},
		{"a\nb\tc", `"a\nb\tc"`},
		// A line terminator, a NUL and the characters that would close the script
		// element: the want is the six characters of a hex escape, spelled here in
		// a raw string, and not the character itself.
		{"a\u2028b\u2029c", `"a\u2028b\u2029c"`},
		{"a\x00b", `"a\u0000b"`},
		{`café`, `"café"`},

		// A number stays a number, which is what a script asks for when it
		// interpolates one, and a string that looks like a number does not.
		{42, `42`},
		{int64(42), `42`},
		{1.5, `1.5`},
		{`42`, `"42"`},
		{true, `true`},
		{false, `false`},
		{nil, `null`},

		// JavaScript spells these as identifiers rather than literals, and an
		// identifier is a name the page can have shadowed.
		{math.NaN(), `null`},
		{math.Inf(1), `null`},
		{math.Inf(-1), `null`},
	} {
		if got := view.TextJS(c.in); got != c.want {
			t.Errorf("TextJS(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestACSSValueThatIsNotOneIsRefused.
//
// A style attribute takes a declaration list, so a value that carries a
// semicolon writes a second declaration, and one that carries parentheses
// writes a url() whose scheme is its own decision again. Neither is spelled
// with a character an HTML escape touches.
func TestACSSValueThatIsNotOneIsRefused(t *testing.T) {
	for _, in := range []string{
		`red; background: url(javascript:alert(1))`,
		`red;}</style><script>alert(1)</script>`,
		`expression(alert(1))`,
		`url(https://evil.example/x)`,
		`red/*hidden*/`,
		`red\3b color:blue`,
		`"quoted"`,
		`café`,
	} {
		got, err := view.TextCSS(in)
		if err == nil {
			t.Errorf("TextCSS(%q) = %q, want a refusal", in, got)
			continue
		}
		if got != "" {
			t.Errorf("TextCSS(%q) refused and still returned %q", in, got)
		}
	}
}

// TestACSSValueThatIsOneGoesOutUnchanged.
//
// The accepted set holds no character the HTML parser reads, which is why
// nothing is escaped on the way out, and this is the test that says so.
func TestACSSValueThatIsOneGoesOutUnchanged(t *testing.T) {
	for _, in := range []string{
		`red`,
		`#ff0000`,
		`12px`,
		`1.5rem`,
		`0 auto`,
		`bold !important`,
		`50%`,
		``,
	} {
		got, err := view.TextCSS(in)
		if err != nil {
			t.Errorf("TextCSS(%q) was refused: %v", in, err)
			continue
		}
		if got != in {
			t.Errorf("TextCSS(%q) = %q, want it unchanged", in, got)
		}
	}
}
