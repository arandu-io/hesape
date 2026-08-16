// Package number formats, spells and parses numbers.
//
// It renders a value as a plain figure (Format), a percentage, an ordinal, a
// currency amount, a file size or a rounded summary (Abbreviate, ForHumans);
// it writes one out in words (Spell, SpellOrdinal); and it reads a formatted
// one back (Parse, ParseInt, ParseFloat).
//
// # Optional arguments
//
// Go has no default arguments, so the trailing optional digit counts arrive as
// a variadic tail in the order they are declared: Format(v) gives neither the
// precision nor the maximum precision, Format(v, 2) gives the precision, and
// Format(v, 2, 4) gives both. Where a boolean follows them -- FileSize and
// ForHumans -- every argument is spelled out instead, and a negative maximum
// precision stands for the one that was not given.
//
// # One set of conventions
//
// Locale-aware formatting needs CLDR data, and carrying it would mean a third
// party dependency this module does not take. This package therefore writes a
// single set of conventions, the ones of en-US: a comma every three digits, a
// dot before the fraction, the sign in front of the currency symbol, English
// ordinal suffixes and English number words.
//
// UseLocale, DefaultLocale and WithLocale still exist and still carry the
// locale, because callers and tests read it back; what they do not do is change
// how a number comes out. That is a stated gap, not an oversight. An
// application that has to render a number for a reader outside that convention
// formats it itself.
//
// UseCurrency, DefaultCurrency and WithCurrency are not in that position: the
// default currency is the one Currency renders an empty code in.
//
// # Rounding
//
// Precision is the number of digits kept after the decimal point, and a value
// below zero is read as zero. Ties round to the nearest even digit, so
// Format(2.5, 0) is "2" and Format(3.5, 0) is "4".
//
// NaN and the infinities are returned as "NaN", "Inf" and "-Inf" by every
// function in this package, with no symbol, suffix or unit attached.
package number
