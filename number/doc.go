// Package number is Illuminate\Support\Number.
//
// It answers for every method on that class: Format, Spell, Ordinal,
// SpellOrdinal, Percentage, Currency, FileSize, Abbreviate, ForHumans, Clamp,
// Pairs, Trim, Parse, ParseInt, ParseFloat, UseLocale, UseCurrency,
// DefaultLocale, DefaultCurrency, WithLocale and WithCurrency.
//
// It was written against the clone in laravel_illuminate/support/Number.php,
// with Parse, ParseInt and ParseFloat taken from the current Laravel in
// reference_laravel/framework/src/Illuminate/Support/Number.php, where they are
// newer than the clone.
//
// # Optional arguments
//
// PHP has default arguments and Go has none, so Illuminate's trailing optional
// numbers arrive as a variadic tail in the order they are declared:
// Format(v) is $precision and $maxPrecision both null, Format(v, 2) sets
// $precision, and Format(v, 2, 4) sets both. Where a boolean follows them --
// FileSize and ForHumans -- every argument is spelled out instead, and a
// negative maxPrecision is the null.
//
// # No locale
//
// Illuminate\Support\Number is a thin wrapper over the intl extension, so each
// of its methods takes a locale and defers to ICU. The core carries no third
// party dependency beyond golang.org/x/crypto, which rules out golang.org/x/text
// and with it the CLDR data that locale-aware formatting needs. This package
// therefore writes one set of conventions, the ones ICU calls en-US: a comma
// every three digits, a dot before the fraction, the sign in front of the
// currency symbol, English ordinal suffixes and English number words.
//
// UseLocale, DefaultLocale and WithLocale still exist and still carry the
// locale, because callers and tests read it back; what they do not do is change
// how a number comes out. That is a stated gap, not an oversight. An
// application that has to render a number for a reader outside that convention
// formats it itself.
//
// UseCurrency, DefaultCurrency and WithCurrency are not in that position: the
// default currency is what Currency renders an empty code in, exactly as in
// Illuminate.
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
