package view_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/view"
)

// TestClassSpreadsAListIntoSeparateClasses pins the shape of the call into
// ToCssClasses, which no compiler check defends.
//
// ToCssClasses is variadic. Handing it a []any as one argument type-checks and
// renders the slice itself through fmt.Sprint, so the attribute comes back as
// the single class "[btn btn-primary]" instead of two classes. The list has to
// be spread for each element to be read on its own.
func TestClassSpreadsAListIntoSeparateClasses(t *testing.T) {
	bag := view.NewComponentAttributeBag(nil).
		Class([]any{"btn", "btn-primary"})

	if got, want := bag.Get("class"), "btn btn-primary"; got != want {
		t.Fatalf("Class([]any{...}) = %q, want %q", got, want)
	}

	rendered := bag.ToHTML()
	if strings.Contains(rendered, "[") || strings.Contains(rendered, "]") {
		t.Fatalf("the class list rendered as one bracketed value: %s", rendered)
	}
	if want := `class="btn btn-primary"`; !strings.Contains(rendered, want) {
		t.Fatalf("ToHTML() = %s, want it to carry %s", rendered, want)
	}
}

// TestStyleSpreadsAListIntoSeparateStyles is TestClassSpreadsAListIntoSeparateClasses
// for the style attribute, which reaches ToCssStyles the same way.
func TestStyleSpreadsAListIntoSeparateStyles(t *testing.T) {
	bag := view.NewComponentAttributeBag(nil).
		Style([]any{"color: red", "margin: 0"})

	if got, want := bag.Get("style"), "color: red; margin: 0;"; got != want {
		t.Fatalf("Style([]any{...}) = %q, want %q", got, want)
	}
}

// TestClassKeepsTheConditionalAndScalarForms holds the two shapes that reach
// Class besides a list: a bare string, and the map whose true keys are kept.
func TestClassKeepsTheConditionalAndScalarForms(t *testing.T) {
	if got, want := view.NewComponentAttributeBag(nil).Class("btn").Get("class"), "btn"; got != want {
		t.Fatalf("Class(string) = %q, want %q", got, want)
	}

	bag := view.NewComponentAttributeBag(nil).
		Class(map[string]bool{"on": true, "off": false})
	if got, want := bag.Get("class"), "on"; got != want {
		t.Fatalf("Class(map[string]bool) = %q, want %q", got, want)
	}
}

// TestABaggedValueCannotEndTheAttributeItSitsIn is the defect this file
// shipped: a quote in a value was escaped as \", which HTML has no notion of.
//
// The backslash is read as a character of the value, the quote behind it ends
// the attribute, and everything the caller wrote after it arrives as attributes
// of the element -- an onerror among them. Asserting on the absence of the
// payload is not enough on its own, because a bag that dropped the attribute
// entirely would pass; the escaped form has to be there too.
func TestABaggedValueCannotEndTheAttributeItSitsIn(t *testing.T) {
	rendered := view.NewComponentAttributeBag(map[string]any{
		"title": `a" onerror=alert(1) x="`,
	}).ToHTML()

	// Counting the quotes is the assertion, and searching for the payload is
	// not: the text onerror=alert(1) is still there, and has to be, because it
	// is what the caller asked to be written. What decides whether it is markup
	// or a value is how many quotes stand unescaped around it. One attribute is
	// two, and the payload's own quotes bring four the moment they survive.
	if got := strings.Count(rendered, `"`); got != 2 {
		t.Fatalf("the value carries %d unescaped quotes, want the 2 that bound it: %s", got, rendered)
	}
	if strings.Contains(rendered, `\"`) {
		t.Fatalf("a quote is still escaped with a backslash: %s", rendered)
	}
	if want := "&#34;"; !strings.Contains(rendered, want) {
		t.Fatalf("the quote was not escaped as %s: %s", want, rendered)
	}
}

// TestAnAngleBracketIsEscapedToo pins the rest of the set, because a value that
// only had its quotes handled still closes the tag it sits in.
func TestAnAngleBracketIsEscapedToo(t *testing.T) {
	rendered := view.NewComponentAttributeBag(map[string]any{
		"title": `<script>alert(1)</script>&`,
	}).ToHTML()

	for _, unwanted := range []string{"<script>", "</script>"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("%s survived into the output: %s", unwanted, rendered)
		}
	}
	if want := "&amp;"; !strings.Contains(rendered, want) {
		t.Fatalf("the ampersand was not escaped: %s", rendered)
	}
}

// TestADefaultIsEscapedOnceAndNotTwice is the other half of moving the escape
// to String: a default used to be escaped on the way in as well, so an
// ampersand written by the component came out as &amp;amp; and read as the
// five characters of the entity.
func TestADefaultIsEscapedOnceAndNotTwice(t *testing.T) {
	rendered := view.NewComponentAttributeBag(nil).
		Merge(map[string]any{"title": "Cost & tax"}).
		ToHTML()

	if want := `title="Cost &amp; tax"`; !strings.Contains(rendered, want) {
		t.Fatalf("ToHTML() = %s, want it to carry %s", rendered, want)
	}
}

// TestANameThatCannotBeWrittenIsDropped covers the field the escape does not
// reach. A space or a quote in a name ends it, and no escape puts one back:
// what follows would be read as attributes of the element.
//
// The legal-but-unusual names are asserted beside them, because refusing those
// would be this bag deciding what a component may carry -- a question it does
// not answer.
func TestANameThatCannotBeWrittenIsDropped(t *testing.T) {
	rendered := view.NewComponentAttributeBag(map[string]any{
		`x" onerror="alert(1)`: "1",
		"two words":            "1",
		"a>b":                  "1",
		"":                     "1",
		"wire:model":           "name",
		"x-data":               "{}",
		"data-ok":              "1",
	}).ToHTML()

	for _, unwanted := range []string{"onerror", "two words", "a>b"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("a name that cannot be written was written: %s", rendered)
		}
	}
	for _, wanted := range []string{`wire:model="name"`, `data-ok="1"`, `x-data="{}"`} {
		if !strings.Contains(rendered, wanted) {
			t.Fatalf("a legal name was dropped, want %s in: %s", wanted, rendered)
		}
	}
}
