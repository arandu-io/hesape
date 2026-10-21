package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/routing/controllers"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

// The aliases, asserted at compile time: each interface here is the generic
// one of hesape/routing instantiated on hesape/http.Context, and the adapter
// the router builds is the parameter hesape/routing takes.
var (
	_ routing.Adapter[hhttp.Context]   = hhttp.NewAdapter(nil, nil, nil)
	_ controllers.Indexer              = routing.Indexer[hhttp.Context](nil)
	_ routing.Destroyer[hhttp.Context] = controllers.Destroyer(nil)
	_ controllers.Indexer              = (*sevenActions)(nil)
	_ controllers.Creator              = (*sevenActions)(nil)
	_ controllers.Storer               = (*sevenActions)(nil)
	_ controllers.Shower               = (*sevenActions)(nil)
	_ controllers.Editor               = (*sevenActions)(nil)
	_ controllers.Updater              = (*sevenActions)(nil)
	_ controllers.Destroyer            = (*sevenActions)(nil)
	_ controllers.Invoker              = routing.Invoker[hhttp.Context](nil)
	_ routing.Invoker[hhttp.Context]   = controllers.Invoker(nil)
)

// recordingRenderer records what an action asked to draw.
type recordingRenderer struct {
	name   string
	status int
}

func (r *recordingRenderer) Render(_ context.Context, w http.ResponseWriter, status int, name string, _ any) error {
	r.name, r.status = name, status
	w.WriteHeader(status)
	return nil
}

// sevenActions implements every one of the seven action interfaces.
type sevenActions struct{ seen []string }

func (c *sevenActions) Index(*hhttp.Context) error   { c.seen = append(c.seen, "index"); return nil }
func (c *sevenActions) Create(*hhttp.Context) error  { c.seen = append(c.seen, "create"); return nil }
func (c *sevenActions) Store(*hhttp.Context) error   { c.seen = append(c.seen, "store"); return nil }
func (c *sevenActions) Show(*hhttp.Context) error    { c.seen = append(c.seen, "show"); return nil }
func (c *sevenActions) Edit(*hhttp.Context) error    { c.seen = append(c.seen, "edit"); return nil }
func (c *sevenActions) Update(*hhttp.Context) error  { c.seen = append(c.seen, "update"); return nil }
func (c *sevenActions) Destroy(*hhttp.Context) error { c.seen = append(c.seen, "destroy"); return nil }

func TestAControllerImplementingAllSevenActionsRegistersSevenRoutes(t *testing.T) {
	r := controllers.NewRouter()
	routes := r.Resource("invoices", &sevenActions{})

	if len(routes) != 7 {
		t.Fatalf("Resource registered %d routes, want 7", len(routes))
	}
	want := []string{
		"invoices.index", "invoices.create", "invoices.store",
		"invoices.show", "invoices.edit", "invoices.update", "invoices.destroy",
	}
	for i, route := range routes {
		if route.RouteName() != want[i] {
			t.Errorf("route %d is named %q, want %q", i, route.RouteName(), want[i])
		}
	}
}

func TestAnActionIsCalledWithTheRendererTheRouterWasWiredWith(t *testing.T) {
	rd := &recordingRenderer{}
	r := controllers.NewRouter().WithRenderer(rd)
	r.Action(http.MethodGet, "/dashboard", func(ctx *hhttp.Context) error {
		return ctx.View("dashboard/index", struct{}{})
	}).Name("dashboard")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if rd.name != "dashboard/index" {
		t.Errorf("the renderer was asked for %q, want %q", rd.name, "dashboard/index")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// An action reads the route table through Context.URL, which is the table the
// router holds.
func TestAnActionBuildsAPathFromTheRoutersTable(t *testing.T) {
	r := controllers.NewRouter()
	r.Get("/invoices/{id}", func(http.ResponseWriter, *http.Request) {}).Name("invoices.show")
	var got string
	r.Action(http.MethodGet, "/here", func(ctx *hhttp.Context) error {
		got = ctx.URL("invoices.show", "7")
		return nil
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/here", nil))

	if got != "/invoices/7" {
		t.Errorf("Context.URL = %q, want %q", got, "/invoices/7")
	}
}

func TestURLAnswersWhatTheRoutingTableAnswers(t *testing.T) {
	r := controllers.NewRouter()
	r.Get("/invoices/{id}", func(http.ResponseWriter, *http.Request) {}).Name("invoices.show")

	got, err := r.Table().URL("invoices.show", "42")
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if got != "/invoices/42" {
		t.Errorf("URL = %q, want %q", got, "/invoices/42")
	}
	if must := r.Table().Must("invoices.show", "42"); must != got {
		t.Errorf("Must = %q, want %q", must, got)
	}
	if all := r.Table().All(); len(all) != 1 {
		t.Errorf("All returned %d routes, want 1", len(all))
	}
	if direct, _ := r.Routing().Table().Route("invoices.show", "42"); direct != got {
		t.Errorf("the routing table answered %q, the router's %q", direct, got)
	}
}

func TestTheActionARouteRequiresIsDeclaredOnTheReturnedRoute(t *testing.T) {
	r := controllers.NewRouter()

	route := r.Get("/invoices", func(http.ResponseWriter, *http.Request) {}).
		Name("invoices.index").
		Can(auth.Action("invoice.list"))

	if got := route.RequiredAction(); got != auth.Action("invoice.list") {
		t.Errorf("RequiredAction = %q, want %q", got, "invoice.list")
	}
}

func TestAnUnknownRouteNameIsAnError(t *testing.T) {
	if _, err := controllers.NewRouter().Table().URL("nope"); err == nil {
		t.Fatal("URL of an unnamed route returned no error")
	}
}

func TestAGroupAppliesItsPrefixAndItsMiddleware(t *testing.T) {
	var wrapped bool
	mark := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped = true
			next.ServeHTTP(w, r)
		})
	}

	r := controllers.NewRouter()
	admin := r.Group("/admin", mark)
	admin.Get("/reports", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/reports", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d: the group prefix did not reach the mux", rec.Code, http.StatusTeapot)
	}
	if !wrapped {
		t.Error("the group middleware did not run")
	}
}

func TestForModuleTagsTheRoutesItRegisters(t *testing.T) {
	r := controllers.NewRouter()
	r.ForModule("invoices").Get("/invoices", func(http.ResponseWriter, *http.Request) {})

	all := r.Routes()
	if len(all) != 1 {
		t.Fatalf("Routes returned %d, want 1", len(all))
	}
	if all[0].Module != "invoices" {
		t.Errorf("Module = %q, want %q", all[0].Module, "invoices")
	}
}

// Routing hands a handler-only module the same table, under the same scope.
func TestRoutingIsTheSameTableUnderTheSameScope(t *testing.T) {
	r := controllers.NewRouter()
	r.ForModule("geo").Group("/geo").Routing().Get("/robots.txt", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).Name("geo.robots")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/geo/robots.txt", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	all := r.Routes()
	if len(all) != 1 || all[0].Module != "geo" {
		t.Fatalf("Routes = %v, want the one route tagged geo", all)
	}
	if path, err := r.Table().URL("geo.robots"); err != nil || path != "/geo/robots.txt" {
		t.Errorf("URL = %q, %v", path, err)
	}
}

func TestARejectionIsFlashedAndRedirectedBack(t *testing.T) {
	flash := session.NewFlash(make([]byte, 32), false)
	r := controllers.NewRouter().WithFlash(flash)
	r.Action(http.MethodPost, "/invoices", func(*hhttp.Context) error {
		return validation.Errors{"title": {"this field is required"}}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/invoices", nil)
	req.Header.Set("Referer", "http://example.test/invoices/create")
	req.Host = "example.test"
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if to := rec.Header().Get("Location"); to != "/invoices/create" {
		t.Errorf("Location = %q, want %q", to, "/invoices/create")
	}

	next := httptest.NewRequest(http.MethodGet, "/invoices/create", nil)
	next.Header.Set("Accept", "text/html")
	for _, c := range rec.Result().Cookies() {
		next.AddCookie(c)
	}
	errs, _, ok := flash.Take(httptest.NewRecorder(), next)
	if !ok {
		t.Fatal("the flash carried nothing back")
	}
	if got := errs["title"]; len(got) != 1 || got[0] != "this field is required" {
		t.Errorf("the messages came back as %v", errs)
	}
}

func TestAnEmptyErrorSetReturnedAsAnErrorIsRefusedLoudly(t *testing.T) {
	defer func() {
		v := recover()
		if v == nil {
			t.Fatal("an empty validation.Errors was accepted")
		}
		if msg, _ := v.(string); !strings.Contains(msg, "empty validation.Errors") {
			t.Errorf("panicked with %v", v)
		}
	}()

	r := controllers.NewRouter().WithFlash(session.NewFlash(make([]byte, 32), false))
	r.Action(http.MethodPost, "/invoices", func(*hhttp.Context) error {
		return validation.Errors{}
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/invoices", nil))
}
