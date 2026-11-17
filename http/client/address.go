package client

import "net/netip"

// sharedAddressSpace is 100.64.0.0/10, the carrier-grade NAT range of RFC
// 6598. It is global unicast to the standard library, and it is where a
// cluster's or a tailnet's own machines answer.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// unroutableSpace is what the standard library calls global unicast and no
// public host answers on: "this network" (0.0.0.0/8, which a Linux host
// connects to itself), the IETF protocol block, the benchmarking range, the
// three IPv4 documentation ranges, the reserved class E (which holds the
// limited broadcast address), the IPv6 discard prefix, Teredo, the IPv6
// benchmarking and documentation prefixes, the SRv6 block, and the deprecated
// IPv6 site-local range.
var unroutableSpace = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

// embeddingSpace is the IPv6 ranges that carry an IPv4 address in their low
// or middle bits, so a network that translates them reaches the IPv4 address
// inside: NAT64 (64:ff9b::/96 and 64:ff9b:1::/48), 6to4 (2002::/16), the
// stateless translation form (::ffff:0:0:0/96) and the deprecated
// IPv4-compatible form (::/96).
var embeddingSpace = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("::ffff:0:0:0/96"),
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

// IsInternalAddress reports whether an address belongs to this machine or to
// the network around it rather than to the internet: loopback, the private
// ranges of RFC 1918 and RFC 4193, link-local (where a cloud metadata service
// answers), multicast, the unspecified address, the shared address space of
// carrier-grade NAT (100.64.0.0/10, where a cluster's or a tailnet's own
// machines answer), the other ranges no public host answers on (0.0.0.0/8,
// the IETF protocol block, benchmarking, documentation, class E and the like),
// and an IPv6 address that carries one of those IPv4 addresses -- the mapped
// form, NAT64, 6to4 and the translated forms -- because a network that
// translates them reaches the IPv4 address inside.
//
// It is the one list the guard of this package and the deliveries of the
// webhook package refuse on, so the two cannot disagree. A zone on an IPv6
// address is ignored.
func IsInternalAddress(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.WithZone("").Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || sharedAddressSpace.Contains(addr) {
		return true
	}
	for _, prefix := range unroutableSpace {
		if prefix.Contains(addr) {
			return true
		}
	}
	if inner, ok := embeddedIPv4(addr); ok {
		return IsInternalAddress(inner)
	}
	return false
}
