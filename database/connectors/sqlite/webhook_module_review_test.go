package sqlite_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/webhook"
)

// TestAnEndpointOnAllEventsReceivesOnlyTheEventsServerEventsLists: with
// Server.Events set, an endpoint subscribed to "*" was sent every event the
// outbox relayed in its tenant -- a registration, a password reset -- because
// the list was asked only by the form.
func TestAnEndpointOnAllEventsReceivesOnlyTheEventsServerEventsLists(t *testing.T) {
	t.Parallel()
	target := newReceiver(t)
	w := sendingWorld(t, target, nil)
	cookies := w.signIn(t, manager(tenant))
	createEndpoint(t, w, cookies, `{"name":"Everything","url":"`+target.srv.URL+`/all","events":["*"],"active":true}`)

	ctx := context.Background()
	for i, name := range []string{"auth.user.registered", "auth.password.reset"} {
		stored := events.Stored{ID: fmt.Sprintf("evt-outside-%d", i), TenantID: tenant, Name: name, Payload: `{"email":"ana@example.test"}`, OccurredAt: time.Now().UTC()}
		if err := w.module.Publisher().Publish(ctx, stored); err != nil {
			t.Fatalf("publishing %s: %v", name, err)
		}
	}
	if n := w.pending(t); n != 0 {
		t.Fatalf("%d deliveries wait for events outside Server.Events, want none", n)
	}
	stored := events.Stored{ID: "evt-paid", TenantID: tenant, Name: "order.paid", Payload: `{}`, OccurredAt: time.Now().UTC()}
	if err := w.module.Publisher().Publish(ctx, stored); err != nil {
		t.Fatalf("publishing order.paid: %v", err)
	}
	if n := w.pending(t); n != 1 {
		t.Fatalf("%d deliveries wait for order.paid, want the one endpoint on AllEvents", n)
	}
}

// TestAnEndpointSubscribedBehindFiveHundredActiveOnesReceivesTheEvent: the
// publisher read 500 active endpoints and then filtered them, so one
// subscribed endpoint whose id sorted after 500 others was left out.
func TestAnEndpointSubscribedBehindFiveHundredActiveOnesReceivesTheEvent(t *testing.T) {
	t.Parallel()
	target := newReceiver(t)
	w := sendingWorld(t, target, nil)
	ctx := context.Background()
	for i := range 505 {
		events := []string{"order.refunded"}
		if i == 504 {
			events = []string{"order.paid"}
		}
		if _, err := w.module.EndpointService().Import(ctx, importer(tenant), webhook.ImportRequest{
			ID:              fmt.Sprintf("ep_%04d", i),
			EndpointRequest: webhook.EndpointRequest{Name: fmt.Sprintf("Endpoint %d", i), URL: target.srv.URL + "/hooks", Events: events, Active: true},
			Secret:          strings.Repeat("s", 32),
		}); err != nil {
			t.Fatalf("importing endpoint %d: %v", i, err)
		}
	}
	stored := events.Stored{ID: "evt-paid", TenantID: tenant, Name: "order.paid", Payload: `{}`, OccurredAt: time.Now().UTC()}
	if err := w.module.Publisher().Publish(ctx, stored); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	if n := w.pending(t); n != 1 {
		t.Fatalf("%d deliveries wait, want the one endpoint subscribed to order.paid", n)
	}
}

// TestAnUpdateThatMovesTheAddressDropsTheStoredHeaders: an update to a new
// URL with no headers kept the sealed ones, so whoever could update an
// endpoint could read them off a delivery to a server of their own.
func TestAnUpdateThatMovesTheAddressDropsTheStoredHeaders(t *testing.T) {
	t.Parallel()
	target := newReceiver(t)
	w := sendingWorld(t, target, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	created := createEndpoint(t, w, cookies, `{"name":"Billing","url":"`+target.srv.URL+`/hooks","events":["order.paid"],"active":true,"headers":"Authorization: Bearer partner-key"}`)

	update := func(url string) {
		t.Helper()
		if _, err := w.module.EndpointService().Update(ctx, manager(tenant), created.Data.ID, webhook.EndpointRequest{
			Name: "Billing", URL: url, Events: []string{"order.paid"}, Active: true,
		}); err != nil {
			t.Fatalf("updating: %v", err)
		}
	}
	deliver := func(id string) received {
		t.Helper()
		if err := w.module.Publisher().Publish(ctx, events.Stored{ID: id, TenantID: tenant, Name: "order.paid", Payload: `{}`, OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatalf("publishing: %v", err)
		}
		if err := w.work(t); err != nil {
			t.Fatalf("delivering: %v", err)
		}
		got := target.requests()
		return got[len(got)-1]
	}

	update(target.srv.URL + "/hooks")
	if got := deliver("evt-same").header.Get("Authorization"); got != "Bearer partner-key" {
		t.Fatalf("an update that kept the address dropped the headers: Authorization %q", got)
	}
	update(target.srv.URL + "/elsewhere")
	if got := deliver("evt-moved").header.Get("Authorization"); got != "" {
		t.Fatalf("an update that moved the address kept the headers: Authorization %q", got)
	}
}

// TestAReceiverKeepsEveryHeaderButTheSecretItChecks: StoreHeaders {"*"} kept
// the HeaderSecret validator's own header in the call row, and URLFull kept
// a token in the query.
func TestAReceiverKeepsEveryHeaderButTheSecretItChecks(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) {
		c.Receivers = []webhook.Receiver{{
			Name: "provider", Tenant: tenant, Secret: sharedSecret, URL: webhook.URLFull,
			Validator: webhook.HeaderSecret{Header: "X-Provider-Secret"}, StoreHeaders: []string{"*"},
		}}
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/provider?event=paid&token=query-token", strings.NewReader(`{"ok":true}`))
	req.Header.Set("X-Provider-Secret", sharedSecret)
	req.Header.Set("Authorization", "Bearer bearer-token")
	req.Header.Set("X-Request-Id", "req-1")
	if rec := w.send(t, req); rec.Code != http.StatusOK {
		t.Fatalf("the call answered %d %s", rec.Code, rec.Body.String())
	}
	calls := w.calls(t, tenant)
	if len(calls) != 1 {
		t.Fatalf("%d calls stored", len(calls))
	}
	stored := calls[0].HeaderData + " " + calls[0].URL
	for _, leaked := range []string{sharedSecret, "bearer-token", "query-token"} {
		if strings.Contains(stored, leaked) {
			t.Errorf("the stored call keeps %q: %s", leaked, stored)
		}
	}
	if !strings.Contains(calls[0].HeaderData, "req-1") || !strings.Contains(calls[0].URL, "event=paid") {
		t.Errorf("the stored call lost what is not a credential: %s", stored)
	}
}
