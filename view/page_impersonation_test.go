package view_test

import (
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/view"
)

// TestNewSaysAnOperatorIsImpersonatingSomebody: a page drawn for an operator
// acting in a customer's account says so, names the operator, and links the
// way back when the application registered it.
func TestNewSaysAnOperatorIsImpersonatingSomebody(t *testing.T) {
	r := routing.NewRouter()
	nothing := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Post("/impersonate/leave", nothing).Name("impersonate.leave")

	acting, err := auth.ImpersonateActing(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	var page view.Impersonation = view.New(contextFor(&acting, r.Table()), "Painel")
	if !page.IsImpersonating() || !page.ImpersonationWrites() || page.ImpersonatorID() != "op-1" || page.LeaveImpersonationLink() != "/impersonate/leave" {
		t.Fatalf("the page reads impersonating=%v writes=%v impersonator=%q leave=%q",
			page.IsImpersonating(), page.ImpersonationWrites(), page.ImpersonatorID(), page.LeaveImpersonationLink())
	}

	viewing, _ := auth.Impersonate(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if p := view.New(contextFor(&viewing, starterKitRoutes()), "Painel"); !p.IsImpersonating() || p.ImpersonationWrites() || p.LeaveImpersonationLink() != "" {
		t.Fatalf("a viewing page without the route reads %+v", p)
	}

	plain := auth.Subject{ID: "user-9", Tenant: "acme"}
	if p := view.New(contextFor(&plain, r.Table()), "Painel"); p.IsImpersonating() || p.ImpersonatorID() != "" || p.LeaveImpersonationLink() != "" {
		t.Fatalf("an ordinary page reads as an impersonation: %+v", p)
	}
}
