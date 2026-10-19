package exception_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
)

// portuguese is an application's Sentences: its own words for two statuses, a
// different 403 under its console, and the framework's for every other one.
func portuguese(r *http.Request, status int) string {
	switch status {
	case http.StatusForbidden:
		if strings.HasPrefix(r.URL.Path, "/admin") {
			return "Esta área é da equipe."
		}
		return "Você não tem acesso a este recurso."
	case http.StatusNotFound:
		return "Página não encontrada."
	}
	return ""
}

func withPortuguese(r *http.Request) *http.Request {
	return r.WithContext(exception.WithSentences(r.Context(), portuguese))
}

func TestSentenceIsTheApplicationsOrTheFallback(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	if got := exception.Sentence(plain, http.StatusNotFound, "Not Found"); got != "Not Found" {
		t.Errorf("without Sentences: %q", got)
	}
	localized := withPortuguese(plain)
	if got := exception.Sentence(localized, http.StatusNotFound, "Not Found"); got != "Página não encontrada." {
		t.Errorf("404: %q", got)
	}
	if got := exception.Sentence(localized, http.StatusTooManyRequests, "Too Many Requests"); got != "Too Many Requests" {
		t.Errorf("a status the application left alone: %q", got)
	}
	console := withPortuguese(httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	if got := exception.Sentence(console, http.StatusForbidden, "Forbidden"); got != "Esta área é da equipe." {
		t.Errorf("403 under the console: %q", got)
	}
	if got := exception.Sentence(nil, http.StatusNotFound, "Not Found"); got != "Not Found" {
		t.Errorf("nil request: %q", got)
	}
}

func TestLocalizeSentencesPutsThemOnEveryRequest(t *testing.T) {
	var got string
	h := exception.LocalizeSentences(portuguese)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = exception.Sentence(r, http.StatusForbidden, "Forbidden")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))
	if got != "Você não tem acesso a este recurso." {
		t.Fatalf("got %q", got)
	}
	// A nil Sentences mounts nothing.
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if exception.LocalizeSentences(nil)(next) == nil {
		t.Fatal("a nil Sentences dropped the handler")
	}
}

func TestTheStatusPageAndTheProblemSayTheApplicationsSentence(t *testing.T) {
	h := exception.NewHandler(exception.Config{})

	page := httptest.NewRecorder()
	h.Render(page, withPortuguese(httptest.NewRequest(http.MethodGet, "/invoices/1", nil)), auth.ErrForbidden)
	if page.Code != http.StatusForbidden || !strings.Contains(page.Body.String(), "Você não tem acesso a este recurso.") {
		t.Fatalf("page %d: %s", page.Code, page.Body.String())
	}

	req := withPortuguese(httptest.NewRequest(http.MethodGet, "/invoices/1", nil))
	req.Header.Set("Accept", "application/json")
	doc := httptest.NewRecorder()
	h.Render(doc, req, auth.ErrForbidden)
	var problem exception.Problem
	if err := json.Unmarshal(doc.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Detail != "Você não tem acesso a este recurso." {
		t.Fatalf("problem detail = %q", problem.Detail)
	}

	written := httptest.NewRecorder()
	exception.WriteProblem(written, withPortuguese(httptest.NewRequest(http.MethodGet, "/x", nil)), http.StatusNotFound, "Not Found")
	if !strings.Contains(written.Body.String(), `"detail":"Página não encontrada."`) {
		t.Fatalf("WriteProblem: %s", written.Body.String())
	}
}

// A message the developer wrote on an *HTTPError was written for the person
// reading it, so it is kept over the application's sentence for the status.
func TestAnAbortMessageIsKept(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	rec := httptest.NewRecorder()
	h.Render(rec, withPortuguese(httptest.NewRequest(http.MethodGet, "/x", nil)), exception.Abort(http.StatusNotFound, "no invoice with that number"))
	if !strings.Contains(rec.Body.String(), "no invoice with that number") {
		t.Fatalf("the developer's message was replaced: %s", rec.Body.String())
	}
}
