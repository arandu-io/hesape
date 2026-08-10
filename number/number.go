package number

import (
	"math"
	"strconv"
	"strings"
)

const (
	groupSeparator   = ','
	decimalSeparator = '.'
)

// Format renders v with a group separator every three integer digits and
// exactly precision digits after the decimal point.
//
//	Format(1234.5678, 2) // "1,234.57"
//	Format(1000000, 0)   // "1,000,000"
func Format(v float64, precision int) string {
	if s, ok := special(v); ok {
		return s
	}
	if precision < 0 {
		precision = 0
	}
	return group(strconv.FormatFloat(v, 'f', precision, 64))
}

// Percentage renders v as a percentage. The value is taken as already being a
// percentage, so Percentage(12.5, 1) is "12.5%", not "1250.0%".
func Percentage(v float64, precision int) string {
	if s, ok := special(v); ok {
		return s
	}
	return Format(v, precision) + "%"
}

// Ordinal renders n in English ordinal form, with the same group separator
// Format uses.
//
//	Ordinal(1)    // "1st"
//	Ordinal(12)   // "12th"
//	Ordinal(1000) // "1,000th"
func Ordinal(n int) string {
	tens := n % 100
	if tens < 0 {
		tens = -tens
	}
	suffix := "th"
	if tens < 11 || tens > 13 {
		switch tens % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return group(strconv.Itoa(n)) + suffix
}

// abbreviations are ordered from the largest magnitude down, because the first
// one v reaches is the one it is rendered in.
var abbreviations = []struct {
	scale  float64
	suffix string
}{
	{1e15, "Q"},
	{1e12, "T"},
	{1e9, "B"},
	{1e6, "M"},
	{1e3, "K"},
}

// Abbreviate renders v against the largest of the thousand-scaled suffixes K,
// M, B, T and Q that it reaches. A value past the quadrillions keeps the Q and
// grows the digits rather than inventing a suffix.
//
//	Abbreviate(1500, 1)    // "1.5K"
//	Abbreviate(-2_000_000, 0) // "-2M"
func Abbreviate(v float64, precision int) string {
	if s, ok := special(v); ok {
		return s
	}
	magnitude := math.Abs(v)
	for _, a := range abbreviations {
		if magnitude >= a.scale {
			return Format(v/a.scale, precision) + a.suffix
		}
	}
	return Format(v, precision)
}

// special reports the rendering of the float64 values that have no digits.
func special(v float64) (string, bool) {
	switch {
	case math.IsNaN(v):
		return "NaN", true
	case math.IsInf(v, 1):
		return "Inf", true
	case math.IsInf(v, -1):
		return "-Inf", true
	}
	return "", false
}

// group inserts the group separator into the integer part of a decimal string
// as strconv writes it, and drops a sign that only survived rounding: a value
// of -0.004 at two digits is "0.00", never "-0.00".
func group(s string) string {
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	integer, fraction := s, ""
	if i := strings.IndexByte(s, decimalSeparator); i >= 0 {
		integer, fraction = s[:i], s[i:]
	}
	if negative && isZero(integer) && isZero(fraction) {
		negative = false
	}

	var b strings.Builder
	b.Grow(len(s) + len(integer)/3 + 1)
	if negative {
		b.WriteByte('-')
	}
	lead := len(integer) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(integer[:lead])
	for i := lead; i < len(integer); i += 3 {
		b.WriteByte(groupSeparator)
		b.WriteString(integer[i : i+3])
	}
	b.WriteString(fraction)
	return b.String()
}

// isZero reports whether s carries no digit other than zero.
func isZero(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '1' && s[i] <= '9' {
			return false
		}
	}
	return true
}
