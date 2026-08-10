package str

import (
	"strconv"
	"strings"
	"unicode"
)

// Plural is the plural of an English noun: "purchase_order" becomes
// "purchase_orders", "person" becomes "people", "knife" becomes "knives".
//
// It pluralizes the last word and leaves the rest alone, so a compound name
// keeps its shape: "sales_person" is "sales_people", not "sales_persons". The
// case of that word is carried over -- "Invoice" is "Invoices" and "INVOICE" is
// "INVOICES" -- because this names a table in one call site and a heading in
// the next.
//
// English pluralization has hundreds of exceptions and this handles the ones a
// schema meets. Everything the tables in this file do not name takes -s or -es
// by rule. A word that comes out wrong is a line to add there, not a rule to
// change.
func Plural(s string) string {
	head, word := splitTail(s)
	if word == "" {
		return s
	}
	return head + matchCase(word, pluralize(strings.ToLower(word)))
}

// PluralN is a count and the noun agreeing with it: "1 rule", "3 rules".
//
// The count is in the answer because every caller was building that sentence by
// hand, and the one that was not printed "1 fields".
func PluralN(n int, singular string) string {
	if n == 1 || n == -1 {
		return strconv.Itoa(n) + " " + singular
	}
	return strconv.Itoa(n) + " " + Plural(singular)
}

// Singular is the singular of an English noun: "purchase_orders" becomes
// "purchase_order", "people" becomes "person", "knives" becomes "knife".
//
// It is Plural read backwards, with the same tables and the same treatment of
// compounds and case. English does not invert cleanly -- "axes" is the plural of
// both "axe" and "axis", and "-ses" is "-se" in "houses" and "-s" in "buses" --
// so where the ending is ambiguous this answers with the commoner word and the
// table in this file holds the corrections.
func Singular(s string) string {
	head, word := splitTail(s)
	if word == "" {
		return s
	}
	return head + matchCase(word, singularize(strings.ToLower(word)))
}

// splitTail cuts a name into everything before the last word and the last word
// itself. Only the last word inflects: "purchase_order" pluralizes "order".
func splitTail(s string) (head, word string) {
	rs := []rune(s)
	for i := len(rs) - 1; i >= 0; i-- {
		if isSeparator(rs[i]) {
			return string(rs[:i+1]), string(rs[i+1:])
		}
	}
	return "", s
}

// matchCase spells out the way the word it replaces was spelled: all capitals
// stay all capitals, an initial capital stays an initial capital.
func matchCase(was, now string) string {
	rs := []rune(was)
	if len(rs) == 0 || now == "" {
		return now
	}
	if !unicode.IsUpper(rs[0]) {
		return now
	}
	allUpper := true
	for _, r := range rs {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			allUpper = false
			break
		}
	}
	if allUpper && len(rs) > 1 {
		return strings.ToUpper(now)
	}
	return UpperFirst(now)
}

func pluralize(w string) string {
	if uncountable[w] {
		return w
	}
	if p, ok := irregularPlural[w]; ok {
		return p
	}
	if p, ok := fToVes[w]; ok {
		return p
	}
	if p, ok := oToOes[w]; ok {
		return p
	}
	switch {
	case hasAnySuffix(w, "s", "x", "z", "ch", "sh"):
		return w + "es"
	case strings.HasSuffix(w, "y") && len(w) > 1 && !isVowel(w[len(w)-2]):
		return w[:len(w)-1] + "ies"
	default:
		return w + "s"
	}
}

func singularize(w string) string {
	if uncountable[w] {
		return w
	}
	if s, ok := irregularSingular[w]; ok {
		return s
	}
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case hasAnySuffix(w, "sses", "xes", "zes", "ches", "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "ses"):
		// "houses" over "buses": a word ending -se is the commoner shape, and
		// the ones that are not are entries in irregularSingular.
		return w[:len(w)-1]
	case strings.HasSuffix(w, "ss"), hasAnySuffix(w, "us", "is"):
		return w
	case strings.HasSuffix(w, "s") && len(w) > 1:
		return w[:len(w)-1]
	default:
		return w
	}
}

func hasAnySuffix(w string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(w, s) {
			return true
		}
	}
	return false
}

func isVowel(b byte) bool { return strings.IndexByte("aeiou", b) >= 0 }

// uncountable is a noun that is its own plural. A word here is never touched by
// either direction.
var uncountable = set(
	"aircraft", "bison", "deer", "equipment", "evidence", "fish", "furniture",
	"information", "money", "moose", "news", "offspring", "rice", "salmon",
	"series", "sheep", "software", "species", "staff", "swine", "trout",
)

// irregularPlural is the plural that no rule produces.
var irregularPlural = map[string]string{
	"analysis":   "analyses",
	"appendix":   "appendices",
	"axis":       "axes",
	"basis":      "bases",
	"cactus":     "cacti",
	"child":      "children",
	"crisis":     "crises",
	"criterion":  "criteria",
	"datum":      "data",
	"diagnosis":  "diagnoses",
	"die":        "dice",
	"focus":      "foci",
	"foot":       "feet",
	"fungus":     "fungi",
	"goose":      "geese",
	"index":      "indices",
	"louse":      "lice",
	"man":        "men",
	"matrix":     "matrices",
	"medium":     "media",
	"memorandum": "memoranda",
	"mouse":      "mice",
	"nucleus":    "nuclei",
	"ox":         "oxen",
	"person":     "people",
	"phenomenon": "phenomena",
	"quiz":       "quizzes",
	"radius":     "radii",
	"thesis":     "theses",
	"tooth":      "teeth",
	"vertex":     "vertices",
	"woman":      "women",
}

// fToVes is the -f and -fe words that take -ves. An allowlist and not a rule,
// because the rule turns "cafe" into "caves" and "roof" into "rooves".
var fToVes = map[string]string{
	"calf":  "calves",
	"elf":   "elves",
	"half":  "halves",
	"hoof":  "hooves",
	"knife": "knives",
	"leaf":  "leaves",
	"life":  "lives",
	"loaf":  "loaves",
	"self":  "selves",
	"sheaf": "sheaves",
	"shelf": "shelves",
	"thief": "thieves",
	"wharf": "wharves",
	"wife":  "wives",
	"wolf":  "wolves",
}

// oToOes is the -o words that take -oes. An allowlist for the same reason:
// "photo" and "piano" take -s, and they are the majority.
var oToOes = map[string]string{
	"buffalo":  "buffaloes",
	"echo":     "echoes",
	"embargo":  "embargoes",
	"hero":     "heroes",
	"mosquito": "mosquitoes",
	"potato":   "potatoes",
	"tomato":   "tomatoes",
	"torpedo":  "torpedoes",
	"veto":     "vetoes",
	"volcano":  "volcanoes",
}

// irregularSingular is every table above read backwards, plus the -es plurals
// whose singular ends in -s and would otherwise lose a letter.
var irregularSingular = reverse(
	irregularPlural, fToVes, oToOes,
	map[string]string{
		"atlas":  "atlases",
		"bias":   "biases",
		"bonus":  "bonuses",
		"bus":    "buses",
		"campus": "campuses",
		"gas":    "gases",
		"lens":   "lenses",
		"plus":   "pluses",
		"status": "statuses",
		"virus":  "viruses",
	},
)

func set(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

func reverse(tables ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, t := range tables {
		for singular, plural := range t {
			out[plural] = singular
		}
	}
	return out
}
