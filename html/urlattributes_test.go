package html_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/html"
)

// TestAnAddressInAttrsIsHeldToTheSchemeRule: Link and Image checked the URL
// they were given, while the same URL passed in Attrs -- an href on a button
// builder, a formaction on a submit, a src on anything -- was written as given,
// javascript: included.
func TestAnAddressInAttrsIsHeldToTheSchemeRule(t *testing.T) {
	refused := []html.Attrs{
		{"href": "javascript:alert(1)"},
		{"HREF": "JavaScript:alert(1)"},
		{"src": "data:text/html,<script>alert(1)</script>"},
		{"formaction": "javascript:alert(1)"},
		{"action": "//evil.example/steal"},
		{"xlink:href": "vbscript:msgbox(1)"},
		{"poster": "java\tscript:alert(1)"},
	}
	for _, attrs := range refused {
		got := string(newBuilder().Attributes(attrs))
		if got != "" {
			t.Errorf("Attributes(%v) = %q, want the refused address left out", attrs, got)
		}
	}

	kept := string(newBuilder().Attributes(html.Attrs{"href": "/invoices?page=2", "formaction": "https://example.test/x", "title": "javascript:is text here"}))
	for _, want := range []string{`href="/invoices?page=2"`, `formaction="https://example.test/x"`, `title="javascript:is text here"`} {
		if !strings.Contains(kept, want) {
			t.Errorf("Attributes = %q, want %s: a relative or http(s) address is written, and a non-URL attribute is text", kept, want)
		}
	}
}

// TestASubmitButtonCannotCarryAScriptAction: formaction overrides the form's own
// action for the button that carries it, so it is the same hole one element
// lower.
func TestASubmitButtonCannotCarryAScriptAction(t *testing.T) {
	form, _ := newForm()
	got := string(form.Submit("Pay", html.Attrs{"formaction": "javascript:alert(1)"}))
	if strings.Contains(strings.ToLower(got), "javascript") {
		t.Fatalf("Submit wrote the script address: %s", got)
	}
}

// TestAFormCannotOpenOnAScriptAction: the action resolved for Open -- here the
// current URL, which is what an unspecified action falls back to -- was written
// without the check Link applies.
func TestAFormCannotOpenOnAScriptAction(t *testing.T) {
	form, urls := newForm()
	urls.current = "javascript:alert(document.cookie)"
	got, err := form.Open(html.OpenOptions{})
	if !errors.Is(err, html.ErrUnsafeAction) {
		t.Fatalf("Open = %q, %v; want ErrUnsafeAction", got, err)
	}
}
