package view_test

import (
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/view"
)

// TestNewSaysAnOperatorIsInsideSomebodysAccount: a page drawn for an operator
// acting in a customer's account says so, names the operator, and links the
// way back when the application registered it.
func TestNewSaysAnOperatorIsInsideSomebodysAccount(t *testing.T) {
	r := routing.NewRouter()
	nothing := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Post("/inspect/leave", nothing).Name("inspect.leave")

	acting, err := auth.ImpersonateActing(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	var page view.Inspection = view.New(contextFor(&acting, r.Table()), "Painel")
	if !page.IsInspecting() || !page.InspectionWrites() || page.InspectorID() != "op-1" || page.LeaveInspectionLink() != "/inspect/leave" {
		t.Fatalf("the page reads inspecting=%v writes=%v inspector=%q leave=%q",
			page.IsInspecting(), page.InspectionWrites(), page.InspectorID(), page.LeaveInspectionLink())
	}

	viewing, _ := auth.Impersonate(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if p := view.New(contextFor(&viewing, starterKitRoutes()), "Painel"); !p.IsInspecting() || p.InspectionWrites() || p.LeaveInspectionLink() != "" {
		t.Fatalf("a viewing page without the route reads %+v", p)
	}

	plain := auth.Subject{ID: "user-9", Tenant: "acme"}
	if p := view.New(contextFor(&plain, r.Table()), "Painel"); p.IsInspecting() || p.InspectorID() != "" || p.LeaveInspectionLink() != "" {
		t.Fatalf("an ordinary page reads as an inspection: %+v", p)
	}
}
