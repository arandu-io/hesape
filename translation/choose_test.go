package translation_test

import (
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// segments builds a translator whose only line is the one under test, so a
// failure names the line and not the fixture.
func segments(line string, locale string) *translation.Translator {
	return translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		locale: {"messages": {"line": line}},
	}), locale, locale)
}

func TestChoiceReadsAnExplicitCount(t *testing.T) {
	tr := segments("{0} There is nothing|{1} There is one|[2,*] There are :count", "en")

	for count, want := range map[int]string{
		0: "There is nothing",
		1: "There is one",
		2: "There are 2",
		9: "There are 9",
	} {
		if got := tr.Choice("en", "messages.line", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChoiceReadsARange(t *testing.T) {
	tr := segments("[0,1] a few|[2,19] some|[20,*] many", "en")

	for count, want := range map[int]string{
		0: "a few", 1: "a few", 2: "some", 19: "some", 20: "many", 200: "many",
	} {
		if got := tr.Choice("en", "messages.line", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChoiceReadsAnOpenLowerBound(t *testing.T) {
	tr := segments("[*,9] few|[10,*] many", "en")

	if got, want := tr.Choice("en", "messages.line", 3, nil), "few"; got != want {
		t.Errorf("Choice(3) = %q, want %q", got, want)
	}
	if got, want := tr.Choice("en", "messages.line", 30, nil), "many"; got != want {
		t.Errorf("Choice(30) = %q, want %q", got, want)
	}
}

// The first condition that matches wins, even when a later one would match too.
func TestChoiceTakesTheFirstConditionThatMatches(t *testing.T) {
	tr := segments("{1} exactly one|[1,*] one or more", "en")

	if got, want := tr.Choice("en", "messages.line", 1, nil), "exactly one"; got != want {
		t.Errorf("Choice(1) = %q, want %q", got, want)
	}
}

// With no condition matching, the conditions are stripped and the plural rule
// of the locale indexes what is left -- a conditional segment keeps its
// position in that count, which is why a line mixes the two forms at the
// author's peril: here the singular of English is the segment written for zero.
func TestChoiceIndexesTheStrippedSegmentsByPosition(t *testing.T) {
	tr := segments("{0} none|one|:count", "en")

	for count, want := range map[int]string{0: "none", 1: "none", 5: "one"} {
		if got := tr.Choice("en", "messages.line", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChoiceFallsThroughToThePluralRule(t *testing.T) {
	tr := segments("one apple|:count apples", "en")

	if got, want := tr.Choice("en", "messages.line", 1, nil), "one apple"; got != want {
		t.Errorf("Choice(1) = %q, want %q", got, want)
	}
	if got, want := tr.Choice("en", "messages.line", 5, nil), "5 apples"; got != want {
		t.Errorf("Choice(5) = %q, want %q", got, want)
	}
}

func TestChoiceIndexesThreeFormsInRussian(t *testing.T) {
	tr := segments("яблоко|яблока|яблок", "ru")

	for count, want := range map[int]string{1: "яблоко", 2: "яблока", 5: "яблок", 21: "яблоко"} {
		if got := tr.Choice("ru", "messages.line", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}

// A catalogue translated only for the singular must not index past its own
// line: the first segment is the one that always exists.
func TestChoiceFallsBackToTheFirstSegment(t *testing.T) {
	tr := segments("одно яблоко", "ru")

	if got, want := tr.Choice("ru", "messages.line", 5, nil), "одно яблоко"; got != want {
		t.Errorf("Choice(5) = %q, want %q", got, want)
	}
}

func TestChoiceTrimsEverySegment(t *testing.T) {
	tr := segments("{1} one | :count many", "en")

	if got, want := tr.Choice("en", "messages.line", 1, nil), "one"; got != want {
		t.Errorf("Choice(1) = %q, want %q", got, want)
	}
	if got, want := tr.Choice("en", "messages.line", 4, nil), "4 many"; got != want {
		t.Errorf("Choice(4) = %q, want %q", got, want)
	}
}

// A line with no segments at all is the ordinary case: one sentence, whatever
// the count.
func TestChoiceOnALineWithNoSegments(t *testing.T) {
	tr := segments("There are :count of them", "en")

	if got, want := tr.Choice("en", "messages.line", 3, nil), "There are 3 of them"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}

// A bracket that opens a sentence rather than a condition is text, not a
// malformed rule to swallow.
func TestChoiceLeavesTextThatMerelyStartsWithABracket(t *testing.T) {
	tr := segments("[draft] not published", "en")

	if got, want := tr.Choice("en", "messages.line", 1, nil), "[draft] not published"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}
