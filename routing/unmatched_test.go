package routing_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/routing"
)

func localized(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	sentences := func(r *http.Request, status int) string {
		switch status {
		case http.StatusNotFound:
			return "Página não encontrada."
		case http.StatusMethodNotAllowed:
			return "Este endereço não aceita este método."
		}
		return ""
	}
	return req.WithContext(exception.WithSentences(req.Context(), sentences))
}

// An address no route answers is refused like every other refusal: with the
// application's sentence, and with HX-Refresh for htmx.
func TestAnUnmatchedAddressIsRefusedWithTheApplicationsSentence(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices", ok)

	rec := httptest.NewRecorder()
	req := localized(http.MethodGet, "/nowhere")
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || strings.TrimSpace(rec.Body.String()) != "Página não encontrada." {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("HX-Refresh") != "true" {
		t.Error("an htmx request was not told to reload")
	}

	wrong := httptest.NewRecorder()
	r.ServeHTTP(wrong, localized(http.MethodPost, "/invoices"))
	if wrong.Code != http.StatusMethodNotAllowed || strings.TrimSpace(wrong.Body.String()) != "Este endereço não aceita este método." {
		t.Fatalf("%d %q", wrong.Code, wrong.Body.String())
	}
	if !strings.Contains(wrong.Header().Get("Allow"), "GET") {
		t.Errorf("the mux's Allow header was lost: %q", wrong.Header().Get("Allow"))
	}

	// Without Sentences the answer is the mux's own, as it always was, and a
	// matched route is untouched.
	plain := httptest.NewRecorder()
	r.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if plain.Code != http.StatusNotFound || strings.TrimSpace(plain.Body.String()) != "404 page not found" {
		t.Fatalf("%d %q", plain.Code, plain.Body.String())
	}
	found := httptest.NewRecorder()
	r.ServeHTTP(found, localized(http.MethodGet, "/invoices"))
	if found.Code != http.StatusOK {
		t.Fatalf("a matched route answered %d", found.Code)
	}
}
