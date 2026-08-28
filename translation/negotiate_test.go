package translation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/translation"
)

func TestNegotiate(t *testing.T) {
	supported := []string{"en", "pt-BR", "fr"}

	for header, want := range map[string]string{
		"":                                      "en",
		"de":                                    "en",
		"fr":                                    "fr",
		"pt-BR":                                 "pt-BR",
		"pt_BR":                                 "pt-BR",
		"PT-br":                                 "pt-BR",
		"pt":                                    "pt-BR",
		"pt-PT":                                 "pt-BR",
		"*":                                     "en",
		"de, fr":                                "fr",
		"de;q=0.9, fr;q=0.8":                    "fr",
		"fr;q=0.2, pt-BR;q=0.9":                 "pt-BR",
		"fr;q=0, pt-BR":                         "pt-BR",
		"fr;q=0":                                "en",
		"pt-BR;q=nonsense, fr":                  "fr",
		"en-GB":                                 "en",
		"de-DE, it;q=0.9, pt-BR;q=0.8, *;q=0.1": "pt-BR",
	} {
		if got := translation.Negotiate(header, supported, "en"); got != want {
			t.Errorf("Negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

// An exact match on a tag the client wants less must not beat a language match
// on the tag it wants most.
func TestNegotiateReadsQualityBeforeExactness(t *testing.T) {
	got := translation.Negotiate("pt;q=0.9, fr;q=0.5", []string{"fr", "pt-BR"}, "en")
	if want := "pt-BR"; got != want {
		t.Errorf("Negotiate = %q, want %q", got, want)
	}
}

func TestNegotiateWithNothingSupported(t *testing.T) {
	if got, want := translation.Negotiate("fr", nil, "en"), "en"; got != want {
		t.Errorf("Negotiate = %q, want %q", got, want)
	}
}

func TestMiddlewarePutsTheLocaleInTheContext(t *testing.T) {
	var seen string
	h := translation.Middleware([]string{"en", "pt-BR"}, "en")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = translation.Locale(r.Context())
		}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "pt-BR,pt;q=0.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if want := "pt-BR"; seen != want {
		t.Errorf("Locale = %q, want %q", seen, want)
	}
	if got, want := w.Header().Get("Content-Language"), "pt-BR"; got != want {
		t.Errorf("Content-Language = %q, want %q", got, want)
	}
	// A page whose text depends on a request header and does not say so is a
	// page a shared cache serves in the wrong language.
	if got, want := w.Header().Get("Vary"), "Accept-Language"; got != want {
		t.Errorf("Vary = %q, want %q", got, want)
	}
}

func TestMiddlewareFallsBackWithNoHeader(t *testing.T) {
	var seen string
	h := translation.Middleware([]string{"en", "pt-BR"}, "en")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = translation.Locale(r.Context())
		}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if want := "en"; seen != want {
		t.Errorf("Locale = %q, want %q", seen, want)
	}
}

func TestInLocalePutsTheDecidedLocaleInTheContext(t *testing.T) {
	var seen string
	h := translation.InLocale("es")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = translation.Locale(r.Context())
		}))

	r := httptest.NewRequest(http.MethodGet, "/es/docs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if want := "es"; seen != want {
		t.Errorf("Locale = %q, want %q", seen, want)
	}
	if got, want := w.Header().Get("Content-Language"), "es"; got != want {
		t.Errorf("Content-Language = %q, want %q", got, want)
	}
}

// The header is not an input here, so a request that carries one is served the
// language of its address anyway.
func TestInLocaleIgnoresAcceptLanguage(t *testing.T) {
	var seen string
	h := translation.InLocale("es")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = translation.Locale(r.Context())
		}))

	r := httptest.NewRequest(http.MethodGet, "/es/docs", nil)
	r.Header.Set("Accept-Language", "en;q=1.0,pt-BR;q=0.9")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if want := "es"; seen != want {
		t.Errorf("Locale = %q, want %q", seen, want)
	}
}

// The one that keeps a path addressed site cacheable: this response does not
// depend on Accept-Language, and saying that it does costs a shared cache a
// stored copy per distinct header value for pages that are all the same.
func TestInLocaleDoesNotVaryOnAcceptLanguage(t *testing.T) {
	h := translation.InLocale("es")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/es/docs", nil))

	if got := w.Header().Values("Vary"); len(got) != 0 {
		t.Errorf("Vary = %q, want none", got)
	}
}

// A caller with no request passes the empty string straight through, and the
// translator reads it as its own locale.
func TestLocaleOfAContextThatHasNone(t *testing.T) {
	if got := translation.Locale(t.Context()); got != "" {
		t.Errorf("Locale = %q, want the empty string", got)
	}
}
