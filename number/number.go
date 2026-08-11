package number

import (
	"cmp"
	"math"
	"strconv"
	"strings"
)

const (
	groupSeparator   = ','
	decimalSeparator = '.'

	// icuDefaultFractionDigits is what NumberFormatter::DECIMAL keeps when
	// neither $precision nor $maxPrecision is given: at most three digits after
	// the point, with the trailing zeros dropped.
	icuDefaultFractionDigits = 3
)

// Format answers for Number::format. It renders v with a group separator every
// three integer digits.
//
// Illuminate's $precision and $maxPrecision are the two optional arguments, in
// that order, because Go has no named arguments:
//
//	Format(1234.5678)       // "1,234.568" -- neither given, ICU's own default
//	Format(1234.5678, 2)    // "1,234.57"  -- $precision, an exact digit count
//	Format(1234.5, 2, 4)    // "1,234.5"   -- $maxPrecision, an upper bound
//
// A $maxPrecision drops the trailing zeros a $precision would keep, which is
// the whole difference between them. A precision below zero is read as zero.
//
// $locale has no equivalent here; see the package comment.
func Format(v float64, precision ...int) string {
	if s, ok := special(v); ok {
		return s
	}
	switch len(precision) {
	case 0:
		return group(trimTrailingZeros(strconv.FormatFloat(v, 'f', icuDefaultFractionDigits, 64)))
	case 1:
		return group(strconv.FormatFloat(v, 'f', max(precision[0], 0), 64))
	default:
		return group(trimTrailingZeros(strconv.FormatFloat(v, 'f', max(precision[1], 0), 64)))
	}
}

// Percentage answers for Number::percentage. It renders v as a percentage.
//
// The value is taken as already being a percentage, the way Illuminate divides
// by a hundred before handing it to a formatter that multiplies by a hundred
// again, so Percentage(12.5, 1) is "12.5%" and not "1250.0%".
//
// The optional arguments are Illuminate's $precision and $maxPrecision, as on
// Format, except that $precision defaults to zero rather than to ICU's own.
func Percentage(v float64, precision ...int) string {
	if s, ok := special(v); ok {
		return s
	}
	if len(precision) == 0 {
		precision = []int{0}
	}
	return Format(v, precision...) + "%"
}

// Ordinal answers for Number::ordinal. It renders n in English ordinal form,
// with the same group separator Format uses.
//
//	Ordinal(1)    // "1st"
//	Ordinal(12)   // "12th"
//	Ordinal(1000) // "1,000th"
//
// Illuminate takes int|float here and hands the value to ICU; this takes an
// int, because a fraction has no ordinal.
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

// abbreviateUnits is Illuminate's abbreviated unit table, keyed by the power of
// ten each suffix stands for.
var abbreviateUnits = []unit{{3, "K"}, {6, "M"}, {9, "B"}, {12, "T"}, {15, "Q"}}

// humanUnits is the spelled-out table Number::forHumans uses when it is not
// abbreviating. The leading space is Illuminate's, and the trim at the end of
// summarize is what keeps it from showing on a value with no unit.
var humanUnits = []unit{
	{3, " thousand"}, {6, " million"}, {9, " billion"},
	{12, " trillion"}, {15, " quadrillion"},
}

type unit struct {
	exponent int
	suffix   string
}

// Abbreviate answers for Number::abbreviate. It is ForHumans with the short
// suffixes K, M, B, T and Q.
//
// The optional arguments are Illuminate's $precision and $maxPrecision.
//
//	Abbreviate(1500, 1)  // "1.5K"
//	Abbreviate(-2000000) // "-2M"
func Abbreviate(v float64, precision ...int) string {
	return ForHumans(v, argAt(precision, 0, 0), argAt(precision, 1, -1), true)
}

// ForHumans answers for Number::forHumans. It renders v against the largest
// thousand-scaled unit it reaches, spelled out unless abbreviate is set.
//
//	ForHumans(1500, 0, -1, false) // "2 thousand"
//	ForHumans(1500, 1, -1, true)  // "1.5K"
//
// Illuminate defaults $precision to 0, $maxPrecision to null and $abbreviate to
// false; Go has no default arguments, so all three are required and a negative
// maxPrecision is the null.
//
// A value past a quadrillion is scaled by a quadrillion and rendered again,
// which is Illuminate's own recursion and is why ForHumans(1e18, 0, -1, true)
// is "1KQ" rather than "1,000Q".
func ForHumans(v float64, precision, maxPrecision int, abbreviate bool) string {
	units := humanUnits
	if abbreviate {
		units = abbreviateUnits
	}
	return summarize(v, precision, maxPrecision, units)
}

// summarize answers for Number::summarize, which is protected in Illuminate and
// is the whole of ForHumans and Abbreviate.
func summarize(v float64, precision, maxPrecision int, units []unit) string {
	if s, ok := special(v); ok {
		return s
	}
	switch {
	case v == 0:
		if precision > 0 {
			return formatWith(0, precision, maxPrecision)
		}
		return "0"
	case v < 0:
		return "-" + summarize(math.Abs(v), precision, maxPrecision, units)
	case v >= 1e15:
		return summarize(v/1e15, precision, maxPrecision, units) + units[len(units)-1].suffix
	}

	numberExponent := int(math.Floor(math.Log10(v)))
	displayExponent := numberExponent - numberExponent%3
	v /= math.Pow(10, float64(displayExponent))

	return strings.TrimSpace(formatWith(v, precision, maxPrecision) + suffixFor(units, displayExponent))
}

// suffixFor is Illuminate's `$units[$displayExponent] ?? empty` lookup: an
// exponent with no unit against it contributes nothing.
func suffixFor(units []unit, exponent int) string {
	for _, u := range units {
		if u.exponent == exponent {
			return u.suffix
		}
	}
	return ""
}

// Clamp answers for Number::clamp. It is v held between minimum and maximum.
//
// Illuminate types the three as int|float; this is generic over every ordered
// type, which is the same freedom without the coercion.
func Clamp[T cmp.Ordered](v, minimum, maximum T) T {
	return min(max(v, minimum), maximum)
}

// Pairs answers for Number::pairs. It splits the range up to `to` into pairs of
// lower and upper bounds, `by` wide:
//
//	Pairs(10, 5)    // [[0 4] [5 9]]
//	Pairs(10, 5, 0) // the same, with Illuminate's $start spelled out
//
// The optional arguments are Illuminate's $start and $offset, which default to
// 0 and 1. A step of zero or less returns nothing: Illuminate loops forever on
// it, and no pair is a better answer than no answer at all.
func Pairs(to, by float64, startOffset ...float64) [][2]float64 {
	if by <= 0 {
		return nil
	}
	start := argAt(startOffset, 0, 0.0)
	offset := argAt(startOffset, 1, 1.0)

	var out [][2]float64
	for lower := start; lower < to; lower += by {
		upper := lower + by - offset
		if upper > to {
			upper = to
		}
		out = append(out, [2]float64{lower, upper})
	}
	return out
}

// Trim answers for Number::trim. It removes the trailing zero digits after the
// decimal point: Trim(12.30) is "12.3" and Trim(12.0) is "12".
//
// Illuminate returns int|float, which it gets by round-tripping the value
// through JSON; the change it makes is to the way the number is written, not to
// the number, so this returns the writing. There is no group separator in it,
// because there is none in what json_encode produces either.
func Trim(v float64) string {
	if s, ok := special(v); ok {
		return s
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatWith is Illuminate's `static::format($number, $precision, $maxPrecision)`
// with a negative maxPrecision standing for its null.
func formatWith(v float64, precision, maxPrecision int) string {
	if maxPrecision >= 0 {
		return Format(v, precision, maxPrecision)
	}
	return Format(v, precision)
}

// argAt reads an optional argument out of a variadic tail, or answers with the
// default Illuminate declares for it.
func argAt[T any](args []T, i int, fallback T) T {
	if i < len(args) {
		return args[i]
	}
	return fallback
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

// trimTrailingZeros drops the zeros a fixed-digit rendering left behind, and
// the point with them when nothing survives it. It is ICU's MIN_FRACTION_DIGITS
// of zero, which is what a $maxPrecision leaves in force.
func trimTrailingZeros(s string) string {
	if !strings.ContainsRune(s, decimalSeparator) {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, string(decimalSeparator))
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
