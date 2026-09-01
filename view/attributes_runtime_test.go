package view_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/view"
)

// TestAttributesWritesSortedPairs holds the shape generated code depends on:
// a leading space before each pair, so the text drops straight into a tag after
// an attribute the component itself wrote, and one order for a given map.
//
// A Go map has no order of its own, so without the sort the same props would
// render two different documents on two runs -- which no test can hold and no
// cache can key.
func TestAttributesWritesSortedPairs(t *testing.T) {
	got, err := view.Attributes(map[string]string{
		"data-testid": "message-actions",
		"data-action": "archive",
		"title":       "Archive",
	})
	if err != nil {
		t.Fatalf("Attributes() = %v", err)
	}

	want := ` data-action="archive" data-testid="message-actions" title="Archive"`
	if got != want {
		t.Fatalf("Attributes() = %q, want %q", got, want)
	}
}

// TestAttributesOfNothingIsNothing pins the empty case, because a component
// writes this call on every element and most elements carry no extra attribute.
// A single stray space is invisible in a browser and visible in every golden
// file that ever compares this output.
func TestAttributesOfNothingIsNothing(t *testing.T) {
	for _, attrs := range []map[string]string{nil, {}} {
		got, err := view.Attributes(attrs)
		if err != nil {
			t.Fatalf("Attributes(%v) = %v", attrs, err)
		}
		if got != "" {
			t.Fatalf("Attributes(%v) = %q, want the empty string", attrs, got)
		}
	}
}

// TestAValueCannotEscapeTheAttribute is the property the escape exists for.
//
// The payload's own text survives, and has to: it is what the caller asked to
// be written. What decides whether it is markup or a value is how many quotes
// stand unescaped around it -- two for one attribute, and four the moment the
// value's own quotes get through.
func TestAValueCannotEscapeTheAttribute(t *testing.T) {
	got, err := view.Attributes(map[string]string{
		"title": `a" onerror=alert(1) x="`,
	})
	if err != nil {
		t.Fatalf("Attributes() = %v", err)
	}
	if n := strings.Count(got, `"`); n != 2 {
		t.Fatalf("the value carries %d unescaped quotes, want the 2 that bound it: %s", n, got)
	}
	if !strings.Contains(got, "&#34;") {
		t.Fatalf("the quote was not escaped: %s", got)
	}
}

// TestAnAttributeThatIsNotInertIsRefused is one case per rule, and the rule is
// the point: a name whose value a browser would act on cannot be made safe by
// escaping it, because nothing about how it is written is what makes it act.
//
// The error is asserted to name the attribute, because the caller who gets it
// is holding a map and needs to know which key of it is the problem.
func TestAnAttributeThatIsNotInertIsRefused(t *testing.T) {
	for _, c := range []struct {
		why  string
		name string
	}{
		{"an event handler", "onclick"},
		{"an htmx handler", "hx-on:click"},
		{"the same handler under the alias htmx also reads", "data-hx-on-click"},
		{"an htmx verb, which is fetched", "hx-post"},
		{"the same verb under the alias", "data-hx-post"},
		{"what htmx sends beside the request", "hx-headers"},
		{"what htmx replaces with the answer", "hx-swap-oob"},
		{"an Alpine directive", "x-data"},
		{"an inline style the policy drops", "style"},
		{"an address, which TextURL decides", "href"},
		{"an address wearing another name", "formaction"},
		{"a document, which escaping hands back", "srcdoc"},
		{"a directive to the browser", "http-equiv"},
		{"the component's own class", "class"},
		{"the component's promise to a screen reader", "aria-label"},
		{"the same promise under its other name", "role"},
		{"a name a space would end", "two words"},
		{"a name a quote would end", `x" onerror="alert(1)`},
		{"no name at all", ""},
	} {
		got, err := view.Attributes(map[string]string{c.name: "1"})
		if err == nil {
			t.Errorf("%s (%q) was written: %s", c.why, c.name, got)
			continue
		}
		if got != "" {
			t.Errorf("%s (%q) was refused and still wrote %q", c.why, c.name, got)
		}
		// strconv.Quote, because every refusal names the attribute with %q --
		// searching for the bare name would miss the two whose own quotes come
		// back escaped, which are the names most worth naming.
		if c.name != "" && !strings.Contains(err.Error(), strconv.Quote(c.name)) {
			t.Errorf("the refusal of %s does not name %q: %v", c.why, c.name, err)
		}
	}
}

// TestOneRefusalRefusesTheWholeSet is the half that is easy to get wrong.
//
// Writing the attributes that passed and dropping the one that did not would
// leave the element carrying most of what was asked for, and the caller reading
// an error about an element that rendered anyway. Everything or nothing is what
// makes the error mean something.
func TestOneRefusalRefusesTheWholeSet(t *testing.T) {
	got, err := view.Attributes(map[string]string{
		"data-ok": "1",
		"onclick": "alert(1)",
	})
	if err == nil {
		t.Fatalf("a set holding onclick was written: %s", got)
	}
	if got != "" {
		t.Fatalf("the refused set still wrote %q", got)
	}
}

// TestAnInertNameIsWritten is the other side of the refusals: the set a caller
// is meant to reach for has to actually get through, or the feature is a list
// of things that do not work.
func TestAnInertNameIsWritten(t *testing.T) {
	for _, name := range []string{
		"data-testid", "id", "title", "lang", "dir", "tabindex",
		"hidden", "draggable", "spellcheck", "inputmode", "data-order",
	} {
		if _, err := view.Attributes(map[string]string{name: "1"}); err != nil {
			t.Errorf("%q is inert and was refused: %v", name, err)
		}
	}
}
