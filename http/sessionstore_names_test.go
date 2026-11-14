package http_test

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

// The names the application passed reach the session cookie and the remembered
// address, write and read alike, and nothing the store writes carries the
// collection's name.
func TestASessionStoreWritesAndReadsTheNamesItWasGiven(t *testing.T) {
	ctx := context.Background()
	names := session.WithCookieNames(session.CookieNamesFor(support.NewBrand("Peráta")))
	store := hhttp.NewSessionStore(sessionAppKey, time.Hour, true, nil, names)

	if s, i := store.CookieNames(); s != "perata_session" || i != "perata_intended" {
		t.Fatalf("CookieNames = %q, %q", s, i)
	}

	w := httptest.NewRecorder()
	if _, err := store.Start(ctx, w, auth.Subject{ID: "u1", Tenant: "acme"}); err != nil {
		t.Fatal(err)
	}
	store.RememberIntended(w, httptest.NewRequest(stdhttp.MethodGet, "/painel", nil))

	seen := map[string]bool{}
	for _, c := range w.Result().Cookies() {
		seen[c.Name] = true
		if strings.Contains(c.Name, "arandu") {
			t.Errorf("cookie %q carries the collection's name", c.Name)
		}
	}
	if !seen["perata_session"] {
		t.Errorf("no perata_session cookie; wrote %v", seen)
	}

	if _, err := store.Load(ctx, carryCookies(w, httptest.NewRequest(stdhttp.MethodGet, "/", nil))); err != nil {
		t.Fatalf("the store did not read the session it wrote: %v", err)
	}

	out := httptest.NewRecorder()
	if err := store.Destroy(ctx, out, ""); err != nil {
		t.Fatal(err)
	}
	if !clears(out, "perata_session") || !clears(out, "perata_intended") {
		t.Error("Destroy did not clear the cookies under the application's names")
	}
}
