package str_test

import (
	"testing"

	"github.com/arandu-io/hesape/str"
)

// TestTheEmptyStringSurvivesEveryTransformation walks the functions that take a
// subject and asks each for the empty string, because that is the argument a
// caller passes without meaning to and the one PHP quietly answers for.
func TestTheEmptyStringSurvivesEveryTransformation(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"Slug", str.Slug("", "-")},
		{"Snake", str.Snake("", "_")},
		{"Kebab", str.Kebab("")},
		{"Camel", str.Camel("")},
		{"Studly", str.Studly("")},
		{"Headline", str.Headline("")},
		{"Title", str.Title("")},
		{"Apa", str.Apa("")},
		{"Plural", str.Plural("")},
		{"Singular", str.Singular("")},
		{"Squish", str.Squish("")},
		{"Reverse", str.Reverse("")},
		{"Numbers", str.Numbers("")},
		{"Ucfirst", str.Ucfirst("")},
		{"Lcfirst", str.Lcfirst("")},
		{"Substr", str.Substr("", 0)},
		{"Take", str.Take("", 5)},
		{"Trim", str.Trim("")},
		{"Transliterate", str.Transliterate("", "?", false)},
		{"Limit", str.Limit("", 5, "...", false)},
		{"Words", str.Words("", 5, "...")},
		{"PluralStudly", str.PluralStudly("")},
	}
	for _, c := range cases {
		if c.got != "" {
			t.Errorf("%s(\"\") = %q, want the empty string", c.name, c.got)
		}
	}
}

// TestLimitWithALimitSmallerThanTheEnding pins the shape PHP has: the ending is
// appended to whatever survived the cut and is never counted against the limit,
// so an ending longer than the limit comes back longer than the limit.
func TestLimitWithALimitSmallerThanTheEnding(t *testing.T) {
	cases := []struct {
		value string
		limit int
		end   string
		want  string
	}{
		{"Laravel", 2, "...", "La..."},
		{"Laravel", 0, "...", "..."},
		{"Laravel", -1, "...", "..."},
		{"Laravel", 7, "...", "Laravel"},
		{"Laravel", 100, "...", "Laravel"},
		{"Laravel", 3, "", "Lar"},
	}
	for _, c := range cases {
		if got := str.Limit(c.value, c.limit, c.end, false); got != c.want {
			t.Errorf("Limit(%q, %d, %q, false) = %q, want %q", c.value, c.limit, c.end, got, c.want)
		}
	}
}

// TestSubstrWithANegativeStart walks the four corners of PHP's substr: a start
// counted from the end, a start past the end, a negative length and a window
// that closes before it opens.
func TestSubstrWithANegativeStart(t *testing.T) {
	cases := []struct {
		value  string
		start  int
		length []int
		want   string
	}{
		{"Laravel", -3, nil, "vel"},
		{"Laravel", -3, []int{2}, "ve"},
		{"Laravel", -100, nil, "Laravel"},
		{"Laravel", 100, nil, ""},
		{"Laravel", 0, []int{-3}, "Lara"},
		{"Laravel", 5, []int{-5}, ""},
		{"Laravel", 2, []int{0}, ""},
		{"Laravel", 2, []int{100}, "ravel"},
		{"café", -1, nil, "é"},
	}
	for _, c := range cases {
		if got := str.Substr(c.value, c.start, c.length...); got != c.want {
			t.Errorf("Substr(%q, %d, %v) = %q, want %q", c.value, c.start, c.length, got, c.want)
		}
	}
}

// TestPadWithAWidthSmallerThanTheString records that PHP's str_pad never
// shortens: a width at or below the length of the string returns it whole.
func TestPadWithAWidthSmallerThanTheString(t *testing.T) {
	for _, width := range []int{-5, 0, 3, 7} {
		if got := str.PadLeft("Laravel", width, "_"); got != "Laravel" {
			t.Errorf("PadLeft(%q, %d, %q) = %q, want %q", "Laravel", width, "_", got, "Laravel")
		}
		if got := str.PadRight("Laravel", width, "_"); got != "Laravel" {
			t.Errorf("PadRight(%q, %d, %q) = %q, want %q", "Laravel", width, "_", got, "Laravel")
		}
		if got := str.PadBoth("Laravel", width, "_"); got != "Laravel" {
			t.Errorf("PadBoth(%q, %d, %q) = %q, want %q", "Laravel", width, "_", got, "Laravel")
		}
	}
	if got := str.PadLeft("7", 3, "0"); got != "007" {
		t.Errorf("PadLeft(%q, 3, %q) = %q, want %q", "7", "0", got, "007")
	}
	if got := str.PadBoth("7", 5, "_"); got != "__7__" {
		t.Errorf("PadBoth(%q, 5, %q) = %q, want %q", "7", "_", got, "__7__")
	}
}

// TestWordsWithACountOfZero pins the branch Illuminate falls through: the
// pattern it builds for a count below one does not match, so the subject comes
// back untouched and no ending is appended.
func TestWordsWithACountOfZero(t *testing.T) {
	for _, count := range []int{0, -1, -100} {
		if got := str.Words("one two three", count, "..."); got != "one two three" {
			t.Errorf("Words(%q, %d, %q) = %q, want the subject", "one two three", count, "...", got)
		}
	}
	if got := str.Words("one two three", 2, "..."); got != "one two..." {
		t.Errorf("Words(%q, 2, %q) = %q, want %q", "one two three", "...", got, "one two...")
	}
	if got := str.Words("one two", 5, "..."); got != "one two" {
		t.Errorf("Words(%q, 5, %q) = %q, want the subject", "one two", "...", got)
	}
}

// TestMaskWithANegativeIndex walks the corners of Str::mask: an index counted
// from the end, one past either end, a length of zero and an empty mask
// character.
func TestMaskWithANegativeIndex(t *testing.T) {
	cases := []struct {
		value     string
		character string
		index     int
		length    []int
		want      string
	}{
		{"taylor@example.com", "*", 3, nil, "tay***************"},
		{"taylor@example.com", "*", 3, []int{8}, "tay********ple.com"},
		{"taylor@example.com", "*", -15, []int{3}, "tay***@example.com"},
		{"taylor", "*", -3, nil, "tay***"},
		{"taylor", "*", -100, nil, "******"},
		{"taylor", "*", 100, nil, "taylor"},
		{"taylor", "*", 2, []int{0}, "taylor"},
		{"taylor", "", 2, nil, "taylor"},
		{"", "*", 0, nil, ""},
	}
	for _, c := range cases {
		if got := str.Mask(c.value, c.character, c.index, c.length...); got != c.want {
			t.Errorf("Mask(%q, %q, %d, %v) = %q, want %q", c.value, c.character, c.index, c.length, got, c.want)
		}
	}
}

// TestPluralWithACount pins the two guards Pluralizer::plural runs before it
// reaches the inflector: a count of one either way, and a value that does not
// end in a word character.
func TestPluralWithACount(t *testing.T) {
	cases := []struct {
		value string
		count []int
		want  string
	}{
		{"rule", nil, "rules"},
		{"rule", []int{1}, "rule"},
		{"rule", []int{-1}, "rule"},
		{"rule", []int{0}, "rules"},
		{"rule", []int{2}, "rules"},
		{"rule!", nil, "rule!"},
		{"rule ", nil, "rule "},
		{"recommended", nil, "recommended"},
		{"related", nil, "related"},
		{"child", nil, "children"},
		{"person", nil, "people"},
		{"mouse", nil, "mice"},
		{"sheep", nil, "sheep"},
	}
	for _, c := range cases {
		if got := str.Plural(c.value, c.count...); got != c.want {
			t.Errorf("Plural(%q, %v) = %q, want %q", c.value, c.count, got, c.want)
		}
	}
}

func TestPluralStudlyPluralizesTheLastWordOnly(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"VerifiedHuman", "VerifiedHumans"},
		{"UserFeedback", "UserFeedback"},
		{"HTTPServer", "HTTPServers"},
		{"Child", "Children"},
		{"user_group", "user_groups"},
	}
	for _, c := range cases {
		if got := str.PluralStudly(c.value); got != c.want {
			t.Errorf("PluralStudly(%q) = %q, want %q", c.value, got, c.want)
		}
	}
	if got := str.PluralStudly("VerifiedHuman", 1); got != "VerifiedHuman" {
		t.Errorf("PluralStudly(%q, 1) = %q, want the subject", "VerifiedHuman", got)
	}
	if got := str.PluralPascal("VerifiedHuman"); got != "VerifiedHumans" {
		t.Errorf("PluralPascal(%q) = %q, want %q", "VerifiedHuman", got, "VerifiedHumans")
	}
}

func TestCountedNamesTheCountAndAgreesWithIt(t *testing.T) {
	cases := []struct {
		value string
		count int
		want  string
	}{
		{"rule", 1, "1 rule"},
		{"rule", 0, "0 rules"},
		{"rule", 3, "3 rules"},
		{"row", 1000, "1,000 rows"},
		{"person", 2, "2 people"},
	}
	for _, c := range cases {
		if got := str.Counted(c.value, c.count); got != c.want {
			t.Errorf("Counted(%q, %d) = %q, want %q", c.value, c.count, got, c.want)
		}
	}
}

func TestUseLanguageAcceptsEnglishAndRefusesTheRest(t *testing.T) {
	if err := str.UseLanguage("english"); err != nil {
		t.Fatalf("UseLanguage(%q) returned %v", "english", err)
	}
	if got := str.Language(); got != "english" {
		t.Errorf("Language() = %q, want %q", got, "english")
	}
	if err := str.UseLanguage("portuguese"); err == nil {
		t.Errorf("UseLanguage(%q) returned no error", "portuguese")
	}
	if got := str.Language(); got != "english" {
		t.Errorf("Language() after a refused language = %q, want %q", got, "english")
	}
}

func TestTransliterate(t *testing.T) {
	cases := []struct {
		value   string
		unknown string
		want    string
	}{
		{"ÀÁÂÃÄÅ", "?", "AAAAAA"},
		{"Ação", "?", "Acao"},
		{"ⓐⓑⓒ", "?", "abc"},
		{"ＡＢＣ", "?", "ABC"},
		{"“quoted”", "?", `"quoted"`},
		{"🎂", "H", "H"},
		{"🎂", "", ""},
		{"plain", "?", "plain"},
		{"日本", "?", "??"},
	}
	for _, c := range cases {
		if got := str.Transliterate(c.value, c.unknown, false); got != c.want {
			t.Errorf("Transliterate(%q, %q, false) = %q, want %q", c.value, c.unknown, got, c.want)
		}
	}
}

func TestPassword(t *testing.T) {
	p := str.Password(32, true, true, true, false)
	if len(p) != 32 {
		t.Errorf("Password(32, ...) is %d characters long, want 32", len(p))
	}
	if !hasAnyOf(p, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("Password(%q) carries no letter", p)
	}
	if !hasAnyOf(p, "0123456789") {
		t.Errorf("Password(%q) carries no digit", p)
	}
	if hasAnyOf(p, " ") {
		t.Errorf("Password(%q) carries a space it was not asked for", p)
	}

	// Every set that is on contributes one character up front, so a length
	// under the number of sets still comes back with one of each.
	if got := str.Password(1, true, true, true, false); len(got) != 3 {
		t.Errorf("Password(1, true, true, true, false) is %d characters long, want 3", len(got))
	}
	if got := str.Password(8, false, false, false, false); got != "" {
		t.Errorf("Password with every set off = %q, want the empty string", got)
	}
	if got := str.Password(0, false, true, false, false); got != "" && !hasAnyOf(got, "0123456789") {
		t.Errorf("Password(0, false, true, false, false) = %q, want a digit", got)
	}
	if str.Password(16, true, true, true, false) == str.Password(16, true, true, true, false) {
		t.Error("two passwords came out the same")
	}
}

func hasAnyOf(s, set string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(set); j++ {
			if s[i] == set[j] {
				return true
			}
		}
	}
	return false
}
