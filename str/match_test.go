package str_test

import (
	"testing"

	"github.com/arandu-io/hesape/str"
)

func TestIs(t *testing.T) {
	for _, c := range []struct {
		pattern string
		value   string
		want    bool
	}{
		{"invoices.index", "invoices.index", true},
		{"invoices.index", "invoices.show", false},
		{"invoices.*", "invoices.index", true},
		{"invoices.*", "invoices.", true},
		{"invoices.*", "orders.index", false},
		{"*", "anything at all", true},
		{"*", "", true},
		{"admin/*/edit", "admin/users/edit", true},
		{"admin/*/edit", "admin/users/roles/edit", true},
		{"admin/*/edit", "admin/users/show", false},
		{"*.example.com", "app.example.com", true},
		{"*.example.com", "example.com", false},
		{"a*a", "a", false},
		{"", "", true},
		{"", "x", false},
	} {
		if got := str.Is([]string{c.pattern}, c.value, false); got != c.want {
			t.Errorf("Is(%q, %q) = %v, want %v", c.pattern, c.value, got, c.want)
		}
	}
}

// TestIsTreatsEveryOtherRuneAsLiteral: a pattern comes from configuration and a
// route name, and neither is escaped before it gets here.
func TestIsTreatsEveryOtherRuneAsLiteral(t *testing.T) {
	if str.Is([]string{"a.c"}, "abc", false) {
		t.Error(`Is("a.c", "abc") is true; the dot is not a wildcard`)
	}
	if !str.Is([]string{"price(+tax)"}, "price(+tax)", false) {
		t.Error(`Is("price(+tax)", "price(+tax)") is false; the pattern is literal`)
	}
}
