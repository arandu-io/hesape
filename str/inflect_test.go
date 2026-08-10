package str_test

import (
	"testing"

	"github.com/arandu-io/hesape/str"
)

// TestPluralNamesTheTable is the case the generator depends on: a module name
// becomes a table, and the answer is written into a migration that runs once.
func TestPluralNamesTheTable(t *testing.T) {
	for in, want := range map[string]string{
		"invoice":        "invoices",
		"purchase_order": "purchase_orders",
		"company":        "companies",
		"day":            "days",
		"address":        "addresses",
		"box":            "boxes",
		"match":          "matches",
		"dish":           "dishes",
		"bus":            "buses",
		"quiz":           "quizzes",
		"person":         "people",
		"sales_person":   "sales_people",
		"child":          "children",
		"datum":          "data",
		"knife":          "knives",
		"leaf":           "leaves",
		"roof":           "roofs",
		"cafe":           "cafes",
		"hero":           "heroes",
		"photo":          "photos",
		"sheep":          "sheep",
		"equipment":      "equipment",
		"":               "",
	} {
		if got := str.Plural(in); got != want {
			t.Errorf("Plural(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluralCarriesTheCaseOver(t *testing.T) {
	for in, want := range map[string]string{
		"Invoice":       "Invoices",
		"INVOICE":       "INVOICES",
		"PurchaseOrder": "PurchaseOrders",
		"Person":        "People",
	} {
		if got := str.Plural(in); got != want {
			t.Errorf("Plural(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSingularInverts(t *testing.T) {
	for in, want := range map[string]string{
		"invoices":        "invoice",
		"purchase_orders": "purchase_order",
		"companies":       "company",
		"addresses":       "address",
		"classes":         "class",
		"boxes":           "box",
		"matches":         "match",
		"dishes":          "dish",
		"houses":          "house",
		"buses":           "bus",
		"people":          "person",
		"children":        "child",
		"knives":          "knife",
		"heroes":          "hero",
		"status":          "status",
		"sheep":           "sheep",
		"ties":            "tie",
		"":                "",
	} {
		if got := str.Singular(in); got != want {
			t.Errorf("Singular(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSingularOfPluralIsTheWordBack for everything the tables claim, in both
// directions, so an entry added to one and not the other is caught here.
func TestSingularOfPluralIsTheWordBack(t *testing.T) {
	for _, word := range []string{
		"invoice", "purchase_order", "company", "address", "box", "match",
		"dish", "bus", "person", "child", "knife", "leaf", "hero", "photo",
		"sheep", "equipment", "man", "woman", "tooth", "foot", "mouse",
	} {
		if got := str.Singular(str.Plural(word)); got != word {
			t.Errorf("Singular(Plural(%q)) = %q, want %q", word, got, word)
		}
	}
}

func TestPluralNCountsAndAgrees(t *testing.T) {
	for _, c := range []struct {
		n    int
		noun string
		want string
	}{
		{0, "rule", "0 rules"},
		{1, "rule", "1 rule"},
		{2, "rule", "2 rules"},
		{3, "field", "3 fields"},
		{2, "person", "2 people"},
	} {
		if got := str.PluralN(c.n, c.noun); got != c.want {
			t.Errorf("PluralN(%d, %q) = %q, want %q", c.n, c.noun, got, c.want)
		}
	}
}
