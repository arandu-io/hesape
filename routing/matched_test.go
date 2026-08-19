package routing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/routing"
)

// TestMatchedFiresWithTheRouteAndTheRequest. Matched is the one way to observe
// that a route answered, and a listener that is handed the route can read
// everything about it -- the name, the action, the pattern, the middleware --
// rather than whatever a fixed event struct chose to carry.
func TestMatchedFiresWithTheRouteAndTheRequest(t *testing.T) {
	r := routing.NewRouter()

	var gotRoute *routing.Route
	var gotPath string
	r.Matched(func(rt *routing.Route, req *http.Request) {
		gotRoute = rt
		gotPath = req.URL.Path
	})

	r.Get("/invoices/{id}", ok).Name("invoices.show")
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices/42", nil))

	if gotRoute == nil {
		t.Fatal("the matched listener did not fire")
	}
	if got := gotRoute.RouteName(); got != "invoices.show" {
		t.Errorf("the listener got the wrong route: %q", got)
	}
	if gotRoute.URI() != "/invoices/{id}" {
		t.Errorf("the listener got the wrong pattern: %q", gotRoute.URI())
	}
	if gotPath != "/invoices/42" {
		t.Errorf("the listener got the wrong request: %q", gotPath)
	}
}

// TestMatchedDoesNotFireWhenNothingMatched, because a listener that ran on a
// 404 would be counting requests rather than route hits.
func TestMatchedDoesNotFireWhenNothingMatched(t *testing.T) {
	r := routing.NewRouter()

	fired := 0
	r.Matched(func(*routing.Route, *http.Request) { fired++ })
	r.Get("/invoices", ok)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nothing-here", nil))

	if fired != 0 {
		t.Fatalf("the matched listener fired %d times on a request no route answered", fired)
	}
}

// TestMatchedFiresBeforeTheMiddleware. The order is the value: a listener that
// ran after the chain could not tell a request the middleware rejected from
// one it never saw.
func TestMatchedFiresBeforeTheMiddleware(t *testing.T) {
	r := routing.NewRouter()

	var order []string
	r.Matched(func(*routing.Route, *http.Request) { order = append(order, "matched") })

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "middleware")
			next.ServeHTTP(w, req)
		})
	}
	r.Get("/invoices", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
	}), pipeline.Middleware[http.Handler](mw))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))

	want := []string{"matched", "middleware", "handler"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestMatchedDoesNotFireForAValueAWhereConstraintRejected. A where constraint
// is matching, so a value that fails it is a request this route does not
// answer -- and a listener told otherwise would report a hit on a route whose
// handler never ran.
func TestMatchedDoesNotFireForAValueAWhereConstraintRejected(t *testing.T) {
	r := routing.NewRouter()

	fired := 0
	r.Matched(func(*routing.Route, *http.Request) { fired++ })
	r.Get("/users/{id}", ok).WhereNumber("id")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/abc", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("a value the constraint rejected was answered %d", rec.Code)
	}
	if fired != 0 {
		t.Fatalf("the matched listener fired %d times for a value the constraint rejected", fired)
	}
}

// TestMatchedListenersFireInRegistrationOrder, so a listener registered to run
// after another one does.
func TestMatchedListenersFireInRegistrationOrder(t *testing.T) {
	r := routing.NewRouter()

	var order []string
	r.Matched(func(*routing.Route, *http.Request) { order = append(order, "first") })
	r.Matched(func(*routing.Route, *http.Request) { order = append(order, "second") })
	r.Get("/invoices", ok)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v, want [first second]", order)
	}
}

// TestAListenerOnTheRootSeesARouteRegisteredInAGroup. A group is a sub-router
// that shares the root's configuration, so registering the listener once
// covers everything below it.
func TestAListenerOnTheRootSeesARouteRegisteredInAGroup(t *testing.T) {
	r := routing.NewRouter()

	var names []string
	r.Matched(func(rt *routing.Route, _ *http.Request) { names = append(names, rt.RouteName()) })

	admin := r.Group(routing.Group{Prefix: "/admin", Name: "admin"})
	admin.Get("/users", ok).Name("users")

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/users", nil))

	if len(names) != 1 || names[0] != "admin.users" {
		t.Fatalf("names = %v, want [admin.users]", names)
	}
}

// TestAListenerRegisteredOnASubRouterFires. Matched writes to the root, and a
// group is a fresh sub-router per call: a listener kept on the sub-router
// itself would be dropped with the value the caller stopped holding, and the
// listener would silently never run.
func TestAListenerRegisteredOnASubRouterFires(t *testing.T) {
	r := routing.NewRouter()

	var names []string
	admin := r.Group(routing.Group{Prefix: "/admin", Name: "admin"})
	admin.Matched(func(rt *routing.Route, _ *http.Request) { names = append(names, rt.RouteName()) })

	admin.Get("/users", ok).Name("users")
	r.Get("/health", ok).Name("health")

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	want := []string{"admin.users", "health"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}
