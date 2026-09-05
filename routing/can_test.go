package routing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/routing"
)

// answer serves one request against the router and reports the status.
func answer(r *routing.Router, method, path string) int {
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder.Code
}

func TestCanDeclaresTheActionARouteRequires(t *testing.T) {
	r := routing.NewRouter()
	route := r.Delete("/invoices/{id}", ok).Name("invoices.destroy").Can("invoice.delete")

	if got := route.RequiredAction(); got != auth.Action("invoice.delete") {
		t.Errorf("RequiredAction() = %q, want invoice.delete", got)
	}
}

func TestARouteThatDeclaresNothingRequiresNothing(t *testing.T) {
	r := routing.NewRouter()
	route := r.Get("/", ok)

	if got := route.RequiredAction(); got != "" {
		t.Errorf("RequiredAction() = %q, want empty", got)
	}
	if got := (*routing.Route)(nil).RequiredAction(); got != "" {
		t.Errorf("a nil route answered %q", got)
	}
	if (*routing.Route)(nil).Can("a.b") != nil {
		t.Error("Can on a nil route returned something")
	}
}

// TestCanDeclaresAndDoesNotDecide. The route answers exactly as it would
// without the declaration: authorization is the handler's call to the policy,
// and a catalogue entry is not a guard.
func TestCanDeclaresAndDoesNotDecide(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices", ok).Can("invoice.list")

	if status := answer(r, http.MethodGet, "/invoices"); status != http.StatusOK {
		t.Errorf("status = %d, want %d -- Can must not answer for the handler", status, http.StatusOK)
	}
}

// TestOneRouteRequiresOneAction. Grant.Check compares one action, so a second
// declaration replaces the first rather than adding to it.
func TestOneRouteRequiresOneAction(t *testing.T) {
	r := routing.NewRouter()
	route := r.Get("/invoices", ok).Can("invoice.list").Can("invoice.read")

	if got := route.RequiredAction(); got != auth.Action("invoice.read") {
		t.Errorf("RequiredAction() = %q, want invoice.read", got)
	}
}

func TestTheSiblingsOfOneRegistrationCarryTheDeclaration(t *testing.T) {
	r := routing.NewRouter()
	r.Match([]string{http.MethodPut, http.MethodPatch}, "/invoices/{id}", ok).Can("invoice.update")

	for _, route := range r.Routes() {
		if got := route.RequiredAction(); got != auth.Action("invoice.update") {
			t.Errorf("%s %s requires %q, want invoice.update", route.Method, route.URI(), got)
		}
	}
}

// TestRequirementsAreTheCatalogue. The router is the enumeration: a list kept
// beside it would offer a permission no route requires the first time somebody
// added a route without remembering it.
func TestRequirementsAreTheCatalogue(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/", ok).Name("home")
	r.Get("/invoices", ok).Name("invoices.index").Can("invoice.list")
	r.Post("/invoices", ok).Name("invoices.store").Can("invoice.create")
	r.Match([]string{http.MethodPut, http.MethodPatch}, "/invoices/{id}", ok).
		Name("invoices.update").Can("invoice.update")

	got := r.Table().Requirements()
	if len(got) != 3 {
		t.Fatalf("got %d requirements, want 3: %+v", len(got), got)
	}

	want := []auth.Action{"invoice.list", "invoice.create", "invoice.update"}
	for i, action := range want {
		if got[i].Action != action {
			t.Errorf("requirement %d = %q, want %q", i, got[i].Action, action)
		}
		if got[i].Route == nil {
			t.Fatalf("requirement %d carries no route", i)
		}
	}
	if got[0].Route.RouteName() != "invoices.index" {
		t.Errorf("requirement 0 is %q", got[0].Route.RouteName())
	}
	// The one requirement of a two-verb registration answers for both verbs.
	if methods := got[2].Route.Methods(); len(methods) != 2 {
		t.Errorf("the update requirement answers %v, want both verbs", methods)
	}
	if got := (*routing.Routes)(nil).Requirements(); got != nil {
		t.Errorf("a nil table answered %v", got)
	}
}
