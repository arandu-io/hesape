package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/arandu-io/hesape/str"
)

// The refusals raised on the way out, before a connection is made, and on the
// way back, while the body is read. Each is what errors.Is matches on.
var (
	// ErrInternalAddress means the destination resolved to an address that
	// belongs to this machine or to the network around it, and no host named to
	// [Factory.AllowInternalHosts] covers it.
	ErrInternalAddress = errors.New("http client: the destination is inside the network")

	// ErrUnsupportedScheme means the URL asks for something other than http or
	// https.
	ErrUnsupportedScheme = errors.New("http client: unsupported URL scheme")

	// ErrResponseTooLarge means the answer is bigger than
	// [Factory.MaxResponseBytes] allows.
	ErrResponseTooLarge = errors.New("http client: the response is too large")
)

// DefaultMaxResponseBytes is how much of an answer the client reads before it
// gives up on it.
//
// Large enough for any document an integration returns, and small enough that a
// destination cannot spend the process's memory by answering forever. Raise it
// per factory with [Factory.MaxResponseBytes] when a download needs more.
const DefaultMaxResponseBytes int64 = 32 << 20

// defaultTimeout is the deadline on a request that was given none. It is the
// same number [Factory.CreatePendingRequest] starts a request with, stated once
// so a client built without a request cannot end up with no deadline at all.
const defaultTimeout = 30 * time.Second

// proxyFromEnvironment is the one answer to "does this request go through a
// proxy", read by the transport that dials and by the guard that decides what
// is being dialed. Two answers to that question would disagree exactly when it
// matters.
var proxyFromEnvironment = http.ProxyFromEnvironment

// defaultTransport is what every request of this package dials on unless the
// caller brought a transport of their own. It carries the address check, and it
// is shared so that connections are pooled across factories.
var defaultTransport = newTransport()

// defaultClient is the client [PendingRequest.BuildClient] answers with when
// nothing else was configured. It is shared: copy it before changing it.
var defaultClient = &http.Client{Transport: defaultTransport, Timeout: defaultTimeout}

func newTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:        10 * time.Second,
		KeepAlive:      30 * time.Second,
		ControlContext: refuseInternalAddress,
	}
	return &http.Transport{
		Proxy:                 proxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// guard is what a factory decided about the requests it sends.
//
// It is settled per factory rather than per request because the connection pool
// is keyed by host and shared: a request that was allowed to reach a host would
// leave a connection behind for a request that was not. A caller who wants a
// narrower policy for one integration gives that integration its own
// [Factory], which is where its base URL and its token already live.
type guard struct {
	allowedInternalHosts []string
	maxResponseBytes     int64
}

// AllowInternalHosts names the hosts this factory may reach at an address
// inside the network -- loopback, a private range, or link-local, which is
// where a cloud metadata service answers.
//
// Every such destination is refused with [ErrInternalAddress] until it is named
// here, so an application that talks to a service of its own declares it:
//
//	factory.AllowInternalHosts("cache.internal", "*.svc.cluster.local")
//
// A host is matched the way [str.Is] matches -- literally, or with * standing
// for any run of characters -- against the host written in the URL, not against
// the address it resolves to. Naming a host therefore accepts every address
// that host can be made to resolve to.
func (f *Factory) AllowInternalHosts(hosts ...string) *Factory {
	f.allowedInternalHosts = append(f.allowedInternalHosts, hosts...)
	return f
}

// MaxResponseBytes sets how much of a response body this factory reads before
// it refuses the rest with [ErrResponseTooLarge]. A value of zero or less
// restores [DefaultMaxResponseBytes].
//
// There is no unlimited setting. A body with no bound is one the destination
// decides the size of.
func (f *Factory) MaxResponseBytes(n int64) *Factory {
	f.maxResponseBytes = n
	return f
}

// guard is this factory's policy, with the defaults filled in. The receiver may
// be nil: a request built without a factory still goes out under the defaults.
func (f *Factory) guard() guard {
	g := guard{maxResponseBytes: DefaultMaxResponseBytes}
	if f == nil {
		return g
	}
	g.allowedInternalHosts = f.allowedInternalHosts
	if f.maxResponseBytes > 0 {
		g.maxResponseBytes = f.maxResponseBytes
	}
	return g
}

// destinationKey carries a destination on a request context.
type destinationKey struct{}

// destination is what the round tripper worked out about one hop and what the
// dialer acts on. It travels on the request context because the dialer sees an
// address and never the name it came from.
type destination struct {
	host          string
	allowInternal bool
}

// guardedTransport settles the scheme and the destination of every hop --
// including the hops a redirect adds, since each one round-trips again -- and
// caps the body that comes back.
type guardedTransport struct {
	next  http.RoundTripper
	guard guard
}

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, req.URL.Scheme)
	}

	dest := destination{
		host:          req.URL.Hostname(),
		allowInternal: str.Is(t.guard.allowedInternalHosts, req.URL.Hostname(), true),
	}
	if !dest.allowInternal {
		// Behind a proxy the address dialed is the proxy's, and where the
		// request goes after that is the proxy's to decide. Refusing the proxy
		// for sitting inside the network would leave the request nowhere to go.
		if proxy, err := proxyFromEnvironment(req); err == nil && proxy != nil {
			dest.allowInternal = true
		}
	}

	resp, err := t.next.RoundTrip(req.Clone(context.WithValue(req.Context(), destinationKey{}, dest)))
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > t.guard.maxResponseBytes {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s declares %d bytes and the limit is %d",
			ErrResponseTooLarge, req.URL.Host, resp.ContentLength, t.guard.maxResponseBytes)
	}
	resp.Body = &limitedBody{next: resp.Body, limit: t.guard.maxResponseBytes, host: req.URL.Host}
	return resp, nil
}

// refuseInternalAddress runs on the dialer, between resolving a name and
// connecting to it, and refuses an address the request was not allowed to
// reach.
//
// It runs there and not on the URL because a name says nothing about where it
// points: a destination that resolves to a public address at the moment it is
// checked and to a loopback address at the moment it is dialed would pass every
// check made earlier. Here the address checked is the address connected to.
func refuseInternalAddress(ctx context.Context, _, address string, _ syscall.RawConn) error {
	dest, _ := ctx.Value(destinationKey{}).(destination)
	if dest.allowInternal {
		return nil
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// The dialer hands over an address it has already resolved. A name that
		// arrived here unresolved is not one to connect to on a guess.
		return fmt.Errorf("%w: %s is not an address", ErrInternalAddress, host)
	}
	if !isInternalAddress(addr) {
		return nil
	}

	if dest.host == "" || dest.host == addr.String() {
		return fmt.Errorf("%w: %s; name it to Factory.AllowInternalHosts to allow it",
			ErrInternalAddress, addr)
	}
	return fmt.Errorf("%w: %s resolves to %s; name it to Factory.AllowInternalHosts to allow it",
		ErrInternalAddress, dest.host, addr)
}

// isInternalAddress reports whether an address belongs to this machine or to
// the network around it rather than to the internet: loopback, the private
// ranges of RFC 1918 and RFC 4193, link-local, multicast, and the unspecified
// address.
//
// The mapped form of an IPv4 address answers the same as the address it wraps,
// because the methods asked here read ::ffff:127.0.0.1 as 127.0.0.1. Unmapping
// it first would be a line that looks like the defence and is not.
func isInternalAddress(addr netip.Addr) bool {
	return !addr.IsGlobalUnicast() || addr.IsPrivate()
}

// limitedBody is a response body with a ceiling. Past it the read fails rather
// than stopping, because a body cut short is a body that parses as something
// else -- and the caller would act on the something else.
type limitedBody struct {
	next  io.ReadCloser
	read  int64
	limit int64
	host  string
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.read > b.limit {
		return 0, b.tooLarge()
	}
	// One byte of headroom past the limit, which is what tells a body ending
	// exactly at the limit from one that runs past it.
	if room := b.limit + 1 - b.read; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := b.next.Read(p)
	b.read += int64(n)
	if b.read > b.limit {
		return n, b.tooLarge()
	}
	return n, err
}

func (b *limitedBody) Close() error { return b.next.Close() }

func (b *limitedBody) tooLarge() error {
	return fmt.Errorf("%w: %s sent more than %d bytes", ErrResponseTooLarge, b.host, b.limit)
}
