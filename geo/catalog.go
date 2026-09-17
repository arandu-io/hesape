package geo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Document is one public canonical page offered to search and generative engines.
// Content is optional plain public prose for llms-full.txt. It is not HTML and
// must never contain private fields or a database dump.
type Document struct {
	Path        string
	Title       string
	Description string
	UpdatedAt   time.Time
	Content     string
}

// Catalog supplies the public documents selected by the application domain.
// Implementations remain responsible for their normal Policy and Grant checks.
type Catalog interface {
	Documents(context.Context) ([]Document, error)
}

// CatalogFunc adapts a function into a Catalog.
type CatalogFunc func(context.Context) ([]Document, error)

// Documents calls f.
func (f CatalogFunc) Documents(ctx context.Context) ([]Document, error) { return f(ctx) }

type resolvedDocument struct {
	Document
	URL string
}

func resolveCatalog(ctx context.Context, cfg Config, catalog Catalog) ([]resolvedDocument, error) {
	if !cfg.Indexing || catalog == nil {
		return nil, nil
	}
	docs, err := catalog.Documents(ctx)
	if err != nil {
		return nil, err
	}
	if len(docs) > cfg.MaxDocuments {
		return nil, ErrCapacity
	}
	seen := make(map[string]struct{}, len(docs))
	out := make([]resolvedDocument, 0, len(docs))
	for _, doc := range docs {
		if !safePath(doc.Path) || strings.TrimSpace(doc.Title) == "" {
			return nil, fmt.Errorf("%w: document %q", ErrInvalidConfig, doc.Path)
		}
		if _, duplicate := seen[doc.Path]; duplicate {
			return nil, fmt.Errorf("%w: duplicate document %q", ErrInvalidConfig, doc.Path)
		}
		seen[doc.Path] = struct{}{}
		doc.Title = strings.Join(strings.Fields(doc.Title), " ")
		doc.Description = strings.Join(strings.Fields(doc.Description), " ")
		doc.Content = normalizeText(doc.Content)
		out = append(out, resolvedDocument{Document: doc, URL: cfg.Origin + doc.Path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func normalizeText(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
