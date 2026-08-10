// Package str is Arandu's Support\Str.
//
// It holds the string transformations a generator, a router and a validator all
// need and none of them owns: Slug, Snake, Studly, Camel, Kebab, Headline,
// Plural, Singular, Limit, Squish, Mask, Is, UUID, ULID and Random.
//
// # Functions, not a fluent object
//
// There is no Str::of, no Stringable and no chain. Illuminate offers both the
// static call and the fluent wrapper; two spellings of one transformation is
// what RULE 9 refuses, and in Go the composition already reads left to right:
// str.Studly(str.Singular(name)).
//
// # Why this package exists at all
//
// Before it, slugging, pluralizing and casing were written five times inside
// the CLI -- internal/gen/spec.go, internal/gen/mail.go, internal/gen/command.go
// and internal/fonts/fonts.go -- and twice more in the validator, as Humanize
// and as an unexported plural. They had drifted: one dropped every accented
// rune on the floor, so a font family named "Josefin Sans Café" and one named
// "Josefin Sans Caf" produced the same file name, and one spelled HTTPServer as
// h_t_t_p_server. This is the one implementation of each.
//
// # What it does not do
//
// ADR 0004 keeps golang.org/x/text out of the core, so nothing here is
// locale-aware. ASCII folds a table of Latin-1 and Latin Extended-A runes and
// drops what it does not know; Plural and Singular are English, and every
// irregular they get right is a line in a table in inflect.go, not a rule.
// Locale-correct pluralization is translation.Choice's problem, not this
// package's.
//
// Headline is sentence case -- "Welcome email", not "Welcome Email" -- because
// its callers put it in front of a message a person reads. Title Case On Every
// Word is what a generator looks like. Title is the one that title-cases.
package str
