package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/webhook"
)

// TestKeepingThePathBelongsToARelayEndpoint: the flag decides where a relayed
// request goes, and a signed endpoint never relays one, so a form that sets it
// on an endpoint of another dialect is refused before the data is reached.
func TestKeepingThePathBelongsToARelayEndpoint(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{}, nil)
	manager := auth.Subject{ID: "user-1", Tenant: "acme", Actions: webhook.Actions(), Verified: true}
	_, _, err := module.EndpointService().Create(context.Background(), manager, webhook.EndpointRequest{
		Name: "Billing", URL: "https://example.com/hooks", Events: []string{"order.paid"}, KeepPath: true,
	})
	var rejected validation.Errors
	if !errors.As(err, &rejected) || len(rejected["keep_path"]) == 0 {
		t.Fatalf("Create answered %v, want keep_path rejected", err)
	}
}

// TestARelayEndpointIsAskedForNoEvent: it subscribes to nothing and signs
// nothing, so the address is the only thing a form can get wrong about it.
func TestARelayEndpointIsAskedForNoEvent(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{}, nil)
	manager := auth.Subject{ID: "user-1", Tenant: "acme", Actions: webhook.Actions(), Verified: true}
	_, _, err := module.EndpointService().Create(context.Background(), manager, webhook.EndpointRequest{
		Name: "Old system", URL: "not an address", Dialect: webhook.DialectRelay, KeepPath: true,
	})
	var rejected validation.Errors
	if !errors.As(err, &rejected) || len(rejected["url"]) == 0 {
		t.Fatalf("Create answered %v, want the address rejected", err)
	}
	if len(rejected["events"]) != 0 || len(rejected["keep_path"]) != 0 || len(rejected["dialect"]) != 0 {
		t.Fatalf("a relay endpoint was refused for more than its address: %v", rejected)
	}
}
