package str_test

import (
	"slices"
	"testing"

	"github.com/arandu-io/hesape/str"
)

// A run of capitals becomes one delimiter per capital, because the delimiter
// goes in before every upper-case letter without asking whether the previous
// one was upper-case too. An existing hyphen is left alone, because the word
// splitting does not break on it.
func TestSnakeMatchesTheRegexIlluminateUses(t *testing.T) {
	for in, want := range map[string]string{
		"PurchaseOrder":  "purchase_order",
		"purchase_order": "purchase_order",
		"purchase-order": "purchase-order",
		"purchase order": "purchase_order",
		"Invoice":        "invoice",
		"HTTPServer":     "h_t_t_p_server",
		"invoiceLine":    "invoice_line",
		"OAuth2Token":    "o_auth2_token",
		"  spaced  out ": "spaced_out",
		"":               "",
	} {
		if got := str.Snake(in, "_"); got != want {
			t.Errorf("Snake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKebabIsSnakeWithHyphens(t *testing.T) {
	for in, want := range map[string]string{
		"WelcomeEmail":   "welcome-email",
		"purchase order": "purchase-order",
		"ResetPassword":  "reset-password",
	} {
		if got := str.Kebab(in); got != want {
			t.Errorf("Kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStudlyAcceptsEverySpelling: the developer this is for types the class
// name however it comes to hand, and a generator takes the module name.
func TestStudlyAcceptsEverySpelling(t *testing.T) {
	for in, want := range map[string]string{
		"invoice_line":  "InvoiceLine",
		"invoice-line":  "InvoiceLine",
		"invoiceLine":   "InvoiceLine",
		"InvoiceLine":   "InvoiceLine",
		"created_at":    "CreatedAt",
		"HTTPServer":    "HTTPServer",
		"purchase_note": "PurchaseNote",
		"":              "",
	} {
		if got := str.Studly(in); got != want {
			t.Errorf("Studly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCamelLowercasesTheInitialOnly(t *testing.T) {
	for in, want := range map[string]string{
		"purchase_order": "purchaseOrder",
		"InvoiceLine":    "invoiceLine",
		"invoice":        "invoice",
		"":               "",
	} {
		if got := str.Camel(in); got != want {
			t.Errorf("Camel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUcfirstLeavesTheRestAlone(t *testing.T) {
	for in, want := range map[string]string{
		"welcome email": "Welcome email",
		"eMail":         "EMail",
		"ação":          "Ação",
		"":              "",
	} {
		if got := str.Ucfirst(in); got != want {
			t.Errorf("Ucfirst(%q) = %q, want %q", in, got, want)
		}
	}
}

// Headline title cases every word: it maps Title over the parts and joins them
// with a space.
func TestHeadlineTitleCasesEveryWord(t *testing.T) {
	for in, want := range map[string]string{
		"WelcomeEmail":          "Welcome Email",
		"password_confirmation": "Password Confirmation",
		"ResetPassword":         "Reset Password",
		"email":                 "Email",
		"":                      "",
	} {
		if got := str.Headline(in); got != want {
			t.Errorf("Headline(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleCapitalizesEveryWordAndKeepsSpacing(t *testing.T) {
	for in, want := range map[string]string{
		"welcome EMAIL": "Welcome Email",
		"two  spaces":   "Two  Spaces",
		"":              "",
	} {
		if got := str.Title(in); got != want {
			t.Errorf("Title(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestUcsplitSplitsOnTheCapitals covers the splitting question: Words is the
// word limiter and not a splitter, so splitting belongs to Ucsplit.
func TestUcsplitSplitsOnTheCapitals(t *testing.T) {
	if got := str.Ucsplit(""); len(got) != 0 {
		t.Errorf("Ucsplit(%q) = %#v, want empty", "", got)
	}
	got := str.Ucsplit("PurchaseOrderLine")
	want := []string{"Purchase", "Order", "Line"}
	if !slices.Equal(got, want) {
		t.Errorf("Ucsplit = %#v, want %#v", got, want)
	}
}
