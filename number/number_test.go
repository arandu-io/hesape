package number

import (
	"math"
	"testing"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0"},
		{123, 0, "123"},
		{1000, 0, "1,000"},
		{1000000, 0, "1,000,000"},
		{1234.5678, 2, "1,234.57"},
		{1234.5678, 0, "1,235"},
		{-1234.5, 1, "-1,234.5"},
		{-1, 0, "-1"},
		{0.5, 3, "0.500"},
		{12345.6, -3, "12,346"},
		{-0.004, 2, "0.00"},
		{2.5, 0, "2"},
		{3.5, 0, "4"},
	}
	for _, c := range cases {
		if got := Format(c.value, c.precision); got != c.want {
			t.Errorf("Format(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
}

func TestFormatSpecialValues(t *testing.T) {
	if got := Format(math.NaN(), 2); got != "NaN" {
		t.Errorf("Format(NaN, 2) = %q, want %q", got, "NaN")
	}
	if got := Format(math.Inf(1), 2); got != "Inf" {
		t.Errorf("Format(+Inf, 2) = %q, want %q", got, "Inf")
	}
	if got := Format(math.Inf(-1), 2); got != "-Inf" {
		t.Errorf("Format(-Inf, 2) = %q, want %q", got, "-Inf")
	}
}

func TestPercentage(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0%"},
		{12.5, 1, "12.5%"},
		{12.5, 0, "12%"},
		{100, 0, "100%"},
		{-7.25, 2, "-7.25%"},
		{1234.5, 1, "1,234.5%"},
	}
	for _, c := range cases {
		if got := Percentage(c.value, c.precision); got != c.want {
			t.Errorf("Percentage(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
	if got := Percentage(math.NaN(), 2); got != "NaN" {
		t.Errorf("Percentage(NaN, 2) = %q, want %q", got, "NaN")
	}
}

func TestOrdinal(t *testing.T) {
	cases := []struct {
		value int
		want  string
	}{
		{0, "0th"},
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{10, "10th"},
		{11, "11th"},
		{12, "12th"},
		{13, "13th"},
		{21, "21st"},
		{22, "22nd"},
		{23, "23rd"},
		{100, "100th"},
		{101, "101st"},
		{111, "111th"},
		{112, "112th"},
		{1000, "1,000th"},
		{1002, "1,002nd"},
		{-1, "-1st"},
		{-11, "-11th"},
	}
	for _, c := range cases {
		if got := Ordinal(c.value); got != c.want {
			t.Errorf("Ordinal(%d) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestAbbreviate(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0"},
		{0, 2, "0.00"},
		{999, 0, "999"},
		{1000, 0, "1K"},
		{1500, 1, "1.5K"},
		{-1500, 1, "-1.5K"},
		{1000000, 0, "1M"},
		{-2000000, 0, "-2M"},
		{1e9, 0, "1B"},
		{1.23e12, 2, "1.23T"},
		{1e15, 0, "1Q"},
		{1e18, 0, "1KQ"},
		{999999, 0, "1,000K"},
	}
	for _, c := range cases {
		if got := Abbreviate(c.value, c.precision); got != c.want {
			t.Errorf("Abbreviate(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
	if got := Abbreviate(math.Inf(1), 0); got != "Inf" {
		t.Errorf("Abbreviate(+Inf, 0) = %q, want %q", got, "Inf")
	}
}

func TestForHumans(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1 thousand"},
		{1000000, "1 million"},
		{1230000, "1 million"},
		{1e9, "1 billion"},
		{1e12, "1 trillion"},
		{1e15, "1 quadrillion"},
		{1e18, "1 thousand quadrillion"},
		{-1000, "-1 thousand"},
	}
	for _, c := range cases {
		if got := ForHumans(c.value, 0, -1, false); got != c.want {
			t.Errorf("ForHumans(%v, 0, -1, false) = %q, want %q", c.value, got, c.want)
		}
	}
}

// TestFormatWithoutAPrecision pins the default Illuminate inherits from ICU:
// three digits at most, with the trailing zeros gone.
func TestFormatWithoutAPrecision(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{1234.5678, "1,234.568"},
		{1234.5, "1,234.5"},
		{1234, "1,234"},
		{0, "0"},
		{0.0001, "0"},
	}
	for _, c := range cases {
		if got := Format(c.value); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

// TestFormatMaxPrecisionOverridesPrecision keeps the two apart: a maximum
// precision is an upper bound that drops trailing zeros, and it wins over the
// exact count, which is the order Illuminate sets the two attributes in.
func TestFormatMaxPrecisionOverridesPrecision(t *testing.T) {
	if got := Format(1234.5, 4, 4); got != "1,234.5" {
		t.Errorf("Format(1234.5, 4, 4) = %q, want %q", got, "1,234.5")
	}
	if got := Format(1234.5, 4); got != "1,234.5000" {
		t.Errorf("Format(1234.5, 4) = %q, want %q", got, "1,234.5000")
	}
	if got := Format(1234.56789, 0, 2); got != "1,234.57" {
		t.Errorf("Format(1234.56789, 0, 2) = %q, want %q", got, "1,234.57")
	}
}

func TestClamp(t *testing.T) {
	if got := Clamp(1, 2, 10); got != 2 {
		t.Errorf("Clamp(1, 2, 10) = %d, want 2", got)
	}
	if got := Clamp(11, 2, 10); got != 10 {
		t.Errorf("Clamp(11, 2, 10) = %d, want 10", got)
	}
	if got := Clamp(5, 2, 10); got != 5 {
		t.Errorf("Clamp(5, 2, 10) = %d, want 5", got)
	}
	if got := Clamp(1.5, 2.0, 10.0); got != 2.0 {
		t.Errorf("Clamp(1.5, 2.0, 10.0) = %v, want 2", got)
	}
}

func TestPairs(t *testing.T) {
	got := Pairs(10, 5)
	want := [][2]float64{{0, 4}, {5, 9}}
	if len(got) != len(want) {
		t.Fatalf("Pairs(10, 5) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Pairs(10, 5)[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if got := Pairs(10, 5, 0, 0); got[0] != [2]float64{0, 5} {
		t.Errorf("Pairs(10, 5, 0, 0)[0] = %v, want [0 5]", got[0])
	}
	if got := Pairs(25, 10); got[2] != [2]float64{20, 25} {
		t.Errorf("Pairs(25, 10)[2] = %v, want [20 25]", got[2])
	}
	if got := Pairs(10, 0); got != nil {
		t.Errorf("Pairs(10, 0) = %v, want nil", got)
	}
}

func TestTrim(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{12.0, "12"},
		{12.3, "12.3"},
		{12.30, "12.3"},
		{0, "0"},
		{-0.5, "-0.5"},
		{1000, "1000"},
	}
	for _, c := range cases {
		if got := Trim(c.value); got != c.want {
			t.Errorf("Trim(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestSpell(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "zero"},
		{1, "one"},
		{10, "ten"},
		{13, "thirteen"},
		{20, "twenty"},
		{21, "twenty-one"},
		{100, "one hundred"},
		{123, "one hundred twenty-three"},
		{1000, "one thousand"},
		{1234, "one thousand two hundred thirty-four"},
		{1000000, "one million"},
		{-2, "minus two"},
		{1.25, "one point two five"},
	}
	for _, c := range cases {
		if got := Spell(c.value); got != c.want {
			t.Errorf("Spell(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestSpellBounds(t *testing.T) {
	if got := Spell(9, 10); got != "9" {
		t.Errorf("Spell(9, 10) = %q, want %q", got, "9")
	}
	if got := Spell(11, 10); got != "eleven" {
		t.Errorf("Spell(11, 10) = %q, want %q", got, "eleven")
	}
	if got := Spell(11, 0, 10); got != "11" {
		t.Errorf("Spell(11, 0, 10) = %q, want %q", got, "11")
	}
	if got := Spell(5, 0, 10); got != "five" {
		t.Errorf("Spell(5, 0, 10) = %q, want %q", got, "five")
	}
}

func TestSpellOrdinal(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "zeroth"},
		{1, "first"},
		{2, "second"},
		{3, "third"},
		{5, "fifth"},
		{8, "eighth"},
		{9, "ninth"},
		{12, "twelfth"},
		{13, "thirteenth"},
		{20, "twentieth"},
		{21, "twenty-first"},
		{40, "fortieth"},
		{100, "one hundredth"},
		{101, "one hundred first"},
		{1000, "one thousandth"},
		{1000000, "one millionth"},
	}
	for _, c := range cases {
		if got := SpellOrdinal(c.value); got != c.want {
			t.Errorf("SpellOrdinal(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		value string
		want  float64
	}{
		{"1,234.56", 1234.56},
		{"1234", 1234},
		{"-1,234", -1234},
		{"  1,000  ", 1000},
		{"1,234 units", 1234},
		{"0", 0},
		{"1.5e3", 1500},
	}
	for _, c := range cases {
		got, err := Parse(c.value)
		if err != nil {
			t.Errorf("Parse(%q) returned %v", c.value, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %v, want %v", c.value, got, c.want)
		}
	}
	for _, bad := range []string{"", "units", "   ", ".", "-"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) returned no error", bad)
		}
	}
}

func TestParseIntTruncates(t *testing.T) {
	got, err := ParseInt("1,234.99")
	if err != nil || got != 1234 {
		t.Errorf("ParseInt(%q) = %d, %v, want 1234, nil", "1,234.99", got, err)
	}
	if got, _ := ParseInt("-1.99"); got != -1 {
		t.Errorf("ParseInt(%q) = %d, want -1", "-1.99", got)
	}
	if got, err := ParseFloat("1,234.99"); err != nil || got != 1234.99 {
		t.Errorf("ParseFloat(%q) = %v, %v, want 1234.99, nil", "1,234.99", got, err)
	}
}

func TestDefaultsAndScopedOverrides(t *testing.T) {
	if got := DefaultLocale(); got != "en" {
		t.Errorf("DefaultLocale() = %q, want %q", got, "en")
	}
	if got := DefaultCurrency(); got != "USD" {
		t.Errorf("DefaultCurrency() = %q, want %q", got, "USD")
	}

	inside := WithCurrency("EUR", func() string { return Currency(1234.56, "") })
	if inside != "€1,234.56" {
		t.Errorf("WithCurrency(EUR) rendered %q, want %q", inside, "€1,234.56")
	}
	if got := DefaultCurrency(); got != "USD" {
		t.Errorf("DefaultCurrency() after WithCurrency = %q, want %q", got, "USD")
	}

	locale := WithLocale("pt", func() string { return DefaultLocale() })
	if locale != "pt" {
		t.Errorf("WithLocale(pt) saw %q, want %q", locale, "pt")
	}
	if got := DefaultLocale(); got != "en" {
		t.Errorf("DefaultLocale() after WithLocale = %q, want %q", got, "en")
	}
}
