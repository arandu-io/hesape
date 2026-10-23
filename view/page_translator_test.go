package view_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/view"
)

// translatorPage is the interface a component library asks a page for when it
// draws a word nobody wrote. It is declared here, by the consumer, so the test
// proves Page answers it without importing the library.
type translatorPage interface {
	Translator() *translation.Translator
	Locale() string
}

var _ translatorPage = view.Page{}

func catalogue() *translation.Translator {
	loader := translation.NewArrayLoader().
		AddMessages("pt-BR", "kyse", translation.Lines{"dialog.cancel": "Cancelar"}, "*")
	return translation.New(loader, "en", "en")
}

// TestNewTakesTheTranslatorAndTheLocaleFromTheRequest: a screen that embeds
// Page says a component's own words in the request's language, because New
// read both from where the request entered the application.
func TestNewTakesTheTranslatorAndTheLocaleFromTheRequest(t *testing.T) {
	tr := catalogue()
	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	ctx := translation.WithLocale(translation.WithTranslator(req.Context(), tr), "pt-BR")
	page := view.New(hhttp.NewContext(httptest.NewRecorder(), req.WithContext(ctx), nil, nil), "Notes")

	if page.Translator() != tr {
		t.Fatal("Translator is not the one on the request")
	}
	if page.Locale() != "pt-BR" {
		t.Fatalf("Locale = %q, want pt-BR", page.Locale())
	}
	if got := page.Translator().Get(page.Locale(), "kyse.dialog.cancel", nil); got != "Cancelar" {
		t.Errorf("the catalogue answered %q, want Cancelar", got)
	}
}

// TestARequestWithNoTranslatorDrawsNoCatalogue: nil is what a component reads
// as "say the English".
func TestARequestWithNoTranslatorDrawsNoCatalogue(t *testing.T) {
	page := view.New(hhttp.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), nil, nil), "Home")
	if page.Translator() != nil || page.Locale() != "" {
		t.Fatalf("Translator = %v, Locale = %q, want none", page.Translator(), page.Locale())
	}

	tr := catalogue()
	if with := page.WithTranslator(tr, "pt-BR"); with.Translator() != tr || with.Locale() != "pt-BR" {
		t.Error("WithTranslator did not replace the catalogue and the locale")
	}
	if page.Translator() != nil {
		t.Error("WithTranslator changed the page it was called on")
	}
}

// TestAPageTranslatesInTheLanguageOfTheRequest: a view wrote
// .Translator().Get(.Locale(), key, nil) for every sentence; Trans and Choice
// say it in one call.
func TestAPageTranslatesInTheLanguageOfTheRequest(t *testing.T) {
	loader := translation.NewArrayLoader().AddMessages("pt-BR", "notes", translation.Lines{
		"saved": "Nota :title salva por :who.",
		"count": "{0} Nenhuma nota|{1} Uma nota|[2,*] :count notas",
	}, "*")
	page := view.Page{}.WithTranslator(translation.New(loader, "en", "en"), "pt-BR")

	if got := page.Trans("notes.saved", translation.Replace{"title": "A", "who": "x"}, translation.Replace{"who": "Ana"}); got != "Nota A salva por Ana." {
		t.Errorf("Trans = %q", got)
	}
	if got := page.Choice("notes.count", 3); got != "3 notas" {
		t.Errorf("Choice = %q", got)
	}
	if got := page.Choice("notes.count", 1); got != "Uma nota" {
		t.Errorf("Choice of one = %q", got)
	}
}

// TestAPageWithNoTranslatorAnswersTheKey: the skeleton wired no translator,
// and a view that asked Translator() for a line panicked on the nil.
func TestAPageWithNoTranslatorAnswersTheKey(t *testing.T) {
	page := view.Page{}
	if got := page.Trans("notes.saved", translation.Replace{"title": "A"}); got != "notes.saved" {
		t.Errorf("Trans = %q, want the key", got)
	}
	if got := page.Choice("notes.count", 2); got != "notes.count" {
		t.Errorf("Choice = %q, want the key", got)
	}
	if got := page.Translator().Get(page.Locale(), "notes.saved", nil); got != "notes.saved" {
		t.Errorf("Translator().Get = %q, want the key", got)
	}
	if got := page.Translator().Choice(page.Locale(), "notes.count", 2, nil); got != "notes.count" {
		t.Errorf("Translator().Choice = %q, want the key", got)
	}
	if page.Translator().Has(page.Locale(), "notes.saved") || page.Translator().HasForLocale(page.Locale(), "notes.saved") {
		t.Error("a nil translator has a line")
	}
}
