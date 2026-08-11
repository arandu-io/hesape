package translation_test

import (
	"sync"
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// catalogue is the fixture every test in this file translates against: two
// locales, one of which is deliberately incomplete.
func catalogue() translation.Loader {
	return translation.NewMap(map[string]map[string]translation.Lines{
		"en": {
			"messages": {
				"welcome":     "Welcome, :name.",
				"apples":      "one apple|:count apples",
				"invoice.due": "The invoice for :customer is due on :date.",
			},
		},
		"pt-BR": {
			"messages": {
				"welcome": "Bem-vindo, :name.",
				"apples":  "uma maçã|:count maçãs",
			},
		},
	})
}

func newTranslator(t *testing.T, opts ...translation.Option) *translation.Translator {
	t.Helper()
	return translation.New(catalogue(), "en", "en", opts...)
}

func TestTReadsTheGroupAndTheItem(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"name": "Ana"}), "Welcome, Ana."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if got, want := tr.Get("pt-BR", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("T in pt-BR = %q, want %q", got, want)
	}
}

func TestTReadsADottedItemAsOneItem(t *testing.T) {
	tr := newTranslator(t)

	got := tr.Get("en", "messages.invoice.due", translation.Replace{"customer": "Ana", "date": "the 9th"})
	if want := "The invoice for Ana is due on the 9th."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

func TestTFallsBackWhenTheLocaleDoesNotCarryTheKey(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	got := tr.Get("pt-BR", "messages.invoice.due", translation.Replace{"customer": "Ana", "date": "the 9th"})
	if want := "The invoice for Ana is due on the 9th."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

func TestTUsesTheDefaultLocaleWhenGivenNone(t *testing.T) {
	tr := translation.New(catalogue(), "pt-BR", "en")

	if got, want := tr.Get("", "messages.welcome", translation.Replace{"name": "Ana"}), "Bem-vindo, Ana."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if got, want := tr.Locale(), "pt-BR"; got != want {
		t.Errorf("Locale = %q, want %q", got, want)
	}
	if got, want := tr.Fallback(), "en"; got != want {
		t.Errorf("Fallback = %q, want %q", got, want)
	}
}

// A key nobody carries has to be visible on the page. Returning a blank would
// make a wrong key look like a sentence somebody chose to leave empty.
func TestTReturnsTheKeyItDidNotFind(t *testing.T) {
	var reported []string
	tr := newTranslator(t, translation.WithMissingKey(func(locale, key string) {
		reported = append(reported, locale+" "+key)
	}))

	if got, want := tr.Get("en", "messages.absent", nil), "messages.absent"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if len(reported) != 1 || reported[0] != "en messages.absent" {
		t.Errorf("WithMissingKey saw %v, want [\"en messages.absent\"]", reported)
	}
}

// A key with no dot names a group and no item, which is not a line. The second
// catalogue format, keyed by the English sentence, is the thing this refuses.
func TestTRefusesAKeyWithNoItem(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages", nil), "messages"; got != want {
		t.Errorf("T = %q, want %q", got, want)
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

func TestHasIsSilentAboutMissingKeys(t *testing.T) {
	reported := 0
	tr := newTranslator(t, translation.WithMissingKey(func(string, string) { reported++ }))

	tr.Has("en", "messages.absent")
	if reported != 0 {
		t.Errorf("WithMissingKey ran %d times during Has, want 0", reported)
	}
}

func TestReplacementLeavesAPlaceholderItHasNoArgumentFor(t *testing.T) {
	tr := newTranslator(t)

	if got, want := tr.Get("en", "messages.welcome", nil), "Welcome, :name."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if got, want := tr.Get("en", "messages.welcome", translation.Replace{"other": "x"}), "Welcome, :name."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

// ":value" must not eat the beginning of ":values". The placeholder is the
// whole run of name characters, not the longest key that happens to prefix it.
func TestReplacementTakesTheWholePlaceholderName(t *testing.T) {
	tr := translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"messages": {"pair": ":value and :values", "punctuated": ":value, :value."}},
	}), "en", "en")

	got := tr.Get("en", "messages.pair", translation.Replace{"value": "one", "values": "many"})
	if want := "one and many"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}

	got = tr.Get("en", "messages.punctuated", translation.Replace{"value": "one"})
	if want := "one, one."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

func TestReplacementRendersWhatItIsGiven(t *testing.T) {
	tr := translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"messages": {"seconds": "Try again in :seconds seconds."}},
	}), "en", "en")

	if got, want := tr.Get("en", "messages.seconds", translation.Replace{"seconds": 60}), "Try again in 60 seconds."; got != want {
		t.Errorf("T = %q, want %q", got, want)
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
	tr := translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"messages": {"apples": "one apple|:count apples"}},
	}), "ja", "en")

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

func TestWithNamespaceIsReachedByAPrefixedKey(t *testing.T) {
	shop := translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"orders": {"title": "Your orders"}},
	})
	tr := newTranslator(t, translation.WithNamespace("shop", shop))

	if got, want := tr.Get("en", "shop::orders.title", nil), "Your orders"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if tr.Has("en", "other::orders.title") {
		t.Error("Has on an unregistered namespace = true, want false")
	}
}

// A namespaced key is answered by that namespace or by nothing. An application
// group of the same name must not shadow a module's own text.
func TestANamespaceDoesNotFallThroughToTheApplication(t *testing.T) {
	shop := translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"orders": {"title": "Your orders"}},
	})
	tr := newTranslator(t, translation.WithNamespace("shop", shop))

	if got, want := tr.Get("en", "shop::messages.welcome", nil), "shop::messages.welcome"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

func TestWithPluralReplacesTheRule(t *testing.T) {
	tr := newTranslator(t, translation.WithPlural(func(locale string, n int) int { return 0 }))

	if got, want := tr.Choice("en", "messages.apples", 7, nil), "one apple"; got != want {
		t.Errorf("Choice = %q, want %q", got, want)
	}
}

func TestANilCatalogueStillTranslatesTheBundledLines(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	if got, want := tr.Get("en", "auth.failed", nil), "These credentials do not match our records."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

// One translator answers every request at once, which is only true while
// nothing in it is written to after New. The race detector is the assertion.
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
			}
		}()
	}
	wg.Wait()
}
