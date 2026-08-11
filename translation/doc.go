// Package translation mirrors Illuminate\Translation.
//
// The files it answers to, in the clone at laravel_illuminate/translation,
// which is the source this package was written from:
//
//	ArrayLoader.php
//	CreatesPotentiallyTranslatedStrings.php
//	FileLoader.php
//	MessageSelector.php
//	PotentiallyTranslatedString.php
//	TranslationServiceProvider.php
//	Translator.php
//	lang/en/auth.php
//	lang/en/pagination.php
//	lang/en/passwords.php
//	lang/en/validation.php
//
// It holds [Translator], [MessageSelector], [Selector], [ArrayLoader],
// [FileLoader], [Loader], [Lines], [Replace], [PotentiallyTranslatedString],
// the four English groups the framework produces sentences from, and
// [Negotiate], [Middleware] and [Locale] for the locale of a request.
//
// # The names are Illuminate's
//
// Every method carries the name its PHP counterpart carries, and the four
// alterations this package makes are these:
//
//   - A name is capitalised to export it: choose becomes
//     [MessageSelector.Choose].
//   - An initialism is spelled in one case, as Go spells it: addJsonPath
//     becomes [Translator.AddJSONPath], jsonPaths becomes
//     [FileLoader.JSONPaths].
//   - Where PHP throws, the Go method returns an error:
//     [Translator.SetLocale] answers one for a locale holding a path separator,
//     which is the InvalidArgumentException of Translator::setLocale.
//   - Where a static method carries its class in the identifier, the class is
//     gone: PHP has static methods and Go does not.
//
// The order of the parameters is not always PHP's. A PHP signature is ordered
// by what may be left out -- get($key, $replace = [], $locale = null) -- and Go
// has no optional arguments to order for, so the locale comes first on
// [Translator.Get], [Translator.Choice] and [Translator.Has]. An empty locale
// still means the translator's own, so the caller that has no request still
// works.
//
// # There is no Lang facade
//
// Illuminate reaches the translator through a facade, so a line can be resolved
// from anywhere with no visible dependency. Here [Translator] is a value: it is
// built once where the application is wired and passed to whatever renders a
// sentence. There is no package level translator and no set of free functions
// that read one.
//
// Illuminate's Translator is not safe for concurrent use and does not have to
// be, because a PHP process serves one request. This one is: every field is
// behind a lock, so one instance answers every request at once while
// [Translator.AddLines] or [Translator.SetLocale] is called on it.
//
// TranslationServiceProvider::register and ::provides are not here. They bind
// the translator and its loader into the container, and there is no container
// (ADR 0001) and no facade (ADR 0002).
//
// # Two catalogue formats, as Illuminate has
//
// A group key is "group.item": "auth.failed" is the item "failed" of the group
// "auth", and "validation.min.string" is the item "min.string", because
// everything after the first dot is the item. [Translator.ParseKey] is what
// splits one, and a "namespace::" prefix names a module's own catalogue.
//
// A JSON key is the sentence itself, read out of one file per locale --
// lang/pt-BR.json holding {"Save changes": "Salvar alterações"}. Illuminate
// checks it before the groups and so does [Translator.Get], because a key with
// no dot names no item and would otherwise resolve to nothing.
//
// [FileLoader] reads both, out of an fs.FS: a group at
// <path>/<locale>/<group>.json, a JSON catalogue at <path>/<locale>.json, and
// an override an application published for a module at
// <path>/vendor/<namespace>/<locale>/<group>.json. PHP reads .php files, which
// are code; a language file is data here, and nested objects flatten to dotted
// items so the four size shapes of a validation message stay grouped in the
// file and are read as "min.string".
//
// It parses every file under its initial paths before returning and reports a
// malformed one as an error there: a language file that cannot be parsed is a
// boot failure, not a sentence that goes missing on the one request that needed
// it. [ArrayLoader] is the same catalogue written in Go, which is what a test
// uses.
//
// The English lines for validation, auth, passwords and pagination are embedded
// here and answered after the application catalogue and its fallback locale.
// Illuminate registers the framework's own lang directory as a second path on
// the file loader to the same end. An application overrides one of them by
// defining the same key; it never has to publish a file to get a sentence.
//
// # Plurals
//
// [Translator.Choice] takes the count and hands the line to the [Selector],
// which is [MessageSelector] unless [Translator.SetSelector] replaced it. A
// segment may open with an explicit condition -- "{0} none|[1,19] some|[20,*] many" --
// and the first one that matches the count wins. With no condition, the count
// chooses by the plural rule of the locale: two forms in English, one in
// Japanese, three in Russian, six in Arabic.
//
// Only the segment a condition selected is trimmed, which is what PHP does and
// is worth knowing before writing "one | :count many": the second segment
// renders with its leading space, because the plural rule is what chose it.
//
// [MessageSelector.GetPluralIndex] carries the rule table. It is keyed by the
// language subtag alone, where PHP switches on the whole locale: no region in
// PHP's own table changes the plural forms of its language, and PHP's list --
// which spells every locale with an underscore -- answers the first form for
// the hyphenated tag an Accept-Language header carries.
//
// # Replacements
//
// [Replace] fills the placeholders of a line. Three spellings of every name are
// replaced, as Illuminate builds three entries per argument: ":name" with the
// value, ":Name" with its first letter uppercased and ":NAME" with it
// uppercased, so one argument serves "The :attribute field is required." and
// ":Attribute is required.". A value of type func(string) string is PHP's
// Closure: it replaces the text between <name> and </name> rather than a
// placeholder.
//
// A placeholder no argument names is left as it stands, and the longest name
// that does match wins at each position -- both of which are what PHP's strtr
// does.
//
// # The locale of a request
//
// The locale is a parameter of [Translator.Get], [Translator.Choice] and
// [Translator.Has] rather than something the caller sets on the translator,
// because the locale belongs to the request and one shared translator serves
// all of them. [Middleware] negotiates it once from Accept-Language and puts it
// in the request context; [Locale] reads it back. [Translator.SetLocale] is
// there because Illuminate has it, and it sets the default for a caller that
// passes none -- a console command, or a queued job.
//
// # What is not here
//
// CreatesPotentiallyTranslatedStrings::pendingPotentiallyTranslatedString
// hangs the write-back of a validation message on PHP's __destruct, which Go
// has no equivalent of. [PotentiallyTranslatedString] is here whole; the
// validator calls [PotentiallyTranslatedString.ToString] where PHP waits for
// the object to fall out of scope.
package translation
