package str

import (
	"strings"
	"unicode"
)

// Snake is the name as a column, a table or a module is spelled:
// "PurchaseOrder", "purchase-order" and "purchase order" all become
// "purchase_order".
//
// It reads a run of capitals as one word, so HTTPServer is http_server and not
// h_t_t_p_server. Illuminate produces the second, and the second is what an
// identifier looks like after a machine has been at it.
func Snake(s string) string { return delimit(s, '_') }

// Kebab is Snake with hyphens: "WelcomeEmail" becomes "welcome-email".
//
// It names files, URL segments and view directories, where a hyphen is what the
// address bar expects and an underscore is what a shell autocompletes badly.
func Kebab(s string) string { return delimit(s, '-') }

// Studly is the Go type a name becomes: "invoice_line", "invoice-line" and
// "invoiceLine" all become InvoiceLine.
//
// It goes through Snake first, which is what lets it accept every spelling.
// That also means it normalizes a run of capitals -- HTTPServer is HttpServer --
// so a generator that reads a specification twice writes the same identifier
// both times.
func Studly(s string) string {
	var b strings.Builder
	for _, w := range strings.Split(Snake(s), "_") {
		b.WriteString(UpperFirst(w))
	}
	return b.String()
}

// Camel is Studly with a lowercase initial: "purchase_order" becomes
// "purchaseOrder". It names an unexported variable or a JSON field.
func Camel(s string) string {
	studly := Studly(s)
	if studly == "" {
		return studly
	}
	rs := []rune(studly)
	rs[0] = unicode.ToLower(rs[0])
	return string(rs)
}

// UpperFirst capitalizes the first letter and leaves the rest alone. Sentence
// case, not Title Case.
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}

// Headline is the name as a sentence starts it: "WelcomeEmail" and
// "password_confirmation" become "Welcome email" and "Password confirmation".
//
// Sentence case is deliberate. Its callers put the result in front of a message
// the reader finishes -- "Password confirmation must match password" -- and a
// capital in the middle of that sentence reads as a proper noun.
func Headline(s string) string {
	return UpperFirst(strings.Join(Words(s), " "))
}

// Title capitalizes every word and lowercases the rest of each one: "welcome
// EMAIL" becomes "Welcome Email". Spacing is left exactly as it was found.
//
// This is the one for a heading a person reads. Headline is the one for the
// first half of a sentence.
func Title(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	boundary := true
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			boundary = true
			b.WriteRune(r)
		case boundary:
			boundary = false
			b.WriteRune(unicode.ToUpper(r))
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Words is the words a name is made of, lowercased: "PurchaseOrder-line"
// becomes ["purchase", "order", "line"].
//
// It splits on the two things that carry structure in a name: a separator rune
// and a capital that starts a word. An empty string has no words, and the slice
// is nil rather than a slice holding one empty string, so a caller can range
// over it without a length check.
func Words(s string) []string {
	snake := Snake(s)
	if snake == "" {
		return nil
	}
	return strings.Split(snake, "_")
}

// isSeparator reports whether r is one of the runes that ends a word without
// being part of one.
func isSeparator(r rune) bool {
	return r == '_' || r == '-' || r == '.' || unicode.IsSpace(r)
}

// delimit is the one word-splitter in this package: Snake, Kebab and everything
// built on Words go through it.
//
// A word ends at a separator, and it ends at a capital that starts one -- after
// a lowercase letter or a digit, or at the end of a run of capitals followed by
// a lowercase letter, which is what keeps HTTPServer in two pieces instead of
// five. Separators never survive into the answer, so no leading, trailing or
// doubled delimiter can come out of it.
func delimit(s string, sep rune) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 4)
	pending := false
	for i, r := range rs {
		if isSeparator(r) {
			pending = b.Len() > 0
			continue
		}
		if unicode.IsUpper(r) && b.Len() > 0 {
			prevWord := unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])
			runEnd := unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if prevWord || runEnd {
				pending = true
			}
		}
		if pending {
			b.WriteRune(sep)
			pending = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
