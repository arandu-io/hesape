package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	httpclient "github.com/arandu-io/hesape/http/client"
)

// refusedAddress reports whether a delivery may not connect to the address. It
// is the list of http/client, which the guard of every request of the framework
// refuses on as well: loopback, private, link-local, multicast, unspecified, the
// shared address space, the other ranges no public host answers on, and an IPv6
// address that carries one of those IPv4 addresses.
func refusedAddress(addr netip.Addr) bool {
	return httpclient.IsInternalAddress(addr)
}

// DestinationLookupTimeout bounds the lookup of a name in CheckDestination.
const DestinationLookupTimeout = 3 * time.Second

// CheckDestination answers whether an endpoint's address may be stored: it is
// what NormalizeURL accepts, its host is not an address a delivery may not
// connect to, not localhost, and does not resolve to one.
//
// It is the creation-time half of the guard whose other half is the dialer of
// every attempt, which resolves the name again and refuses it again: a name
// that answers a public address today and an internal one tomorrow (DNS
// rebinding) passes this check and stops at the dialer. A name that does not
// resolve now is accepted, because the merchant may publish the record after
// registering the endpoint, and an attempt that cannot resolve it fails like
// any other. names nil is net.DefaultResolver.
//
// A refusal wraps http/client's ErrInternalAddress; an address that is not an
// http or https URL wraps ErrInvalidURL.
func CheckDestination(ctx context.Context, value string, names HostResolver) error {
	normalized, err := NormalizeURL(value)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ErrInvalidURL
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return ErrInvalidURL
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		if refusedAddress(literal) {
			return fmt.Errorf("%w: %s", httpclient.ErrInternalAddress, host)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: %s", httpclient.ErrInternalAddress, host)
	}
	if names == nil {
		names = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, DestinationLookupTimeout)
	defer cancel()
	addrs, err := names.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if refusedAddress(addr) {
			return fmt.Errorf("%w: %s resolves to %s", httpclient.ErrInternalAddress, host, addr)
		}
	}
	return nil
}

// newDeliveryClient is the client the deliveries go out on.
//
// A client the application gave keeps its transport, wrapped in the guard
// of http/client, which settles the scheme and caps the answer on every hop. It
// is the application's client, so where it may connect stays the application's
// answer -- which is what a test that delivers to a local server needs -- unless
// guard is true, which puts the dialer below in front of a copy of its transport
// as well, with no proxy: behind a proxy the name is resolved by the proxy and
// the guard would see only the proxy, so a delivery goes to the address it names.
// Without a client, the transport is a copy of the guarded one, with no proxy --
// a delivery goes to the address it names -- and a dialer in front of the
// guard's that resolves the host itself, refuses the whole answer when any
// address is refused, and hands the guard's dialer the address it checked, which
// checks it again as it connects. Nothing is resolved twice, so a name that
// answers differently the second time reaches nothing it was not checked for.
func newDeliveryClient(custom *http.Client, guard bool) (*http.Client, error) {
	if custom != nil && !guard {
		return httpclient.NewFactory(custom).CreatePendingRequest().CreateClient(nil), nil
	}
	var base *http.Transport
	if custom == nil {
		var ok bool
		base, ok = httpclient.NewFactory(nil).CreatePendingRequest().BuildClient().Transport.(*http.Transport)
		if !ok {
			return nil, errors.New("webhook: the http client has no transport to guard")
		}
	} else {
		switch rt := custom.Transport.(type) {
		case nil:
			base = http.DefaultTransport.(*http.Transport)
		case *http.Transport:
			base = rt
		default:
			return nil, fmt.Errorf("webhook: GuardHTTPClient needs the client's transport to be an *http.Transport, which it can put the dialer in front of, and it is %T", rt)
		}
		if base.DialTLSContext != nil || base.DialTLS != nil {
			return nil, errors.New("webhook: GuardHTTPClient needs a transport that dials through DialContext, and this one dials TLS itself")
		}
	}
	transport := base.Clone()
	transport.Proxy = nil
	next := transport.DialContext
	if next == nil {
		next = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	transport.DialContext = publicDialer(next, net.DefaultResolver)
	if custom == nil {
		return httpclient.NewFactory(nil).CreatePendingRequest().CreateClient(transport), nil
	}
	client := *custom
	client.Transport = transport
	return httpclient.NewFactory(&client).CreatePendingRequest().CreateClient(nil), nil
}

// HostResolver is the one method of net.Resolver the dialer and CheckDestination
// need; *net.Resolver is one.
type HostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// resolver is the older, unexported name of HostResolver.
type resolver = HostResolver

// publicDialer resolves the host, refuses it when any of its addresses is
// refused, and dials the addresses it checked through next.
func publicDialer(next func(context.Context, string, string) (net.Conn, error), names resolver) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: %s is not an address", httpclient.ErrInternalAddress, address)
		}
		var addrs []netip.Addr
		if literal, err := netip.ParseAddr(host); err == nil {
			addrs = []netip.Addr{literal}
		} else if addrs, err = names.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("webhook: %s resolves to no address", host)
		}
		for _, addr := range addrs {
			if refusedAddress(addr) {
				return nil, fmt.Errorf("%w: %s resolves to %s", httpclient.ErrInternalAddress, host, addr)
			}
		}
		var failures error
		for _, addr := range addrs {
			conn, err := next(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}
			failures = errors.Join(failures, err)
		}
		return nil, failures
	}
}
