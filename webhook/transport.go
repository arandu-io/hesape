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

// sharedAddressSpace is 100.64.0.0/10, the carrier-grade NAT range of RFC
// 6598. It is global unicast to the standard library, so the guard of
// http/client lets
// it through, and it is where a cluster's or a tailnet's own machines answer.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// unroutableSpace is what the standard library calls global unicast and no
// public host answers on: "this network" (0.0.0.0/8, which a Linux host
// connects to itself), the IETF protocol block, the benchmarking range, the
// reserved class E, and the deprecated IPv6 site-local range.
var unroutableSpace = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"),
}

// embeddingSpace is the IPv6 ranges that carry an IPv4 address in their low
// or middle bits, so a network that translates them reaches the IPv4 address
// inside: NAT64 (64:ff9b::/96 and 64:ff9b:1::/48), 6to4 (2002::/16) and the
// deprecated IPv4-compatible form (::/96).
var embeddingSpace = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("::/96"),
}

// embeddedIPv4 is the IPv4 address an IPv6 address of embeddingSpace carries,
// at the offset RFC 6052 gives its prefix length.
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	if !addr.Is6() {
		return netip.Addr{}, false
	}
	b := addr.As16()
	for _, prefix := range embeddingSpace {
		if !prefix.Contains(addr) {
			continue
		}
		switch prefix.Bits() {
		case 16: // 6to4: 2002:AABB:CCDD::
			return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
		case 48: // RFC 6052 for a /48 prefix: bytes 6 and 7, skip the u octet, then 9 and 10
			return netip.AddrFrom4([4]byte{b[6], b[7], b[9], b[10]}), true
		default: // the /96 forms carry it in the last four bytes
			return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
		}
	}
	return netip.Addr{}, false
}

// refusedAddress reports whether a delivery may not connect to the address:
// loopback, private, link-local, multicast, unspecified, in the shared address
// space or another range no public host answers on, or an IPv6 address that
// carries one of those IPv4 addresses.
func refusedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || sharedAddressSpace.Contains(addr) {
		return true
	}
	for _, prefix := range unroutableSpace {
		if prefix.Contains(addr) {
			return true
		}
	}
	if inner, ok := embeddedIPv4(addr); ok {
		return refusedAddress(inner)
	}
	return false
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
// of http/client, which settles the scheme and caps the answer on every hop. Without
// one, the transport is a copy of the guarded one, with no proxy -- a
// delivery goes to the address it names -- and a dialer in front of the guard's
// that resolves the host itself, refuses the whole answer when any address is
// refused, and hands the guard's dialer the address it checked, which checks it
// again as it connects. Nothing is resolved twice, so a name that answers
// differently the second time reaches nothing it was not checked for.
func newDeliveryClient(custom *http.Client) (*http.Client, error) {
	if custom != nil {
		return httpclient.NewFactory(custom).CreatePendingRequest().CreateClient(nil), nil
	}
	base, ok := httpclient.NewFactory(nil).CreatePendingRequest().BuildClient().Transport.(*http.Transport)
	if !ok {
		return nil, errors.New("webhook: the http client has no transport to guard")
	}
	transport := base.Clone()
	transport.Proxy = nil
	transport.DialContext = publicDialer(transport.DialContext, net.DefaultResolver)
	return httpclient.NewFactory(nil).CreatePendingRequest().CreateClient(transport), nil
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
