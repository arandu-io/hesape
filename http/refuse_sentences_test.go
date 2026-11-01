package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
	hhttp "github.com/arandu-io/hesape/http"
)

func TestRefuseSaysTheApplicationsSentence(t *testing.T) {
	sentences := func(r *stdhttp.Request, status int) string {
		if status == stdhttp.StatusForbidden {
			return "Você não tem acesso a este recurso."
		}
		return ""
	}
	req := httptest.NewRequest(stdhttp.MethodGet, "/invoices", nil)
	req = req.WithContext(exception.WithSentences(req.Context(), sentences))

	refused := httptest.NewRecorder()
	hhttp.Refuse(refused, req, stdhttp.StatusForbidden, "Forbidden")
	if refused.Code != stdhttp.StatusForbidden || strings.TrimSpace(refused.Body.String()) != "Você não tem acesso a este recurso." {
		t.Fatalf("%d %q", refused.Code, refused.Body.String())
	}

	// A status the application left alone keeps the caller's sentence.
	limited := httptest.NewRecorder()
	hhttp.Refuse(limited, req, stdhttp.StatusTooManyRequests, "slow down")
	if strings.TrimSpace(limited.Body.String()) != "slow down" {
		t.Fatalf("%q", limited.Body.String())
	}
}

// A browser that asked for a page and was refused reads a page, not a bare line
// of text that looks like the server broke. The status is the caller's, the
// sentence is the application's, and what the browser is told varies with what
// it asked for.
func TestARefusedBrowserIsShownAPageAndAnAPIClientTheSentence(t *testing.T) {
	sentences := func(r *stdhttp.Request, status int) string {
		if status == 419 {
			return "A página ficou aberta por muito tempo."
		}
		return ""
	}
	ask := func(accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(stdhttp.MethodPost, "/inspecionar/take/1", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req = req.WithContext(exception.WithSentences(req.Context(), sentences))
		rec := httptest.NewRecorder()
		hhttp.Refuse(rec, req, 419, "this CSRF token is no longer valid")
		return rec
	}

	page := ask("text/html,application/xhtml+xml,*/*;q=0.8")
	if page.Code != 419 {
		t.Fatalf("a browser was answered %d, want the caller's 419", page.Code)
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser was answered %q, want a page", ct)
	}
	if !strings.Contains(page.Body.String(), "A página ficou aberta por muito tempo.") {
		t.Errorf("the page lost the application's sentence: %q", page.Body.String())
	}
	if !strings.Contains(page.Header().Get("Vary"), "Accept") {
		t.Errorf("Vary = %q, want Accept: the same address answers a page and a line of text", page.Header().Get("Vary"))
	}

	for _, accept := range []string{"", "*/*", "application/json"} {
		plain := ask(accept)
		if ct := plain.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("Accept %q was answered %q, want plain text", accept, ct)
		}
		if strings.TrimSpace(plain.Body.String()) != "A página ficou aberta por muito tempo." {
			t.Errorf("Accept %q was answered %q", accept, plain.Body.String())
		}
	}
}

// htmx is told to reload and never shown a page of its own: the reload is the
// navigation that draws it.
func TestARefusedHTMXRequestKeepsThePlainAnswerEvenWhenItAcceptsHTML(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodPost, "/x", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	hhttp.Refuse(rec, req, 419, "expired")
	if rec.Header().Get("HX-Refresh") != "true" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("htmx got HX-Refresh=%q Content-Type=%q, want the reload and plain text", rec.Header().Get("HX-Refresh"), rec.Header().Get("Content-Type"))
	}
}
