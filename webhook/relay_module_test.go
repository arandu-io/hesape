package webhook_test

import (
	"context"
	"errors"
	"net/netip"
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

// TestAnEndpointAtAnInternalAddressIsRefusedOnTheModulesOwnTransport: the
// deliveries go out on a transport that refuses internal addresses, so an
// endpoint that could never be reached is refused when it is stored, and says
// why, for a literal address and for a name that resolves to one.
func TestAnEndpointAtAnInternalAddressIsRefusedOnTheModulesOwnTransport(t *testing.T) {
	t.Parallel()

	module := newModule(t, webhook.Config{Server: webhook.Server{Resolver: hostsAt{"rebind.example.com": "169.254.169.254"}}}, nil)
	manager := auth.Subject{ID: "user-1", Tenant: "acme", Actions: webhook.Actions(), Verified: true}
	for _, address := range []string{
		"https://localhost/hook", "https://127.0.0.1/hook", "https://10.0.0.5/hook", "https://169.254.169.254/hook",
		"https://[::1]/hook", "https://100.64.0.1/hook", "https://rebind.example.com/hook",
	} {
		_, _, err := module.EndpointService().Create(context.Background(), manager, webhook.EndpointRequest{
			Name: "Billing", URL: address, Events: []string{"order.paid"},
		})
		var rejected validation.Errors
		if !errors.As(err, &rejected) || len(rejected["url"]) == 0 {
			t.Errorf("Create(%s) answered %v, want the address rejected", address, err)
		}
		_, err = module.EndpointService().Update(context.Background(), manager, "any", webhook.EndpointRequest{
			Name: "Billing", URL: address, Events: []string{"order.paid"},
		})
		if !errors.As(err, &rejected) || len(rejected["url"]) == 0 {
			t.Errorf("Update(%s) answered %v, want the address rejected", address, err)
		}
	}
}

// hostsAt answers each host with one address, and every other name with none.
type hostsAt map[string]string

func (h hostsAt) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if addr, ok := h[host]; ok {
		return []netip.Addr{netip.MustParseAddr(addr)}, nil
	}
	return nil, errors.New("no such host")
}
