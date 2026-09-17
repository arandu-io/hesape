package geo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/geo"
)

func TestHTTPRoutesExposeTheSelectedNativeHandlers(t *testing.T) {
	module := geo.NewModule(geo.Config{
		Enabled: true, Indexing: false,
		Surfaces: geo.Robots.AsSet() | geo.LLMs.AsSet(),
	}, nil)
	if err := module.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes := module.HTTPRoutes()
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	for i, want := range []struct{ path, name string }{{"/robots.txt", "robots"}, {"/llms.txt", "llms"}} {
		if routes[i].Method != http.MethodGet || routes[i].Path != want.path || routes[i].Name != want.name || routes[i].Handler == nil {
			t.Fatalf("route %d = %#v", i, routes[i])
		}
	}

	for _, route := range routes {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://attacker.invalid"+route.Path, nil)
		route.Handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s handler = %d", route.Path, recorder.Code)
		}
	}
}

func TestHTTPRoutesAreEmptyWhenTheModuleIsDisabled(t *testing.T) {
	module := geo.NewModule(geo.Config{Enabled: false, Surfaces: geo.AllSurfaces}, nil)
	if err := module.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if routes := module.HTTPRoutes(); len(routes) != 0 {
		t.Fatalf("disabled routes = %d", len(routes))
	}
}
