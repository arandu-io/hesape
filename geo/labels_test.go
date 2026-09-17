package geo

import (
	"strings"
	"testing"
)

func TestModelFacingDocumentsUseApplicationVocabulary(t *testing.T) {
	cfg := Config{
		Enabled: true, Indexing: true, Origin: "https://example.test", Language: "pt-BR", Surfaces: GenerativeSurfaces,
		IndexTitle: "Example", IndexSummary: "Conteúdo público autorizado.",
		CorpusTitle: "Example — conteúdo público", CorpusIntro: "Somente conteúdo publicado.",
		Labels: Labels{Pages: "Páginas", Optional: "Opcional", CanonicalURL: "URL canônica", PublicPageContent: "Conteúdo público"},
	}
	_, r := bootModule(t, cfg, []Document{{Path: "/guia", Title: "Guia", Content: "Texto público"}})
	short := request(t, r, "/llms.txt").Body.String()
	full := request(t, r, "/llms-full.txt").Body.String()
	for _, want := range []string{"# Example", "Conteúdo público autorizado.", "## Páginas", "## Opcional", "Example — conteúdo público"} {
		if !strings.Contains(short, want) {
			t.Fatalf("llms.txt lacks %q: %s", want, short)
		}
	}
	for _, want := range []string{"# Example — conteúdo público", "Somente conteúdo publicado.", "URL canônica:", "### Conteúdo público"} {
		if !strings.Contains(full, want) {
			t.Fatalf("llms-full.txt lacks %q: %s", want, full)
		}
	}
}
