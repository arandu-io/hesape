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
