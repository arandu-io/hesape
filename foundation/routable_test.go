package foundation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/foundation"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing/controllers"
)

// accounts is a module whose routes are a controller action, registered on
// the router an application registers its own controllers on.
type accounts struct{}

func (accounts) Name() string { return "accounts" }

func (accounts) Routes(r *controllers.Router) {
	r.Action(http.MethodGet, "/accounts/{id}", func(*hhttp.Context) error {
		return auth.ErrForbidden
	}).Name("accounts.show")
}

// outbox has no HTTP surface, so it declares no routes at all.
type outbox struct{}

func (outbox) Name() string { return "outbox" }

var _ foundation.Routable = accounts{}

// TestARoutableModuleRegistersAControllerActionAnsweredByTheAdapter: the
// action's refusal is answered with its status, which only the adapter the
// router holds does -- a handler registered by hand would have panicked.
func TestARoutableModuleRegistersAControllerActionAnsweredByTheAdapter(t *testing.T) {
	t.Parallel()

	r := controllers.NewRouter()
	for _, m := range []any{accounts{}, outbox{}} {
		named := m.(interface{ Name() string })
		if routable, ok := m.(foundation.Routable); ok {
			routable.Routes(r.ForModule(named.Name()))
		}
	}

	routes := r.Routes()
	if len(routes) != 1 || routes[0].Module != "accounts" {
		t.Fatalf("routes = %v, want the one route of accounts, tagged with its name", routes)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/accounts/7", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 from the adapter", rec.Code)
	}
}

// TestAModuleWithNoRoutesIsNotRoutable: the absence of routes is said by not
// implementing the interface.
func TestAModuleWithNoRoutesIsNotRoutable(t *testing.T) {
	t.Parallel()

	if _, ok := any(outbox{}).(foundation.Routable); ok {
		t.Fatal("a module with no Routes method satisfied Routable")
	}
}
