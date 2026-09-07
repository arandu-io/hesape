package translation_test

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

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
	// Spanish, and not Portuguese, because the bundled catalogue ships pt-BR
	// now: a locale this package already answers would not test the question,
	// which is whether a project's own catalogue wins over the bundled one.
	l.AddMessages("es", "auth", translation.Lines{"failed": "Estas credenciales no coinciden con nuestros registros."}, "")
	tr := translation.New(l, "es", "en")

	if got, want := tr.Get("es", "auth.failed", nil), "Estas credenciales no coinciden con nuestros registros."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	// And a group it has not translated falls through to English rather than
	// showing the key.
	if got, want := tr.Get("es", "passwords.reset", nil), "Your password has been reset."; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// The whole route a project takes to add a locale: a directory of locale
// directories, read by a [translation.FileLoader], handed to [translation.New].
//
// It is asserted end to end rather than at the loader, because it is the layout
// the package comment tells a project to use, and a layout that is only
// described drifts from the one that works. Nothing here is a change to this
// package: adding a language is a project writing files.
func TestAProjectAddsALocaleWithFilesAlone(t *testing.T) {
	files := fstest.MapFS{
		// A group file: one item of one group, translated.
		"lang/es/auth.json": &fstest.MapFile{
			Data: []byte(`{"failed": "Estas credenciales no coinciden con nuestros registros."}`),
		},
		// The JSON catalogue of the same locale, whose keys are sentences.
		"lang/es.json": &fstest.MapFile{
			Data: []byte(`{"Save changes": "Guardar cambios"}`),
		},
		// The project's own English, overriding one bundled line.
		"lang/en/auth.json": &fstest.MapFile{
			Data: []byte(`{"failed": "We do not know that email and password."}`),
		},
	}
	loader, err := translation.NewFileLoader(files, "lang")
	if err != nil {
		t.Fatalf("NewFileLoader: %v", err)
	}
	tr := translation.New(loader, "es", "en")

	for _, c := range []struct {
		name   string
		locale string
		key    string
		want   string
	}{
		{"a group line the project translated", "es", "auth.failed",
			"Estas credenciales no coinciden con nuestros registros."},
		{"a sentence key from the JSON catalogue", "es", "Save changes",
			"Guardar cambios"},
		{"a line the project did not translate falls to the fallback locale", "es", "auth.password",
			"The provided password is incorrect."},
		{"a nested item still flattens", "es", "validation.min.string",
			"The :attribute field must be at least :min characters."},
		{"the project's English wins over the bundled line", "en", "auth.failed",
			"We do not know that email and password."},
		{"the rest of the bundled group is untouched by that override", "en", "auth.throttle",
			"Too many login attempts. Please try again in :seconds seconds."},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := tr.Get(c.locale, c.key, nil); got != c.want {
				t.Errorf("Get(%q, %q) = %q, want %q", c.locale, c.key, got, c.want)
			}
		})
	}
}
