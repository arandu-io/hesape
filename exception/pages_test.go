package exception

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The status pages this package ships. The audit
// found there was no equivalent anywhere, so a 404 answered with an empty body.
func TestEveryShippedStatusHasItsOwnSentence(t *testing.T) {
	statuses := []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		StatusPageExpired,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	seen := map[string]int{}
	for _, s := range statuses {
		msg := statusMessage(s)
		if msg == "" {
			t.Errorf("%d has no sentence", s)
			continue
		}
		if !strings.HasSuffix(msg, ".") {
			t.Errorf("%d: %q is not a sentence", s, msg)
		}
		// Two statuses sharing a sentence means one of them is not being
		// answered, it is being absorbed.
		if other, ok := seen[msg]; ok {
			t.Errorf("%d and %d say the same thing: %q", other, s, msg)
		}
		seen[msg] = s
	}
}

// 419 is not in any RFC, so http.StatusText has nothing for it, and a page
// headed "419" with no words is what the fallback must not produce.
func TestPageExpiredHasATitle(t *testing.T) {
	if got := statusTitle(StatusPageExpired); got == "Error" || got == "" {
		t.Fatalf("statusTitle(419) = %q", got)
	}
}

func TestAnUnlistedStatusFallsBackToTheStandardText(t *testing.T) {
	if got := statusTitle(http.StatusTeapot); got != "I'm a teapot" {
		t.Fatalf("statusTitle(418) = %q", got)
	}
	if got := statusMessage(http.StatusTeapot); got == "" {
		t.Fatal("an unlisted status has no sentence at all")
	}
	// A 5xx nobody listed still says the failure was ours.
	if got := statusMessage(http.StatusBadGateway); !strings.Contains(got, "our side") {
		t.Fatalf("statusMessage(502) = %q", got)
	}
}

func TestTheStatusPageIsSelfContained(t *testing.T) {
	rec := httptest.NewRecorder()
	statusPage(rec, PageData{Status: 404, Title: "Not Found", Message: "There is nothing at this address.", RequestID: "req-1"})

	body := rec.Body.String()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{`<html lang="en">`, "404", "Not Found", "req-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
	// No CDN, no external stylesheet, no script. The page has to render
	// when the asset build is what broke.
	for _, forbidden := range []string{"<script", "http://", "https://", "<link"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page reaches outside itself: %q", forbidden)
		}
	}
}

// Nothing the caller did not write goes on the page as markup.
func TestTheMessageIsEscaped(t *testing.T) {
	rec := httptest.NewRecorder()
	statusPage(rec, PageData{Status: 400, Title: "Bad Request", Message: `<script>alert(1)</script>`})

	if strings.Contains(rec.Body.String(), "<script>alert") {
		t.Fatal("the message was written as markup")
	}
}
