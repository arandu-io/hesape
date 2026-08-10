package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/routing/middleware"
)

func signer(t *testing.T) *encryption.Signer {
	t.Helper()
	key, err := encryption.ParseKey(encryption.GenerateKey())
	if err != nil {
		t.Fatalf("parsing a freshly generated key: %v", err)
	}
	return encryption.NewSigner(key)
}

func unsubscribeRouter() *routing.Router {
	r := routing.NewRouter()
	r.Get("/unsubscribe/{id}", reached).Name("unsubscribe")
	return r
}

// TestASignedLinkReachesTheHandler is the whole point: the URL
// routing.SignedRoute builds is the URL this lets through.
func TestASignedLinkReachesTheHandler(t *testing.T) {
	s := signer(t)
	r := unsubscribeRouter()
	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", time.Hour, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}

	var refused refusal
	h := middleware.ValidateSignature(s, refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, link, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("a signed link = %d, want 200 (%s)", rec.Code, refused.message)
	}
	if refused.called {
		t.Fatal("a valid link was refused")
	}
}

// TestAnUnsignedRequestIsRefused, and refused through the Refuse the caller
// gave: a 403 written with http.Error is a 403 an HTMX request never shows.
func TestAnUnsignedRequestIsRefused(t *testing.T) {
	var refused refusal
	h := middleware.ValidateSignature(signer(t), refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unsubscribe/list-7", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("an unsigned request = %d, want 403", rec.Code)
	}
	if !refused.called {
		t.Fatal("the refusal did not go through the Refuse given to the middleware")
	}
	if strings.Contains(refused.message, "expired") {
		t.Fatalf("an unsigned link was reported as expired: %q", refused.message)
	}
}

// TestAnExpiredLinkSaysSo, because "ask for a new one" is something the person
// can act on and "this link is not valid" is not.
func TestAnExpiredLinkSaysSo(t *testing.T) {
	s := signer(t)
	r := unsubscribeRouter()
	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", -time.Second, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}

	var refused refusal
	h := middleware.ValidateSignature(s, refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, link, nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("an expired link = %d, want 403", rec.Code)
	}
	if !strings.Contains(refused.message, "expired") {
		t.Fatalf("the sentence does not say the link expired: %q", refused.message)
	}
}

// TestASignatureIssuedForAnotherAddressIsRefused: without the comparison, one
// signed link would open every route the middleware guards.
func TestASignatureIssuedForAnotherAddressIsRefused(t *testing.T) {
	s := signer(t)
	r := routing.NewRouter()
	r.Get("/unsubscribe/{id}", reached).Name("unsubscribe")
	r.Get("/admin/{id}", reached).Name("admin")

	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", time.Hour, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}
	stolen := strings.Replace(link, "/unsubscribe/", "/admin/", 1)

	var refused refusal
	h := middleware.ValidateSignature(s, refused.refuse)(reached)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, stolen, nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a signature moved to another path = %d, want 403", rec.Code)
	}
}
