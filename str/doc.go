// Package str is Illuminate\Support\Str, Illuminate\Support\Stringable and
// Illuminate\Support\Pluralizer.
//
// It holds the string transformations a generator, a router and a validator all
// need and none of them owns: Slug, Snake, Studly, Camel, Kebab, Headline,
// Plural, Singular, Limit, Squish, Mask, Is, UUID, ULID and Random, and the
// eighty-odd others Str carries.
//
// It was written against the clone in laravel_illuminate/support/, with the
// symbols marked as such in their own comments -- Counted, Initials, Ucwords,
// ResetFactoryState, DoesntContain, DoesntStartWith, DoesntEndWith and the two
// WhenDoesnt methods on Stringable -- taken from the current Laravel in
// reference_laravel/framework/src/Illuminate/Support/, where they are newer
// than the clone.
//
// # Functions and the chain
//
// Every transformation is a package-level function, which is Str's static call:
//
//	str.Studly(str.Singular(name))
//
// Of returns a Stringable, which is Illuminate's Str::of and the same
// transformations read the other way round:
//
//	str.Of(name).Singular().Studly().ToString()
//
// They are one implementation with two spellings of the call, not two
// implementations: every Stringable method forwards to the function of the same
// name. Illuminate offers both and the callers of this package expect both.
//
// # Optional arguments
//
// PHP has default arguments and Go has none. A trailing run of optional
// arguments of one type arrives as a variadic tail -- Substr(value, start),
// Substr(value, start, length) -- and where a boolean or a second type follows
// them, every argument is spelled out instead, as on Limit and Replace. Each
// function's comment names the Illuminate defaults it no longer applies.
//
// # What it does not do
//
// ADR 0004 keeps golang.org/x/text out of the core, so nothing here is
// locale-aware. ASCII folds a table of Latin-1 and Latin Extended-A runes and
// drops what it does not know, and Transliterate puts a stand-in in the place
// of what that table cannot spell; Plural and Singular are English, and every
// irregular they get right is a line in a table in inflect.go, not a rule.
// UseLanguage refuses any language but English rather than answer in English
// under another one's name. Locale-correct pluralization is translation.Choice's
// problem, not this package's.
//
// Markdown and InlineMarkdown are this package's own renderer, because
// league/commonmark is a dependency the core does not carry. They cover
// CommonMark and the GitHub flavour's strikethrough, task lists, tables and
// bare-URL autolinking; Illuminate's $options and $extensions, which configure
// that library, have no equivalent.
package str
