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

// TestCurrencyFromCents proves the whole range an amount can take without a
// float64 ever holding it: the sign, zero, an amount below one unit, the two
// group separators, and the currencies whose minor unit is not a hundredth.
func TestCurrencyFromCents(t *testing.T) {
	cases := []struct {
		name  string
		cents int64
		code  string
		want  string
	}{
		{"zero", 0, "USD", "$0.00"},
		{"below one unit", 5, "USD", "$0.05"},
		{"below one unit, two digits", 99, "USD", "$0.99"},
		{"negative below one unit", -99, "USD", "-$0.99"},
		{"one unit", 100, "USD", "$1.00"},
		{"thousand", 123450, "USD", "$1,234.50"},
		{"negative thousand", -123450, "USD", "-$1,234.50"},
		{"million", 123456789, "USD", "$1,234,567.89"},
		{"lowercase code", 123450, "usd", "$1,234.50"},
		{"padded code", 123450, " usd ", "$1,234.50"},
		{"empty code takes the default", 123450, "", "$1,234.50"},

		// The yen has no minor unit, so the count is already whole units and
		// no point is written at all.
		{"currency without minor units", 1234, "JPY", "¥1,234"},
		{"currency without minor units, zero", 0, "JPY", "¥0"},
		{"currency without minor units, negative", -1234, "JPY", "-¥1,234"},
		{"currency without minor units, million", 1234567, "KRW", "₩1,234,567"},

		// The dinars keep three digits, so a thousand minor units is one unit.
		{"three minor digits", 1234500, "BHD", "BHD 1,234.500"},
		{"three minor digits, below one unit", 5, "BHD", "BHD 0.005"},

		{"code with no symbol", 123450, "SEK", "SEK 1,234.50"},
		{"code with no symbol, negative", -9900, "CHF", "-CHF 99.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CurrencyFromCents(c.cents, c.code); got != c.want {
				t.Errorf("CurrencyFromCents(%d, %q) = %q, want %q", c.cents, c.code, got, c.want)
			}
		})
	}
}

// TestCurrencyFromCentsExtremes fixes the two counts that have no counterpart:
// the most negative int64 cannot be negated inside its own type, and the most
// positive one is the longest string of groups the formatter ever writes.
func TestCurrencyFromCentsExtremes(t *testing.T) {
	if got, want := CurrencyFromCents(math.MinInt64, "USD"), "-$92,233,720,368,547,758.08"; got != want {
		t.Errorf("CurrencyFromCents(MinInt64, USD) = %q, want %q", got, want)
	}
	if got, want := CurrencyFromCents(math.MaxInt64, "USD"), "$92,233,720,368,547,758.07"; got != want {
		t.Errorf("CurrencyFromCents(MaxInt64, USD) = %q, want %q", got, want)
	}
}

// TestCurrencyInLocale proves the conventions each locale carries, on both
// entry points, and that a locale carrying none is written the en-US way rather
// than failing.
func TestCurrencyInLocale(t *testing.T) {
	cases := []struct {
		name   string
		locale string
		cents  int64
		code   string
		want   string
	}{
		{"pt-BR zero", "pt-BR", 0, "BRL", "R$ 0,00"},
		{"pt-BR below one real", "pt-BR", 5, "BRL", "R$ 0,05"},
		{"pt-BR thousand", "pt-BR", 123450, "BRL", "R$ 1.234,50"},
		{"pt-BR million", "pt-BR", 123456789, "BRL", "R$ 1.234.567,89"},
		{"pt-BR negative", "pt-BR", -123450, "BRL", "-R$ 1.234,50"},
		{"pt-BR without minor units", "pt-BR", 1234, "JPY", "¥ 1.234"},
		{"pt-BR code with no symbol", "pt-BR", 123450, "SEK", "SEK 1.234,50"},

		// The tag is normalised before the lookup, so the three spellings of
		// one locale are one locale.
		{"pt_BR underscore", "pt_BR", 123450, "BRL", "R$ 1.234,50"},
		{"pt-br lowercase", "pt-br", 123450, "BRL", "R$ 1.234,50"},

		// A region with no row of its own falls to its language; a language
		// with no row falls to the en-US conventions.
		{"en-GB falls to en", "en-GB", 123450, "GBP", "£1,234.50"},
		{"pt-PT is not answered by pt-BR", "pt-PT", 123450, "EUR", "€1,234.50"},
		{"unknown locale", "xx-YY", 123450, "USD", "$1,234.50"},
		{"empty locale", "", 123450, "USD", "$1,234.50"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WithLocale(c.locale, func() string { return CurrencyFromCents(c.cents, c.code) })
			if got != c.want {
				t.Errorf("CurrencyFromCents(%d, %q) in %q = %q, want %q", c.cents, c.code, c.locale, got, c.want)
			}
		})
	}
}

// TestCurrencyAgreesWithCurrencyFromCents keeps the two entry points from
// drifting: an amount that has an exact count of minor units has to come out
// the same whichever of them is handed it.
func TestCurrencyAgreesWithCurrencyFromCents(t *testing.T) {
	cases := []struct {
		cents int64
		value float64
		code  string
	}{
		{0, 0, "USD"},
		{5, 0.05, "USD"},
		{-99, -0.99, "USD"},
		{123450, 1234.50, "USD"},
		{123456789, 1234567.89, "BRL"},
		{1234, 1234, "JPY"},
		{-9900, -99, "CHF"},
	}
	for _, locale := range []string{"en", "pt-BR"} {
		for _, c := range cases {
			WithLocale(locale, func() struct{} {
				fromCents := CurrencyFromCents(c.cents, c.code)
				fromFloat := Currency(c.value, c.code)
				if fromCents != fromFloat {
					t.Errorf("in %q: CurrencyFromCents(%d, %q) = %q, Currency(%v, %q) = %q",
						locale, c.cents, c.code, fromCents, c.value, c.code, fromFloat)
				}
				return struct{}{}
			})
		}
	}
}

// TestCurrencyInLocaleForFloat fixes that the float64 entry point reads the
// locale too, so that the two do not answer one amount two ways.
func TestCurrencyInLocaleForFloat(t *testing.T) {
	got := WithLocale("pt-BR", func() string { return Currency(1234.5, "BRL") })
	if want := "R$ 1.234,50"; got != want {
		t.Errorf("Currency(1234.5, BRL) in pt-BR = %q, want %q", got, want)
	}
	// The gap belongs to the locale and not to the currency, so a symbol other
	// than the local one takes it too. The sign that only survived rounding is
	// dropped here as it is in en-US.
	got = WithLocale("pt-BR", func() string { return Currency(-0.001, "USD") })
	if want := "$ 0,00"; got != want {
		t.Errorf("Currency(-0.001, USD) in pt-BR = %q, want %q", got, want)
	}
}

// TestCurrencySymbolTrails covers the placement no shipped locale uses yet. The
// field is the axis a trailing-symbol locale is carried on, and an axis nothing
// exercises is one nobody knows the effect of.
func TestCurrencySymbolTrails(t *testing.T) {
	trailing := currencyFormat{group: ".", decimal: ",", symbolTrails: true, symbolGap: " "}
	cases := []struct {
		negative bool
		integer  string
		fraction string
		code     string
		want     string
	}{
		{false, "1234", "50", "EUR", "1.234,50 €"},
		{true, "1234", "50", "EUR", "-1.234,50 €"},
		{false, "1234", "50", "SEK", "1.234,50 SEK"},
		{false, "1234", "", "JPY", "1.234 ¥"},
	}
	for _, c := range cases {
		got := renderCurrency(c.negative, c.integer, c.fraction, c.code, trailing)
		if got != c.want {
			t.Errorf("renderCurrency(%v, %q, %q, %q) with a trailing symbol = %q, want %q",
				c.negative, c.integer, c.fraction, c.code, got, c.want)
		}
	}
}

// TestNumberFormattingIgnoresLocale fixes the half of the package the locale
// does not reach. Format and Parse round-trip one another, and a locale that
// changed only one of them would break that silently.
func TestNumberFormattingIgnoresLocale(t *testing.T) {
	WithLocale("pt-BR", func() struct{} {
		if got, want := Format(1234.5, 2), "1,234.50"; got != want {
			t.Errorf("Format(1234.5, 2) in pt-BR = %q, want %q", got, want)
		}
		if got, want := Ordinal(1000), "1,000th"; got != want {
			t.Errorf("Ordinal(1000) in pt-BR = %q, want %q", got, want)
		}
		v, err := Parse("1,234.56")
		if err != nil || v != 1234.56 {
			t.Errorf("Parse(%q) in pt-BR = %v, %v, want 1234.56, nil", "1,234.56", v, err)
		}
		return struct{}{}
	})
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
