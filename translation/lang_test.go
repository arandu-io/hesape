package translation_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/translation"
)

// bundledGroups are the four the framework produces sentences for. The list is
// written out rather than read from the directory, so a group that stops being
// embedded fails here instead of being quietly absent from what is checked.
var bundledGroups = []string{"auth", "passwords", "pagination", "validation"}

// Every line the catalogue carries has to come back through a Translator. What
// makes this worth asserting over the whole catalogue rather than on a sample is
// the failure it catches: a group that does not reach the lookup answers with
// the key, so "validation.min.string" is what prints where the sentence was, and
// a spot check of five keys says nothing about the other hundred and thirty.
func TestEveryBundledLineResolvesThroughATranslator(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	for _, group := range bundledGroups {
		lines := translation.Bundled("en", group)
		if len(lines) == 0 {
			t.Errorf("the bundled catalogue carries no %q group", group)
			continue
		}
		for _, item := range slices.Sorted(maps.Keys(lines)) {
			key := group + "." + item
			if got := tr.Get("en", key, nil); got == key {
				t.Errorf("%s does not resolve: it would print as the key", key)
			}
		}
	}
}

// An empty line resolves -- it is not the key -- and prints as nothing, which is
// the one way a missing sentence gets past the test above.
func TestNoBundledLineIsEmpty(t *testing.T) {
	for _, group := range bundledGroups {
		for item, line := range translation.Bundled("en", group) {
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s.%s is empty", group, item)
			}
		}
	}
}

// The four groups a framework produces sentences for before an application has
// written a line. They resolve with no catalogue configured at all.
func TestTheBundledCatalogueAnswersTheFrameworkGroups(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	for key, want := range map[string]string{
		"auth.failed":           "These credentials do not match our records.",
		"passwords.sent":        "We have emailed your password reset link.",
		"pagination.next":       "Next &raquo;",
		"validation.required":   "The :attribute field is required.",
		"validation.min.string": "The :attribute field must be at least :min characters.",
	} {
		if got := tr.Get("en", key, nil); got != want {
			t.Errorf("Get(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestTheBundledCatalogueInterpolates(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	got := tr.Get("en", "auth.throttle", translation.Replace{"seconds": 60})
	if want := "Too many login attempts. Please try again in 60 seconds."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}

	got = tr.Get("en", "validation.between.numeric", translation.Replace{"attribute": "age", "min": 18, "max": 120})
	if want := "The age field must be between 18 and 120."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// The application catalogue is read first, so one key is overridden by defining
// it and nothing has to be published to change a sentence.
func TestTheApplicationCatalogueOverridesABundledLine(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("en", "auth", translation.Lines{"failed": "We do not know that email and password."}, "")
	tr := translation.New(l, "en", "en")

	if got, want := tr.Get("en", "auth.failed", nil), "We do not know that email and password."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	// Everything the application did not override still answers.
	if got, want := tr.Get("en", "auth.password", nil), "The provided password is incorrect."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// The bundled lines are English, and a locale that carries its own must win
// over them even though they are the last catalogue consulted.
func TestALocaleCatalogueWinsOverTheBundledEnglish(t *testing.T) {
	l := translation.NewArrayLoader()
	l.AddMessages("pt-BR", "auth", translation.Lines{"failed": "Estas credenciais não correspondem aos nossos registros."}, "")
	tr := translation.New(l, "pt-BR", "en")

	if got, want := tr.Get("pt-BR", "auth.failed", nil), "Estas credenciais não correspondem aos nossos registros."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	// And a group it has not translated falls through to English rather than
	// showing the key.
	if got, want := tr.Get("pt-BR", "passwords.reset", nil), "Your password has been reset."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}
