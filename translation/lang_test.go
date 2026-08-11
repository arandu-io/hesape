package translation_test

import (
	"testing"

	"github.com/arandu-io/hesape/translation"
)

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
			t.Errorf("T(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestTheBundledCatalogueInterpolates(t *testing.T) {
	tr := translation.New(nil, "en", "en")

	got := tr.Get("en", "auth.throttle", translation.Replace{"seconds": 60})
	if want := "Too many login attempts. Please try again in 60 seconds."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}

	got = tr.Get("en", "validation.between.numeric", translation.Replace{"attribute": "age", "min": 18, "max": 120})
	if want := "The age field must be between 18 and 120."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

// The application catalogue is read first, so one key is overridden by defining
// it and nothing has to be published to change a sentence.
func TestTheApplicationCatalogueOverridesABundledLine(t *testing.T) {
	tr := translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		"en": {"auth": {"failed": "We do not know that email and password."}},
	}), "en", "en")

	if got, want := tr.Get("en", "auth.failed", nil), "We do not know that email and password."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	// Everything the application did not override still answers.
	if got, want := tr.Get("en", "auth.password", nil), "The provided password is incorrect."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

// The bundled lines are English, and a locale that carries its own must win
// over them even though they are the last catalogue consulted.
func TestALocaleCatalogueWinsOverTheBundledEnglish(t *testing.T) {
	tr := translation.New(translation.NewMap(map[string]map[string]translation.Lines{
		"pt-BR": {"auth": {"failed": "Estas credenciais não correspondem aos nossos registros."}},
	}), "pt-BR", "en")

	if got, want := tr.Get("pt-BR", "auth.failed", nil), "Estas credenciais não correspondem aos nossos registros."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	// And a group it has not translated falls through to English rather than
	// showing the key.
	if got, want := tr.Get("pt-BR", "passwords.reset", nil), "Your password has been reset."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}
