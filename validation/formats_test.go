package validation

import (
	"slices"
	"strings"
	"testing"
)

// lines is a Translator over a map, which is what a test needs and what an
// application's catalogue is.
type lines map[string]any

func (l lines) Get(key string, replace map[string]any, locale string) any {
	if value, held := l[key]; held {
		return value
	}
	return key
}

func (l lines) Choice(key string, number int, replace map[string]any, locale string) string {
	return line(l.Get(key, replace, locale), key)
}

// english is the catalogue this package carries, which is what a Validator gets
// the moment one is wired at all.
var english = englishTranslator{}

func TestDisplayableAttributeIsSnakeCaseOpenedOutAndLowercase(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"password_confirmation": "required"}))

	// The last line of getDisplayableAttribute is
	// str_replace('_', ' ', Str::snake($attribute)). It is NOT Str::headline:
	// the name sits inside the sentence, so it is never capitalised.
	if got := v.GetDisplayableAttribute("password_confirmation"); got != "password confirmation" {
		t.Fatalf("got %q, want %q", got, "password confirmation")
	}
	if got := v.GetDisplayableAttribute("firstName"); got != "first name" {
		t.Fatalf("got %q, want %q", got, "first name")
	}
}

func TestACustomAttributeNameIsUsedOverTheDefault(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"email": "required"}),
		WithCustomAttributes(map[string]string{"email": "e-mail address"}))

	if got := v.GetDisplayableAttribute("email"); got != "e-mail address" {
		t.Fatalf("got %q", got)
	}
}

func TestAttributeNamesComeFromTheTranslatorWhenNothingInlineNamesThem(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"email": "required"}), WithTranslator(lines{
		"validation.attributes": map[string]any{"email": "address"},
	}))

	if got := v.GetDisplayableAttribute("email"); got != "address" {
		t.Fatalf("got %q", got)
	}
}

func TestATranslatorTurnsTheShortSentenceIntoAFullOne(t *testing.T) {
	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}), WithTranslator(english))

	if got := v.Errors().First("name"); got != "The name field is required." {
		t.Fatalf("got %q", got)
	}
}

func TestWithNoTranslatorTheCompiledSentenceIsKept(t *testing.T) {
	// Wiring no catalogue must change nothing: this is what the package said
	// before FormatsMessages existed, and a field is drawn with it.
	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}))

	if got := v.Errors().First("name"); got != "is required" {
		t.Fatalf("got %q", got)
	}
}

func TestAnInlineMessageBeatsEverything(t *testing.T) {
	v := Make(Data{"email": ""}, MustCompile(Rules{"email": "required"}),
		WithTranslator(english),
		WithCustomMessages(map[string]any{
			"email.required": "we need an address to send the receipt to",
		}))

	if got := v.Errors().First("email"); got != "we need an address to send the receipt to" {
		t.Fatalf("got %q", got)
	}
}

func TestAnInlineMessageKeyedOnTheRuleAloneCoversEveryField(t *testing.T) {
	v := Make(Data{"a": "", "b": ""}, MustCompile(Rules{"a": "required", "b": "required"}),
		WithCustomMessages(map[string]any{"required": "please fill :attribute in"}))

	if got := v.Errors().First("a"); got != "please fill a in" {
		t.Fatalf("a: got %q", got)
	}
	if got := v.Errors().First("b"); got != "please fill b in" {
		t.Fatalf("b: got %q", got)
	}
}

func TestAnInlineMessageKeyMayCarryAStar(t *testing.T) {
	v := Make(Data{"user_email": ""}, MustCompile(Rules{"user_email": "required"}),
		WithCustomMessages(map[string]any{"user_*.required": "matched by pattern"}))

	if got := v.Errors().First("user_email"); got != "matched by pattern" {
		t.Fatalf("got %q", got)
	}
}

func TestTheCustomKeyOfTheTranslatorIsRead(t *testing.T) {
	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}), WithTranslator(lines{
		"validation.custom.name.required": "your name, please",
	}))

	if got := v.Errors().First("name"); got != "your name, please" {
		t.Fatalf("got %q", got)
	}
}

func TestASizeRuleReadsTheVariantOfWhatItMeasures(t *testing.T) {
	// A string counts characters and a number bounds the value, and the two say
	// different things. getSizeMessage is what picks between them.
	text := Make(Data{"name": "ab"}, MustCompile(Rules{"name": "min:5"}), WithTranslator(english))
	if got := text.Errors().First("name"); got != "The name field must be at least 5 characters." {
		t.Fatalf("string: got %q", got)
	}

	number := Make(Data{"age": "2"}, MustCompile(Rules{"age": "integer|min:5"}), WithTranslator(english))
	if got := number.Errors().First("age"); got != "The age field must be at least 5." {
		t.Fatalf("numeric: got %q", got)
	}
}

func TestTheThreeSpellingsOfTheAttributePlaceholder(t *testing.T) {
	v := Make(Data{"first_name": ""}, MustCompile(Rules{"first_name": "required"}))

	got := v.MakeReplacements(":attribute / :ATTRIBUTE / :Attribute", "first_name", "required", nil)

	if got != "first name / FIRST NAME / First name" {
		t.Fatalf("got %q", got)
	}
}

func TestTheInputPlaceholderIsWhatWasActuallySent(t *testing.T) {
	v := Make(Data{"role": "wizard"}, MustCompile(Rules{"role": "in:admin,editor"}))

	got := v.MakeReplacements(":input is not allowed", "role", "in", []string{"admin", "editor"})

	if got != "wizard is not allowed" {
		t.Fatalf("got %q", got)
	}
}

func TestACustomValueNameIsUsedInsideTheSentence(t *testing.T) {
	v := Make(Data{"payment": "cc"}, MustCompile(Rules{"payment": "in:paypal"}),
		WithCustomValues(map[string]map[string]string{"payment": {"cc": "credit card"}}))

	if got := v.GetDisplayableValue("payment", "cc"); got != "credit card" {
		t.Fatalf("got %q", got)
	}
}

func TestIndexAndPositionPlaceholders(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"name": "required"}))

	got := v.MakeReplacements("row :index / :position", "items.3.name", "required", nil)

	if got != "row 3 / 4" {
		t.Fatalf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Concerns\ReplacesAttributes.
// ---------------------------------------------------------------------------

func TestTheReplacersFillEachRulesOwnPlaceholders(t *testing.T) {
	set := MustCompile(Rules{
		"low":   "numeric",
		"high":  "numeric",
		"name":  "required",
		"other": "required",
	})
	v := Make(Data{"low": "1", "high": "9", "other": "yes"}, set)

	cases := []struct {
		rule       string
		message    string
		parameters []string
		want       string
	}{
		{"between", "between :min and :max", []string{"2", "8"}, "between 2 and 8"},
		{"min", "at least :min", []string{"5"}, "at least 5"},
		{"max", "at most :max", []string{"5"}, "at most 5"},
		{"size", "exactly :size", []string{"5"}, "exactly 5"},
		{"digits", ":digits digits", []string{"4"}, "4 digits"},
		{"decimal", ":decimal places", []string{"2", "4"}, "2-4 places"},
		{"date_format", "format :format", []string{"2006-01-02"}, "format 2006-01-02"},
		{"same", "must match :other", []string{"other"}, "must match other"},
		{"different", "differ from :other", []string{"other"}, "differ from other"},
		{"in", "one of :values", []string{"a", "b"}, "one of a, b"},
		{"starts_with", "start with :values", []string{"x", "y"}, "start with x, y"},
		{"required_with", "needed with :values", []string{"low", "high"}, "needed with low / high"},
		{"required_if", "needed when :other is :value", []string{"other", "yes"}, "needed when other is yes"},
		{"required_unless", "unless :other is :values", []string{"other", "no", "maybe"}, "unless other is no, maybe"},
		{"after", "after :date", []string{"2026-01-01"}, "after 2026-01-01"},
		{"before", "before :date", []string{"low"}, "before low"},
		{"dimensions", "at least :min_width wide", []string{"min_width=100"}, "at least 100 wide"},
		{"multiple_of", "a multiple of :value", []string{"3"}, "a multiple of 3"},
		{"mimes", "one of :values", []string{"png", "jpg"}, "one of png, jpg"},
	}

	for _, c := range cases {
		if got := v.MakeReplacements(c.message, "name", c.rule, c.parameters); got != c.want {
			t.Errorf("%s: got %q, want %q", c.rule, got, c.want)
		}
	}
}

func TestAComparisonMeasuresTheOtherFieldTheWayThisOneIsMeasured(t *testing.T) {
	// getSize is asked about THIS attribute, so a comparison on a numeric field
	// reads the other field's value and one on a string field counts its
	// characters.
	set := MustCompile(Rules{"low": "numeric", "high": "numeric", "name": "required"})
	v := Make(Data{"low": "1", "high": "9"}, set)

	if got := v.MakeReplacements("at most :value", "low", "lte", []string{"high"}); got != "at most 9" {
		t.Fatalf("numeric: got %q", got)
	}
	if got := v.MakeReplacements("at most :value", "name", "lte", []string{"high"}); got != "at most 1" {
		t.Fatalf("string: got %q", got)
	}
}

func TestAComparisonNamesTheOtherFieldWhenItWasNotSent(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"low": "numeric", "high": "numeric"}))

	got := v.MakeReplacements("greater than :value", "high", "gt", []string{"low"})

	if got != "greater than low" {
		t.Fatalf("got %q", got)
	}
}

func TestARegisteredReplacerWinsOverTheBuiltInOne(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"name": "required"}))
	v.AddReplacer("min", func(message, attribute, rule string, parameters []string, validator *Validator) string {
		return "replaced"
	})

	if got := v.MakeReplacements("at least :min", "name", "min", []string{"5"}); got != "replaced" {
		t.Fatalf("got %q", got)
	}
}

func TestTheWholeMessagePipelineEndToEnd(t *testing.T) {
	v := Make(
		Data{"email": "", "password": "abc"},
		MustCompile(Rules{"email": "required", "password": "min:12"}),
		WithTranslator(english),
		WithCustomAttributes(map[string]string{"email": "e-mail"}),
	)

	if got := v.Errors().First("email"); got != "The e-mail field is required." {
		t.Fatalf("email: got %q", got)
	}
	if got := v.Errors().First("password"); got != "The password field must be at least 12 characters." {
		t.Fatalf("password: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// The English catalogue.
// ---------------------------------------------------------------------------

// Every key the message pipeline builds must resolve to a sentence in the
// shipped catalogue. A key that resolves to nothing is not a quiet fallback: the
// translator answers with the key itself, and "validation.min.string" is what
// then prints on the page where the sentence belonged.
//
// It asks through the translator rather than indexing the table, so it fails the
// same way whether a line went missing from the catalogue, a group stopped being
// a group, or the lookup that reads it broke.
func TestEveryKeyTheMessagePipelineBuildsResolvesToASentence(t *testing.T) {
	// The flow markers say nothing when they fail -- bail and sometimes cannot
	// fail at all, nullable only changes what counts as absent, and the exclude
	// family removes the field instead of putting a message on it. The
	// catalogue carries no line for any of them either.
	silent := append([]string{"bail", "sometimes", "nullable"}, excludeRules...)

	// getMessage builds "validation.<rule>" for every rule, and getSizeMessage
	// builds "validation.<rule>.<type>" for the eight whose sentence depends on
	// what is being measured -- the four getAttributeType can answer.
	var keys []string
	for name := range specs {
		if slices.Contains(silent, name) {
			continue
		}
		if slices.Contains(sizeRules, name) {
			for _, measured := range []string{"numeric", "file", "array", "string"} {
				keys = append(keys, "validation."+name+"."+measured)
			}
			continue
		}
		keys = append(keys, "validation."+name)
	}
	// The password rule reports which requirement failed, and each segment is a
	// key of its own that no rule name spells.
	for _, segment := range []string{"letters", "mixed", "numbers", "symbols", "uncompromised"} {
		keys = append(keys, "validation.password."+segment)
	}

	slices.Sort(keys)
	for _, key := range keys {
		got := line(english.Get(key, nil, ""), key)
		if got == key {
			t.Errorf("%s has no line in the shipped English catalogue: it would print as the key", key)
		}
	}
}

// A size rule is asked for its whole group as well, in the branch that reads a
// custom message written as one, so each of the eight must answer with a group
// and not with a sentence.
func TestEverySizeRuleAnswersWithItsGroup(t *testing.T) {
	for _, name := range sizeRules {
		key := "validation." + name
		group, isGroup := english.Get(key, nil, "").(map[string]string)
		if !isGroup {
			t.Errorf("%s is not a group in the shipped English catalogue", key)
			continue
		}
		if len(group) != 4 {
			t.Errorf("%s has %d lines, want the four an attribute can measure", key, len(group))
		}
	}
}

func TestTheEnglishTranslatorAnswersWithTheKeyForWhatItDoesNotHold(t *testing.T) {
	if got := line(english.Get("validation.nope", nil, ""), "validation.nope"); got != "validation.nope" {
		t.Fatalf("got %q", got)
	}
	if got := line(english.Get("validation.min.string", nil, ""), ""); !strings.Contains(got, ":min") {
		t.Fatalf("got %q", got)
	}
}
