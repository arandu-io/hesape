package routing_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/pipeline"

	"github.com/arandu-io/hesape/routing"
)

// request stands in for the request context a controller action receives.
//
// The real one is hesape/hhttp.Context, and routing never names it: the type is
// a parameter, so a controller can be registered without this package knowing
// what an answer looks like. Declaring a local one here is not a shortcut for
// the test -- it is the property being tested.
type request struct {
	w http.ResponseWriter
	r *http.Request
}

// adapt is what hesape/hhttp.Action is: it builds the context, runs the action
// and decides what a returned error means. Here, "means" is a 500, which is
// enough to prove the error travelled.
func adapt(h func(*request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(&request{w: w, r: r}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

// invoices implements all seven actions, so Resource registers all seven.
type invoices struct{ seen *[]string }

func (c invoices) note(action string) error { *c.seen = append(*c.seen, action); return nil }

func (c invoices) Index(*request) error   { return c.note("index") }
func (c invoices) Create(*request) error  { return c.note("create") }
func (c invoices) Store(*request) error   { return c.note("store") }
func (c invoices) Show(*request) error    { return c.note("show") }
func (c invoices) Edit(*request) error    { return c.note("edit") }
func (c invoices) Update(*request) error  { return c.note("update") }
func (c invoices) Destroy(*request) error { return c.note("destroy") }

// TestResourceRegistersTheSevenRoutes is the whole point of the command:
// Resource produces the same seven routes, at the same paths, with the same
// names, every time.
//
// If any row of this table drifts, the promise that a developer recognizes
// the vocabulary immediately stops being true.
func TestResourceRegistersTheSevenRoutes(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Resource(r, "invoices", invoices{seen: &seen}, adapt)

	want := []struct {
		method, pattern, name string
	}{
		{"GET", "/invoices", "invoices.index"},
		{"GET", "/invoices/create", "invoices.create"},
		{"POST", "/invoices", "invoices.store"},
		{"GET", "/invoices/{id}", "invoices.show"},
		{"GET", "/invoices/{id}/edit", "invoices.edit"},
		{"PUT", "/invoices/{id}", "invoices.update"},
		{"PATCH", "/invoices/{id}", "invoices.update"},
		{"DELETE", "/invoices/{id}", "invoices.destroy"},
	}

	got := map[string]string{} // "METHOD pattern" -> name
	for _, route := range r.Routes() {
		got[route.Method+" "+route.Pattern] = route.RouteName()
	}

	for _, w := range want {
		key := w.method + " " + w.pattern
		name, registered := got[key]
		if !registered {
			t.Errorf("%s was not registered", key)
			continue
		}
		if name != w.name {
			t.Errorf("%s is named %q, want %q", key, name, w.name)
		}
	}

	// PATCH shows the name and is not indexed by it, so generating the URL has
	// one answer instead of two.
	if path, err := r.Table().Route("invoices.update", "42"); err != nil || path != "/invoices/42" {
		t.Errorf("Route(invoices.update, 42) = %q, %v; want /invoices/42", path, err)
	}
}

// listOnly implements two of the seven. Only the routes a controller
// implements are registered: a route that exists is a route that answers.
type listOnly struct{}

func (listOnly) Index(*request) error { return nil }
func (listOnly) Show(*request) error  { return nil }

func TestResourceRegistersOnlyWhatTheControllerImplements(t *testing.T) {
	r := routing.NewRouter()
	routing.Resource(r, "reports", listOnly{}, adapt)

	for _, route := range r.Routes() {
		switch route.RouteName() {
		case "reports.index", "reports.show":
		default:
			t.Errorf("registered %s %s (%s) for a controller that does not implement it",
				route.Method, route.Pattern, route.RouteName())
		}
	}
	if n := len(r.Routes()); n != 2 {
		t.Errorf("registered %d routes, want 2", n)
	}
}

// nothing implements none of the seven. Zero routes is a wiring mistake worth
// seeing in the route table, and it must not be a panic.
type nothing struct{}

func TestResourceOnAControllerThatImplementsNothingRegistersNothing(t *testing.T) {
	r := routing.NewRouter()
	if routes := routing.Resource(r, "ghosts", nothing{}, adapt); len(routes) != 0 {
		t.Fatalf("registered %d routes, want 0", len(routes))
	}
}

// TestTheResourceRoutesActuallyAnswer: registering metadata is not the same as
// wiring a handler, and a table test alone would not tell them apart.
func TestTheResourceRoutesActuallyAnswer(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Resource(r, "invoices", invoices{seen: &seen}, adapt)

	for _, call := range []struct{ method, path, action string }{
		{"GET", "/invoices", "index"},
		{"GET", "/invoices/create", "create"},
		{"POST", "/invoices", "store"},
		{"GET", "/invoices/42", "show"},
		{"GET", "/invoices/42/edit", "edit"},
		{"PUT", "/invoices/42", "update"},
		{"PATCH", "/invoices/42", "update"},
		{"DELETE", "/invoices/42", "destroy"},
	} {
		seen = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(call.method, call.path, nil))

		if len(seen) != 1 || seen[0] != call.action {
			t.Errorf("%s %s reached %v, want [%s]", call.method, call.path, seen, call.action)
		}
	}
}

// TestCreateIsNotReadAsAnID is the ambiguity every router has to resolve:
// /invoices/create and /invoices/{id} both match "create".
func TestCreateIsNotReadAsAnID(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Resource(r, "invoices", invoices{seen: &seen}, adapt)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices/create", nil))

	if len(seen) != 1 || seen[0] != "create" {
		t.Fatalf("GET /invoices/create reached %v, want [create] -- it was read as an id", seen)
	}
}

// TestAResourceInheritsItsGroup: prefix, name and middleware. A resource
// registered inside a guarded group that came out unguarded is authorization
// that looks wired and is not.
func TestAResourceInheritsItsGroup(t *testing.T) {
	r := routing.NewRouter()
	guarded := false
	admin := r.Group(routing.Group{
		Prefix: "/admin",
		Name:   "admin",
		Middleware: []pipeline.Middleware[http.Handler]{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					guarded = true
					next.ServeHTTP(w, req)
				})
			},
		},
	})
	routing.Resource(admin, "invoices", invoices{seen: new([]string)}, adapt)

	path, err := r.Table().Route("admin.invoices.show", "42")
	if err != nil {
		t.Fatalf("Route(admin.invoices.show): %v", err)
	}
	if path != "/admin/invoices/42" {
		t.Fatalf("Route(admin.invoices.show, 42) = %q, want /admin/invoices/42", path)
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	if !guarded {
		t.Fatal("the group's middleware did not run for a route registered by Resource")
	}
}

// broken returns an error from every action, which is what proves the adapter
// -- and not the router -- decides what a returned error means.
type broken struct{}

func (broken) Index(*request) error { return errors.New("the database is asleep") }

func TestWhatAnActionReturnsReachesTheAdapter(t *testing.T) {
	r := routing.NewRouter()
	routing.Resource(r, "reports", broken{}, adapt)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: the error did not reach the adapter", rec.Code)
	}
}
