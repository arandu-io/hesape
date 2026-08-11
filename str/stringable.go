package str

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Stringable is Illuminate\Support\Stringable: a string carried through a chain
// of the transformations in this package, where every step that produces a
// string produces another Stringable.
//
//	str.Of("  Purchase Order  ").Trim().Snake("_").ToString() // "purchase_order"
//
// It is a value, not a handle: every method returns a new Stringable and none
// of them changes the one it was called on, so a Stringable can be kept, passed
// and reused without anyone else's chain reaching it.
//
// The methods that end a chain -- Length, Contains, ToString and the rest --
// return the Go type the answer is, not a Stringable.
type Stringable struct {
	value string
}

// Of answers for Str::of, and for `new Stringable($value)`, which in PHP are
// the same call written two ways.
func Of(value string) Stringable { return Stringable{value: value} }

// String answers for Stringable::__toString. It is also fmt.Stringer, which is
// what makes a Stringable print as its own text.
func (s Stringable) String() string { return s.value }

// ToString answers for Stringable::toString. It is the string itself.
func (s Stringable) ToString() string { return s.value }

// Value answers for Stringable::value. Illuminate defines it as an alias of
// toString, and so does this.
func (s Stringable) Value() string { return s.value }

// After answers for Stringable::after.
func (s Stringable) After(search string) Stringable { return Of(After(s.value, search)) }

// AfterLast answers for Stringable::afterLast.
func (s Stringable) AfterLast(search string) Stringable { return Of(AfterLast(s.value, search)) }

// Append answers for Stringable::append. It puts the values on the end, in
// order.
func (s Stringable) Append(values ...string) Stringable {
	return Of(s.value + strings.Join(values, ""))
}

// Prepend answers for Stringable::prepend. It puts the values on the front, in
// order.
func (s Stringable) Prepend(values ...string) Stringable {
	return Of(strings.Join(values, "") + s.value)
}

// NewLine answers for Stringable::newLine. It appends count line breaks;
// passing none appends one, which is Illuminate's default.
//
// The break is "\n". PHP writes PHP_EOL, which is the platform's; Go's line
// break is "\n" on every platform it builds for.
func (s Stringable) NewLine(count ...int) Stringable {
	n := 1
	if len(count) > 0 {
		n = count[0]
	}
	return s.Append(Repeat("\n", n))
}

// ASCII answers for Stringable::ascii. Illuminate's $language argument is gone;
// see the comment on ASCII.
func (s Stringable) ASCII() Stringable { return Of(ASCII(s.value)) }

// Transliterate answers for Stringable::transliterate.
func (s Stringable) Transliterate(unknown string, strict bool) Stringable {
	return Of(Transliterate(s.value, unknown, strict))
}

// Basename answers for Stringable::basename. It is the last component of a
// path, with the suffix removed when one is given and the component is more
// than the suffix itself.
func (s Stringable) Basename(suffix ...string) Stringable {
	name := strings.TrimRight(s.value, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if len(suffix) > 0 && suffix[0] != "" && name != suffix[0] && strings.HasSuffix(name, suffix[0]) {
		name = name[:len(name)-len(suffix[0])]
	}
	return Of(name)
}

// Dirname answers for Stringable::dirname. It is the path with its last
// component removed, levels times over; passing none removes one, which is
// Illuminate's default.
func (s Stringable) Dirname(levels ...int) Stringable {
	n := 1
	if len(levels) > 0 {
		n = levels[0]
	}
	dir := s.value
	for range n {
		dir = phpDirname(dir)
	}
	return Of(dir)
}

// ClassBasename answers for Stringable::classBasename, which is Illuminate's
// class_basename helper: the part of a fully qualified class name after the
// last namespace separator.
func (s Stringable) ClassBasename() Stringable {
	return Of(AfterLast(strings.ReplaceAll(s.value, `\`, "/"), "/"))
}

// Before answers for Stringable::before.
func (s Stringable) Before(search string) Stringable { return Of(Before(s.value, search)) }

// BeforeLast answers for Stringable::beforeLast.
func (s Stringable) BeforeLast(search string) Stringable { return Of(BeforeLast(s.value, search)) }

// Between answers for Stringable::between.
func (s Stringable) Between(from, to string) Stringable { return Of(Between(s.value, from, to)) }

// BetweenFirst answers for Stringable::betweenFirst.
func (s Stringable) BetweenFirst(from, to string) Stringable {
	return Of(BetweenFirst(s.value, from, to))
}

// Camel answers for Stringable::camel.
func (s Stringable) Camel() Stringable { return Of(Camel(s.value)) }

// Kebab answers for Stringable::kebab.
func (s Stringable) Kebab() Stringable { return Of(Kebab(s.value)) }

// Snake answers for Stringable::snake.
func (s Stringable) Snake(delimiter string) Stringable { return Of(Snake(s.value, delimiter)) }

// Studly answers for Stringable::studly.
func (s Stringable) Studly() Stringable { return Of(Studly(s.value)) }

// Pascal answers for Stringable::pascal.
func (s Stringable) Pascal() Stringable { return Of(Pascal(s.value)) }

// Title answers for Stringable::title.
func (s Stringable) Title() Stringable { return Of(Title(s.value)) }

// Headline answers for Stringable::headline.
func (s Stringable) Headline() Stringable { return Of(Headline(s.value)) }

// Apa answers for Stringable::apa.
func (s Stringable) Apa() Stringable { return Of(Apa(s.value)) }

// Lower answers for Stringable::lower.
func (s Stringable) Lower() Stringable { return Of(Lower(s.value)) }

// Upper answers for Stringable::upper.
func (s Stringable) Upper() Stringable { return Of(Upper(s.value)) }

// Ucfirst answers for Stringable::ucfirst.
func (s Stringable) Ucfirst() Stringable { return Of(Ucfirst(s.value)) }

// Lcfirst answers for Stringable::lcfirst.
func (s Stringable) Lcfirst() Stringable { return Of(Lcfirst(s.value)) }

// Ucwords answers for Stringable::ucwords.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) Ucwords(separators ...string) Stringable {
	return Of(Ucwords(s.value, separators...))
}

// Ucsplit answers for Stringable::ucsplit. Illuminate wraps the pieces in a
// Collection; this returns the slice, because collections are their own package
// and this one carries no dependency.
func (s Stringable) Ucsplit() []string { return Ucsplit(s.value) }

// ConvertCase answers for Stringable::convertCase. The mode is one of
// CaseUpper, CaseLower, CaseTitle or CaseFold.
func (s Stringable) ConvertCase(mode int) Stringable { return Of(ConvertCase(s.value, mode)) }

// CharAt answers for Stringable::charAt. The second result is the false PHP
// returns for an index outside the string.
func (s Stringable) CharAt(index int) (string, bool) { return CharAt(s.value, index) }

// ChopStart answers for Stringable::chopStart.
func (s Stringable) ChopStart(needle ...string) Stringable {
	return Of(ChopStart(s.value, needle...))
}

// ChopEnd answers for Stringable::chopEnd.
func (s Stringable) ChopEnd(needle ...string) Stringable { return Of(ChopEnd(s.value, needle...)) }

// Contains answers for Stringable::contains.
func (s Stringable) Contains(needles []string, ignoreCase bool) bool {
	return Contains(s.value, needles, ignoreCase)
}

// ContainsAll answers for Stringable::containsAll.
func (s Stringable) ContainsAll(needles []string, ignoreCase bool) bool {
	return ContainsAll(s.value, needles, ignoreCase)
}

// DoesntContain answers for Stringable::doesntContain.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) DoesntContain(needles []string, ignoreCase bool) bool {
	return DoesntContain(s.value, needles, ignoreCase)
}

// StartsWith answers for Stringable::startsWith.
func (s Stringable) StartsWith(needles ...string) bool { return StartsWith(s.value, needles...) }

// EndsWith answers for Stringable::endsWith.
func (s Stringable) EndsWith(needles ...string) bool { return EndsWith(s.value, needles...) }

// DoesntStartWith answers for Stringable::doesntStartWith.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) DoesntStartWith(needles ...string) bool {
	return DoesntStartWith(s.value, needles...)
}

// DoesntEndWith answers for Stringable::doesntEndWith.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) DoesntEndWith(needles ...string) bool {
	return DoesntEndWith(s.value, needles...)
}

// Deduplicate answers for Stringable::deduplicate.
func (s Stringable) Deduplicate(character ...string) Stringable {
	return Of(Deduplicate(s.value, character...))
}

// Exactly answers for Stringable::exactly. It is string equality.
func (s Stringable) Exactly(value string) bool { return s.value == value }

// Excerpt answers for Stringable::excerpt. The second result is the null PHP
// returns when the phrase is not in the text.
//
// Illuminate reads the radius and the omission out of an options array; they
// are arguments here, and Illuminate's defaults are 100 and "...".
func (s Stringable) Excerpt(phrase string, radius int, omission string) (string, bool) {
	return Excerpt(s.value, phrase, radius, omission)
}

// Explode answers for Stringable::explode. Illuminate wraps the pieces in a
// Collection; this returns the slice.
//
// The optional limit is PHP's: a positive one leaves the rest of the string in
// the last piece, a negative one drops that many pieces off the end, and zero
// is read as one.
func (s Stringable) Explode(delimiter string, limit ...int) []string {
	if delimiter == "" {
		return nil
	}
	if len(limit) == 0 {
		return strings.Split(s.value, delimiter)
	}
	n := limit[0]
	switch {
	case n > 0:
		return strings.SplitN(s.value, delimiter, n)
	case n == 0:
		return strings.SplitN(s.value, delimiter, 1)
	default:
		all := strings.Split(s.value, delimiter)
		if kept := len(all) + n; kept > 0 {
			return all[:kept]
		}
		return nil
	}
}

// Split answers for Stringable::split. Illuminate wraps the pieces in a
// Collection; this returns the slice.
//
// The pattern is Illuminate's mixed: an int splits the string into pieces that
// many characters wide, and a *regexp.Regexp or a string splits it on the
// matches. The optional limit is the greatest number of pieces to produce, and
// a value below one means no limit.
func (s Stringable) Split(pattern any, limit ...int) []string {
	n := -1
	if len(limit) > 0 && limit[0] > 0 {
		n = limit[0]
	}
	switch p := pattern.(type) {
	case int:
		return chunkRunes(s.value, p)
	case *regexp.Regexp:
		return p.Split(s.value, n)
	case string:
		re, err := regexp.Compile(p)
		if err != nil {
			return nil
		}
		return re.Split(s.value, n)
	default:
		return nil
	}
}

// Scan answers for Stringable::scan. Illuminate wraps the fields in a
// Collection; this returns them as strings.
//
// The format is sscanf's, reduced to what a caller of this reaches for: %s for
// a run of non-space characters, %d for an integer, %f for a decimal, %c for
// one character, %% for a literal per cent, whitespace for any run of
// whitespace, and anything else for itself. A field that does not match ends
// the scan, and what was read up to there is the answer.
func (s Stringable) Scan(format string) []string { return sscanf(s.value, format) }

// Finish answers for Stringable::finish.
func (s Stringable) Finish(cap string) Stringable { return Of(Finish(s.value, cap)) }

// Start answers for Stringable::start.
func (s Stringable) Start(prefix string) Stringable { return Of(Start(s.value, prefix)) }

// Wrap answers for Stringable::wrap.
func (s Stringable) Wrap(before string, after ...string) Stringable {
	return Of(Wrap(s.value, before, after...))
}

// Unwrap answers for Stringable::unwrap.
func (s Stringable) Unwrap(before string, after ...string) Stringable {
	return Of(Unwrap(s.value, before, after...))
}

// Is answers for Stringable::is.
func (s Stringable) Is(pattern []string, ignoreCase bool) bool {
	return Is(pattern, s.value, ignoreCase)
}

// IsASCII answers for Stringable::isAscii.
func (s Stringable) IsASCII() bool { return IsASCII(s.value) }

// IsJSON answers for Stringable::isJson.
func (s Stringable) IsJSON() bool { return IsJSON(s.value) }

// IsURL answers for Stringable::isUrl.
func (s Stringable) IsURL(protocols ...string) bool { return IsURL(s.value, protocols...) }

// IsUUID answers for Stringable::isUuid.
func (s Stringable) IsUUID() bool { return IsUUID(s.value) }

// IsULID answers for Stringable::isUlid.
func (s Stringable) IsULID() bool { return IsULID(s.value) }

// IsEmpty answers for Stringable::isEmpty.
func (s Stringable) IsEmpty() bool { return s.value == "" }

// IsNotEmpty answers for Stringable::isNotEmpty.
func (s Stringable) IsNotEmpty() bool { return !s.IsEmpty() }

// Length answers for Stringable::length.
func (s Stringable) Length() int { return Length(s.value) }

// Limit answers for Stringable::limit.
func (s Stringable) Limit(limit int, end string, preserveWords bool) Stringable {
	return Of(Limit(s.value, limit, end, preserveWords))
}

// Words answers for Stringable::words.
func (s Stringable) Words(words int, end string) Stringable {
	return Of(Words(s.value, words, end))
}

// WordCount answers for Stringable::wordCount.
func (s Stringable) WordCount(characters ...string) int {
	return WordCount(s.value, characters...)
}

// WordWrap answers for Stringable::wordWrap.
func (s Stringable) WordWrap(characters int, brk string, cutLongWords bool) Stringable {
	return Of(WordWrap(s.value, characters, brk, cutLongWords))
}

// Markdown answers for Stringable::markdown.
func (s Stringable) Markdown() Stringable { return Of(Markdown(s.value)) }

// InlineMarkdown answers for Stringable::inlineMarkdown.
func (s Stringable) InlineMarkdown() Stringable { return Of(InlineMarkdown(s.value)) }

// Mask answers for Stringable::mask.
func (s Stringable) Mask(character string, index int, length ...int) Stringable {
	return Of(Mask(s.value, character, index, length...))
}

// Match answers for Stringable::match.
func (s Stringable) Match(pattern *regexp.Regexp) Stringable {
	return Of(Match(pattern, s.value))
}

// MatchAll answers for Stringable::matchAll. Illuminate wraps the matches in a
// Collection; this returns the slice.
func (s Stringable) MatchAll(pattern *regexp.Regexp) []string { return MatchAll(pattern, s.value) }

// IsMatch answers for Stringable::isMatch.
func (s Stringable) IsMatch(patterns []*regexp.Regexp) bool { return IsMatch(patterns, s.value) }

// Test answers for Stringable::test. Illuminate defines it as an alias of
// isMatch, and so does this.
func (s Stringable) Test(patterns []*regexp.Regexp) bool { return s.IsMatch(patterns) }

// ReplaceMatches answers for Stringable::replaceMatches.
func (s Stringable) ReplaceMatches(pattern *regexp.Regexp, replace string, limit int) Stringable {
	return Of(ReplaceMatches(pattern, replace, s.value, limit))
}

// Numbers answers for Stringable::numbers.
func (s Stringable) Numbers() Stringable { return Of(Numbers(s.value)) }

// PadBoth answers for Stringable::padBoth.
func (s Stringable) PadBoth(length int, pad ...string) Stringable {
	return Of(PadBoth(s.value, length, pad...))
}

// PadLeft answers for Stringable::padLeft.
func (s Stringable) PadLeft(length int, pad ...string) Stringable {
	return Of(PadLeft(s.value, length, pad...))
}

// PadRight answers for Stringable::padRight.
func (s Stringable) PadRight(length int, pad ...string) Stringable {
	return Of(PadRight(s.value, length, pad...))
}

// ParseCallback answers for Stringable::parseCallback.
func (s Stringable) ParseCallback(fallback string) (string, string) {
	return ParseCallback(s.value, fallback)
}

// Pipe answers for Stringable::pipe. It hands the Stringable to the callback
// and wraps what comes back.
func (s Stringable) Pipe(callback func(Stringable) string) Stringable { return Of(callback(s)) }

// Plural answers for Stringable::plural.
func (s Stringable) Plural(count ...int) Stringable { return Of(Plural(s.value, count...)) }

// PluralStudly answers for Stringable::pluralStudly.
func (s Stringable) PluralStudly(count ...int) Stringable {
	return Of(PluralStudly(s.value, count...))
}

// PluralPascal answers for Stringable::pluralPascal.
func (s Stringable) PluralPascal(count ...int) Stringable {
	return Of(PluralPascal(s.value, count...))
}

// Singular answers for Stringable::singular.
func (s Stringable) Singular() Stringable { return Of(Singular(s.value)) }

// Counted answers for Stringable::counted.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) Counted(count int) Stringable { return Of(Counted(s.value, count)) }

// Position answers for Stringable::position. It returns -1 where PHP returns
// false; see the comment on Position.
func (s Stringable) Position(needle string, offset int) int {
	return Position(s.value, needle, offset)
}

// Remove answers for Stringable::remove.
func (s Stringable) Remove(search []string, caseSensitive bool) Stringable {
	return Of(Remove(search, s.value, caseSensitive))
}

// Reverse answers for Stringable::reverse.
func (s Stringable) Reverse() Stringable { return Of(Reverse(s.value)) }

// Repeat answers for Stringable::repeat.
func (s Stringable) Repeat(times int) Stringable { return Of(Repeat(s.value, times)) }

// Replace answers for Stringable::replace.
func (s Stringable) Replace(search, replace []string, caseSensitive bool) Stringable {
	return Of(Replace(search, replace, s.value, caseSensitive))
}

// ReplaceArray answers for Stringable::replaceArray.
func (s Stringable) ReplaceArray(search string, replace []string) Stringable {
	return Of(ReplaceArray(search, replace, s.value))
}

// ReplaceFirst answers for Stringable::replaceFirst.
func (s Stringable) ReplaceFirst(search, replace string) Stringable {
	return Of(ReplaceFirst(search, replace, s.value))
}

// ReplaceLast answers for Stringable::replaceLast.
func (s Stringable) ReplaceLast(search, replace string) Stringable {
	return Of(ReplaceLast(search, replace, s.value))
}

// ReplaceStart answers for Stringable::replaceStart.
func (s Stringable) ReplaceStart(search, replace string) Stringable {
	return Of(ReplaceStart(search, replace, s.value))
}

// ReplaceEnd answers for Stringable::replaceEnd.
func (s Stringable) ReplaceEnd(search, replace string) Stringable {
	return Of(ReplaceEnd(search, replace, s.value))
}

// Slug answers for Stringable::slug.
func (s Stringable) Slug(separator string) Stringable { return Of(Slug(s.value, separator)) }

// Squish answers for Stringable::squish.
func (s Stringable) Squish() Stringable { return Of(Squish(s.value)) }

// StripTags answers for Stringable::stripTags. Illuminate's $allowedTags is
// gone: nothing is allowed through, which is the default.
func (s Stringable) StripTags() Stringable { return Of(stripTags(s.value)) }

// Substr answers for Stringable::substr.
func (s Stringable) Substr(start int, length ...int) Stringable {
	return Of(Substr(s.value, start, length...))
}

// SubstrCount answers for Stringable::substrCount.
func (s Stringable) SubstrCount(needle string, offset int, length ...int) int {
	return SubstrCount(s.value, needle, offset, length...)
}

// SubstrReplace answers for Stringable::substrReplace.
func (s Stringable) SubstrReplace(replace string, offset int, length ...int) Stringable {
	return Of(SubstrReplace(s.value, replace, offset, length...))
}

// Swap answers for Stringable::swap.
func (s Stringable) Swap(swap map[string]string) Stringable { return Of(Swap(swap, s.value)) }

// Take answers for Stringable::take.
func (s Stringable) Take(limit int) Stringable { return Of(Take(s.value, limit)) }

// Trim answers for Stringable::trim.
func (s Stringable) Trim(characters ...string) Stringable {
	return Of(Trim(s.value, characters...))
}

// Ltrim answers for Stringable::ltrim.
func (s Stringable) Ltrim(characters ...string) Stringable {
	return Of(Ltrim(s.value, characters...))
}

// Rtrim answers for Stringable::rtrim.
func (s Stringable) Rtrim(characters ...string) Stringable {
	return Of(Rtrim(s.value, characters...))
}

// ToBase64 answers for Stringable::toBase64.
func (s Stringable) ToBase64() Stringable { return Of(ToBase64(s.value)) }

// FromBase64 answers for Stringable::fromBase64. What does not decode becomes
// the empty string, which is the false PHP puts through its string cast.
func (s Stringable) FromBase64(strict bool) Stringable {
	decoded, _ := FromBase64(s.value, strict)
	return Of(decoded)
}

// Initials answers for Stringable::initials.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) Initials(capitalize bool) Stringable {
	return Of(Initials(s.value, capitalize))
}

// When answers for Conditionable::when, which Stringable takes on. It runs the
// callback when the condition holds, and the optional second callback when it
// does not, and hands back whatever comes out.
func (s Stringable) When(condition bool, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	if condition {
		if callback != nil {
			return callback(s)
		}
		return s
	}
	if len(otherwise) > 0 && otherwise[0] != nil {
		return otherwise[0](s)
	}
	return s
}

// Unless answers for Conditionable::unless. It is When with the condition read
// the other way.
func (s Stringable) Unless(condition bool, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(!condition, callback, otherwise...)
}

// Tap answers for Tappable::tap, which Stringable takes on. It hands the
// Stringable to the callback and returns it unchanged.
func (s Stringable) Tap(callback func(Stringable)) Stringable {
	if callback != nil {
		callback(s)
	}
	return s
}

// Dump answers for Dumpable::dump, which Stringable takes on. It writes the
// value out and returns the Stringable so a chain carries on through it.
func (s Stringable) Dump(args ...any) Stringable {
	fmt.Println(append([]any{s.value}, args...)...)
	return s
}

// WhenContains answers for Stringable::whenContains.
func (s Stringable) WhenContains(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.Contains(needles, false), callback, otherwise...)
}

// WhenContainsAll answers for Stringable::whenContainsAll.
func (s Stringable) WhenContainsAll(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.ContainsAll(needles, false), callback, otherwise...)
}

// WhenEmpty answers for Stringable::whenEmpty.
func (s Stringable) WhenEmpty(callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.IsEmpty(), callback, otherwise...)
}

// WhenNotEmpty answers for Stringable::whenNotEmpty.
func (s Stringable) WhenNotEmpty(callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.IsNotEmpty(), callback, otherwise...)
}

// WhenStartsWith answers for Stringable::whenStartsWith.
func (s Stringable) WhenStartsWith(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.StartsWith(needles...), callback, otherwise...)
}

// WhenEndsWith answers for Stringable::whenEndsWith.
func (s Stringable) WhenEndsWith(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.EndsWith(needles...), callback, otherwise...)
}

// WhenDoesntStartWith answers for Stringable::whenDoesntStartWith.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) WhenDoesntStartWith(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.DoesntStartWith(needles...), callback, otherwise...)
}

// WhenDoesntEndWith answers for Stringable::whenDoesntEndWith.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func (s Stringable) WhenDoesntEndWith(needles []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.DoesntEndWith(needles...), callback, otherwise...)
}

// WhenExactly answers for Stringable::whenExactly.
func (s Stringable) WhenExactly(value string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.Exactly(value), callback, otherwise...)
}

// WhenNotExactly answers for Stringable::whenNotExactly.
func (s Stringable) WhenNotExactly(value string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(!s.Exactly(value), callback, otherwise...)
}

// WhenIs answers for Stringable::whenIs.
func (s Stringable) WhenIs(pattern []string, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.Is(pattern, false), callback, otherwise...)
}

// WhenIsASCII answers for Stringable::whenIsAscii.
func (s Stringable) WhenIsASCII(callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.IsASCII(), callback, otherwise...)
}

// WhenIsUUID answers for Stringable::whenIsUuid.
func (s Stringable) WhenIsUUID(callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.IsUUID(), callback, otherwise...)
}

// WhenIsULID answers for Stringable::whenIsUlid.
func (s Stringable) WhenIsULID(callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.IsULID(), callback, otherwise...)
}

// WhenTest answers for Stringable::whenTest.
func (s Stringable) WhenTest(patterns []*regexp.Regexp, callback func(Stringable) Stringable, otherwise ...func(Stringable) Stringable) Stringable {
	return s.When(s.Test(patterns), callback, otherwise...)
}

// ToInteger answers for Stringable::toInteger. It reads the longest integer at
// the front of the string and answers zero when there is none, which is what
// PHP's intval does.
//
// The optional base is intval's, and defaults to ten. Base zero reads the
// prefix -- 0x for hexadecimal, 0b for binary, 0 for octal.
func (s Stringable) ToInteger(base ...int) int {
	b := 10
	if len(base) > 0 {
		b = base[0]
	}
	return phpIntval(s.value, b)
}

// ToFloat answers for Stringable::toFloat. It reads the longest decimal at the
// front of the string and answers zero when there is none, which is what PHP's
// floatval does.
func (s Stringable) ToFloat() float64 { return phpFloatval(s.value) }

// ToBoolean answers for Stringable::toBoolean. It is PHP's
// FILTER_VALIDATE_BOOLEAN: "1", "true", "on" and "yes" are true, in any case
// and with the ends trimmed, and everything else is false.
func (s Stringable) ToBoolean() bool {
	switch Lower(strings.TrimSpace(s.value)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// ToDate answers for Stringable::toDate. It reads the string as a time.
//
// With a format, the format is PHP's -- "Y-m-d H:i:s" and the rest of the date()
// letters -- and the string has to match it whole, which is
// Date::createFromFormat. With none it tries the layouts a machine writes, which
// is what Date::parse does. Illuminate throws for a string it cannot read; the
// error is that throw.
//
// Illuminate's $tz is gone: a layout that carries no zone is read as UTC, and a
// caller that wants another one converts what comes back.
func (s Stringable) ToDate(format ...string) (time.Time, error) {
	if len(format) > 0 {
		layout, err := goLayout(format[0])
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(layout, s.value)
	}
	return parseDate(s.value)
}
