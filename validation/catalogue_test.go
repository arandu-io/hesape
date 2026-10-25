package validation

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// illuminateLines are the keys of Illuminate's lang/en/validation.php. A key it
// adds is a line to add here, and then to both catalogues.
var illuminateLines = []string{
	"accepted", "accepted_if", "active_url", "after", "after_or_equal", "alpha", "alpha_dash",
	"alpha_num", "any_of", "array", "array_keys", "ascii", "base64", "before", "before_or_equal",
	"between", "boolean", "can", "confirmed", "contains", "current_password", "date", "date_equals",
	"date_format", "decimal", "declined", "declined_if", "different", "digits", "digits_between",
	"dimensions", "distinct", "doesnt_contain", "doesnt_end_with", "doesnt_start_with", "email",
	"encoding", "ends_with", "enum", "exists", "extensions", "file", "filled", "gt", "gte",
	"hex_color", "image", "in", "in_array", "in_array_keys", "integer", "ip", "ipv4", "ipv6", "json",
	"list", "lowercase", "lt", "lte", "mac_address", "max", "max_digits", "mimes", "mimetypes", "min",
	"min_digits", "missing", "missing_if", "missing_unless", "missing_with", "missing_with_all",
	"multiple_of", "not_in", "not_regex", "numeric", "password", "present", "present_if",
	"present_unless", "present_with", "present_with_all", "prohibited", "prohibited_if",
	"prohibited_if_accepted", "prohibited_if_declined", "prohibited_unless", "prohibits", "regex",
	"required", "required_array_keys", "required_if", "required_if_accepted", "required_if_declined",
	"required_unless", "required_with", "required_with_all", "required_without",
	"required_without_all", "same", "size", "starts_with", "string", "timezone", "ulid", "unique",
	"uploaded", "uppercase", "url", "uuid",
}

var placeholderPattern = regexp.MustCompile(":[A-Za-z_]+")

func placeholdersOf(sentence string) []string {
	found := placeholderPattern.FindAllString(sentence, -1)
	slices.Sort(found)
	return slices.Compact(found)
}

// TestTheCatalogueSaysEveryRuleInPortugueseAndEnglish. A rule with a sentence in
// Illuminate has a sentence in both languages, with the same placeholders, and
// the Portuguese one is Portuguese.
func TestTheCatalogueSaysEveryRuleInPortugueseAndEnglish(t *testing.T) {
	english := translation.Bundled("en", "validation")
	portuguese := translation.Bundled("pt-BR", "validation")

	for _, key := range illuminateLines {
		inEnglish, hasEnglish := lineOf(english, key)
		inPortuguese, hasPortuguese := lineOf(portuguese, key)
		if !hasEnglish || !hasPortuguese {
			t.Errorf("%s: english %v, portuguese %v", key, hasEnglish, hasPortuguese)
			continue
		}
		for item, sentence := range inEnglish {
			other, ok := inPortuguese[item]
			if !ok {
				t.Errorf("%s%s has no Portuguese sentence", key, item)
				continue
			}
			if !slices.Equal(placeholdersOf(sentence), placeholdersOf(other)) {
				t.Errorf("%s%s: placeholders %v in English, %v in Portuguese", key, item,
					placeholdersOf(sentence), placeholdersOf(other))
			}
			if sentence == other {
				t.Errorf("%s%s: the Portuguese sentence is the English one", key, item)
			}
			if !strings.Contains(other, ":attribute") && key != "current_password" {
				t.Errorf("%s%s: %q does not name the field", key, item, other)
			}
		}
	}
}

// lineOf reads a key of the catalogue as its items: itself, or the items of a
// group like min.string.
func lineOf(lines translation.Lines, key string) (map[string]string, bool) {
	if sentence, ok := lines[key]; ok {
		return map[string]string{"": sentence}, true
	}
	out := map[string]string{}
	for item, sentence := range lines {
		if rest, under := strings.CutPrefix(item, key+"."); under {
			out["."+rest] = sentence
		}
	}
	return out, len(out) > 0
}

// TestEveryRuleOfTheCatalogueHasALineInBothLanguages. The set of rules this
// package has and the set of lines the catalogue has do not drift apart.
func TestEveryRuleOfTheCatalogueHasALineInBothLanguages(t *testing.T) {
	markers := []string{"bail", "sometimes", "nullable", "exclude", "exclude_if", "exclude_unless", "exclude_with", "exclude_without"}
	for _, locale := range []string{"en", "pt-BR"} {
		lines := translation.Bundled(locale, "validation")
		for name := range specs {
			if slices.Contains(markers, name) {
				continue
			}
			if _, ok := lineOf(lines, name); !ok {
				t.Errorf("%s: the rule %q has no line", locale, name)
			}
		}
		for _, name := range []string{"invalid", "uploaded"} {
			if _, ok := lineOf(lines, name); !ok {
				t.Errorf("%s: the line %q is missing", locale, name)
			}
		}
	}
}
