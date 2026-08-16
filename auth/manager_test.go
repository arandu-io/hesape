package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// managerFor builds a manager configured the usual way: a session guard called
// web, a token guard called api, and one provider.
func managerFor(t *testing.T) (*auth.AuthManager, *fakeProvider, *fakeCookieJar, *fakeRequest, *fakeDispatcher) {
	t.Helper()

	provider := &fakeProvider{
		user:     &fakeUser{id: 7, password: "hashed:secret"},
		email:    "person@example.com",
		password: "secret",
	}
	jar := newFakeCookieJar()
	request := newFakeRequest()
	dispatcher := &fakeDispatcher{}

	manager := auth.NewAuthManager(auth.ManagerConfig{
		DefaultGuard:    "web",
		DefaultProvider: "users",
		Guards: map[string]map[string]any{
			"web":    {"driver": "session", "provider": "users", "remember": 120},
			"api":    {"driver": "token", "provider": "users", "input_key": "token", "storage_key": "api_token"},
			"broken": {"driver": "nonsense"},
		},
		Providers: map[string]map[string]any{
			"users":   {"driver": "eloquent", "model": "App\\Models\\User"},
			"nowhere": {"driver": "carrier-pigeon"},
		},
		Session:         newFakeSession(),
		Cookies:         jar,
		Events:          dispatcher,
		Request:         request,
		Hasher:          fakeHasher{},
		RehashOnLogin:   true,
		TimeboxDuration: 1,
		HashKey:         "app-key",
	})

	manager.Provider("eloquent", func(map[string]any) (auth.UserProvider, error) { return provider, nil })

	return manager, provider, jar, request, dispatcher
}

func TestTheManagerBuildsTheSessionGuardItIsConfiguredFor(t *testing.T) {
	manager, provider, jar, _, dispatcher := managerFor(t)

	guard, err := manager.Guard("web")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}

	session, ok := guard.(*auth.SessionGuard)
	if !ok {
		t.Fatalf("the web guard is a %T, want *auth.SessionGuard", guard)
	}
	if session.Name != "web" {
		t.Fatalf("the guard is named %q", session.Name)
	}
	if session.GetProvider() != provider {
		t.Fatal("the guard was not given the configured provider")
	}

	if !session.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, true) {
		t.Fatal("the guard the manager built cannot sign anybody in")
	}

	// The cookie jar and the dispatcher were wired, and auth.guards.web.remember
	// became the recaller's lifetime.
	recaller, ok := jar.queued[session.GetRecallerName()]
	if !ok {
		t.Fatal("the guard has no cookie jar, so nobody can be remembered")
	}
	if recaller.MaxAge != 120*60 {
		t.Fatalf("the recaller lasts %d seconds, want the configured 120 minutes", recaller.MaxAge)
	}
	if _, ok := firstEvent[auth.Login](dispatcher); !ok {
		t.Fatal("the guard has no dispatcher, so nothing can listen to a sign-in")
	}
}

func TestTheManagerBuildsTheTokenGuardItIsConfiguredFor(t *testing.T) {
	manager, provider, _, request, _ := managerFor(t)

	provider.user.rememberToken = "the-token"
	request.query["token"] = "the-token"

	guard, err := manager.Guard("api")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}

	token, ok := guard.(*auth.TokenGuard)
	if !ok {
		t.Fatalf("the api guard is a %T, want *auth.TokenGuard", guard)
	}
	if token.GetTokenForRequest() != "the-token" {
		t.Fatalf("the guard read %q, so input_key was not used", token.GetTokenForRequest())
	}
	if token.User() != provider.user {
		t.Fatal("the token guard did not resolve the user")
	}
}

func TestAGuardIsResolvedOnceAndForgottenOnRequest(t *testing.T) {
	manager, _, _, _, _ := managerFor(t)

	if manager.HasResolvedGuards() {
		t.Fatal("a fresh manager already holds guards")
	}

	first, err := manager.Guard("web")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}
	second, err := manager.Guard("")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}

	if first != second {
		t.Fatal("the default name did not reach the same cached guard")
	}
	if !manager.HasResolvedGuards() {
		t.Fatal("HasResolvedGuards says no after one was resolved")
	}

	manager.ForgetGuards()

	if manager.HasResolvedGuards() {
		t.Fatal("ForgetGuards kept the guards, and each one holds the user of a finished request")
	}

	third, _ := manager.Guard("web")
	if third == first {
		t.Fatal("the guard survived ForgetGuards")
	}
}

func TestAGuardOrADriverThatIsNotConfiguredSaysWhichOne(t *testing.T) {
	manager, _, _, _, _ := managerFor(t)

	_, err := manager.Guard("nowhere-near")
	if err == nil || !strings.Contains(err.Error(), "nowhere-near") {
		t.Fatalf("Guard answered %v, and it should name the guard", err)
	}

	_, err = manager.Guard("broken")
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("Guard answered %v, and it should name the driver", err)
	}
}

func TestTheDefaultDriverIsReadAndWritten(t *testing.T) {
	manager, _, _, _, _ := managerFor(t)

	if manager.GetDefaultDriver() != "web" {
		t.Fatalf("the default guard is %q", manager.GetDefaultDriver())
	}
	if manager.GetDefaultUserProvider() != "users" {
		t.Fatalf("the default provider is %q", manager.GetDefaultUserProvider())
	}

	manager.SetDefaultDriver("api")

	if manager.GetDefaultDriver() != "api" {
		t.Fatalf("SetDefaultDriver did not take: %q", manager.GetDefaultDriver())
	}

	manager.ShouldUse("web")

	if manager.GetDefaultDriver() != "web" {
		t.Fatalf("ShouldUse did not take: %q", manager.GetDefaultDriver())
	}

	guard, err := manager.Guard("")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}
	if _, ok := guard.(*auth.SessionGuard); !ok {
		t.Fatalf("the default guard is a %T after ShouldUse(web)", guard)
	}
}

func TestTheUserResolverGoesThroughTheDefaultGuard(t *testing.T) {
	manager, provider, _, _, _ := managerFor(t)

	guard, _ := manager.Guard("web")
	guard.SetUser(provider.user)

	if manager.UserResolver()("") != provider.user {
		t.Fatal("the resolver did not find the user of the default guard")
	}
	if manager.UserResolver()("nowhere-near") != nil {
		t.Fatal("the resolver invented a user for a guard that is not configured")
	}

	manager.ResolveUsersUsing(func(string) auth.Authenticatable { return nil })

	if manager.UserResolver()("") != nil {
		t.Fatal("ResolveUsersUsing did not take")
	}

	manager.ShouldUse("web")

	if manager.UserResolver()("") != provider.user {
		t.Fatal("ShouldUse did not put the default resolver back")
	}
}

func TestExtendRegistersADriverOfSomebodyElsesMaking(t *testing.T) {
	manager, provider, _, _, _ := managerFor(t)

	built := 0
	manager.Extend("nonsense", func(m *auth.AuthManager, name string, config map[string]any) (auth.Guard, error) {
		built++

		if m == nil || name != "broken" || config["driver"] != "nonsense" {
			t.Errorf("the creator was called with (%v, %q, %v)", m, name, config)
		}

		return auth.NewTokenGuard(provider, newFakeRequest(), "", "", false), nil
	})

	guard, err := manager.Guard("broken")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}
	if built != 1 {
		t.Fatalf("the custom creator ran %d times, want 1", built)
	}
	if _, ok := guard.(*auth.TokenGuard); !ok {
		t.Fatalf("the custom guard is a %T", guard)
	}
}

func TestViaRequestRegistersAGuardThatIsOneCallback(t *testing.T) {
	manager, provider, _, request, _ := managerFor(t)

	request.query["who"] = "7"

	manager.ViaRequest("nonsense", func(r auth.Request, p auth.UserProvider) auth.Authenticatable {
		if r == nil || p == nil || r.Query("who") != "7" {
			return nil
		}
		return provider.user
	})

	guard, err := manager.Guard("broken")
	if err != nil {
		t.Fatalf("Guard answered %v", err)
	}
	if _, ok := guard.(*auth.RequestGuard); !ok {
		t.Fatalf("the guard is a %T, want *auth.RequestGuard", guard)
	}
	if guard.User() != provider.user {
		t.Fatal("the callback's user did not come back")
	}
}

func TestTheProviderRegistryIsWhereTheUserProvidersComeFrom(t *testing.T) {
	manager, provider, _, _, _ := managerFor(t)

	built, err := manager.CreateUserProvider("")
	if err != nil || built != provider {
		t.Fatalf("CreateUserProvider answered (%v, %v)", built, err)
	}

	built, err = manager.CreateUserProvider("users")
	if err != nil || built != provider {
		t.Fatalf("CreateUserProvider(users) answered (%v, %v)", built, err)
	}

	// A provider whose driver nobody registered is an error naming the driver.
	if _, err := manager.CreateUserProvider("nowhere"); err == nil || !strings.Contains(err.Error(), "carrier-pigeon") {
		t.Fatalf("CreateUserProvider answered %v, and it should name the driver", err)
	}

	// A provider that is not configured at all is nil and no error: a guard may
	// have none.
	if built, err := manager.CreateUserProvider("not-configured"); built != nil || err != nil {
		t.Fatalf("CreateUserProvider answered (%v, %v), want (nil, nil)", built, err)
	}
}

func TestAGuardWhoseProviderCannotBeBuiltDoesNotComeBackHalfWired(t *testing.T) {
	manager := auth.NewAuthManager(auth.ManagerConfig{
		DefaultGuard: "web",
		Guards: map[string]map[string]any{
			"web": {"driver": "session", "provider": "users"},
		},
		Providers: map[string]map[string]any{
			"users": {"driver": "carrier-pigeon"},
		},
		Session: newFakeSession(),
	})

	guard, err := manager.Guard("web")
	if err == nil {
		t.Fatal("the guard was built on a provider that could not be")
	}
	if guard != nil {
		t.Fatalf("Guard answered a %T alongside the error", guard)
	}
	if manager.HasResolvedGuards() {
		t.Fatal("the failed guard was cached")
	}
}
