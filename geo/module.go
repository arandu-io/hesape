package geo

import (
	"context"
	"net/http"

	"github.com/arandu-io/hesape/routing"
)

// Module exposes only the GEO surfaces selected by Config.
// Registering the module is opt-in; each route is independently opt-in again.
type Module struct {
	config  Config
	catalog Catalog
}

// NewModule creates a GEO module. Boot validates its configured public origin.
func NewModule(config Config, catalog Catalog) *Module {
	return &Module{config: config, catalog: catalog}
}

// Name is the stable module identifier.
func (*Module) Name() string { return "geo" }

// Boot validates configuration before any route is registered.
func (m *Module) Boot(context.Context) error {
	config, err := m.config.normalized()
	if err != nil {
		return err
	}
	m.config = config
	return nil
}

// Routes registers only explicitly enabled machine surfaces.
func (m *Module) Routes(r *routing.Router) {
	if !m.config.Enabled {
		return
	}
	if m.config.Surfaces.Has(Robots) {
		r.Get("/robots.txt", http.HandlerFunc(m.robots)).Name("robots")
	}
	if m.config.Surfaces.Has(Sitemap) {
		r.Get("/sitemap.xml", http.HandlerFunc(m.sitemap)).Name("sitemap")
	}
	if m.config.Surfaces.Has(LLMs) {
		r.Get("/llms.txt", http.HandlerFunc(m.llms)).Name("llms")
	}
	if m.config.Surfaces.Has(LLMsFull) {
		r.Get("/llms-full.txt", http.HandlerFunc(m.llmsFull)).Name("llms.full")
	}
}
