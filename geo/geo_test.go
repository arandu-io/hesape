package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/routing"
)

func bootModule(t *testing.T, cfg Config, docs []Document) (*Module, *routing.Router) {
	t.Helper()
	m := NewModule(cfg, CatalogFunc(func(context.Context) ([]Document, error) { return docs, nil }))
	if err := m.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := routing.NewRouter()
	m.Routes(r)
	return m, r
}

func request(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://attacker.invalid"+path, nil))
	return rec
}

func TestModuleRegistersOnlySelectedSurfaces(t *testing.T) {
	_, r := bootModule(t, Config{Enabled: true, Indexing: true, Origin: "https://example.test", Surfaces: Surfaces(Robots | LLMs)}, nil)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/robots.txt", http.StatusOK}, {"/llms.txt", http.StatusOK},
		{"/sitemap.xml", http.StatusNotFound}, {"/llms-full.txt", http.StatusNotFound},
	} {
		if got := request(t, r, tc.path).Code; got != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.path, got, tc.want)
		}
	}
}

func TestDisabledModuleRegistersNothing(t *testing.T) {
	_, r := bootModule(t, Config{Enabled: false, Surfaces: AllSurfaces}, nil)
	for _, path := range []string{"/robots.txt", "/sitemap.xml", "/llms.txt", "/llms-full.txt"} {
		if got := request(t, r, path).Code; got != http.StatusNotFound {
			t.Errorf("disabled GEO route %s = %d", path, got)
		}
	}
}

func TestIndexingDisabledFailsClosedWithoutCallingCatalog(t *testing.T) {
	calls := 0
	m := NewModule(Config{Enabled: true, Indexing: false, Surfaces: AllSurfaces}, CatalogFunc(func(context.Context) ([]Document, error) {
		calls++
		return []Document{{Path: "/private", Title: "Private"}}, nil
	}))
	if err := m.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := routing.NewRouter()
	m.Routes(r)
	if body := request(t, r, "/robots.txt").Body.String(); !strings.Contains(body, "Disallow: /") {
		t.Fatalf("robots did not fail closed: %s", body)
	}
	if body := request(t, r, "/sitemap.xml").Body.String(); strings.Contains(body, "<loc>") {
		t.Fatalf("disabled sitemap leaked a location: %s", body)
	}
	for _, path := range []string{"/llms.txt", "/llms-full.txt"} {
		if body := request(t, r, path).Body.String(); !strings.Contains(body, "Discovery disabled") {
			t.Fatalf("%s did not explain the disabled state: %s", path, body)
		}
	}
	if calls != 0 {
		t.Fatalf("disabled indexing called the catalog %d times", calls)
	}
}

func TestOneCatalogDrivesSearchAndGenerativeRepresentations(t *testing.T) {
	docs := []Document{
		{Path: "/z", Title: "Zeta [guide]", Description: "Last page", UpdatedAt: time.Date(2026, 9, 17, 12, 30, 0, 0, time.UTC), Content: "Public body Z\n## not-a-structure"},
		{Path: "/a", Title: "Alpha", Description: "First page", Content: "Public body A"},
	}
	_, r := bootModule(t, Config{Enabled: true, Indexing: true, Origin: "https://example.test", Language: "pt-BR", Surfaces: AllSurfaces}, docs)
	sitemap := request(t, r, "/sitemap.xml")
	if sitemap.Code != http.StatusOK || !strings.Contains(sitemap.Body.String(), "https://example.test/a") || !strings.Contains(sitemap.Body.String(), "2026-09-17T12:30:00Z") {
		t.Fatalf("unexpected sitemap: %s", sitemap.Body.String())
	}
	short := request(t, r, "/llms.txt")
	full := request(t, r, "/llms-full.txt")
	for _, body := range []string{short.Body.String(), full.Body.String()} {
		if !strings.Contains(body, "https://example.test/a") || !strings.Contains(body, "https://example.test/z") {
			t.Fatalf("representation diverged from catalog: %s", body)
		}
	}
	if strings.Index(short.Body.String(), "/a") > strings.Index(short.Body.String(), "/z") {
		t.Fatal("catalog output is not deterministic")
	}
	if !strings.Contains(short.Body.String(), `Zeta \[guide\]`) {
		t.Fatalf("markdown label was not escaped: %s", short.Body.String())
	}
	if !strings.Contains(full.Body.String(), "Public page content") || !strings.Contains(full.Body.String(), "> Public body A") || strings.Contains(full.Body.String(), "\n## not-a-structure") {
		t.Fatalf("expanded corpus omitted public content: %s", full.Body.String())
	}
	if got := full.Header().Get("Link"); got != `<https://example.test/llms.txt>; rel="describedby"` {
		t.Fatalf("describedby = %q", got)
	}
}

func TestConfiguredOriginCannotBePoisonedByRequestHost(t *testing.T) {
	_, r := bootModule(t, Config{Enabled: true, Indexing: true, Origin: "https://canonical.example", Surfaces: Surfaces(Sitemap | LLMs)}, []Document{{Path: "/page", Title: "Page"}})
	for _, path := range []string{"/sitemap.xml", "/llms.txt"} {
		body := request(t, r, path).Body.String()
		if strings.Contains(body, "attacker.invalid") || !strings.Contains(body, "https://canonical.example/page") {
			t.Fatalf("request host affected %s: %s", path, body)
		}
	}
}

func TestUnsafeOriginAndPathsFailClosed(t *testing.T) {
	for _, origin := range []string{"http://example.test", "https://user:secret@example.test", "https://example.test/path", "https://example.test/?secret=1"} {
		m := NewModule(Config{Enabled: true, Indexing: true, Origin: origin, Surfaces: AllSurfaces}, nil)
		if err := m.Boot(context.Background()); err == nil {
			t.Errorf("unsafe origin %q booted", origin)
		}
	}
	_, r := bootModule(t, Config{Enabled: true, Indexing: true, Origin: "https://example.test", Surfaces: Surfaces(Sitemap)}, []Document{{Path: "//evil.example/x", Title: "Bad"}})
	if got := request(t, r, "/sitemap.xml").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("unsafe page path = %d", got)
	}
}

func TestDuplicateAndOversizedCatalogsRefuseInsteadOfTruncating(t *testing.T) {
	cfg := Config{Enabled: true, Indexing: true, Origin: "https://example.test", Surfaces: AllSurfaces, MaxDocuments: 1}
	_, r := bootModule(t, cfg, []Document{{Path: "/a", Title: "A"}, {Path: "/b", Title: "B"}})
	if got := request(t, r, "/sitemap.xml").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("oversized catalog = %d", got)
	}
	cfg.MaxDocuments = 10
	_, r = bootModule(t, cfg, []Document{{Path: "/a", Title: "A"}, {Path: "/a", Title: "Again"}})
	if got := request(t, r, "/llms.txt").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("duplicate catalog = %d", got)
	}
}

func TestMetadataUsesConfiguredOriginAndEscapesStructuredData(t *testing.T) {
	cfg := Config{Enabled: true, Indexing: true, Origin: "https://example.test", Language: "pt-BR"}
	meta, err := MetadataFor(cfg, MetadataInput{
		Path: "/page", Title: `Title </script><script>alert(1)</script>`, Description: `A & B`,
		SiteName: "Example", Locale: "pt_BR", Indexable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Canonical != "https://example.test/page" || meta.Robots != "index, follow, max-image-preview:large" {
		t.Fatalf("metadata = %#v", meta)
	}
	if strings.Contains(string(meta.Schema), "</script>") || !strings.Contains(string(meta.Schema), `\u003c/script\u003e`) {
		t.Fatalf("structured data can close its inert script: %s", meta.Schema)
	}
	var decoded map[string]any
	if err := json.Unmarshal(meta.Schema, &decoded); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if strings.Contains(string(meta.SchemaMarkup()), "<script>alert") {
		t.Fatal("schema markup contains executable hostile text")
	}
	private, err := MetadataFor(cfg, MetadataInput{Path: "/account", Title: "Account", SiteName: "Example", Indexable: false})
	if err != nil {
		t.Fatal(err)
	}
	if private.Canonical != "" || private.Robots != "noindex, nofollow" || private.Schema != nil || private.SchemaMarkup() != "" {
		t.Fatalf("private metadata became discoverable: %#v", private)
	}
}

func TestOutputSizeLimitRefusesLargeBodies(t *testing.T) {
	_, r := bootModule(t, Config{
		Enabled: true, Indexing: true, Origin: "https://example.test",
		Surfaces: LLMsFull.AsSet(), MaxBytes: 64,
	}, []Document{{Path: "/large", Title: "Large", Content: strings.Repeat("x", 256)}})
	if got := request(t, r, "/llms-full.txt").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("large GEO response = %d", got)
	}
}
