package geo

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
)

type sitemapEntry struct {
	Loc          string `xml:"loc"`
	LastModified string `xml:"lastmod,omitempty"`
}

type sitemapDocument struct {
	XMLName xml.Name       `xml:"urlset"`
	Xmlns   string         `xml:"xmlns,attr"`
	URLs    []sitemapEntry `xml:"url"`
}

func (m *Module) documents(ctx context.Context) ([]resolvedDocument, error) {
	return resolveCatalog(ctx, m.config, m.catalog)
}

func (m *Module) robots(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	if !m.config.Indexing {
		b.WriteString("Disallow: /\n")
		m.write(w, "text/plain; charset=utf-8", b.String())
		return
	}
	if len(m.config.Robots.Allow) == 0 && len(m.config.Robots.Disallow) == 0 {
		b.WriteString("Allow: /\n")
	}
	for _, path := range m.config.Robots.Allow {
		b.WriteString("Allow: " + path + "\n")
	}
	for _, path := range m.config.Robots.Disallow {
		b.WriteString("Disallow: " + path + "\n")
	}
	if m.config.Surfaces.Has(Sitemap) {
		b.WriteString("\nSitemap: " + m.config.Origin + "/sitemap.xml\n")
	}
	m.write(w, "text/plain; charset=utf-8", b.String())
}

func (m *Module) sitemap(w http.ResponseWriter, r *http.Request) {
	docs, err := m.documents(r.Context())
	if err != nil {
		m.failure(w, err)
		return
	}
	set := sitemapDocument{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, doc := range docs {
		last := ""
		if !doc.UpdatedAt.IsZero() {
			last = doc.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		set.URLs = append(set.URLs, sitemapEntry{Loc: doc.URL, LastModified: last})
	}
	body, err := xml.MarshalIndent(set, "", "\t")
	if err != nil {
		m.failure(w, err)
		return
	}
	var out bytes.Buffer
	out.WriteString(xml.Header)
	if m.config.SitemapStylesheet != "" {
		out.WriteString(`<?xml-stylesheet type="text/css" href="` + xmlEscape(m.config.SitemapStylesheet) + `"?>` + "\n")
	}
	out.Write(body)
	out.WriteByte('\n')
	m.write(w, "application/xml; charset=utf-8", out.String())
}

func (m *Module) llms(w http.ResponseWriter, r *http.Request) {
	docs, err := m.documents(r.Context())
	if err != nil {
		m.failure(w, err)
		return
	}
	if !m.config.Indexing {
		m.write(w, "text/plain; charset=utf-8", "# "+markdownLabel(m.config.Labels.DisabledTitle)+"\n\n> "+markdownText(m.config.Labels.DisabledIndex)+"\n")
		return
	}
	var b strings.Builder
	b.WriteString("# " + markdownLabel(m.config.IndexTitle) + "\n\n")
	if m.config.IndexSummary != "" {
		b.WriteString("> " + markdownText(m.config.IndexSummary) + "\n\n")
	}
	b.WriteString("## " + markdownLabel(m.config.Labels.Pages) + "\n\n")
	for _, doc := range docs {
		b.WriteString("- [" + markdownLabel(doc.Title) + "](" + doc.URL + ")")
		if doc.Description != "" {
			b.WriteString(": " + markdownText(doc.Description))
		}
		b.WriteByte('\n')
	}
	if m.config.Surfaces.Has(LLMsFull) {
		b.WriteString("\n## " + markdownLabel(m.config.Labels.Optional) + "\n\n- [" + markdownLabel(m.config.CorpusTitle) + "](" + m.config.Origin + "/llms-full.txt)\n")
	}
	m.write(w, "text/plain; charset=utf-8", b.String())
}

func (m *Module) llmsFull(w http.ResponseWriter, r *http.Request) {
	docs, err := m.documents(r.Context())
	if err != nil {
		m.failure(w, err)
		return
	}
	if !m.config.Indexing {
		m.write(w, "text/plain; charset=utf-8", "# "+markdownLabel(m.config.Labels.DisabledTitle)+"\n\n> "+markdownText(m.config.Labels.DisabledCorpus)+"\n")
		return
	}
	var b strings.Builder
	b.WriteString("# " + markdownLabel(m.config.CorpusTitle) + "\n")
	if m.config.CorpusIntro != "" {
		b.WriteString("\n" + markdownText(m.config.CorpusIntro) + "\n")
	}
	for _, doc := range docs {
		b.WriteString("\n## " + markdownLabel(doc.Title) + "\n\n" + markdownLabel(m.config.Labels.CanonicalURL) + ": " + doc.URL + "\n")
		if doc.Description != "" {
			b.WriteString("\n" + doc.Description + "\n")
		}
		if doc.Content != "" {
			b.WriteString("\n### " + markdownLabel(m.config.Labels.PublicPageContent) + "\n\n" + markdownQuote(doc.Content) + "\n")
		}
	}
	if m.config.Surfaces.Has(LLMs) {
		w.Header().Set("Link", "<"+m.config.Origin+"/llms.txt>; rel=\"describedby\"")
	}
	m.write(w, "text/plain; charset=utf-8", b.String())
}

func (m *Module) write(w http.ResponseWriter, contentType, body string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Language", m.config.Language)
	if len(body) > m.config.MaxBytes {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (m *Module) failure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	status := http.StatusInternalServerError
	if errors.Is(err, ErrCapacity) || errors.Is(err, ErrInvalidConfig) {
		status = http.StatusServiceUnavailable
	}
	http.Error(w, http.StatusText(status), status)
}

func markdownLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ").Replace(value)
}

func markdownText(value string) string {
	return markdownLabel(strings.Join(strings.Fields(value), " "))
}

func markdownQuote(value string) string {
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func xmlEscape(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
