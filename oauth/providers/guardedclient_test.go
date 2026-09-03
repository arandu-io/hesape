package providers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	httpclient "github.com/arandu-io/hesape/http/client"
	"github.com/arandu-io/hesape/oauth/providers"
)

// TestTheDefaultClientRefusesAnEndpointInsideTheNetwork proves the provider
// that was given no client dials through the guard.
//
// Two of the constructors here take the three endpoints as strings, so an
// application that reads them from configuration can end up pointing the token
// exchange at an address inside its own network -- and a metadata service
// answers that kind of request with credentials. The refusal has to be on the
// client the provider reaches for when nobody supplied one, because that is the
// one nobody looked at.
//
// The server is on loopback, which is inside the network by the same rule, and
// its address is never named to the factory. Reaching it would mean the default
// client dials anywhere it is pointed.
func TestTheDefaultClientRefusesAnEndpointInsideTheNetwork(t *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.Write([]byte("access_token=stolen"))
	}))
	defer server.Close()

	store := &fakeStore{state: "bar"}
	p := providers.NewProvider(store, "client", "secret",
		server.URL+"/authorize", server.URL+"/token", server.URL+"/me")

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	_, err := p.GetAccessToken(r)

	if !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("GetAccessToken = %v, want a refusal wrapping ErrInternalAddress", err)
	}
	if reached {
		t.Error("the request reached a server inside the network")
	}
}

// TestASuppliedClientIsStillTheOneUsed is the other half: the guard is the
// default and not a wall.
//
// An application that has its own client -- one that declares the internal host
// it talks to, or one that records what it sent -- keeps it. A change that
// routed every provider through the guarded client regardless would pass the
// test above and break every deployment with a private identity provider.
func TestASuppliedClientIsStillTheOneUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("access_token=token"))
	}))
	defer server.Close()

	store := &fakeStore{state: "bar"}
	p := providers.NewProvider(store, "client", "secret",
		server.URL+"/authorize", server.URL+"/token", server.URL+"/me").
		SetHTTPClient(httpclient.NewFactory(nil).AllowInternalHosts("127.0.0.1").Client())

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	token, err := p.GetAccessToken(r)
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if token.GetValue() != "token" {
		t.Errorf("token = %q, want token", token.GetValue())
	}
}
