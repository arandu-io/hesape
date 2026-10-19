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
