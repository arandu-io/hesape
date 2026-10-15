package webhook_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/webhook"
)

// These tests drive the Module the way an application does: build it, mount
// its routes, make a request. The database handle wraps nothing, and that is
// the assertion: a request that reached a statement would panic, so every
// answer below is proof that the refusal came before the data.

var _ foundation.Module = (*webhook.Module)(nil)

// managementRoutes are the routes behind the guard, by method, path under
// the default prefix, and the name they were published with.
var managementRoutes = [][3]string{
	{http.MethodGet, "/webhook/endpoints", "webhook.endpoints.index"},
	{http.MethodPost, "/webhook/endpoints", "webhook.endpoints.store"},
	{http.MethodGet, "/webhook/endpoints/{id}", "webhook.endpoints.show"},
	{http.MethodPut, "/webhook/endpoints/{id}", "webhook.endpoints.update"},
	{http.MethodDelete, "/webhook/endpoints/{id}", "webhook.endpoints.destroy"},
	{http.MethodGet, "/webhook/endpoints/{id}/secret", "webhook.endpoints.secret"},
	{http.MethodPost, "/webhook/endpoints/{id}/test", "webhook.endpoints.test"},
	{http.MethodGet, "/webhook/endpoints/{id}/deliveries", "webhook.endpoints.deliveries"},
	{http.MethodPost, "/webhook/endpoints/{id}/rotate", "webhook.endpoints.rotate"},
	{http.MethodPost, "/webhook/endpoints/{id}/enable", "webhook.endpoints.enable"},
	{http.MethodPost, "/webhook/endpoints/{id}/disable", "webhook.endpoints.disable"},
	{http.MethodGet, "/webhook/deliveries/{id}", "webhook.deliveries.show"},
	{http.MethodGet, "/webhook/deliveries/{id}/attempts", "webhook.deliveries.attempts"},
	{http.MethodPost, "/webhook/deliveries/{id}/redeliver", "webhook.deliveries.redeliver"},
	{http.MethodGet, "/webhook/calls", "webhook.calls.index"},
	{http.MethodGet, "/webhook/calls/{id}", "webhook.calls.show"},
	{http.MethodPost, "/webhook/calls/{id}/process", "webhook.calls.process"},
}

func process(context.Context, auth.Grant, *webhook.Call) error { return nil }

// billing is a static receiver answering POST and PUT.
func billing() webhook.Receiver {
	return webhook.Receiver{Name: "billing", Tenant: "acme", Secret: testKey, Methods: []string{"post", "put"},
		Process: webhook.ProcessorFunc(process)}
}

func newModule(t *testing.T, cfg webhook.Config, guard pipeline.Middleware[http.Handler]) *webhook.Module {
	t.Helper()
	cfg.Key = []byte(testKey)
	module, err := webhook.New(cfg, database.Wrap(nil, database.DialectSQLite), guard)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return module
}

// signedIn is a guard that admits every request as the subject.
func signedIn(subject auth.Subject) pipeline.Middleware[http.Handler] {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithSubject(r.Context(), subject)))
		})
	}
}

// refusing is a guard that admits nobody.
func refusing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
}

func TestHTTPActionsKeepThePublishedRoutesAndPutTheGuardOnTheManagementOnes(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{Receivers: []webhook.Receiver{billing()}}, refusing)
	actions := module.HTTPActions()
	if len(actions) != 2+len(managementRoutes) {
		t.Fatalf("HTTPActions() = %d actions, want %d", len(actions), 2+len(managementRoutes))
	}
	for i, want := range [][3]string{
		{http.MethodPost, "/webhooks/billing", "webhook.receive.billing"},
		{http.MethodPut, "/webhooks/billing", "webhook.receive.billing.put"},
	} {
		got := actions[i]
		if got.Method != want[0] || got.Path != want[1] || got.Name != want[2] || got.Action == nil || len(got.Middleware) != 0 {
			t.Errorf("receiver action %d = %s %s %q with %d middleware, want %v and none", i, got.Method, got.Path, got.Name, len(got.Middleware), want)
		}
	}
	for i, want := range managementRoutes {
		got := actions[2+i]
		if got.Method != want[0] || got.Path != want[1] || got.Name != want[2] || got.Action == nil {
			t.Errorf("management action %d = %s %s %q, want %v", i, got.Method, got.Path, got.Name, want)
		}
		if len(got.Middleware) != 1 {
			t.Errorf("%s sits behind %d middleware, want the guard alone", got.Name, len(got.Middleware))
		}
	}
}

func TestWithNoGuardTheModuleMountsOnlyItsReceivers(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{Receivers: []webhook.Receiver{billing()}}, nil)
	for _, action := range module.HTTPActions() {
		if !strings.HasPrefix(action.Name, "webhook.receive.") {
			t.Fatalf("with no guard the module registered %s %s", action.Method, action.Path)
		}
	}
	r := routing.NewRouter()
	module.Routes(r)
	if routes := r.Routes(); len(routes) != 2 {
		t.Fatalf("Routes registered %d routes with no guard, want the receiver's 2", len(routes))
	}
}

func TestRoutesRegisterEveryActionOnTheRouterUnderItsName(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{Prefix: "/widgets", Receivers: []webhook.Receiver{billing()}}, refusing)
	r := routing.NewRouter()
	module.Routes(r.ForModule(module.Name()))
	actions := module.HTTPActions()
	routes := r.Routes()
	if len(routes) != len(actions) {
		t.Fatalf("Routes registered %d routes for %d actions", len(routes), len(actions))
	}
	for i, route := range routes {
		if route.Method != actions[i].Method || route.Pattern != actions[i].Path || route.RouteName() != actions[i].Name || route.Module != "webhook" {
			t.Errorf("route %d = %s %s %q in %q, want %s %s %q", i, route.Method, route.Pattern, route.RouteName(), route.Module,
				actions[i].Method, actions[i].Path, actions[i].Name)
		}
		if strings.HasPrefix(route.Pattern, "/_arandu/") {
			t.Errorf("%s lands in the framework's namespace", route.Pattern)
		}
	}
}

// request makes one request against the module's routes on routing.Router.
func request(t *testing.T, module *webhook.Module, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := routing.NewRouter()
	module.Routes(r)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for name, value := range header {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s %s panicked: %v", method, target, recovered)
		}
	}()
	r.ServeHTTP(rec, req)
	return rec
}

func concrete(path string) string {
	return strings.NewReplacer("{id}", "x-1").Replace(path)
}

func TestTheGuardStandsBeforeEveryManagementRoute(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{}, refusing)
	for _, route := range managementRoutes {
		if rec := request(t, module, route[0], concrete(route[1]), "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d past a guard that admits nobody", route[0], route[1], rec.Code)
		}
	}
}

func TestASignedInSubjectIsRefusedByThePolicyBeforeTheData(t *testing.T) {
	t.Parallel()

	administrator := auth.Subject{ID: "user-1", Tenant: "acme", Roles: []string{"admin"}, Verified: true}
	module := newModule(t, webhook.Config{}, signedIn(administrator))
	form := `{"name":"Billing","url":"https://example.com/hooks","events":["order.paid"],"active":true}`
	for _, route := range managementRoutes {
		body := ""
		if route[0] == http.MethodPost || route[0] == http.MethodPut {
			body = form
		}
		rec := request(t, module, route[0], concrete(route[1]), body,
			map[string]string{"Accept": "application/json", "Content-Type": "application/json"})
		if rec.Code != http.StatusForbidden || rec.Header().Get("Content-Type") != exception.ProblemContentType {
			t.Errorf("%s %s answered %d %q, want a 403 problem", route[0], route[1], rec.Code, rec.Header().Get("Content-Type"))
		}
	}

	// A request that does not ask for JSON gets the status and its sentence.
	rec := request(t, module, http.MethodGet, "/webhook/endpoints", "", nil)
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != http.StatusText(http.StatusForbidden) {
		t.Fatalf("a refusal to a page answered %d %q", rec.Code, rec.Body.String())
	}
}

func TestARejectedInputIsAnsweredAsAValidationProblem(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{}, signedIn(auth.Subject{ID: "user-1", Tenant: "acme", Verified: true}))
	rec := request(t, module, http.MethodPost, "/webhook/endpoints", `{"name":""}`,
		map[string]string{"Accept": "application/json", "Content-Type": "application/json"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty name answered %d", rec.Code)
	}
	var problem exception.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || len(problem.Errors["name"]) == 0 {
		t.Fatalf("the problem %s carries no message for name: %v", rec.Body.String(), err)
	}
}

func TestAReceiverRefusalIsAnsweredAsTheReceiverSays(t *testing.T) {
	t.Parallel()

	texted, empty := billing(), billing()
	empty.Name, empty.RefusalBody = "quiet", webhook.RefusalEmpty
	module := newModule(t, webhook.Config{Receivers: []webhook.Receiver{texted, empty}}, nil)

	rec := request(t, module, http.MethodPost, "/webhooks/billing", `{"a":1}`, map[string]string{"Signature": "00"})
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != http.StatusText(http.StatusForbidden) {
		t.Fatalf("a bad signature answered %d %q, want 403 and its sentence", rec.Code, rec.Body.String())
	}
	rec = request(t, module, http.MethodPost, "/webhooks/billing", `{"a":1}`, map[string]string{"Signature": "00", "Accept": "application/json"})
	if rec.Code != http.StatusForbidden || rec.Header().Get("Content-Type") != exception.ProblemContentType {
		t.Fatalf("a bad signature wanting JSON answered %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = request(t, module, http.MethodPost, "/webhooks/quiet", `{"a":1}`, map[string]string{"Signature": "00"})
	if rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
		t.Fatalf("RefusalEmpty answered %d %q, want the status alone", rec.Code, rec.Body.String())
	}
}

func TestTheRefusalsNameTheStatusTheyAreAnsweredWith(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]int{
		webhook.ErrNotFound:           http.StatusNotFound,
		webhook.ErrInvalidSignature:   http.StatusForbidden,
		webhook.ErrBodyTooLarge:       http.StatusRequestEntityTooLarge,
		webhook.ErrDeliveryIDTaken:    http.StatusConflict,
		webhook.ErrEndpointExists:     http.StatusConflict,
		webhook.ErrDeliveryInProgress: http.StatusConflict,
		webhook.ErrNotRedeliverable:   http.StatusConflict,
	} {
		if got, ok := exception.StatusOf(err); !ok || got != want {
			t.Errorf("StatusOf(%v) = %d, %v, want %d", err, got, ok, want)
		}
	}
}

func TestReceivePathsListEveryAddressToExemptFromCSRF(t *testing.T) {
	t.Parallel()

	mounted := billing()
	mounted.Name, mounted.Methods, mounted.Mount = "lpr", nil, "/integrations/lpr/{org}/{key}"
	module := newModule(t, webhook.Config{Receivers: []webhook.Receiver{billing(), mounted}}, nil)
	if got := module.ReceivePaths(); !slices.Equal(got, []string{"/webhooks/", "/integrations/lpr/"}) {
		t.Fatalf("ReceivePaths() = %v", got)
	}
}

func TestTheModuleNeverDeclaresManagersTable(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{}, nil)
	names := []string{}
	for _, migration := range module.Migrations() {
		if _, ok := migration.(webhook.CreateDeliveriesTable); ok {
			t.Fatal("Module.Migrations declares CreateDeliveriesTable")
		}
		names = append(names, migration.GetName())
	}
	want := []string{
		"20261014_0001_create_webhook_server_endpoints",
		"20261014_0002_create_webhook_client_calls",
		"20261014_0003_add_dialect_and_policy_to_webhook_server_endpoints",
		"20261014_0004_create_webhook_server_deliveries",
		"20261014_0005_create_webhook_server_delivery_attempts",
		"20261015_0006_add_reference_to_webhook_client_calls",
		"20261015_0007_add_remote_addr_and_headers_digest_to_webhook_client_calls",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("Migrations() = %v, want the names they were published with: %v", names, want)
	}
}
