package view_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/view"
)

type tokenPage struct{ token string }

func (p tokenPage) CSRFToken() string { return p.token }

// TestCSRFEscapesTheToken: the field was written with the token formatted
// straight into the quoted value, so a token carrying a quote closed the
// attribute and the rest of it was markup on the page.
func TestCSRFEscapesTheToken(t *testing.T) {
	var out strings.Builder
	if err := view.CSRF(&out, tokenPage{token: `"><script>alert(1)</script>`}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	want := `<input type="hidden" name="_token" value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;">`
	if got != want {
		t.Fatalf("CSRF = %q, want %q", got, want)
	}
}

// TestCSRFWritesAnOrdinaryTokenUnchanged: a token of letters and digits needs
// no escape, and comes out as it went in.
func TestCSRFWritesAnOrdinaryTokenUnchanged(t *testing.T) {
	var out strings.Builder
	if err := view.CSRF(&out, tokenPage{token: "a1B2c3"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), `<input type="hidden" name="_token" value="a1B2c3">`; got != want {
		t.Fatalf("CSRF = %q, want %q", got, want)
	}
}

// TestActiveAttributeRefusesWhatEscapingDoesNotAnswer: the one list every
// writer of a data-named attribute consults. Case does not matter to HTML, so
// it does not matter here.
func TestActiveAttributeRefusesWhatEscapingDoesNotAnswer(t *testing.T) {
	for _, name := range []string{
		"onclick", "ONCLICK", "onmouseover", "hx-post", "HX-GET", "data-hx-vals",
		"x-data", "data-x-on", "@click", ":class", "style", "STYLE", "srcdoc", "http-equiv",
	} {
		if err := view.ActiveAttribute(name); err == nil {
			t.Errorf("ActiveAttribute(%q) = nil, want a refusal", name)
		}
	}
}

// TestActiveAttributeLeavesTheInertOnes: the names a builder writes on every
// form -- and the ones a component owns, which are not this list's business.
func TestActiveAttributeLeavesTheInertOnes(t *testing.T) {
	for _, name := range []string{
		"id", "class", "name", "value", "type", "data-copy-text", "aria-label",
		"role", "href", "action", "accept-charset", "required", "open",
	} {
		if err := view.ActiveAttribute(name); err != nil {
			t.Errorf("ActiveAttribute(%q) = %v, want nil", name, err)
		}
	}
}
