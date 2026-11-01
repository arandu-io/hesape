package query

import (
	"strings"
	"unicode"
)

// accentFamilies are the letters a search folds, by the plain letter each one
// folds to: the Latin-1 accented letters and those of Latin Extended-A, which
// Portuguese, Spanish, French, German and the other languages of Western and
// Central Europe are written with.
//
// Every letter is folded in both of its cases because the fold also lowers:
// SQLite's lower() lowers the letters of ASCII and no others, so "CÂM" would
// stay "CÂM" and never meet "câm" unless the fold lowers "Â" itself.
var accentFamilies = []struct{ base, letters string }{
	{"a", "àáâãäåāăą"}, {"c", "çćĉċč"}, {"d", "ďđ"}, {"e", "èéêëēĕėęě"},
	{"g", "ĝğġģ"}, {"h", "ĥħ"}, {"i", "ìíîïĩīĭįıİ"}, {"j", "ĵ"}, {"k", "ķ"},
	{"l", "ĺļľł"}, {"n", "ńņňñ"}, {"o", "òóôõöøōŏő"}, {"r", "ŕŗř"},
	{"s", "śŝşš"}, {"t", "ţť"}, {"u", "ùúûüũūŭůűų"}, {"w", "ŵ"},
	{"y", "ýÿŷ"}, {"z", "źżž"},
}

// accentLetters and accentBases are accentFamilies flattened, in step: the
// letter at one position folds to the letter at the same position of the other.
var accentLetters, accentBases = func() (string, string) {
	var letters, bases strings.Builder
	add := func(letter rune, base string) {
		letters.WriteRune(letter)
		bases.WriteString(base)
	}
	for _, family := range accentFamilies {
		for _, letter := range family.letters {
			add(letter, family.base)
			// The capital, unless it is a plain letter already: lower() has it.
			if upper := unicode.ToUpper(letter); upper != letter && upper > unicode.MaxASCII {
				add(upper, family.base)
			}
		}
	}
	return letters.String(), bases.String()
}()

// FoldAccentsTranslate is the SQL of a text expression with its case and its
// accents folded, for an engine that has translate(): lower case, and each
// letter of the table above replaced by the plain letter. It is the fold of the
// search of a listing on PostgreSQL, which needs no extension and no
// configuration for it.
//
// Both sides of a comparison go through the same function, so a column and a
// binding are folded alike whatever the engine's collation says.
func FoldAccentsTranslate(expression string) string {
	return "translate(lower(" + expression + "), '" + accentLetters + "', '" + accentBases + "')"
}

// FoldAccentsReplace is the same fold for an engine that has no translate(),
// SQLite: one replace() for each letter, nested, inside lower(). It is long, and
// it is the same text on every statement; the engine parses it once per
// statement.
func FoldAccentsReplace(expression string) string {
	letters, bases := []rune(accentLetters), []rune(accentBases)
	var out strings.Builder
	for range letters {
		out.WriteString("replace(")
	}
	out.WriteString("lower(" + expression + ")")
	// The innermost call replaces the first letter, so the arguments close in
	// the order the letters were opened.
	for at, letter := range letters {
		out.WriteString(", '" + string(letter) + "', '" + string(bases[at]) + "')")
	}
	return out.String()
}

// accentFolder is what a grammar implements to say how its engine folds the
// case and the accents of a text expression. A grammar that does not implement
// it leaves the search at lower(), which is right where the engine's own
// collation already ignores accents, as the default collations of MySQL and
// MariaDB do.
type accentFolder interface {
	// FoldAccents is the SQL of expression with its case and accents folded.
	FoldAccents(expression string) string
}

// fold is the SQL a listing's search compares a text expression through.
func (b *Builder) fold(expression string) string {
	if folder, ok := b.Grammar.(accentFolder); ok {
		return folder.FoldAccents(expression)
	}
	return "lower(" + expression + ")"
}
