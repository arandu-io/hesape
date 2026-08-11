package translation_test

import (
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// The families, one count per form. A wrong index here is a sentence that
// disagrees with its own number, in a language the author does not read.
func TestGetPluralIndexPicksTheFormOfTheLanguage(t *testing.T) {
	for _, c := range []struct {
		locale string
		counts map[int]int
	}{
		{"ja", map[int]int{0: 0, 1: 0, 2: 0, 11: 0}},
		{"en", map[int]int{0: 1, 1: 0, 2: 1, 21: 1}},
		{"fr", map[int]int{0: 0, 1: 0, 2: 1, 21: 1}},
		{"mk", map[int]int{0: 1, 1: 0, 2: 1, 21: 0}},
		{"ru", map[int]int{0: 2, 1: 0, 2: 1, 5: 2, 11: 2, 21: 0, 22: 1, 25: 2}},
		{"cs", map[int]int{0: 2, 1: 0, 2: 1, 4: 1, 5: 2}},
		{"ga", map[int]int{0: 2, 1: 0, 2: 1, 3: 2}},
		{"lt", map[int]int{0: 2, 1: 0, 2: 1, 11: 2, 21: 0, 22: 1}},
		{"sl", map[int]int{0: 3, 1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 101: 0, 102: 1}},
		{"mt", map[int]int{0: 1, 1: 0, 2: 1, 10: 1, 11: 2, 19: 2, 20: 3}},
		{"lv", map[int]int{0: 0, 1: 1, 2: 2, 11: 2, 21: 1}},
		{"pl", map[int]int{0: 2, 1: 0, 2: 1, 5: 2, 12: 2, 22: 1}},
		{"cy", map[int]int{0: 3, 1: 0, 2: 1, 8: 2, 11: 2, 12: 3}},
		{"ro", map[int]int{0: 1, 1: 0, 2: 1, 19: 1, 20: 2, 101: 1}},
		{"ar", map[int]int{0: 0, 1: 1, 2: 2, 3: 3, 10: 3, 11: 4, 99: 4, 100: 5}},
	} {
		for count, want := range c.counts {
			if got := (translation.MessageSelector{}).GetPluralIndex(c.locale, count); got != want {
				t.Errorf("GetPluralIndex(%q, %d) = %d, want %d", c.locale, count, got, want)
			}
		}
	}
}

// The rule belongs to the language, and the region is only a spelling of it.
func TestGetPluralIndexIgnoresTheRegionAndTheSeparator(t *testing.T) {
	for _, locale := range []string{"pt", "pt-BR", "pt_BR", "PT-br", "pt-PT"} {
		if got := (translation.MessageSelector{}).GetPluralIndex(locale, 2); got != 1 {
			t.Errorf("GetPluralIndex(%q, 2) = %d, want 1", locale, got)
		}
	}
}

func TestGetPluralIndexReadsACountByMagnitude(t *testing.T) {
	selector := translation.MessageSelector{}
	if got, want := selector.GetPluralIndex("ru", -22), selector.GetPluralIndex("ru", 22); got != want {
		t.Errorf("GetPluralIndex(ru, -22) = %d, want %d", got, want)
	}
}

// A language nobody wrote a rule for has to answer something, and the first
// segment is the only one a catalogue is guaranteed to carry.
func TestGetPluralIndexAnswersZeroForAnUnknownLanguage(t *testing.T) {
	for _, locale := range []string{"", "xx", "klingon"} {
		if got := (translation.MessageSelector{}).GetPluralIndex(locale, 7); got != 0 {
			t.Errorf("GetPluralIndex(%q, 7) = %d, want 0", locale, got)
		}
	}
}
