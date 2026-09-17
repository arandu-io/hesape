// Package geo provides deterministic Search and Generative Engine Optimization
// surfaces for an Arandu application.
//
// The package owns representation, never publication policy. Applications pass
// only documents they have already decided may be public. A private record has
// no alternate path through this package to become visible.
package geo

import (
	"errors"
	"net/url"
	"strings"
)

// Surface identifies one machine-facing HTTP representation.
type Surface uint8

const (
	// Robots serves robots.txt.
	Robots Surface = 1 << iota
	// Sitemap serves sitemap.xml.
	Sitemap
	// LLMs serves the short model-facing index.
	LLMs
	// LLMsFull serves the expanded public corpus.
	LLMsFull
)

// Surfaces is a set of independently enabled GEO surfaces.
type Surfaces uint8

// Has reports whether surface is enabled.
func (s Surfaces) Has(surface Surface) bool { return uint8(s)&uint8(surface) != 0 }

// AsSet converts one surface into a set, useful for a single-feature configuration.
func (s Surface) AsSet() Surfaces { return Surfaces(s) }

// SearchSurfaces enables the traditional crawler documents.
const SearchSurfaces Surfaces = Surfaces(Robots | Sitemap)

// GenerativeSurfaces enables the model-facing documents.
const GenerativeSurfaces Surfaces = Surfaces(LLMs | LLMsFull)

// AllSurfaces enables every HTTP representation owned by this package.
const AllSurfaces Surfaces = SearchSurfaces | GenerativeSurfaces

// Config controls which surfaces exist and whether they expose public content.
type Config struct {
	// Enabled makes the module register its selected routes. When false, the
	// module is inert even if Surfaces contains values.
	Enabled bool
	// Indexing says public discovery is allowed in this deployment. When false,
	// robots denies crawling and the index documents expose no application pages.
	Indexing bool
	// Origin is the configured public origin. It is never inferred from Host or
	// forwarded request headers.
	Origin string
	// Surfaces selects the independently enabled routes.
	Surfaces Surfaces
	// Language is the BCP 47 language reported by text representations.
	Language string
	// IndexTitle is the heading of llms.txt. Empty uses "Public index".
	IndexTitle string
	// IndexSummary is optional prose under the llms.txt heading.
	IndexSummary string
	// CorpusTitle is the heading of llms-full.txt. Empty uses "Public corpus".
	CorpusTitle string
	// CorpusIntro is optional prose under the llms-full.txt heading.
	CorpusIntro string
	// Labels localizes the fixed words used by model-facing documents.
	Labels Labels
	// Robots customizes allow and deny prefixes when Indexing is true.
	Robots RobotsPolicy
	// SitemapStylesheet is an optional same-origin path to an XML stylesheet.
	SitemapStylesheet string
	// MaxDocuments refuses an unexpectedly large export rather than truncating it.
	MaxDocuments int
	// MaxBytes refuses a representation larger than this many bytes.
	MaxBytes int
}

// RobotsPolicy is the application's public crawler policy.
type RobotsPolicy struct {
	Allow    []string
	Disallow []string
}

// Labels is the localized fixed vocabulary of model-facing documents.
type Labels struct {
	Pages             string
	Optional          string
	CanonicalURL      string
	PublicPageContent string
	DisabledTitle     string
	DisabledIndex     string
	DisabledCorpus    string
}

var (
	// ErrInvalidConfig means the module cannot safely derive public URLs.
	ErrInvalidConfig = errors.New("geo: invalid configuration")
	// ErrCapacity means a catalog needs partitioning before it can be exported.
	ErrCapacity = errors.New("geo: export capacity exceeded")
)

const (
	defaultMaxDocuments = 10000
	defaultMaxBytes     = 8 << 20
)

func (c Config) normalized() (Config, error) {
	if !c.Enabled {
		return c, nil
	}
	if c.MaxDocuments <= 0 {
		c.MaxDocuments = defaultMaxDocuments
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = defaultMaxBytes
	}
	if c.Language == "" {
		c.Language = "en"
	}
	if strings.TrimSpace(c.IndexTitle) == "" {
		c.IndexTitle = "Public index"
	} else {
		c.IndexTitle = strings.Join(strings.Fields(c.IndexTitle), " ")
	}
	c.IndexSummary = strings.Join(strings.Fields(c.IndexSummary), " ")
	if strings.TrimSpace(c.CorpusTitle) == "" {
		c.CorpusTitle = "Public corpus"
	} else {
		c.CorpusTitle = strings.Join(strings.Fields(c.CorpusTitle), " ")
	}
	c.CorpusIntro = strings.Join(strings.Fields(c.CorpusIntro), " ")
	if strings.TrimSpace(c.Labels.Pages) == "" {
		c.Labels.Pages = "Pages"
	}
	if strings.TrimSpace(c.Labels.Optional) == "" {
		c.Labels.Optional = "Optional"
	}
	if strings.TrimSpace(c.Labels.CanonicalURL) == "" {
		c.Labels.CanonicalURL = "Canonical URL"
	}
	if strings.TrimSpace(c.Labels.PublicPageContent) == "" {
		c.Labels.PublicPageContent = "Public page content"
	}
	if strings.TrimSpace(c.Labels.DisabledTitle) == "" {
		c.Labels.DisabledTitle = "Discovery disabled"
	}
	if strings.TrimSpace(c.Labels.DisabledIndex) == "" {
		c.Labels.DisabledIndex = "This deployment does not publish a model-facing index."
	}
	if strings.TrimSpace(c.Labels.DisabledCorpus) == "" {
		c.Labels.DisabledCorpus = "This deployment does not publish an expanded model-facing corpus."
	}
	if c.Indexing {
		u, err := url.Parse(c.Origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return Config{}, ErrInvalidConfig
		}
		c.Origin = strings.TrimSuffix(u.String(), "/")
	}
	if c.SitemapStylesheet != "" && !safePath(c.SitemapStylesheet) {
		return Config{}, ErrInvalidConfig
	}
	for _, path := range append(append([]string{}, c.Robots.Allow...), c.Robots.Disallow...) {
		if !safePath(path) {
			return Config{}, ErrInvalidConfig
		}
	}
	return c, nil
}

func safePath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && !strings.ContainsAny(path, "?#\\\r\n")
}
