package view_test

import (
	"regexp"
	"testing"

	"github.com/arandu-io/hesape/view"
)

// TestStyleClassIsStableAndShaped pins the two properties the build depends on:
// the same text is always the same class, and the class is a legal CSS
// identifier so the rule compiled for it can be written at all.
func TestStyleClassIsStableAndShaped(t *testing.T) {
	const css = "& { gap: 6px; }"

	got := view.StyleClass(css)
	if again := view.StyleClass(css); got != again {
		t.Fatalf("StyleClass is not stable: %q then %q", got, again)
	}

	shape := regexp.MustCompile(`^k-[0-9a-f]{12}$`)
	if !shape.MatchString(got) {
		t.Fatalf("StyleClass(%q) = %q, want it to match %s", css, got, shape)
	}
}

// TestStyleClassSeparatesDifferentText is the half that makes the hash a name
// rather than a decoration: two blocks that say different things have to land
// on different rules, or one silently takes the other's styling.
func TestStyleClassSeparatesDifferentText(t *testing.T) {
	if a, b := view.StyleClass("& { gap: 6px; }"), view.StyleClass("& { gap: 8px; }"); a == b {
		t.Fatalf("two different blocks share the class %q", a)
	}
}

// TestStyleClassDoesNotNormalise records the decision as a test, because it is
// the kind that gets "fixed" later by somebody trimming the input.
//
// Whitespace is part of the text that is hashed. The cost is two identical
// rules under two names; the thing it buys is that the build and the render
// cannot disagree about which name a block has, because neither of them
// interprets the block at all.
func TestStyleClassDoesNotNormalise(t *testing.T) {
	if a, b := view.StyleClass("& { gap: 6px; }"), view.StyleClass("  & { gap: 6px; }  "); a == b {
		t.Fatal("StyleClass folded whitespace, which puts a second implementation of the folding on the build side")
	}
}

// TestStyleClassOfNothing holds the empty case: a component with no scoped
// block still calls this, and an empty class attribute is markup nobody meant
// to write. The caller checks the text, not the hash -- so what matters here is
// only that it does not panic and still answers a legal identifier.
func TestStyleClassOfNothing(t *testing.T) {
	if got := view.StyleClass(""); !regexp.MustCompile(`^k-[0-9a-f]{12}$`).MatchString(got) {
		t.Fatalf(`StyleClass("") = %q, want a legal identifier`, got)
	}
}
