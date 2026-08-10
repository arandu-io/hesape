package str_test

import (
	"testing"

	"github.com/arandu-io/hesape/str"
)

// TestSlugKeepsAccentedRunes is the defect this package was extracted to fix.
// The font vendorer dropped them, so two families that differ only in their
// accents wrote to one file and the second silently won.
func TestSlugKeepsAccentedRunes(t *testing.T) {
	for in, want := range map[string]string{
		"Young Serif":                "young-serif",
		"Josefin Sans Café":          "josefin-sans-cafe",
		"Josefin Sans Caf":           "josefin-sans-caf",
		"Ação entre Sócios":          "acao-entre-socios",
		"Straße":                     "strasse",
		"  Leading  and  trailing  ": "leading-and-trailing",
		"already-a-slug":             "already-a-slug",
		"under_scored":               "under-scored",
		"paulo@example.com":          "paulo-at-example-com",
		"":                           "",
		"日本語":                        "",
	} {
		if got := str.Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSlugWithNoSeparatorIsTheRepositoryDirectory: the font source spells a
// family directory with the words run together, and using the hyphenated form
// for it is a 404 on every licence fetch.
func TestSlugWithNoSeparatorIsTheRepositoryDirectory(t *testing.T) {
	if got := str.SlugWith("Young Serif", ""); got != "youngserif" {
		t.Errorf(`SlugWith("Young Serif", "") = %q, want "youngserif"`, got)
	}
	if got := str.SlugWith("Purchase Order", "_"); got != "purchase_order" {
		t.Errorf(`SlugWith("Purchase Order", "_") = %q, want "purchase_order"`, got)
	}
}

func TestASCIIFoldsWhatItKnowsAndDropsWhatItDoesNot(t *testing.T) {
	for in, want := range map[string]string{
		"plain ascii": "plain ascii",
		"Ação":        "Acao",
		"Œuvre":       "OEuvre",
		"Þingvellir":  "THingvellir",
		"Đorđe":       "Dorde",
		"Łódź":        "Lodz",
		"Ελλάδα":      "",
	} {
		if got := str.ASCII(in); got != want {
			t.Errorf("ASCII(%q) = %q, want %q", in, got, want)
		}
	}
}
