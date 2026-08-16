package translation_test

import (
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// choose is the selector under test. It is the value Translator.Choice goes
// through, and it holds nothing, so one is enough for the file.
var choose = translation.MessageSelector{}.Choose

func TestChooseReadsAnExplicitCount(t *testing.T) {
	line := "{0} There is nothing|{1} There is one|[2,*] There are :count"

	for count, want := range map[int]string{
		0: "There is nothing",
		1: "There is one",
		2: "There are :count",
		9: "There are :count",
	} {
		if got := choose(line, count, "en"); got != want {
			t.Errorf("Choose(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChooseReadsARange(t *testing.T) {
	line := "[0,1] a few|[2,19] some|[20,*] many"

	for count, want := range map[int]string{
		0: "a few", 1: "a few", 2: "some", 19: "some", 20: "many", 200: "many",
	} {
		if got := choose(line, count, "en"); got != want {
			t.Errorf("Choose(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChooseReadsAnOpenLowerBound(t *testing.T) {
	line := "[*,9] few|[10,*] many"

	if got, want := choose(line, 3, "en"), "few"; got != want {
		t.Errorf("Choose(3) = %q, want %q", got, want)
	}
	if got, want := choose(line, 30, "en"), "many"; got != want {
		t.Errorf("Choose(30) = %q, want %q", got, want)
	}
}

// The first condition that matches wins, even when a later one would match too.
func TestChooseTakesTheFirstConditionThatMatches(t *testing.T) {
	if got, want := choose("{1} exactly one|[1,*] one or more", 1, "en"), "exactly one"; got != want {
		t.Errorf("Choose(1) = %q, want %q", got, want)
	}
}

// With no condition matching, the conditions are stripped and the plural rule
// of the locale indexes what is left -- a conditional segment keeps its
// position in that count, which is why a line mixes the two forms at the
// author's peril: here the singular of English is the segment written for zero.
func TestChooseIndexesTheStrippedSegmentsByPosition(t *testing.T) {
	line := "{0} none|one|:count"

	for count, want := range map[int]string{0: "none", 1: " none", 5: "one"} {
		if got := choose(line, count, "en"); got != want {
			t.Errorf("Choose(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChooseFallsThroughToThePluralRule(t *testing.T) {
	line := "one apple|:count apples"

	if got, want := choose(line, 1, "en"), "one apple"; got != want {
		t.Errorf("Choose(1) = %q, want %q", got, want)
	}
	if got, want := choose(line, 5, "en"), ":count apples"; got != want {
		t.Errorf("Choose(5) = %q, want %q", got, want)
	}
}

func TestChooseIndexesThreeFormsInRussian(t *testing.T) {
	line := "яблоко|яблока|яблок"

	for count, want := range map[int]string{1: "яблоко", 2: "яблока", 5: "яблок", 21: "яблоко"} {
		if got := choose(line, count, "ru"); got != want {
			t.Errorf("Choose(%d) = %q, want %q", count, got, want)
		}
	}
}

// A catalogue translated only for the singular must not index past its own
// line: the first segment is the one that always exists.
func TestChooseFallsBackToTheFirstSegment(t *testing.T) {
	if got, want := choose("одно яблоко", 5, "ru"), "одно яблоко"; got != want {
		t.Errorf("Choose(5) = %q, want %q", got, want)
	}
}

// Only the segment a condition selected is trimmed, and nothing else is, so a
// line spaced out either side of the bar renders the space it was written with
// once the plural rule is what chooses.
func TestChooseTrimsOnlyWhatAConditionSelected(t *testing.T) {
	line := "{1} one | :count many"

	if got, want := choose(line, 1, "en"), "one"; got != want {
		t.Errorf("Choose(1) = %q, want %q", got, want)
	}
	if got, want := choose(line, 4, "en"), " :count many"; got != want {
		t.Errorf("Choose(4) = %q, want %q", got, want)
	}
}

// A line with no segments at all is the ordinary case: one sentence, whatever
// the count.
func TestChooseOnALineWithNoSegments(t *testing.T) {
	if got, want := choose("There are :count of them", 3, "en"), "There are :count of them"; got != want {
		t.Errorf("Choose = %q, want %q", got, want)
	}
}

// A line that opens with a bracket loses it, whatever is inside: stripConditions
// removes anything matching ^[{[]...[}\]] before the plural rule indexes the
// segments, and "draft" is not a number, so no condition claimed it first. It is
// why a sentence that begins with a bracket is written with a leading space or
// not at all.
func TestChooseStripsABracketThatOpensASentence(t *testing.T) {
	if got, want := choose("[draft] not published", 1, "en"), " not published"; got != want {
		t.Errorf("Choose = %q, want %q", got, want)
	}
}

// A bracket the stripper does not match survives, because the body of a
// condition may hold no further bracket.
func TestChooseKeepsABracketThatIsNotAWellFormedCondition(t *testing.T) {
	if got, want := choose("[draft [2]] not published", 1, "en"), "[draft [2]] not published"; got != want {
		t.Errorf("Choose = %q, want %q", got, want)
	}
}

// Through the translator, the same line comes back with :count filled and the
// segment chosen -- which is the whole of Choice.
func TestChoiceThroughTheTranslatorFillsTheCount(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{
		"line": "{0} There is nothing|{1} There is one|[2,*] There are :count",
	}, "")
	tr := translation.New(l, "en", "en")

	for count, want := range map[int]string{
		0: "There is nothing",
		1: "There is one",
		9: "There are 9",
	} {
		if got := tr.Choice("en", "messages.line", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}
