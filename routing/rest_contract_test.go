package routing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/routing"
)

func TestRESTMethodHelpersDispatchOnlyTheirDeclaredMethod(t *testing.T) {
	tests := []struct {
		method   string
		register func(*routing.Router, http.Handler) *routing.Route
	}{
		{method: http.MethodGet, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Get("/resource", handler)
		}},
		{method: http.MethodPost, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Post("/resource", handler)
		}},
		{method: http.MethodPut, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Put("/resource", handler)
		}},
		{method: http.MethodPatch, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Patch("/resource", handler)
		}},
		{method: http.MethodDelete, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Delete("/resource", handler)
		}},
		{method: http.MethodOptions, register: func(router *routing.Router, handler http.Handler) *routing.Route {
			return router.Options("/resource", handler)
		}},
	}

	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			router := routing.NewRouter()
			seen := ""
			test.register(router, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				seen = req.Method
				w.WriteHeader(http.StatusNoContent)
			}))

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(test.method, "/resource", nil))
			if recorder.Code != http.StatusNoContent || seen != test.method {
				t.Fatalf("%s helper dispatched %d with method %q, want 204 and %s", test.method, recorder.Code, seen, test.method)
			}

			if test.method != http.MethodGet {
				recorder = httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/resource", nil))
				if recorder.Code != http.StatusMethodNotAllowed {
					t.Errorf("GET reached a route registered for %s with status %d", test.method, recorder.Code)
				}
			}
		})
	}
}

func TestGETRouteUsesTheStandardHEADSemantics(t *testing.T) {
	router := routing.NewRouter()
	router.Get("/resource", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Handled", "yes")
		_, _ = w.Write([]byte("body"))
	}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/resource", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-Handled") != "yes" {
		t.Fatalf("HEAD = %d X-Handled %q, want 200 yes", recorder.Code, recorder.Header().Get("X-Handled"))
	}
}

func TestRouterPreservesRequestCancellation(t *testing.T) {
	router := routing.NewRouter()
	seen := make(chan error, 1)
	router.Get("/resource", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen <- req.Context().Err()
		w.WriteHeader(http.StatusNoContent)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/resource", nil).WithContext(ctx)
	router.ServeHTTP(httptest.NewRecorder(), request)
	if err := <-seen; err != context.Canceled {
		t.Fatalf("handler context error = %v, want context.Canceled", err)
	}
}

func TestOptionalRouteIsOneLogicalRouteWithTwoConcretePaths(t *testing.T) {
	router := routing.NewRouter()
	route := router.Get("/users/{id?}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.PathValue("id")))
	})).Name("users.show")

	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/users", body: ""},
		{path: "/users/u-42", body: "u-42"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != test.body {
			t.Errorf("GET %s = %d %q, want 200 %q", test.path, recorder.Code, recorder.Body.String(), test.body)
		}
	}

	if routes := router.Routes(); len(routes) != 1 || routes[0] != route {
		t.Fatalf("route table = %v, want the one logical route", routes)
	}
	if route.Pattern != "/users/{id?}" {
		t.Errorf("logical pattern = %q, want /users/{id?}", route.Pattern)
	}
	withoutID, err := router.Table().Route("users.show")
	if err != nil || withoutID != "/users" {
		t.Errorf("Route(users.show) = %q, %v; want /users", withoutID, err)
	}
	withID, err := router.Table().Route("users.show", "u-42")
	if err != nil || withID != "/users/u-42" {
		t.Errorf("Route(users.show, u-42) = %q, %v; want /users/u-42", withID, err)
	}
}

func TestTrailingOptionalParametersExpandWithoutAmbiguousPatterns(t *testing.T) {
	router := routing.NewRouter()
	router.Get("/reports/{year?}/{month?}", ok)

	for _, path := range []string{"/reports", "/reports/2026", "/reports/2026/09"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, recorder.Code)
		}
	}
}

func TestRequiredSegmentAfterOptionalSegmentIsRejectedAtRegistration(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("router accepted a required segment after an optional segment")
		}
	}()
	routing.NewRouter().Get("/reports/{year?}/summary", ok)
}

func TestQualifiedBindingRegistersAndDispatchesTheCleanPattern(t *testing.T) {
	router := routing.NewRouter()
	route := router.Group(routing.Group{Prefix: "/api"}).Match(
		[]string{http.MethodGet, http.MethodPatch},
		"/posts/{post:slug}",
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte(req.PathValue("post")))
		}),
	)

	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/posts/hello-world", nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != "hello-world" {
			t.Errorf("%s qualified route = %d %q, want 200 hello-world", method, recorder.Code, recorder.Body.String())
		}
	}
	if route.Pattern != "/api/posts/{post}" {
		t.Errorf("clean pattern = %q, want /api/posts/{post}", route.Pattern)
	}
	if field := route.BindingFieldFor("post"); field != "slug" {
		t.Errorf("binding field = %q, want slug", field)
	}
	for _, registered := range router.Routes() {
		if registered.Pattern != "/api/posts/{post}" || registered.BindingFieldFor("post") != "slug" {
			t.Errorf("registered route = %q field %q, want clean pattern and slug", registered.Pattern, registered.BindingFieldFor("post"))
		}
	}
}

func TestRouteConstraintsMatchTheWholeParameterValue(t *testing.T) {
	for _, configure := range []struct {
		name string
		path string
		bad  []string
		good []string
		add  func(*routing.Route)
	}{
		{
			name: "number",
			path: "/numbers/{value}",
			bad:  []string{"abc1", "1abc"},
			good: []string{"123"},
			add:  func(route *routing.Route) { route.WhereNumber("value") },
		},
		{
			name: "explicit regex",
			path: "/codes/{value}",
			bad:  []string{"xAB", "ABy"},
			good: []string{"AB"},
			add:  func(route *routing.Route) { route.Where("value", `[A-Z]{2}`) },
		},
	} {
		t.Run(configure.name, func(t *testing.T) {
			router := routing.NewRouter()
			route := router.Get(configure.path, ok)
			configure.add(route)
			for _, value := range configure.good {
				assertRouteStatus(t, router, strings.Replace(configure.path, "{value}", value, 1), http.StatusOK)
			}
			for _, value := range configure.bad {
				assertRouteStatus(t, router, strings.Replace(configure.path, "{value}", value, 1), http.StatusNotFound)
			}
		})
	}
}

func TestWhereInTreatsEveryValueLiterally(t *testing.T) {
	router := routing.NewRouter()
	router.Get("/versions/{version}", ok).WhereIn("version", "v1.0", "a|b")

	for _, value := range []string{"v1.0", "a|b"} {
		assertRouteStatus(t, router, "/versions/"+value, http.StatusOK)
	}
	for _, value := range []string{"v1x0", "a", "b", "not-v1.0"} {
		assertRouteStatus(t, router, "/versions/"+value, http.StatusNotFound)
	}
}

func TestRegistrarAndGlobalConstraintsUseTheSameWholeValueRules(t *testing.T) {
	registrarRouter := routing.NewRouter()
	registrarRouter.Prefix("").WhereIn("version", "v1.0", "a|b").Get("/versions/{version}", ok)
	for _, value := range []string{"v1.0", "a|b"} {
		assertRouteStatus(t, registrarRouter, "/versions/"+value, http.StatusOK)
	}
	for _, value := range []string{"v1x0", "a", "not-v1.0"} {
		assertRouteStatus(t, registrarRouter, "/versions/"+value, http.StatusNotFound)
	}

	globalRouter := routing.NewRouter()
	globalRouter.Pattern("id", `[0-9]+`)
	globalRouter.Get("/users/{id}", ok)
	assertRouteStatus(t, globalRouter, "/users/42", http.StatusOK)
	assertRouteStatus(t, globalRouter, "/users/user-42", http.StatusNotFound)
}

func TestInvalidConstraintFailsAtRegistration(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("invalid constraint was silently ignored")
		}
	}()
	routing.NewRouter().Get("/users/{id}", ok).Where("id", "[")
}

func assertRouteStatus(t *testing.T, router *routing.Router, path string, want int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != want {
		t.Errorf("GET %s = %d, want %d", path, recorder.Code, want)
	}
}
