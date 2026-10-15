package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAStoredCallKeepsNoCredentialInClear: StoreHeaders {"*"} kept every
// header as it arrived -- the HeaderSecret validator's shared secret,
// Authorization, Cookie -- in a row anyone reading the calls panel reads, and
// URLFull kept a token in the query the same way. The name stays, so what
// arrived can still be told apart; the value does not.
func TestAStoredCallKeepsNoCredentialInClear(t *testing.T) {
	h := http.Header{
		"Authorization":       {"Bearer live-token"},
		"Proxy-Authorization": {"Basic cHJveHk="},
		"Cookie":              {"session=abc"},
		"X-Api-Key":           {"key-123"},
		"X-Auth-Token":        {"tok-456"},
		"X-Shared-Secret":     {"the-shared-secret"},
		"X-Provider-Shared":   {"provider-header-secret"},
		"Content-Type":        {"application/json"},
		"X-Request-Id":        {"req-1"},
	}
	validator := HeaderSecret{Header: "X-Provider-Shared"}
	for _, keep := range [][]string{{"*"}, {"Authorization", "X-Provider-Shared", "Cookie", "X-Request-Id"}} {
		var stored map[string][]string
		if err := json.Unmarshal([]byte(keptHeaders(h, keep, secretHeadersOf(validator))), &stored); err != nil {
			t.Fatal(err)
		}
		for name, values := range stored {
			for _, value := range values {
				if strings.Contains(value, "token") || strings.Contains(value, "secret") || strings.Contains(value, "session=") ||
					strings.Contains(value, "key-123") || strings.Contains(value, "tok-456") || strings.Contains(value, "cHJveHk") {
					t.Errorf("StoreHeaders %v kept %s as %q", keep, name, value)
				}
			}
		}
		if got := stored["x-provider-shared"]; len(got) != 1 || got[0] != redacted {
			t.Errorf("StoreHeaders %v kept the validator's header as %q, want it named and redacted", keep, got)
		}
		if got := stored["x-request-id"]; len(got) != 1 || got[0] != "req-1" {
			t.Errorf("StoreHeaders %v kept X-Request-Id as %q, want it as it arrived", keep, got)
		}
	}

	r := httptest.NewRequest(http.MethodPost, "https://app.test/webhooks/billing?event=paid&token=t0k&api_key=k3y&key=abc&sig=s1g&page=2", nil)
	got := keptURL(r, &receiver{Receiver: Receiver{URL: URLFull}})
	want := "https://app.test/webhooks/billing?event=paid&token=[redacted]&api_key=[redacted]&key=[redacted]&sig=[redacted]&page=2"
	if got != want {
		t.Fatalf("keptURL = %q, want %q", got, want)
	}
	plain := httptest.NewRequest(http.MethodPost, "https://app.test/webhooks/billing?event=paid", nil)
	if got := keptURL(plain, &receiver{Receiver: Receiver{URL: URLFull}}); got != "https://app.test/webhooks/billing?event=paid" {
		t.Fatalf("keptURL = %q, want an address with no credential kept as it arrived", got)
	}
}
