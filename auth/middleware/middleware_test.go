package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/middleware"
	"github.com/arandu-io/hesape/session"
	sessionmw "github.com/arandu-io/hesape/session/middleware"
)

// user is an account with nothing to confirm. It does NOT implement
// auth.MustVerifyEmail, which is what makes it the other half of the type
// assertion the middleware does -- see [verifiable].
type user struct {
	id       string
	email    string
	verified bool
	remember string
}

func (u *user) GetAuthIdentifierName() string   { return "id" }
func (u *user) GetAuthIdentifier() any          { return u.id }
func (u *user) GetAuthPasswordName() string     { return "password" }
func (u *user) GetAuthPassword() string         { return "$2y$10$hash" }
func (u *user) GetRememberToken() string        { return u.remember }
func (u *user) SetRememberToken(token string)   { u.remember = token }
func (u *user) GetRememberTokenName() string    { return "remember_token" }
func (u *user) GetEmailForVerification() string { return u.email }

// verifiable wraps a user into an account that must confirm its address.
type verifiable struct{ *user }

func (v verifiable) HasVerifiedEmail() bool                    { return v.verified }
func (v verifiable) MarkEmailAsVerified(context.Context) error { v.verified = true; return nil }
func (v verifiable) SendEmailVerificationNotification(context.Context) error {
	return nil
}

// guard is an auth.Guard whose answers a test sets directly.
type guard struct {
	signedIn bool
	current  auth.Authenticatable
	// basicErr is what Basic answers, for AuthenticateWithBasicAuth.
	basicErr error
	// basicField records the column the middleware matched on.
	basicField string
}

func (g *guard) Check() bool                { return g.signedIn }
func (g *guard) Guest() bool                { return !g.signedIn }
func (g *guard) User() auth.Authenticatable { return g.current }
func (g *guard) ID() any {
	if g.current == nil {
		return nil
	}
	return g.current.GetAuthIdentifier()
}
func (g *guard) Validate(context.Context, map[string]any) bool { return false }
func (g *guard) HasUser() bool                                 { return g.current != nil }
func (g *guard) SetUser(u auth.Authenticatable)                { g.current = u; g.signedIn = u != nil }

func (g *guard) Basic(_ context.Context, field string, _ map[string]any) error {
	g.basicField = field
	if g.basicErr != nil {
		return g.basicErr
	}
	g.signedIn = true
	return nil
}

func (g *guard) OnceBasic(ctx context.Context, field string, extra map[string]any) error {
	return g.Basic(ctx, field, extra)
}

// factory is a middleware.Factory over a fixed set of guards.
type factory struct {
	guards map[string]*guard
	// used records what ShouldUse was told, which is how the test proves the
	// middleware named the guard that actually answered.
	used string
}

func newFactory(guards map[string]*guard) *factory { return &factory{guards: guards} }

func (f *factory) Guard(name string) auth.Guard {
	g, ok := f.guards[name]
	if !ok {
		return nil
	}
	return g
}

func (f *factory) ShouldUse(name string) { f.used = name }

// subjectFor is the resolver every test wires. It is what a real application
// must NOT write this way: the tenant here is a constant, and in production it
// comes off the session.
func subjectFor(_ *http.Request, u auth.Authenticatable) (auth.Subject, bool) {
	return auth.Subject{ID: u.GetAuthIdentifier().(string), Tenant: "acme"}, true
}

// ok is the handler at the end of every chain. It records that it ran.
func ok(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthenticateRefusesARequestWithNoSession(t *testing.T) {
	var ran bool
	m := middleware.NewAuthenticate(newFactory(map[string]*guard{"": {}}), subjectFor)

	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if ran {
		t.Fatal("the handler ran for a request nobody was signed in on")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Unauthenticated.") {
		t.Fatalf("body = %q, want Illuminate's message", rec.Body.String())
	}
}

func TestAuthenticateRedirectsABrowserWhenThereIsSomewhereToSendIt(t *testing.T) {
	var ran bool
	m := middleware.NewAuthenticate(newFactory(map[string]*guard{"": {}}), subjectFor).
		RedirectUsing(func(*http.Request) string { return "/login" })

	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if ran {
		t.Fatal("the handler ran for a request nobody was signed in on")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}
}

// TestAuthenticateAnswers401ToAClientThatAskedForJSON pins the JSON branch: a
// redirect to a sign-in page is meaningless to an API client, which needs the
// status.
func TestAuthenticateAnswers401ToAClientThatAskedForJSON(t *testing.T) {
	var ran bool
	m := middleware.NewAuthenticate(newFactory(map[string]*guard{"": {}}), subjectFor).
		RedirectUsing(func(*http.Request) string { return "/login" })

	r := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	r.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("Location") != "" {
		t.Fatal("a JSON client was redirected")
	}
}

func TestAuthenticatePassesARequestWithASessionAndPutsTheSubjectOnIt(t *testing.T) {
	var got auth.Subject
	var found bool

	f := newFactory(map[string]*guard{"": {signedIn: true, current: &user{id: "7"}}})
	m := middleware.NewAuthenticate(f, subjectFor)

	handler := m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, found = auth.SubjectFrom(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if !found {
		t.Fatal("the handler saw no subject: nothing downstream can authorize")
	}
	if got.ID != "7" || got.Tenant != "acme" {
		t.Fatalf("subject = %+v, want id 7 of acme", got)
	}
	if !auth.Check(context.Background()) && f.used != "" {
		t.Fatalf("ShouldUse was told %q, want the default guard", f.used)
	}
}

// TestAuthenticateTriesEveryGuardItWasGiven pins the order: the first guard
// that answers wins, and it is the one ShouldUse is told about.
func TestAuthenticateTriesEveryGuardItWasGiven(t *testing.T) {
	f := newFactory(map[string]*guard{
		"web": {},
		"api": {signedIn: true, current: &user{id: "7"}},
	})

	var ran bool
	m := middleware.NewAuthenticate(f, subjectFor).Using("web", "api")
	m.Handle(ok(&ran)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !ran {
		t.Fatal("the handler did not run, though the api guard was signed in")
	}
	if f.used != "api" {
		t.Fatalf("ShouldUse = %q, want api", f.used)
	}
}

// TestUsingReturnsACopy is the reason Using does not mutate: one middleware
// bound two ways must not have the second binding overwrite the first.
func TestUsingReturnsACopy(t *testing.T) {
	f := newFactory(map[string]*guard{"web": {signedIn: true, current: &user{id: "7"}}, "api": {}})
	base := middleware.NewAuthenticate(f, subjectFor)

	web := base.Using("web")
	api := base.Using("api")

	var webRan, apiRan bool
	web.Handle(ok(&webRan)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	api.Handle(ok(&apiRan)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !webRan {
		t.Fatal("the web-bound middleware refused a request its guard was signed in on")
	}
	if apiRan {
		t.Fatal("the api-bound middleware let through a request nobody was signed in on")
	}
}

func TestAuthenticateWithBasicAuthMatchesOnEmailByDefault(t *testing.T) {
	g := &guard{}
	var ran bool
	m := middleware.NewAuthenticateWithBasicAuth(newFactory(map[string]*guard{"": g}), subjectFor)

	m.Handle(ok(&ran)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !ran {
		t.Fatal("the handler did not run, though the guard accepted the credentials")
	}
	if g.basicField != "email" {
		t.Fatalf("field = %q, want email -- Illuminate's `$field ?: 'email'`", g.basicField)
	}
}

func TestAuthenticateWithBasicAuthAsksTheBrowserForCredentials(t *testing.T) {
	g := &guard{basicErr: errors.New("no such account")}
	var ran bool
	m := middleware.NewAuthenticateWithBasicAuth(newFactory(map[string]*guard{"": g}), subjectFor).
		Using("", "username")

	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ran {
		t.Fatal("the handler ran on credentials the guard refused")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("WWW-Authenticate = %q: without it a 401 is a blank page", got)
	}
	if g.basicField != "username" {
		t.Fatalf("field = %q, want the one Using named", g.basicField)
	}
}

// gate answers a fixed verdict and records what it was asked.
type gate struct {
	err     error
	ability string
	args    []any
}

func (g *gate) Authorize(_ context.Context, _ auth.Subject, ability string, args ...any) (auth.Grant, error) {
	g.ability, g.args = ability, args
	if g.err != nil {
		return auth.Grant{}, g.err
	}
	return auth.SystemGrant(auth.Action(ability), "acme"), nil
}

func TestAuthorizeRefusesWhatThePolicyRefuses(t *testing.T) {
	var ran bool
	g := &gate{err: auth.ErrForbidden}
	m := middleware.NewAuthorize(g).Using("invoice.delete", "invoice")

	r := httptest.NewRequest(http.MethodDelete, "/invoices/114", nil)
	r = r.WithContext(auth.WithSubject(r.Context(), auth.Subject{ID: "7", Tenant: "acme"}))
	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, r)

	if ran {
		t.Fatal("the handler ran on an action the gate refused")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "forbidden") {
		t.Fatalf("body = %q: the policy's own sentence describes data the caller may not see", rec.Body.String())
	}
}

func TestAuthorizeRefusesARequestWithNoSubjectBeforeAskingTheGate(t *testing.T) {
	var ran bool
	g := &gate{}
	m := middleware.NewAuthorize(g).Using("invoice.delete")

	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/invoices/114", nil))

	if ran {
		t.Fatal("the handler ran without a subject")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 -- no subject is a session nobody loaded, not a refusal", rec.Code)
	}
	if g.ability != "" {
		t.Fatal("the gate was asked about nobody")
	}
}

// TestAuthorizeResolvesTheRouteParametersItWasNamed pins the lookup: the
// middleware parameters are the names of route parameters, and what reaches the
// gate is their values.
func TestAuthorizeResolvesTheRouteParametersItWasNamed(t *testing.T) {
	g := &gate{}
	m := middleware.NewAuthorize(g).Using("invoice.view", "invoice", `'draft'`, "models.Invoice")

	mux := http.NewServeMux()
	var ran bool
	mux.Handle("GET /invoices/{invoice}", m.Handle(ok(&ran)))
	mux.ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/invoices/114", nil)
		return r.WithContext(auth.WithSubject(r.Context(), auth.Subject{ID: "7", Tenant: "acme"}))
	}())

	if !ran {
		t.Fatal("the handler did not run on an allowed action")
	}
	if len(g.args) != 3 {
		t.Fatalf("args = %v, want three", g.args)
	}
	if g.args[0] != "114" {
		t.Fatalf("args[0] = %v, want the route parameter's value", g.args[0])
	}
	if g.args[1] != "draft" {
		t.Fatalf("args[1] = %v, want the quoted literal unquoted", g.args[1])
	}
	if g.args[2] != "models.Invoice" {
		t.Fatalf("args[2] = %v, want the class name passed through", g.args[2])
	}
}

func TestEnsureEmailIsVerifiedStopsAnAccountThatNeverConfirmed(t *testing.T) {
	var ran bool
	u := verifiable{&user{id: "7", email: "ada@example.com"}}
	f := newFactory(map[string]*guard{"": {signedIn: true, current: u}})
	m := middleware.NewEnsureEmailIsVerified(f)

	rec := httptest.NewRecorder()
	m.Handle(ok(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if ran {
		t.Fatal("the handler ran for an account whose address was never confirmed")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != middleware.VerificationNoticeURI {
		t.Fatalf("Location = %q, want %q", got, middleware.VerificationNoticeURI)
	}
}

func TestEnsureEmailIsVerifiedAnswers403ToAClientThatAskedForJSON(t *testing.T) {
	var ran bool
	u := verifiable{&user{id: "7", email: "ada@example.com"}}
	f := newFactory(map[string]*guard{"": {signedIn: true, current: u}})

	r := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	r.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	middleware.NewEnsureEmailIsVerified(f).Handle(ok(&ran)).ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not verified") {
		t.Fatalf("body = %q, want Illuminate's sentence", rec.Body.String())
	}
}

func TestEnsureEmailIsVerifiedLetsThroughAConfirmedAccount(t *testing.T) {
	var ran bool
	u := verifiable{&user{id: "7", email: "ada@example.com", verified: true}}
	f := newFactory(map[string]*guard{"": {signedIn: true, current: u}})

	middleware.NewEnsureEmailIsVerified(f).Handle(ok(&ran)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if !ran {
		t.Fatal("a confirmed account was stopped")
	}
}

// TestEnsureEmailIsVerifiedLetsThroughAnAccountWithNothingToVerify pins the
// type assertion: a user type that does not have to confirm an address is not
// asked to.
func TestEnsureEmailIsVerifiedLetsThroughAnAccountWithNothingToVerify(t *testing.T) {
	var ran bool
	f := newFactory(map[string]*guard{"": {signedIn: true, current: &user{id: "7"}}})

	middleware.NewEnsureEmailIsVerified(f).Handle(ok(&ran)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if !ran {
		t.Fatal("an account with no address to confirm was stopped")
	}
}

func TestEnsureEmailIsVerifiedSendsWhereRedirectToNamed(t *testing.T) {
	var ran bool
	u := verifiable{&user{id: "7", email: "ada@example.com"}}
	f := newFactory(map[string]*guard{"": {signedIn: true, current: u}})

	rec := httptest.NewRecorder()
	middleware.NewEnsureEmailIsVerified(f).RedirectTo("/confirm-your-address").
		Handle(ok(&ran)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if got := rec.Header().Get("Location"); got != "/confirm-your-address" {
		t.Fatalf("Location = %q", got)
	}
}

func TestRedirectIfAuthenticatedTakesASignedInPersonOffTheSignInScreen(t *testing.T) {
	var ran bool
	f := newFactory(map[string]*guard{"": {signedIn: true, current: &user{id: "7"}}})

	rec := httptest.NewRecorder()
	middleware.NewRedirectIfAuthenticated(f).
		RedirectUsing(func(*http.Request) string { return "/dashboard" }).
		Handle(ok(&ran)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if ran {
		t.Fatal("the sign-in screen was rendered to somebody already signed in")
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location = %q", got)
	}
}

func TestRedirectIfAuthenticatedLetsAGuestReachTheSignInScreen(t *testing.T) {
	var ran bool
	f := newFactory(map[string]*guard{"": {}})

	rec := httptest.NewRecorder()
	middleware.NewRedirectIfAuthenticated(f).Handle(ok(&ran)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if !ran {
		t.Fatalf("a guest was redirected away from the sign-in screen, to %q", rec.Header().Get("Location"))
	}
}

// TestRedirectIfAuthenticatedFallsBackToTheRoot pins what
// defaultRedirectUri became: the route-registry probe is not portable, the
// fallback is.
func TestRedirectIfAuthenticatedFallsBackToTheRoot(t *testing.T) {
	f := newFactory(map[string]*guard{"": {signedIn: true, current: &user{id: "7"}}})

	var ran bool
	rec := httptest.NewRecorder()
	middleware.NewRedirectIfAuthenticated(f).Handle(ok(&ran)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if got := rec.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want /", got)
	}
}

// withConfirmation returns a request carrying a session whose password was
// confirmed `ago` in the past. A negative ago means never.
func withConfirmation(t *testing.T, ago time.Duration) *http.Request {
	t.Helper()

	store := session.NewStore("arandu_session", session.NewArraySessionHandler(time.Hour), "")
	if ago >= 0 {
		store.Put(session.PasswordConfirmedKey, time.Now().Add(-ago).Unix())
	}

	r := httptest.NewRequest(http.MethodPost, "/settings/email", nil)
	return r.WithContext(sessionmw.WithSession(r.Context(), store))
}

func TestRequirePasswordAsksAgainWhenTheWindowHasPassed(t *testing.T) {
	var ran bool
	rec := httptest.NewRecorder()
	middleware.NewRequirePassword(time.Hour).Handle(ok(&ran)).
		ServeHTTP(rec, withConfirmation(t, 2*time.Hour))

	if ran {
		t.Fatal("a sensitive action ran on a session whose confirmation had expired")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != middleware.PasswordConfirmURI {
		t.Fatalf("Location = %q, want %q", got, middleware.PasswordConfirmURI)
	}
}

func TestRequirePasswordLetsThroughARecentConfirmation(t *testing.T) {
	var ran bool
	middleware.NewRequirePassword(time.Hour).Handle(ok(&ran)).
		ServeHTTP(httptest.NewRecorder(), withConfirmation(t, time.Minute))

	if !ran {
		t.Fatal("a password typed a minute ago was not accepted")
	}
}

// TestRequirePasswordAsksWhenNothingWasEverConfirmed is the direction this has
// to fail in: an absent stamp is unconfirmed, never recent.
func TestRequirePasswordAsksWhenNothingWasEverConfirmed(t *testing.T) {
	var ran bool
	middleware.NewRequirePassword(time.Hour).Handle(ok(&ran)).
		ServeHTTP(httptest.NewRecorder(), withConfirmation(t, -1))

	if ran {
		t.Fatal("a session that never confirmed a password walked past the guard")
	}
}

// TestRequirePasswordAsksWhenThereIsNoSessionAtAll is the same rule for a
// request that never loaded one.
func TestRequirePasswordAsksWhenThereIsNoSessionAtAll(t *testing.T) {
	var ran bool
	middleware.NewRequirePassword(time.Hour).Handle(ok(&ran)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/settings/email", nil))

	if ran {
		t.Fatal("a request with no session walked past the password guard")
	}
}

func TestRequirePasswordAnswers423ToAClientThatAskedForJSON(t *testing.T) {
	var ran bool
	r := withConfirmation(t, 2*time.Hour)
	r.Header.Set("Accept", "application/json")

	rec := httptest.NewRecorder()
	middleware.NewRequirePassword(time.Hour).Handle(ok(&ran)).ServeHTTP(rec, r)

	if rec.Code != http.StatusLocked {
		t.Fatalf("status = %d, want 423 -- the credentials are fine, the resource is closed", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Password confirmation required.") {
		t.Fatalf("body = %q, want Illuminate's message", rec.Body.String())
	}
}

// TestRequirePasswordDefaultsToTheFrameworkWindow pins that a zero timeout is
// session.PasswordConfirmationWindow.
func TestRequirePasswordDefaultsToTheFrameworkWindow(t *testing.T) {
	var justInside, justOutside bool

	middleware.NewRequirePassword(0).Handle(ok(&justInside)).
		ServeHTTP(httptest.NewRecorder(), withConfirmation(t, session.PasswordConfirmationWindow-time.Minute))
	middleware.NewRequirePassword(0).Handle(ok(&justOutside)).
		ServeHTTP(httptest.NewRecorder(), withConfirmation(t, session.PasswordConfirmationWindow+time.Minute))

	if !justInside {
		t.Fatalf("a confirmation inside the %v window was refused", session.PasswordConfirmationWindow)
	}
	if justOutside {
		t.Fatalf("a confirmation outside the %v window was accepted", session.PasswordConfirmationWindow)
	}
}

// TestRequirePasswordUsingOverridesBoth is RequirePassword::using.
func TestRequirePasswordUsingOverridesBoth(t *testing.T) {
	var ran bool
	rec := httptest.NewRecorder()
	middleware.NewRequirePassword(time.Hour).Using("/password/confirm", time.Minute).
		Handle(ok(&ran)).
		ServeHTTP(rec, withConfirmation(t, 10*time.Minute))

	if ran {
		t.Fatal("a confirmation older than the minute Using named was accepted")
	}
	if got := rec.Header().Get("Location"); got != "/password/confirm" {
		t.Fatalf("Location = %q", got)
	}
}
