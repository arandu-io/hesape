package webhook

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	httpclient "github.com/arandu-io/hesape/http/client"
)

// A client the application gives keeps where it may connect, which is what a
// test that delivers to a local server needs, and GuardHTTPClient takes that
// away: the client keeps its timeouts and pool and gains the guarded dialer.
func TestAGivenClientReachesLoopbackUnlessItIsGuarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	own := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}

	plain, err := newDeliveryClient(own, false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := plain.Get(server.URL)
	if err != nil {
		t.Fatalf("an unguarded given client should keep its own reach: %v", err)
	}
	resp.Body.Close()

	guarded, err := newDeliveryClient(own, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guarded.Get(server.URL); !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("a guarded given client reached %s: %v", server.URL, err)
	}
	if own.Transport == guarded.Transport {
		t.Fatal("the application's client was changed in place")
	}
	// The client given is left as it was: its own reach did not change.
	resp, err = own.Get(server.URL)
	if err != nil {
		t.Fatalf("guarding a copy changed the client that was given: %v", err)
	}
	resp.Body.Close()
}

func TestAGuardedGivenClientRefusesEveryHopOfARedirectInside(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://100.64.0.1:9/", http.StatusFound)
	}))
	defer public.Close()

	// The first hop is loopback too, so it is the guard of the first hop that
	// refuses: there is no way to name a public listener without a network.
	client, err := newDeliveryClient(&http.Client{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(public.URL); !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("got %v", err)
	}
}

func TestAGuardedGivenClientDropsTheProxyOfItsCopy(t *testing.T) {
	asked := 0
	proxied := http.DefaultTransport.(*http.Transport).Clone()
	proxied.Proxy = func(*http.Request) (*url.URL, error) { asked++; return nil, nil }
	client, err := newDeliveryClient(&http.Client{Transport: proxied}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get("http://127.0.0.1:9/"); !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("got %v", err)
	}
	if asked != 0 {
		t.Fatal("the guarded copy consulted the proxy of the transport it was made from")
	}
	if proxied.Proxy == nil {
		t.Fatal("the transport the application gave was changed in place")
	}
}

func TestAGuardedGivenClientNeedsATransportTheDialerCanGoIn(t *testing.T) {
	for name, c := range map[string]struct {
		client *http.Client
		says   string
	}{
		"a transport that is not an *http.Transport": {&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}, "*http.Transport"},
		"a transport that dials TLS itself":          {&http.Client{Transport: &http.Transport{DialTLSContext: nil, DialTLS: func(string, string) (net.Conn, error) { return nil, nil }}}, "dials TLS"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newDeliveryClient(c.client, true)
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want an error naming %q", err, c.says)
			}
			if _, err := newDeliveryClient(c.client, false); err != nil {
				t.Fatalf("a client that is not guarded keeps working as it did: %v", err)
			}
		})
	}
}
