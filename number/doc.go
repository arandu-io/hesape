// Package number is Arandu's Support\Number.
//
// It holds: Format, FileSize, FileSizeBinary, Abbreviate, Currency, Percentage
// and Ordinal. Every function takes the value first and returns a string; there
// is no formatter object to build, no default to set globally and no state.
//
// # No locale
//
// Illuminate\Support\Number is a thin wrapper over the intl extension, so each
// of its methods takes a locale and defers to ICU. The core carries no third
// party dependency beyond golang.org/x/crypto, which rules out golang.org/x/text
// and with it the CLDR data that locale-aware formatting needs. This package
// therefore writes one set of conventions, the ones ICU calls en-US: a comma
// every three digits, a dot before the fraction, the sign in front of the
// currency symbol, and English ordinal suffixes.
//
// That is a stated gap, not an oversight. An application that has to render a
// number for a reader outside that convention formats it itself.
//
// # Rounding
//
// Precision is the number of digits kept after the decimal point, and a value
// below zero is read as zero. Ties round to the nearest even digit, which is
// what ICU does by default, so Format(2.5, 0) is "2" and Format(3.5, 0) is "4".
//
// NaN and the infinities are returned as "NaN", "Inf" and "-Inf" by every
// function in this package, with no symbol, suffix or unit attached.
package number
