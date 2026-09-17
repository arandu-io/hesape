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

// HTTPRoute is one GET endpoint owned by the GEO module.
//
// It exists so a framework router envelope can register the same handler without
// reimplementing any GEO behavior. Method is currently always net/http.MethodGet.
type HTTPRoute struct {
	Method  string
	Path    string
	Name    string
	Handler http.Handler
}

// HTTPRoutes returns the selected machine-facing endpoints in stable order.
// Disabled surfaces are absent rather than registered behind a runtime branch.
func (m *Module) HTTPRoutes() []HTTPRoute {
	if !m.config.Enabled {
		return nil
	}
	routes := make([]HTTPRoute, 0, 4)
	if m.config.Surfaces.Has(Robots) {
		routes = append(routes, HTTPRoute{Method: http.MethodGet, Path: "/robots.txt", Name: "robots", Handler: http.HandlerFunc(m.robots)})
	}
	if m.config.Surfaces.Has(Sitemap) {
		routes = append(routes, HTTPRoute{Method: http.MethodGet, Path: "/sitemap.xml", Name: "sitemap", Handler: http.HandlerFunc(m.sitemap)})
	}
	if m.config.Surfaces.Has(LLMs) {
		routes = append(routes, HTTPRoute{Method: http.MethodGet, Path: "/llms.txt", Name: "llms", Handler: http.HandlerFunc(m.llms)})
	}
	if m.config.Surfaces.Has(LLMsFull) {
		routes = append(routes, HTTPRoute{Method: http.MethodGet, Path: "/llms-full.txt", Name: "llms.full", Handler: http.HandlerFunc(m.llmsFull)})
	}
	return routes
}

// Routes registers only explicitly enabled machine surfaces.
func (m *Module) Routes(r *routing.Router) {
	for _, route := range m.HTTPRoutes() {
		r.Get(route.Path, route.Handler).Name(route.Name)
	}
}
