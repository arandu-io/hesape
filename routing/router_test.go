package routing_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/routing"
)

// ok is the handler every test that only cares about matching registers.
var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestRouterMatchesMethodAndPath(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/users", ok)
	r.Post("/users", ok)

	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/users", http.StatusOK},
		{http.MethodPost, "/users", http.StatusOK},
		{http.MethodDelete, "/users", http.StatusMethodNotAllowed},
		{http.MethodGet, "/unknown", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}

func TestGroupPrefixes(t *testing.T) {
	r := routing.NewRouter()
	api := r.Group(routing.Group{Prefix: "/api"})
	v1 := api.Group(routing.Group{Prefix: "/v1"})
	v1.Get("/health", ok)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: nested groups must concatenate prefixes", rec.Code)
	}
}

func TestPathParameters(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.PathValue("id")))
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/u-42", nil))

	if got := rec.Body.String(); got != "u-42" {
		t.Fatalf("path value = %q, want u-42", got)
	}
}

// TestGroupMiddlewareIsInherited: a group is how a module protects a whole area
// at once, so inheritance is the property that matters.
func TestGroupMiddlewareIsInherited(t *testing.T) {
	r := routing.NewRouter()
	blocked := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}
	admin := r.Group(routing.Group{
		Prefix:     "/admin",
		Middleware: []pipeline.Middleware[http.Handler]{blocked},
	})
	admin.Group(routing.Group{Prefix: "/reports"}).Get("/monthly", ok)
	r.Get("/public", ok)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/reports/monthly", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nested group status = %d, want 403: middleware must be inherited", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sibling route status = %d, want 200: group middleware must not leak", rec.Code)
	}
}

// TestGroupMiddlewareRunsOutsideTheRouteMiddleware pins the order across the
// two places middleware can be given. A group's guard running after the route's
// own is a guard that can be skipped by whatever the route's middleware answers
// first.
func TestGroupMiddlewareRunsOutsideTheRouteMiddleware(t *testing.T) {
	r := routing.NewRouter()

	var order []string
	mark := func(name string) pipeline.Middleware[http.Handler] {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, req)
			})
		}
	}

	r.Group(routing.Group{
		Prefix:     "/admin",
		Middleware: []pipeline.Middleware[http.Handler]{mark("group")},
	}).Get("/reports", ok, mark("route"))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/reports", nil))

	if strings.Join(order, ",") != "group,route" {
		t.Fatalf("middleware ran as %v, want [group route]", order)
	}
}

func TestRoutesMetadataCarriesTheModule(t *testing.T) {
	r := routing.NewRouter()
	auth := r.ForModule("auth").Group(routing.Group{Prefix: "/auth"})
	auth.Get("/login", ok)
	auth.Post("/login", ok)
	r.ForModule("billing").Get("/invoices", ok)

	routes := r.Routes()

	if len(routes) != 3 {
		t.Fatalf("registered %d routes, want 3", len(routes))
	}
	byPattern := map[string]*routing.Route{}
	for _, rt := range routes {
		byPattern[rt.Method+" "+rt.Pattern] = rt
	}
	if got := byPattern["GET /auth/login"].Module; got != "auth" {
		t.Errorf("module of GET /auth/login = %q, want auth", got)
	}
	if got := byPattern["GET /invoices"].Module; got != "billing" {
		t.Errorf("module of GET /invoices = %q, want billing", got)
	}
}

func TestRootAndTrailingSlashJoin(t *testing.T) {
	r := routing.NewRouter()
	r.Group(routing.Group{Prefix: "/"}).Get("/", ok)
	r.Group(routing.Group{Prefix: "/api/"}).Get("v1", ok)

	for _, path := range []string{"/", "/api/v1"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

// TestPutAndPatchRegisterTheirOwnMethod covers the two verbs nothing reached.
//
// Resource registers update through its own path and never calls Put or Patch,
// so both were unexercised: a copy-paste that left Put registering POST would
// have compiled, served, and shown the wrong verb in `aru routes` -- and the
// symptom is a form that silently hits the wrong handler.
func TestPutAndPatchRegisterTheirOwnMethod(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			r := routing.NewRouter()
			seen := ""
			record := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				seen = req.Method
				w.WriteHeader(http.StatusOK)
			})

			switch method {
			case http.MethodPut:
				r.Put("/invoices/{id}", record)
			case http.MethodPatch:
				r.Patch("/invoices/{id}", record)
			}

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(method, "/invoices/inv-1", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s /invoices/inv-1 = %d, want 200", method, rec.Code)
			}
			if seen != method {
				t.Fatalf("the handler ran for %s, want %s", seen, method)
			}

			// Registering one verb must not open the others. A route that also
			// answers POST is a route where CSRF and the policy were written
			// for a request shape that is not the one arriving.
			for _, other := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(other, "/invoices/inv-1", nil))
				if rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("%s /invoices/inv-1 = %d, want 405: %s must register %s only", other, rec.Code, method, method)
				}
			}

			// The table is what `aru routes` prints and what Routes.Route
			// resolves against. A wrong verb there is a wrong verb in the form.
			routes := r.Routes()
			if len(routes) != 1 {
				t.Fatalf("registered %d routes, want 1", len(routes))
			}
			if routes[0].Method != method {
				t.Errorf("the route table says %s, want %s", routes[0].Method, method)
			}
			if routes[0].Pattern != "/invoices/{id}" {
				t.Errorf("the route table says %q, want /invoices/{id}", routes[0].Pattern)
			}
		})
	}
}

// TestPutAndPatchRunTheirMiddleware: both take middleware in their signature,
// and middleware that is accepted and not applied is authorization that looks
// wired and is not.
func TestPutAndPatchRunTheirMiddleware(t *testing.T) {
	r := routing.NewRouter()

	var ran []string
	stamp := func(name string) pipeline.Middleware[http.Handler] {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ran = append(ran, name)
				next.ServeHTTP(w, req)
			})
		}
	}

	r.Put("/invoices/{id}", ok, stamp("put"))
	r.Patch("/invoices/{id}", ok, stamp("patch"))

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/invoices/inv-1", nil))
	}

	if strings.Join(ran, ",") != "put,patch" {
		t.Fatalf("middleware ran as %v, want [put patch]", ran)
	}
}

// TestMatchAnswersEveryMethodItNamesAndNoOther is the whole contract of Match:
// the listed verbs reach the handler, the unlisted ones do not.
func TestMatchAnswersEveryMethodItNamesAndNoOther(t *testing.T) {
	r := routing.NewRouter()
	r.Match([]string{http.MethodGet, http.MethodPost}, "/search", ok).Name("search")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/search", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s /search = %d, want 200", method, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/search", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /search = %d, want 405", rec.Code)
	}

	// Both rows show the name, and generating a URL from it has one answer.
	for _, route := range r.Routes() {
		if route.RouteName() != "search" {
			t.Errorf("%s %s is named %q, want search", route.Method, route.Pattern, route.RouteName())
		}
	}
	got, err := r.Table().Route("search")
	if err != nil || got != "/search" {
		t.Errorf("Route(search) = %q, %v; want /search", got, err)
	}
}

// TestMatchWithoutMethodsIsRefusedAtBoot: the alternative is a route that
// answers nothing and appears in `aru routes` as though it did.
func TestMatchWithoutMethodsIsRefusedAtBoot(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Match accepted an empty list of methods")
		}
	}()
	routing.NewRouter().Match(nil, "/search", ok)
}

// TestAnyAnswersEveryMethod, including the ones nobody thought to list. It is
// one row in the table, labelled ANY.
func TestAnyAnswersEveryMethod(t *testing.T) {
	r := routing.NewRouter()
	r.Any("/hook", ok)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/hook", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s /hook = %d, want 200", method, rec.Code)
		}
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Fatalf("registered %d routes, want 1", len(routes))
	}
	if routes[0].Method != "ANY" {
		t.Errorf("the route table says %q, want ANY", routes[0].Method)
	}
}

// TestFallbackAnswersTheMissesAndNothingElse: a fallback that shadowed a
// registered route would turn every page into the 404 page.
func TestFallbackAnswersTheMissesAndNothingElse(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/known", ok)
	r.Fallback(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("fallback"))
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/known", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /known = %d, want 200: the fallback shadowed a registered route", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nothing/here", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "fallback" {
		t.Errorf("GET /nothing/here = %d %q, want 404 \"fallback\"", rec.Code, rec.Body.String())
	}
}

// TestAGroupCanAnswerItsOwnMisses is why Fallback is per-router: an API subtree
// answers a miss with JSON while the rest of the site answers with a page.
func TestAGroupCanAnswerItsOwnMisses(t *testing.T) {
	r := routing.NewRouter()
	r.Fallback(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("page"))
	}))
	r.Group(routing.Group{Prefix: "/api"}).Fallback(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("json"))
	}))

	for _, c := range []struct{ path, want string }{
		{"/nothing", "page"},
		{"/api/nothing", "json"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Body.String() != c.want {
			t.Errorf("GET %s answered %q, want %q", c.path, rec.Body.String(), c.want)
		}
	}
}
