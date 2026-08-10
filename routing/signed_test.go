package routing_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/routing"
)

func testSigner(t *testing.T) *encryption.Signer {
	t.Helper()
	key, err := encryption.ParseKey(encryption.GenerateKey())
	if err != nil {
		t.Fatalf("parsing a freshly generated key: %v", err)
	}
	return encryption.NewSigner(key)
}

// TestASignedRouteVerifies is the round trip: the link this builds is the link
// the middleware lets through.
func TestASignedRouteVerifies(t *testing.T) {
	s := testSigner(t)
	r := routing.NewRouter()
	r.Get("/unsubscribe/{id}", ok).Name("unsubscribe")

	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", time.Hour, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}
	if !strings.HasPrefix(link, "/unsubscribe/list-7?") {
		t.Fatalf("link = %q, want it to start with the route's own path", link)
	}

	if err := routing.VerifySignature(s, httptest.NewRequest(http.MethodGet, link, nil)); err != nil {
		t.Fatalf("a freshly signed link did not verify: %v", err)
	}
}

// TestAnUnknownRouteIsRefusedBeforeItIsSigned: signing "" would produce a link
// to the front page that verifies, which is worse than an error.
func TestAnUnknownRouteIsRefusedBeforeItIsSigned(t *testing.T) {
	s := testSigner(t)
	if _, err := routing.SignedRoute(routing.NewRoutes(), s, "nowhere", time.Hour); err == nil {
		t.Fatal("an unknown route name was signed")
	}
}

// TestATamperedSignedLinkIsRefused covers the four ways a link arrives wrong.
func TestATamperedSignedLinkIsRefused(t *testing.T) {
	s := testSigner(t)
	other := testSigner(t)

	r := routing.NewRouter()
	r.Get("/unsubscribe/{id}", ok).Name("unsubscribe")
	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", time.Hour, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}

	cases := []struct {
		name string
		url  string
		with *encryption.Signer
	}{
		{"no signature at all", "/unsubscribe/list-7", s},
		{"a signature this application did not issue", link, other},
		{"a different id than the one signed", strings.Replace(link, "list-7", "list-8", 1), s},
		{"an extra parameter appended to a signed link", link + "&admin=1", s},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := routing.VerifySignature(c.with, httptest.NewRequest(http.MethodGet, c.url, nil))
			if err == nil {
				t.Fatal("it was accepted")
			}
			if !errors.Is(err, encryption.ErrSignature) {
				t.Fatalf("error = %v, want one that unwraps to encryption.ErrSignature", err)
			}
		})
	}
}

// TestAnExpiredLinkIsToldApart, because "ask for another one" is a different
// sentence from "this link is not ours" and only one of them is actionable.
func TestAnExpiredLinkIsToldApart(t *testing.T) {
	s := testSigner(t)
	r := routing.NewRouter()
	r.Get("/unsubscribe/{id}", ok).Name("unsubscribe")

	link, err := routing.SignedRoute(r.Table(), s, "unsubscribe", -time.Second, "list-7")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}

	err = routing.VerifySignature(s, httptest.NewRequest(http.MethodGet, link, nil))
	if !errors.Is(err, encryption.ErrExpired) {
		t.Fatalf("error = %v, want encryption.ErrExpired", err)
	}
}

// TestTheSignatureCoversTheQueryString: a signed link carrying its own
// parameters must not accept a reordering, and must not accept a new one.
func TestTheSignatureCoversTheQueryString(t *testing.T) {
	s := testSigner(t)
	r := routing.NewRouter()
	r.Get("/download/{id}", ok).Name("download")

	link, err := routing.SignedRoute(r.Table(), s, "download", time.Hour, "doc-1")
	if err != nil {
		t.Fatalf("SignedRoute: %v", err)
	}

	// The token is the only parameter, so moving it is a no-op and the link
	// still verifies -- while anything added alongside it does not.
	if err := routing.VerifySignature(s, httptest.NewRequest(http.MethodGet, link, nil)); err != nil {
		t.Fatalf("the link did not verify: %v", err)
	}
	if err := routing.VerifySignature(s, httptest.NewRequest(http.MethodGet, link+"&pages=all", nil)); err == nil {
		t.Fatal("a parameter that was never signed was accepted")
	}
}
