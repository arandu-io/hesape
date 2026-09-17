package geo

import (
	"encoding/json"
	"html/template"
)

// Metadata is the page state shared by SSR and enhanced navigation.
type Metadata struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Canonical   string          `json:"canonical"`
	Language    string          `json:"language"`
	Locale      string          `json:"locale"`
	Locales     []string        `json:"locales,omitempty"`
	Alternates  []Alternate     `json:"alternates,omitempty"`
	Robots      string          `json:"robots"`
	Image       string          `json:"image"`
	ImageWidth  int             `json:"imageWidth,omitempty"`
	ImageHeight int             `json:"imageHeight,omitempty"`
	ImageType   string          `json:"imageType,omitempty"`
	ImageAlt    string          `json:"imageAlt,omitempty"`
	TwitterCard string          `json:"twitterCard,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// Alternate is one reciprocal language variant of a canonical page.
type Alternate struct {
	Tag string `json:"Tag"`
	URL string `json:"URL"`
}

// AlternateInput lets an application describe an alternate without supplying an origin.
type AlternateInput struct {
	Tag  string
	Path string
}

// MetadataInput is the application-owned presentation of one public or private page.
type MetadataInput struct {
	Path                string
	Title               string
	Description         string
	Language            string
	Locale              string
	OpenGraphAlternates []string
	Alternates          []AlternateInput
	SocialImagePath     string
	SocialImageWidth    int
	SocialImageHeight   int
	SocialImageType     string
	SocialImageAlt      string
	TwitterCard         string
	SiteName            string
	Indexable           bool
}

// MetadataFor creates canonical metadata without reading request host headers.
func MetadataFor(cfg Config, input MetadataInput) (Metadata, error) {
	normalized, err := cfg.normalized()
	if err != nil {
		return Metadata{}, err
	}
	meta := Metadata{
		Title: input.Title, Description: input.Description, Language: input.Language,
		Locale: input.Locale, Robots: "noindex, nofollow",
	}
	if meta.Language == "" {
		meta.Language = normalized.Language
	}
	if input.Indexable && normalized.Enabled && normalized.Indexing {
		if !safePath(input.Path) {
			return Metadata{}, ErrInvalidConfig
		}
		meta.Canonical = normalized.Origin + input.Path
		meta.Robots = "index, follow, max-image-preview:large"
		for _, alternate := range input.Alternates {
			if alternate.Tag == "" || !safePath(alternate.Path) {
				return Metadata{}, ErrInvalidConfig
			}
			meta.Alternates = append(meta.Alternates, Alternate{Tag: alternate.Tag, URL: normalized.Origin + alternate.Path})
		}
		meta.Locales = append([]string(nil), input.OpenGraphAlternates...)
		if input.SocialImagePath != "" {
			if !safePath(input.SocialImagePath) {
				return Metadata{}, ErrInvalidConfig
			}
			meta.Image = normalized.Origin + input.SocialImagePath
			meta.ImageWidth, meta.ImageHeight = input.SocialImageWidth, input.SocialImageHeight
			meta.ImageType, meta.ImageAlt, meta.TwitterCard = input.SocialImageType, input.SocialImageAlt, input.TwitterCard
		}
	}
	if meta.Canonical == "" {
		return meta, nil
	}
	root := normalized.Origin
	type entity struct {
		Type        string `json:"@type"`
		ID          string `json:"@id"`
		Name        string `json:"name"`
		URL         string `json:"url,omitempty"`
		Description string `json:"description,omitempty"`
		Language    string `json:"inLanguage,omitempty"`
	}
	body, _ := json.Marshal(struct {
		Context string   `json:"@context"`
		Graph   []entity `json:"@graph"`
	}{"https://schema.org", []entity{
		{Type: "WebSite", ID: root + "#website", Name: input.SiteName, URL: root},
		{Type: "WebPage", ID: meta.Canonical + "#webpage", Name: input.Title, URL: meta.Canonical, Description: input.Description, Language: meta.Language},
	}})
	meta.Schema = body
	return meta, nil
}

// JSON serializes metadata for a same-origin navigation behavior.
func (m Metadata) JSON() string {
	body, _ := json.Marshal(m)
	return string(body)
}

// SchemaMarkup returns an inert JSON-LD data block.
func (m Metadata) SchemaMarkup() template.HTML {
	if len(m.Schema) == 0 {
		return ""
	}
	return template.HTML(`<script type="application/ld+json" data-page-schema>` + string(m.Schema) + `</script>`)
}
