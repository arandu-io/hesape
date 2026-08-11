package translation_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// catalogue is the fixture every test in this file translates against: two
// locales, one of which is deliberately incomplete.
func catalogue() *translation.ArrayLoader {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{
		"welcome":     "Welcome, :name.",
		"apples":      "one apple|:count apples",
		"invoice.due": "The invoice for :customer is due on :date.",
	}, "")
	l.AddMessages("pt-BR", "messages", translation.Lines{
		"welcome": "Bem-vindo, :name.",
		"apples":  "uma maçã|:count maçãs",
	}, "")
	return l
}

func newTranslator(t *testing.T) *translation.Translator {
	t.Helper()
	return translation.New(catalogue(), "en", "en")
}

func TestGetReadsTheGroupAndTheItem(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"name": "Ana"}), "Welcome, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if got, want := tr.Get("pt-BR", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("Get in pt-BR = %q, want %q", got, want)
	}
}

func TestGetReadsADottedItemAsOneItem(t *testing.T) {
	tr := newTranslator(t)

	got := tr.Get("en", "messages.invoice.due", translation.Replace{"customer": "Ana", "date": "the 9th"})
	if want := "The invoice for Ana is due on the 9th."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestGetFallsBackWhenTheLocaleDoesNotCarryTheKey(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	got := tr.Get("pt-BR", "messages.invoice.due", translation.Replace{"customer": "Ana", "date": "the 9th"})
	if want := "The invoice for Ana is due on the 9th."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestGetUsesTheDefaultLocaleWhenGivenNone(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	if got, want := tr.Get("", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if got, want := tr.GetLocale(), "pt-BR"; got != want {
		t.Errorf("GetLocale = %q, want %q", got, want)
	}
	if got, want := tr.Locale(), "pt-BR"; got != want {
		t.Errorf("Locale = %q, want %q", got, want)
	}
	if got, want := tr.GetFallback(), "en"; got != want {
		t.Errorf("GetFallback = %q, want %q", got, want)
	}
}

func TestSetLocaleAndSetFallbackMoveTheDefaults(t *testing.T) {
	tr := newTranslator(t)

	if err := tr.SetLocale("pt-BR"); err != nil {
		t.Fatalf("SetLocale: %v", err)
	}
	tr.SetFallback("en")

	if got, want := tr.Get("", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if got, want := tr.Get("", "messages.invoice.due", translation.Replace{"customer": "Ana", "date": "the 9th"}), "The invoice for Ana is due on the 9th."; got != want {
		t.Errorf("Get through the fallback = %q, want %q", got, want)
	}
}

// A locale reaches the filesystem as a directory name, so one carrying a path
// separator reads a catalogue somewhere else entirely.
func TestSetLocaleRefusesAPathSeparator(t *testing.T) {
	tr := newTranslator(t)

	for _, locale := range []string{"../secrets", `en\..`} {
		if err := tr.SetLocale(locale); err == nil {
			t.Errorf("SetLocale(%q) was accepted", locale)
		}
	}
	if got, want := tr.GetLocale(), "en"; got != want {
		t.Errorf("GetLocale = %q, want %q -- a refused locale was kept", got, want)
	}
}

// A key nobody carries has to be visible on the page. Returning a blank would
// make a wrong key look like a sentence somebody chose to leave empty.
func TestGetReturnsTheKeyItDidNotFind(t *testing.T) {
	var reported []string
	tr := newTranslator(t)
	tr.HandleMissingKeysUsing(func(key string, replace translation.Replace, locale string, fallback bool) string {
		reported = append(reported, locale+" "+key)
		return ""
	})

	if got, want := tr.Get("en", "messages.absent", nil), "messages.absent"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if len(reported) != 1 || reported[0] != "en messages.absent" {
		t.Errorf("HandleMissingKeysUsing saw %v, want [\"en messages.absent\"]", reported)
	}
}

// The callback may answer with a sentence of its own, which is what a project
// that reports the key to the reader rather than to a log wants.
func TestHandleMissingKeysUsingCanAnswerTheKey(t *testing.T) {
	tr := newTranslator(t)
	tr.HandleMissingKeysUsing(func(key string, replace translation.Replace, locale string, fallback bool) string {
		return "[" + key + "]"
	})

	if got, want := tr.Get("en", "messages.absent", nil), "[messages.absent]"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// A key with no dot names a group and no item, which is not a line.
func TestGetRefusesAKeyWithNoItem(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages", nil), "messages"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if tr.Has("en", "messages") {
		t.Error("Has(\"messages\") = true, want false")
	}
}

func TestHasLooksInTheFallbackToo(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	for key, want := range map[string]bool{
		"messages.welcome":     true,
		"messages.invoice.due": true,
		"messages.absent":      false,
	} {
		if got := tr.Has("pt-BR", key); got != want {
			t.Errorf("Has(%q) = %v, want %v", key, got, want)
		}
	}
}

// HasForLocale asks about one locale alone: the line exists in English and the
// Portuguese catalogue does not carry it.
func TestHasForLocaleDoesNotLookInTheFallback(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	if !tr.Has("pt-BR", "messages.invoice.due") {
		t.Error("Has = false, want true")
	}
	if tr.HasForLocale("pt-BR", "messages.invoice.due") {
		t.Error("HasForLocale = true, want false")
	}
	if !tr.HasForLocale("en", "messages.invoice.due") {
		t.Error("HasForLocale in English = false, want true")
	}
}

func TestHasIsSilentAboutMissingKeys(t *testing.T) {
	reported := 0
	tr := newTranslator(t)
	tr.HandleMissingKeysUsing(func(string, translation.Replace, string, bool) string {
		reported++
		return ""
	})

	tr.Has("en", "messages.absent")
	if reported != 0 {
		t.Errorf("the missing key callback ran %d times during Has, want 0", reported)
	}
}

func TestParseKeySplitsTheThreeParts(t *testing.T) {
	tr := newTranslator(t)

	for key, want := range map[string][3]string{
		"messages.welcome":      {"*", "messages", "welcome"},
		"validation.min.string": {"*", "validation", "min.string"},
		"shop::orders.title":    {"shop", "orders", "title"},
		"messages":              {"*", "messages", ""},
	} {
		namespace, group, item := tr.ParseKey(key)
		if got := [3]string{namespace, group, item}; got != want {
			t.Errorf("ParseKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestReplacementLeavesAPlaceholderItHasNoArgumentFor(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages.welcome", nil), "Welcome, :name."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"other": "x"}), "Welcome, :name."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// ":value" must not eat the beginning of ":values" when both are given: the
// longest name that matches wins, which is what PHP's strtr does.
func TestReplacementTakesTheLongestPlaceholderName(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{
		"pair":       ":value and :values",
		"punctuated": ":value, :value.",
	}, "")
	tr := translation.New(l, "en", "en")

	got := tr.Get("en", "messages.pair", translation.Replace{"value": "one", "values": "many"})
	if want := "one and many"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}

	got = tr.Get("en", "messages.punctuated", translation.Replace{"value": "one"})
	if want := "one, one."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// The three spellings PHP builds for every argument: the value, the value with
// its first letter uppercased, and the value uppercased.
func TestReplacementOffersTheUpperCaseForms(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{
		"forms": ":attribute, :Attribute, :ATTRIBUTE",
	}, "")
	tr := translation.New(l, "en", "en")

	got := tr.Get("en", "messages.forms", translation.Replace{"attribute": "email address"})
	if want := "email address, Email address, EMAIL ADDRESS"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// A closure argument names a pair of tags rather than a placeholder: the text
// between them is handed to the caller to wrap.
func TestReplacementRunsAClosureOverWhatItWraps(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{
		"terms": "Please read the <link>terms of service</link> first.",
	}, "")
	tr := translation.New(l, "en", "en")

	got := tr.Get("en", "messages.terms", translation.Replace{
		"link": func(inner string) string { return `<a href="/terms">` + inner + "</a>" },
	})
	if want := `Please read the <a href="/terms">terms of service</a> first.`; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestReplacementRendersWhatItIsGiven(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{"seconds": "Try again in :seconds seconds."}, "")
	tr := translation.New(l, "en", "en")

	if got, want := tr.Get("en", "messages.seconds", translation.Replace{"seconds": 60}), "Try again in 60 seconds."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

type money struct{ cents int }

// Stringable registers how one type renders when it is passed as an argument,
// which PHP names with a class name string and Go with a zero value.
func TestStringableRendersOneType(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{"total": "You owe :amount."}, "")
	tr := translation.New(l, "en", "en")

	tr.Stringable(money{}, func(value any) string {
		cents := value.(money).cents
		return fmt.Sprintf("R$ %d.%02d", cents/100, cents%100)
	})

	if got, want := tr.Get("en", "messages.total", translation.Replace{"amount": money{cents: 1250}}), "You owe R$ 12.50."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestChoiceCountsAndFillsTheCount(t *testing.T) {
	tr := newTranslator(t)

	for count, want := range map[int]string{
		0: "0 apples",
		1: "one apple",
		2: "2 apples",
	} {
		if got := tr.Choice("en", "messages.apples", count, nil); got != want {
			t.Errorf("Choice(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestChoiceKeepsAnExplicitCount(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Choice("en", "messages.apples", 2, translation.Replace{"count": "a few"}), "a few apples"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}

func TestChoiceDoesNotWriteIntoTheCallersArguments(t *testing.T) {
	tr := newTranslator(t)
	replace := translation.Replace{"name": "Ana"}

	tr.Choice("en", "messages.apples", 3, replace)
	if _, written := replace["count"]; written {
		t.Error("Choice wrote count into the map it was given")
	}
}

// The line only exists in the fallback, so the plural rule that applies is the
// fallback's -- picking the segment of a language the line is not written in
// would select the wrong one as soon as the two rules differ.
func TestChoicePluralizesInTheLocaleThatAnsweredTheKey(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "messages", translation.Lines{"apples": "one apple|:count apples"}, "")
	tr := translation.New(l, "ja", "en")

	if got, want := tr.Choice("ja", "messages.apples", 2, nil), "2 apples"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}

func TestChoiceOnAMissingKeyReturnsTheKey(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Choice("en", "messages.absent", 3, nil), "messages.absent"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}

// AddLines writes straight into the loaded groups, and a group written that way
// is never asked of the loader.
func TestAddLinesWritesAGroup(t *testing.T) {
	tr := newTranslator(t)

	tr.AddLines(map[string]string{
		"messages.welcome": "Hello again, :name.",
		"letters.opening":  "Dear :name,",
	}, "en", "")

	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"name": "Ana"}), "Hello again, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if got, want := tr.Get("en", "letters.opening", translation.Replace{"name": "Ana"}), "Dear Ana,"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestAddLinesReachesANamespace(t *testing.T) {
	tr := newTranslator(t)

	tr.AddLines(map[string]string{"orders.title": "Your orders"}, "en", "shop")

	if got, want := tr.Get("en", "shop::orders.title", nil), "Your orders"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	// A namespaced key is answered by that namespace or by nothing: an
	// application group of the same name must not shadow a module's own text.
	if got, want := tr.Get("en", "shop::messages.welcome", nil), "shop::messages.welcome"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestSetLoadedReplacesEverythingLoaded(t *testing.T) {
	tr := newTranslator(t)

	tr.SetLoaded(map[string]map[string]map[string]translation.Lines{
		"*": {"messages": {"en": {"welcome": "Loaded straight in, :name."}}},
	})

	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"name": "Ana"}), "Loaded straight in, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// The selector is the extension point for a language whose plural rule this
// package gets wrong.
func TestSetSelectorReplacesTheRule(t *testing.T) {
	tr := newTranslator(t)
	tr.SetSelector(firstSegment{})

	if got, want := tr.Choice("en", "messages.apples", 7, nil), "one apple"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
	if _, ok := tr.GetSelector().(firstSegment); !ok {
		t.Errorf("GetSelector = %T, want the selector that was set", tr.GetSelector())
	}
}

// firstSegment answers the singular whatever the count, which is what a
// language with one form needs.
type firstSegment struct{}

func (firstSegment) Choose(line string, number int, locale string) string {
	return strings.SplitN(line, "|", 2)[0]
}

func (firstSegment) GetPluralIndex(locale string, number int) int { return 0 }

// DetermineLocalesUsing decides which locales a key is looked for in, which is
// what a regional catalogue needs: pt-BR answers out of pt before it reaches
// the fallback.
func TestDetermineLocalesUsingWidensTheSearch(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("pt", "messages", translation.Lines{"welcome": "Bem-vindo, :name."}, "")
	tr := translation.New(l, "pt-BR", "en")

	if got, want := tr.Get("pt-BR", "messages.welcome", nil), "messages.welcome"; got != want {
		t.Errorf("Get = %q, want %q -- pt-BR should not read pt yet", got, want)
	}

	tr.DetermineLocalesUsing(func(locales []string) []string {
		return append([]string{locales[0], "pt"}, locales[1:]...)
	})

	if got, want := tr.Get("pt-BR", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestGetLoaderAnswersTheLoaderItWasBuiltWith(t *testing.T) {
	l := catalogue()
	tr := translation.New(l, "en", "en")

	if tr.GetLoader() != translation.Loader(l) {
		t.Error("GetLoader did not answer the loader New was given")
	}
}

func TestANilCatalogueStillTranslatesTheBundledLines(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	if got, want := tr.Get("en", "auth.failed", nil), "These credentials do not match our records."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// One translator answers every request at once. The race detector is the
// assertion.
func TestTheTranslatorIsSafeForConcurrentUse(t *testing.T) {
	tr := newTranslator(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				tr.Get("pt-BR", "messages.welcome", translation.Replace{"name": "Ana"})
				tr.Choice("en", "messages.apples", i, nil)
				tr.Has("en", "auth.failed")
				tr.GetLocale()
			}
		}()
	}
	wg.Wait()
}

func TestPotentiallyTranslatedStringTranslatesAKeyAndKeepsASentence(t *testing.T) {
	tr := newTranslator(t)

	key := translation.NewPotentiallyTranslatedString("messages.welcome", tr)
	key.Translate(translation.Replace{"name": "Ana"}, "en")
	if got, want := key.ToString(), "Welcome, Ana."; got != want {
		t.Errorf("ToString = %q, want %q", got, want)
	}
	if got, want := key.Original(), "messages.welcome"; got != want {
		t.Errorf("Original = %q, want %q", got, want)
	}

	sentence := translation.NewPotentiallyTranslatedString("The URL did not answer.", tr)
	if got, want := sentence.String(), "The URL did not answer."; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
	sentence.Translate(nil, "en")
	if got, want := sentence.ToString(), "The URL did not answer."; got != want {
		t.Errorf("ToString of an untranslated sentence = %q, want %q", got, want)
	}
}

func TestPotentiallyTranslatedStringTranslatesAChoice(t *testing.T) {
	tr := newTranslator(t)

	s := translation.NewPotentiallyTranslatedString("messages.apples", tr)
	s.TranslateChoice(3, nil, "en")

	if got, want := s.ToString(), "3 apples"; got != want {
		t.Errorf("ToString = %q, want %q", got, want)
	}
}
