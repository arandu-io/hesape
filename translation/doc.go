// Package translation mirrors Illuminate\Translation.
//
// The files it answers to, in the clone at
// laravel_illuminate/translation:
//
//	ArrayLoader.php
//	CreatesPotentiallyTranslatedStrings.php
//	FileLoader.php
//	MessageSelector.php
//	PotentiallyTranslatedString.php
//	TranslationServiceProvider.php
//	Translator.php
//
// It holds: Translator, New, Lines, Replace, Loader, NewFS, NewMap, T, Choice,
// Has, Negotiate, Middleware, Locale, WithLocale, Plural, DefaultPlural and the
// English catalogue that ships with the framework.
//
// # There is no Lang facade
//
// Illuminate reaches the translator through a facade, so a line can be resolved
// from anywhere with no visible dependency. Here [Translator] is a value: it is
// built once where the application is wired and passed to whatever renders a
// sentence. There is no package level translator, no set of free functions that
// read one, and nothing in this package holds mutable state after [New]
// returns, so one instance is safe for every request at once.
//
// The locale is not on the translator either. It is a parameter of [Translator.T],
// [Translator.Choice] and [Translator.Has], because the locale belongs to the
// request and a field on a shared value would be a race with a wrong sentence
// as its output. [Middleware] negotiates it once and puts it in the request
// context; [Locale] reads it back. An empty locale means the default the
// translator was built with, so a caller that has no request still works.
//
// # One catalogue format
//
// A key is "group.item": "auth.failed" is the item "failed" of the group
// "auth", and "validation.min.string" is the item "min.string". A key with no
// dot names no item and never resolves. Illuminate has a second format --
// lang/en.json, keyed by the English sentence itself -- and it is not here:
// two catalogues for one message is the shape docs/09-uma-forma-so.md exists to
// refuse.
//
// [NewFS] reads a filesystem laid out as <locale>/<group>.json, in which nested
// objects flatten to dotted items, so the four size shapes of a validation
// message stay grouped in the file and are read as "min.string". It reads
// everything at construction and reports a malformed file as an error there: a
// language file that cannot be parsed is a boot failure, not a sentence that
// goes missing on the one request that needed it. [NewMap] is the same
// catalogue written in Go, which is what a test uses.
//
// The English lines for validation, auth, passwords and pagination are embedded
// in this package and answered last, after the application catalogue and its
// fallback locale. An application overrides one of them by defining the same
// key; it never has to publish a file to get a sentence.
//
// # Plurals
//
// [Translator.Choice] takes the count and picks a segment of a line divided by
// "|". A segment may open with an explicit condition -- "{0} none|[1,19] some|[20,*] many" --
// and the first one that matches the count wins. With no condition, the count
// chooses by the plural rule of the locale: two forms in English, one in
// Japanese, three in Russian, six in Arabic. [DefaultPlural] carries that table
// and [WithPlural] replaces it for an application whose locale it gets wrong.
//
// The rule is keyed by the language subtag alone. No region changes the plural
// forms of a language, so pt-BR and pt-PT resolve the same, and carrying every
// region separately would be a longer table saying the same thing.
//
// # What this package does not carry
//
// Illuminate replaces ":attribute" and also ":Attribute" and ":ATTRIBUTE",
// which is a case convention hidden inside a placeholder. Only ":attribute" is
// replaced here, and a placeholder with no matching entry in [Replace] is left
// alone rather than blanked, so a missing argument reads as the bug it is
// instead of a sentence with a hole in it.
package translation
