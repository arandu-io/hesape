package view_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/support"
	"github.com/arandu-io/hesape/view"
)

// served fetches an asset's body through the handler, at the address the page
// would have been given.
func served(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	view.Handler(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code, rec.Body.String(), rec.Header()
}

// The application's name reaches the address, the keys the scripts keep their
// preferences under and the address of the token renewal, and the old address
// keeps answering for the pages that were cached before.
func TestUseBrandServesTheAssetsUnderTheApplicationsOwnName(t *testing.T) {
	legacy := view.Asset("theme.js")
	t.Cleanup(func() { view.UseBrand(support.Brand{}) })

	view.UseBrand(support.NewBrand("Peráta"))

	if got := view.AssetPrefix(); got != "/_perata/assets/" {
		t.Fatalf("AssetPrefix = %q", got)
	}
	for _, name := range []string{"app.css", "ui.js", "theme.js", "htmx.min.js"} {
		if url := view.Asset(name); !strings.HasPrefix(url, "/_perata/assets/") || strings.Contains(url, "arandu") {
			t.Errorf("Asset(%q) = %q, want it under /_perata/assets/", name, url)
		}
	}
	for _, r := range view.Assets() {
		if strings.Contains(r.URL, "arandu") {
			t.Errorf("%s is served at %s", r.Name, r.URL)
		}
	}

	code, theme, header := served(t, view.Asset("theme.js"))
	if code != http.StatusOK || !strings.Contains(theme, "'perata.theme'") || strings.Contains(theme, "'arandu.theme'") || strings.Contains(theme, "@app@") {
		t.Fatalf("theme.js: status %d, key not renamed", code)
	}
	if !strings.Contains(header.Get("Cache-Control"), "immutable") {
		t.Errorf("Cache-Control = %q, want the hashed address cached", header.Get("Cache-Control"))
	}

	_, ui, _ := served(t, view.Asset("ui.js"))
	for _, want := range []string{"'perata.sidebar.'", "'/_perata/csrf'"} {
		if !strings.Contains(ui, want) {
			t.Errorf("ui.js lost %s", want)
		}
	}
	if strings.Contains(ui, "'arandu.sidebar.'") || strings.Contains(ui, "/_arandu/") || strings.Contains(ui, "@app@") {
		t.Error("ui.js still names the collection in a key or an address")
	}

	// The address a page cached before the change points at still answers.
	code, body, header := served(t, legacy)
	if code != http.StatusOK || body == "" {
		t.Fatalf("the old address answered %d", code)
	}
	if strings.Contains(header.Get("Cache-Control"), "immutable") {
		t.Errorf("the old hash is a different body, but was marked immutable: %q", header.Get("Cache-Control"))
	}
}

// An application that never calls UseBrand is served as before.
func TestTheDefaultBrandChangesNothing(t *testing.T) {
	view.UseBrand(support.Brand{})
	if got := view.AssetPrefix(); got != view.AssetPath {
		t.Fatalf("AssetPrefix = %q, want %q", got, view.AssetPath)
	}
	_, theme, _ := served(t, view.Asset("theme.js"))
	if !strings.Contains(theme, "'arandu.theme'") {
		t.Error("the default brand did not keep the key theme.js has always used")
	}
}
