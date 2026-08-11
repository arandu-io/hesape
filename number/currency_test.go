package number

import (
	"math"
	"testing"
)

func TestCurrency(t *testing.T) {
	cases := []struct {
		value float64
		code  string
		want  string
	}{
		{1234.5, "USD", "$1,234.50"},
		{1234.5, "usd", "$1,234.50"},
		{1234.5, " usd ", "$1,234.50"},
		{0, "USD", "$0.00"},
		{-99, "USD", "-$99.00"},
		{1234.56, "EUR", "€1,234.56"},
		{1234.56, "BRL", "R$1,234.56"},
		{1234.56, "JPY", "¥1,235"},
		{1234.5, "BHD", "BHD 1,234.500"},
		{1234.5, "SEK", "SEK 1,234.50"},
		{-99, "CHF", "-CHF 99.00"},
		{1234.5, "", "$1,234.50"},
		{-0.001, "USD", "$0.00"},
	}
	for _, c := range cases {
		if got := Currency(c.value, c.code); got != c.want {
			t.Errorf("Currency(%v, %q) = %q, want %q", c.value, c.code, got, c.want)
		}
	}
}

func TestCurrencySpecialValues(t *testing.T) {
	if got := Currency(math.NaN(), "USD"); got != "NaN" {
		t.Errorf("Currency(NaN, USD) = %q, want %q", got, "NaN")
	}
	if got := Currency(math.Inf(-1), "USD"); got != "-Inf" {
		t.Errorf("Currency(-Inf, USD) = %q, want %q", got, "-Inf")
	}
}

// TestCurrencyDigitsAgreeWithSymbols keeps the two tables from drifting: every
// currency that carries a symbol has to round to the digit count that currency
// actually uses, and the only way to get that wrong silently is to add a symbol
// and forget the minor unit.
func TestCurrencyDigitsAgreeWithSymbols(t *testing.T) {
	for _, code := range []string{"JPY", "KRW", "VND"} {
		if currencyDigits[code] != 0 {
			t.Errorf("currencyDigits[%q] = %d, want 0", code, currencyDigits[code])
		}
	}
	for _, table := range []map[string]int{currencyDigits} {
		for code := range table {
			assertISOCode(t, code)
		}
	}
	for code := range currencySymbols {
		assertISOCode(t, code)
	}
}

// assertISOCode fails when a table key is not a three-letter uppercase code,
// because Currency uppercases its argument before the lookup and a lowercase
// key would never be found.
func assertISOCode(t *testing.T, code string) {
	t.Helper()
	if len(code) != 3 {
		t.Errorf("currency code %q is not three letters long", code)
		return
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			t.Errorf("currency code %q is not uppercase ASCII", code)
			return
		}
	}
}
