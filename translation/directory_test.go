package translation_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/arandu-io/hesape/translation"
)

// langDirectory is a project's lang directory: two locales as directories, one as
// a sentence-keyed file only, and a module override that is not a locale.
func langDirectory() fstest.MapFS {
	return fstest.MapFS{
		"lang/pt-BR/auth.json":             {Data: []byte(`{"failed": "Credenciais inválidas."}`)},
		"lang/pt-BR.json":                  {Data: []byte(`{"Save changes": "Salvar alterações"}`)},
		"lang/en/auth.json":                {Data: []byte(`{"failed": "Bad credentials."}`)},
		"lang/es.json":                     {Data: []byte(`{"Save changes": "Guardar cambios"}`)},
		"lang/vendor/billing/pt-BR/x.json": {Data: []byte(`{"y": "z"}`)},
	}
}

// TestNewFromDirectoryAnswersOutOfTheCatalogue: the locale asked for, then the
// fallback, then the English embedded here.
func TestNewFromDirectoryAnswersOutOfTheCatalogue(t *testing.T) {
	tr, err := translation.NewFromDirectory(langDirectory(), "lang", "pt-BR", "en")
	if err != nil {
		t.Fatalf("NewFromDirectory: %v", err)
	}
	if got := tr.Get("", "auth.failed", nil); got != "Credenciais inválidas." {
		t.Errorf("auth.failed = %q, want the pt-BR line", got)
	}
	if got := tr.Get("pt-BR", "Save changes", nil); got != "Salvar alterações" {
		t.Errorf("the sentence key = %q, want the pt-BR catalogue", got)
	}
	if got := tr.Get("fr", "auth.failed", nil); got != "Bad credentials." {
		t.Errorf("auth.failed in fr = %q, want the fallback locale's line", got)
	}
	if got := tr.Get("pt-BR", "auth.throttle", nil); got == "auth.throttle" {
		t.Error("a line the catalogue lacks must come from the embedded English, not come back as its key")
	}
}

// TestNewFromDirectoryRefusesAFileThatDoesNotParse: a broken catalogue is a
// boot failure, not a missing sentence on one request.
func TestNewFromDirectoryRefusesAFileThatDoesNotParse(t *testing.T) {
	broken := fstest.MapFS{"lang/pt-BR/auth.json": {Data: []byte(`{"failed": `)}}
	if _, err := translation.NewFromDirectory(broken, "lang", "pt-BR", "en"); err == nil {
		t.Fatal("a malformed language file was accepted")
	}
}

// TestNewFromDirectoryWithoutADirectoryStillTranslates: a project that ships
// no lang directory answers in the embedded English.
func TestNewFromDirectoryWithoutADirectoryStillTranslates(t *testing.T) {
	tr, err := translation.NewFromDirectory(fstest.MapFS{}, "lang", "en", "en")
	if err != nil {
		t.Fatalf("NewFromDirectory: %v", err)
	}
	if got := tr.Get("", "auth.failed", nil); got == "auth.failed" {
		t.Error("the embedded English did not answer")
	}
}

// TestLocalesListsWhatTheCatalogueCarries: directories and sentence files,
// once each, sorted, and not the vendor tree.
func TestLocalesListsWhatTheCatalogueCarries(t *testing.T) {
	got := translation.Locales(langDirectory(), "lang")
	if want := []string{"en", "es", "pt-BR"}; !slices.Equal(got, want) {
		t.Fatalf("Locales = %v, want %v", got, want)
	}
	if got := translation.Locales(fstest.MapFS{}, "lang"); len(got) != 0 {
		t.Fatalf("Locales of a missing directory = %v, want none", got)
	}
}
