package number

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrNotANumber is returned by Parse, ParseInt and ParseFloat for a string with
// no number at the front of it. Illuminate returns false there; this is the
// error that false becomes.
var ErrNotANumber = errors.New("number: string does not begin with a number")

// Parse answers for Number::parse. It reads a formatted number back:
//
//	Parse("1,234.56") // 1234.56
//	Parse("-1,234")   // -1234
//
// The group separator is dropped before the digits are read, which is what
// makes a formatted string round-trip. Reading stops at the first character
// that cannot belong to the number, the way ICU's own parser stops, so
// "1,234 units" is 1234 and not an error.
//
// Illuminate's $type selects between a double and a 32-bit integer; ParseFloat
// and ParseInt are those two choices, and this is the double. $locale has no
// equivalent here; see the package comment.
func Parse(s string) (float64, error) {
	cleaned := strings.Map(dropGroupSeparators, strings.TrimSpace(s))

	v, err := strconv.ParseFloat(numericPrefix(cleaned), 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("%w: %q", ErrNotANumber, s)
	}
	return v, nil
}

// ParseFloat answers for Number::parseFloat. It is Parse with Illuminate's
// TYPE_DOUBLE, which is the default, so the two agree.
func ParseFloat(s string) (float64, error) { return Parse(s) }

// ParseInt answers for Number::parseInt. It is Parse with Illuminate's
// TYPE_INT32: the fraction is dropped towards zero rather than rounded, so
// ParseInt("1,234.99") is 1234.
//
// Illuminate clamps the result to 32 bits, because that is what its ICU
// constant names; this returns an int and does not clamp, because on every
// platform Go builds for an int is at least that wide.
func ParseInt(s string) (int, error) {
	v, err := Parse(s)
	if err != nil {
		return 0, err
	}
	return int(math.Trunc(v)), nil
}

// dropGroupSeparators removes the characters a formatter puts between groups of
// digits: the comma this package writes, and the three spaces other
// conventions write.
func dropGroupSeparators(r rune) rune {
	switch r {
	case groupSeparator, ' ', '\u00a0', '\u202f':
		return -1
	}
	return r
}

// numericPrefix is the longest leading run that can be a decimal number: a
// sign, digits, a point with more digits, and an exponent. Everything past it
// is what ICU stops at.
func numericPrefix(s string) string {
	i, n := 0, len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < n && isDigit(s[i]) {
		i++
	}
	if i < n && s[i] == byte(decimalSeparator) {
		point := i
		i++
		for i < n && isDigit(s[i]) {
			i++
		}
		if i == point+1 {
			// A point with no digit behind it is not part of the number.
			i = point
		}
	}
	if i == start {
		return ""
	}
	mantissa := i
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		exponent := i
		for i < n && isDigit(s[i]) {
			i++
		}
		if i == exponent {
			i = mantissa
		}
	}
	return s[:i]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
