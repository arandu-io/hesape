package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// TestIsInternalAddressRefusesEveryRangeNoPublicHostAnswersOn is the one list
// the guard and the webhook deliveries refuse on, held against the ranges the
// standard library alone lets through (global unicast, not private) and the
// IPv6 forms that carry an IPv4 address a translator reaches.
func TestIsInternalAddressRefusesEveryRangeNoPublicHostAnswersOn(t *testing.T) {
	for _, c := range []struct {
		address  string
		internal bool
		why      string
	}{
		{"100.64.0.1", true, "carrier-grade NAT"},
		{"100.127.255.254", true, "the top of carrier-grade NAT"},
		{"198.18.0.1", true, "benchmarking"},
		{"198.19.255.254", true, "the top of benchmarking"},
		{"0.0.0.0", true, "unspecified"},
		{"0.1.2.3", true, "this network"},
		{"192.0.0.1", true, "IETF protocol block"},
		{"192.0.2.1", true, "documentation"},
		{"198.51.100.1", true, "documentation"},
		{"203.0.113.1", true, "documentation"},
		{"240.0.0.1", true, "class E"},
		{"255.255.255.255", true, "limited broadcast"},
		{"169.254.169.254", true, "the cloud metadata service"},
		{"224.0.0.1", true, "multicast"},
		{"::ffff:127.0.0.1", true, "a mapped loopback"},
		{"::ffff:100.64.0.1", true, "a mapped shared address"},
		{"::ffff:0:10.0.0.1", true, "a stateless-translated private address"},
		{"64:ff9b::a00:1", true, "NAT64 of 10.0.0.1"},
		{"64:ff9b::7f00:1", true, "NAT64 of 127.0.0.1"},
		{"64:ff9b::a9fe:a9fe", true, "NAT64 of the metadata service"},
		{"64:ff9b:1:a00:0:1::", true, "local-use NAT64 of 10.0.0.1 in a /48"},
		{"2002:0a00:0001::1", true, "6to4 of 10.0.0.1"},
		{"2002:7f00:0001::1", true, "6to4 of 127.0.0.1"},
		{"2002:6440:0001::1", true, "6to4 of 100.64.0.1"},
		{"::10.0.0.1", true, "IPv4-compatible 10.0.0.1"},
		{"::1", true, "loopback"},
		{"::", true, "unspecified"},
		{"fe80::1", true, "link-local"},
		{"fe80::1%eth0", true, "link-local with a zone"},
		{"fc00::1", true, "unique-local"},
		{"fec0::1", true, "site-local"},
		{"ff02::1", true, "multicast"},
		{"100::1", true, "the discard prefix"},
		{"2001::1", true, "Teredo"},
		{"2001:db8::1", true, "documentation"},

		{"8.8.8.8", false, "a public IPv4 address"},
		{"93.184.216.34", false, "a public IPv4 address"},
		{"100.63.255.255", false, "just below carrier-grade NAT"},
		{"100.128.0.1", false, "just above carrier-grade NAT"},
		{"198.17.255.255", false, "just below benchmarking"},
		{"198.20.0.1", false, "just above benchmarking"},
		{"::ffff:8.8.8.8", false, "a mapped public address"},
		{"64:ff9b::808:808", false, "NAT64 of a public host"},
		{"2002:0808:0808::1", false, "6to4 of a public host"},
		{"2606:4700:4700::1111", false, "a public IPv6 address"},
		{"2a00:1450:4001:81b::200e", false, "a public IPv6 address"},
	} {
		addr, err := netip.ParseAddr(c.address)
		if err != nil {
			t.Fatalf("parsing %s: %v", c.address, err)
		}
		if got := IsInternalAddress(addr); got != c.internal {
			t.Errorf("IsInternalAddress(%s) = %v, want %v (%s)", c.address, got, c.internal, c.why)
		}
	}
	if !IsInternalAddress(netip.Addr{}) {
		t.Error("the zero address is not an address to connect to")
	}
}

// resolvingTransport answers every name with the one address it was given, and
// connects through the dialer of this package, which is what checks the address
// between resolving a name and connecting to it. It is a resolver that is
// faked and a dialer that is not.
func resolvingTransport(address string) *http.Transport {
	tr := newTransport()
	tr.Proxy = nil
	dial := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		return dial(ctx, network, net.JoinHostPort(address, port))
	}
	return tr
}

// TestADialThatResolvesToAnySpecialRangeIsRefused drives a real request whose
// name resolves, by a stand-in resolver, to each address a plain
// IsGlobalUnicast-and-not-private check lets through. The refusal is raised by
// the dialer, before any connection, so nothing is sent and the test needs no
// network.
func TestADialThatResolvesToAnySpecialRangeIsRefused(t *testing.T) {
	for _, address := range []string{
		"100.64.0.1", "198.18.0.1", "64:ff9b::a00:1", "2002:0a00:0001::1",
		"::ffff:127.0.0.1", "169.254.169.254", "0.0.0.0", "64:ff9b:1:a00:0:1::", "240.0.0.1",
	} {
		t.Run(address, func(t *testing.T) {
			factory := NewFactory(&http.Client{Transport: resolvingTransport(address)})
			_, err := factory.CreatePendingRequest().Get(t.Context(), "http://rebinding.example.test/", nil)
			if !errors.Is(err, ErrInternalAddress) {
				t.Fatalf("a name resolving to %s should be refused, got %v", address, err)
			}
			if !strings.Contains(err.Error(), "rebinding.example.test") {
				t.Fatalf("the refusal should name the host asked for: %v", err)
			}
		})
	}
}

// TestADialThatResolvesToAPublicAddressIsNotRefused is the other side: the
// address the dialer is about to connect to passes, so the guard is not a wall.
func TestADialThatResolvesToAPublicAddressIsNotRefused(t *testing.T) {
	ctx := context.WithValue(t.Context(), destinationKey{}, destination{host: "hooks.example.test"})
	for _, address := range []string{"93.184.216.34:443", "[2606:4700:4700::1111]:443", "[64:ff9b::808:808]:443"} {
		if err := refuseInternalAddress(ctx, "tcp", address, nil); err != nil {
			t.Errorf("a public destination %s was refused: %v", address, err)
		}
	}
}

// TestARedirectToTheSharedAddressSpaceIsRefusedAfterAPublicFirstHop sends a
// request to a host that is allowed and follows its redirect to an address the
// standard library calls global unicast.
func TestARedirectToTheSharedAddressSpaceIsRefusedAfterAPublicFirstHop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://100.64.0.1:9/", http.StatusFound)
	}))
	defer server.Close()

	_, err := NewFactory(nil).AllowInternalHosts(loopbackHost).CreatePendingRequest().
		Get(t.Context(), server.URL, nil)
	if !errors.Is(err, ErrInternalAddress) {
		t.Fatalf("the redirect should be refused, got %v", err)
	}
}

// proxying makes the guard and a transport that follows it see a proxy, then
// restores what was there.
func proxying(t *testing.T, proxy string) {
	t.Helper()
	target, err := url.Parse(proxy)
	if err != nil {
		t.Fatal(err)
	}
	was := proxyFromEnvironment
	proxyFromEnvironment = func(*http.Request) (*url.URL, error) { return target, nil }
	t.Cleanup(func() { proxyFromEnvironment = was })
}

// TestBehindAProxyTheDestinationIsStillRefusedWhenItIsAnInternalLiteral. With a
// proxy the dialer connects to the proxy, which is allowed to sit inside the
// network, and the name is resolved by the proxy. What can still be told is an
// address written in the URL, and localhost.
func TestBehindAProxyTheDestinationIsStillRefusedWhenItIsAnInternalLiteral(t *testing.T) {
	reached := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.Write([]byte("proxied"))
	}))
	defer proxy.Close()
	proxying(t, proxy.URL)

	tr := newTransport()
	factory := NewFactory(&http.Client{Transport: tr})
	tr.Proxy = proxyFromEnvironment

	for _, destination := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://100.64.0.1/",
		"http://[64:ff9b::a00:1]/",
		"http://127.0.0.1:9/",
		"http://localhost:9/",
		"http://metadata.localhost/",
	} {
		_, err := factory.CreatePendingRequest().Get(t.Context(), destination, nil)
		if !errors.Is(err, ErrInternalAddress) {
			t.Errorf("%s behind a proxy should be refused, got %v", destination, err)
		}
	}
	if reached != 0 {
		t.Fatalf("a refused destination reached the proxy %d times", reached)
	}

	resp, err := factory.CreatePendingRequest().Get(t.Context(), "http://hooks.example.test/", nil)
	if err != nil {
		t.Fatalf("a named public destination should go through the proxy: %v", err)
	}
	if resp.Body() != "proxied" || reached != 1 {
		t.Fatalf("the request did not reach the proxy: body %q, reached %d", resp.Body(), reached)
	}

	// A host the factory declared is the caller's word, proxy or not.
	declared := NewFactory(&http.Client{Transport: tr}).AllowInternalHosts("100.64.0.1")
	if _, err := declared.CreatePendingRequest().Get(t.Context(), "http://100.64.0.1/", nil); err != nil {
		t.Fatalf("a declared host behind a proxy should go through: %v", err)
	}
}
