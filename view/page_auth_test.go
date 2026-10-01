package view_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/view"
)

// starterKitRoutes is the route table the authentication starter kit and the
// skeleton register, by the names New reads.
func starterKitRoutes() *routing.Routes {
	r := routing.NewRouter()
	nothing := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/{$}", nothing).Name("home")
	r.Get("/auth/login", nothing).Name("auth.login")
	r.Post("/auth/logout", nothing).Name("auth.logout")
	r.Get("/auth/register", nothing).Name("auth.register")
	return r.Table()
}

func contextFor(subject *auth.Subject, urls hhttp.URLGenerator) *hhttp.Context {
	req := httptest.NewRequest(http.MethodGet, "/posts", nil)
	if subject != nil {
		req = req.WithContext(auth.WithSubject(req.Context(), *subject))
	}
	return hhttp.NewContext(httptest.NewRecorder(), req, nil, urls)
}

// TestNewDrawsTheSignedInNavigationForASignedInPerson: a page built with New
// showed a signed-in person the guest navigation -- a sign-in link with no
// address and no way to sign out.
func TestNewDrawsTheSignedInNavigationForASignedInPerson(t *testing.T) {
	subject := auth.Subject{ID: "u1", Tenant: "acme"}
	page := view.New(contextFor(&subject, starterKitRoutes()), "Posts")

	if !page.SignedIn() {
		t.Error("SignedIn = false for a request carrying a signed-in subject")
	}
	if got := page.LogoutLink(); got != "/auth/logout" {
		t.Errorf("LogoutLink = %q, want /auth/logout", got)
	}
	if got := page.HomeLink(); got != "/" {
		t.Errorf("HomeLink = %q, want /", got)
	}
	if got := page.LoginLink(); got != "/auth/login" {
		t.Errorf("LoginLink = %q, want /auth/login", got)
	}
	if got := page.RegisterLink(); got != "/auth/register" {
		t.Errorf("RegisterLink = %q, want /auth/register", got)
	}
	// The subject carries no display name, and New does not invent one.
	if got := page.SignedInName(); got != "" {
		t.Errorf("SignedInName = %q, want empty", got)
	}
}

// TestNewTreatsAGuestAsSignedOut: a declared guest and a request with no
// subject are both signed out.
func TestNewTreatsAGuestAsSignedOut(t *testing.T) {
	guest := auth.Guest("acme")
	for name, ctx := range map[string]*hhttp.Context{
		"guest":      contextFor(&guest, starterKitRoutes()),
		"no subject": contextFor(nil, starterKitRoutes()),
	} {
		page := view.New(ctx, "Posts")
		if page.SignedIn() {
			t.Errorf("%s: SignedIn = true", name)
		}
		if got := page.LoginLink(); got != "/auth/login" {
			t.Errorf("%s: LoginLink = %q, want /auth/login", name, got)
		}
	}
}

// TestNewLeavesAnUnregisteredRouteEmpty: an application without registration
// has no auth.register route, and the page draws no link to it rather than a
// broken one. Without a route table at all, every link is empty.
func TestNewLeavesAnUnregisteredRouteEmpty(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/auth/login", http.NotFoundHandler()).Name("auth.login")

	page := view.New(contextFor(nil, r.Table()), "Sign in")
	if page.LoginURL != "/auth/login" {
		t.Errorf("LoginURL = %q", page.LoginURL)
	}
	if page.RegisterURL != "" || page.HomeURL != "" || page.LogoutURL != "" {
		t.Errorf("unregistered routes drew links: %q %q %q", page.RegisterURL, page.HomeURL, page.LogoutURL)
	}

	bare := view.New(contextFor(nil, nil), "Sign in")
	if bare.LoginURL != "" || bare.HomeURL != "" {
		t.Errorf("a context with no route table drew links: %q %q", bare.LoginURL, bare.HomeURL)
	}
}

// onlyRoute is a URL generator that answers Route and nothing else.
type onlyRoute map[string]string

func (o onlyRoute) Route(name string, _ ...string) (string, error) {
	if path, ok := o[name]; ok {
		return path, nil
	}
	return "", errors.New("no route named " + name)
}

// TestHasRouteAsksAGeneratorThatOnlyBuildsPaths: a URL generator other than
// the route table is asked by building the path.
func TestHasRouteAsksAGeneratorThatOnlyBuildsPaths(t *testing.T) {
	ctx := contextFor(nil, onlyRoute{"auth.login": "/login"})
	if !ctx.HasRoute("auth.login") || ctx.HasRoute("auth.register") {
		t.Errorf("HasRoute = %v, %v", ctx.HasRoute("auth.login"), ctx.HasRoute("auth.register"))
	}
	if got := view.New(ctx, "Sign in").LoginURL; got != "/login" {
		t.Errorf("LoginURL = %q, want /login", got)
	}
}
