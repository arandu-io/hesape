package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// loopbackHost is the host httptest listens on, and the one a test has to name
// before it may reach its own server.
const loopbackHost = "127.0.0.1"

// TestARequestToALoopbackAddressIsRefused is the check the whole guard exists
// for, taken at its smallest: a destination that resolves inside the network is
// not connected to.
func TestARequestToALoopbackAddressIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := NewFactory(nil).CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if !errors.Is(err, ErrInternalAddress) {
		t.Fatalf("a request to %s should be refused, got %v", server.URL, err)
	}
}

// TestANamedHostReachesAnInternalAddress is the other half: a service of one's
// own is reachable once it is declared, so the refusal is a default and not a
// wall.
func TestANamedHostReachesAnInternalAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	f := NewFactory(nil).AllowInternalHosts(loopbackHost)
	resp, err := f.CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("a declared host should be reachable: %v", err)
	}
	if resp.Body() != `{"ok":true}` {
		t.Fatalf("body = %q", resp.Body())
	}
}

// TestARefusalNamesTheHostAndTheWayToAllowIt pins that the refusal is not
// silent: it says which destination was refused and what declares it.
func TestARefusalNamesTheHostAndTheWayToAllowIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	_, err := NewFactory(nil).CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{loopbackHost, "AllowInternalHosts"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// TestARedirectDoesNotEscapeTheCheck is the classic case. The first hop is a
// host the factory declared; the answer sends the client somewhere it did not.
// Following the redirect must be checked again rather than inherit the first
// hop's permission.
//
// The three destinations are the three shapes the escape takes: the machine
// itself, the metadata service every cloud answers on a link-local address, and
// a unique-local address, which is the IPv6 form and the one a check written
// against the IPv4 private ranges alone would let through.
func TestARedirectDoesNotEscapeTheCheck(t *testing.T) {
	for _, target := range []struct {
		name string
		url  string
		says string
	}{
		// A loopback address other than the one the first hop declared: the
		// permission is per host, and 127.0.0.2 is the machine itself just as
		// much as 127.0.0.1 is.
		{"the machine itself", "http://127.0.0.2:9/", "127.0.0.2"},
		{"the metadata service", "http://169.254.169.254/latest/meta-data/", "169.254.169.254"},
		{"a unique-local address", "http://[fd00::1]:9/", "fd00::1"},
	} {
		t.Run(target.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Redirect(w, &http.Request{}, target.url, http.StatusFound)
			}))
			defer server.Close()

			f := NewFactory(nil).AllowInternalHosts(loopbackHost)
			_, err := f.CreatePendingRequest().Get(t.Context(), server.URL, nil)
			if !errors.Is(err, ErrInternalAddress) {
				t.Fatalf("the redirect to %s should be refused, got %v", target.url, err)
			}
			if !strings.Contains(err.Error(), target.says) {
				t.Fatalf("the refusal should name the address redirected to: %v", err)
			}
		})
	}
}

// TestASchemeThatDoesNotLeaveIsRefused. Only http and https go out, so a URL
// that reached the client from somewhere else cannot ask it to read a file.
func TestASchemeThatDoesNotLeaveIsRefused(t *testing.T) {
	_, err := NewFactory(nil).CreatePendingRequest().Get(t.Context(), "file:///etc/passwd", nil)
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("file:// should be refused, got %v", err)
	}
}

// TestADeclaredBodyLargerThanTheLimitIsRefusedBeforeItIsRead. The answer says
// how big it is, so nothing is read at all.
func TestADeclaredBodyLargerThanTheLimitIsRefusedBeforeItIsRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Repeat("a", 512)))
	}))
	defer server.Close()

	f := NewFactory(nil).AllowInternalHosts(loopbackHost).MaxResponseBytes(64)
	_, err := f.CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("a 512 byte answer under a 64 byte limit should be refused, got %v", err)
	}
	// The wording separates the two paths: an answer that says how big it is
	// is refused whole, and one that does not is refused while it is read. Only
	// the first leaves nothing read at all.
	if !strings.Contains(err.Error(), "declares 512 bytes") {
		t.Fatalf("a declared size should be refused before the body is read: %v", err)
	}
}

// TestAStreamedBodyLargerThanTheLimitIsRefusedWhileItIsRead. The answer does
// not say how big it is, which is the shape the limit has to hold against: it
// must fail rather than hand back the first 64 bytes as if they were the whole
// document.
func TestAStreamedBodyLargerThanTheLimitIsRefusedWhileItIsRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		for range 8 {
			w.Write([]byte(strings.Repeat("a", 128)))
			flusher.Flush()
		}
	}))
	defer server.Close()

	f := NewFactory(nil).AllowInternalHosts(loopbackHost).MaxResponseBytes(64)
	_, err := f.CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("a streamed answer past the limit should be refused, got %v", err)
	}
}

// TestABodyEndingExactlyAtTheLimitIsKept. The limit is a ceiling and not a
// fence one byte below it.
func TestABodyEndingExactlyAtTheLimitIsKept(t *testing.T) {
	body := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		w.Write([]byte(body))
		flusher.Flush()
	}))
	defer server.Close()

	f := NewFactory(nil).AllowInternalHosts(loopbackHost).MaxResponseBytes(64)
	resp, err := f.CreatePendingRequest().Get(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("a body of exactly the limit should be kept: %v", err)
	}
	if resp.Body() != body {
		t.Fatalf("body length = %d, want %d", len(resp.Body()), len(body))
	}
}

// TestTheDefaultClientHasADeadline. The client this package hands out used to
// be the process-wide default one, which has none: a destination that accepted
// the connection and never answered held the caller forever.
func TestTheDefaultClientHasADeadline(t *testing.T) {
	if got := NewFactory(nil).Client().Timeout; got == 0 {
		t.Fatal("the client built for a factory has no deadline")
	}
	if got := NewFactory(nil).CreatePendingRequest().BuildClient().Timeout; got == 0 {
		t.Fatal("the client a request goes out on has no deadline")
	}
	if http.DefaultClient.Timeout != 0 {
		t.Fatal("the process-wide client was changed, which is not this package's to change")
	}
}

// TestSetHandlerLeavesTheSharedClientAlone. The transport used to be written
// into whatever BuildClient answered with, and a factory that was never given a
// client answers with the one shared across the process -- so one request
// installing a recording transport would have put every later request through
// it.
func TestSetHandlerLeavesTheSharedClientAlone(t *testing.T) {
	before := defaultClient.Transport

	// A zero-value Factory holds no client, which is the case that reaches the
	// shared one.
	(&Factory{}).CreatePendingRequest().SetHandler(&countingTransport{})

	if defaultClient.Transport != before {
		t.Fatal("SetHandler replaced the transport of the shared client")
	}
}

// TestTheDialRefusesEveryInternalLiteral walks the ranges the guard names, and
// the mapped form of a loopback address, which is the same address written so
// that a check on the IPv6 form alone misses it.
//
// It drives the dialer hook rather than the classification underneath it,
// because the hook is what a connection actually goes through: a classification
// that is right and a hook that never consults it would pass a test written one
// layer down and connect anyway.
//
// The context carries no destination, which is the case a request that skipped
// the round tripper produces. Nothing is allowed then, and that is the answer
// this fixes.
func TestTheDialRefusesEveryInternalLiteral(t *testing.T) {
	for _, c := range []struct {
		address  string
		internal bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"::ffff:192.168.1.1", true},
		{"fd00::1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"224.0.0.1", true},
		{"93.184.216.34", false},
		{"8.8.8.8", false},
		{"2606:2800:220:1:248:1893:25c8:1946", false},
	} {
		if _, err := netip.ParseAddr(c.address); err != nil {
			t.Fatalf("parsing %s: %v", c.address, err)
		}

		err := refuseInternalAddress(t.Context(), "tcp", net.JoinHostPort(c.address, "443"), nil)
		if refused := errors.Is(err, ErrInternalAddress); refused != c.internal {
			t.Errorf("dialing %s was refused=%v, want %v (%v)", c.address, refused, c.internal, err)
		}
	}
}

// TestAnAddressThatAnsweredPublicIsStillRefusedWhenItIsDialedPrivate is the
// rebinding case: one name, two answers, and the second one points inside.
//
// The guard has no separate check to race, which is the whole design -- it runs
// between resolving a name and connecting to the address, so the address it
// reads is the address the connection uses. This drives that hook with the two
// answers in order, under the destination the round tripper builds from a URL
// whose host says nothing about where it points.
//
// It stands in for a resolver rather than running one: a DNS server in a unit
// test would prove the same property and would be the only test here that can
// fail because of the machine it runs on. What is fixed is that the first
// answer buys nothing for the second.
func TestAnAddressThatAnsweredPublicIsStillRefusedWhenItIsDialedPrivate(t *testing.T) {
	// The destination the round tripper puts on the context for a URL whose
	// host is not on any allow list.
	ctx := context.WithValue(t.Context(), destinationKey{}, destination{host: "rebinding.example"})

	answers := []struct {
		address string
		refused bool
	}{
		{"93.184.216.34:443", false},
		{"127.0.0.1:443", true},
	}

	for i, answer := range answers {
		err := refuseInternalAddress(ctx, "tcp", answer.address, nil)
		if refused := errors.Is(err, ErrInternalAddress); refused != answer.refused {
			t.Fatalf("answer %d (%s) was refused=%v, want %v (%v)", i+1, answer.address, refused, answer.refused, err)
		}
	}

	err := refuseInternalAddress(ctx, "tcp", "127.0.0.1:443", nil)
	if !strings.Contains(err.Error(), "rebinding.example") || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("the refusal names neither the host asked for nor the address it resolved to: %v", err)
	}
}

// TestAClientFromAFactoryHoldingItsOwnTransportKeepsTheCallersReach fixes the
// boundary of the guard, because the guard used to be documented as having
// none.
//
// The refusal of an address inside the network is on the dialer, so it travels
// with the transport. A factory built on a client of its own answers with that
// client's transport, and a request on it reaches whatever that transport's
// dialer reaches -- here, a server on loopback. Nothing about this is a bug to
// fix by moving the check onto the URL: an address checked before it is dialed
// is not the address dialed.
func TestAClientFromAFactoryHoldingItsOwnTransportKeepsTheCallersReach(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	factory := NewFactory(&http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()})

	resp, err := factory.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("the caller's own transport did not reach %s: %v", server.URL, err)
	}
	resp.Body.Close()
}

// TestAClientFromADefaultFactoryRefusesTheNetwork is the other side of that
// boundary: the factory that built the transport carries the dialer that
// refuses, so the promise holds where it is made.
func TestAClientFromADefaultFactoryRefusesTheNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for name, factory := range map[string]*Factory{
		"a nil client":                          NewFactory(nil),
		"a zero-value factory":                  {},
		"a client with no transport of its own": NewFactory(&http.Client{}),
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := factory.Client().Get(server.URL)
			if err == nil {
				resp.Body.Close()
			}
			if !errors.Is(err, ErrInternalAddress) {
				t.Fatalf("a request to %s was not refused: %v", server.URL, err)
			}
		})
	}
}

// TestTheGuardOnACallersTransportStillRefusesTheScheme is the half of the guard
// that does not depend on the dialer, and so holds on any transport. It is in
// the round tripper the factory wraps around whatever it was given.
func TestTheGuardOnACallersTransportStillRefusesTheScheme(t *testing.T) {
	factory := NewFactory(&http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()})

	_, err := factory.Client().Get("ftp://example.test/file")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("the scheme was not refused: %v", err)
	}
}

// TestTheGuardOnACallersTransportStillCapsTheBody is the other half that holds
// on any transport, for the same reason.
func TestTheGuardOnACallersTransportStillCapsTheBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer server.Close()

	factory := NewFactory(&http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}).
		MaxResponseBytes(16)

	resp, err := factory.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("the request failed before the body could be read: %v", err)
	}
	defer resp.Body.Close()

	if _, err := io.ReadAll(resp.Body); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("reading the body = %v, want ErrResponseTooLarge", err)
	}
}

// TestTheClientAFactoryHandsOutIsACopy is the change no signature reports. The
// method returned the factory's own client, so a caller could set a timeout on
// it and change every request the factory sends; it returns a copy now, and
// that caller changes nothing at all.
func TestTheClientAFactoryHandsOutIsACopy(t *testing.T) {
	factory := NewFactory(nil)

	factory.Client().Timeout = time.Minute

	if got := factory.CreatePendingRequest().BuildClient().Timeout; got == time.Minute {
		t.Fatal("mutating the client handed out changed the one the factory sends on")
	}

	factory.GlobalOptions(func(c *http.Client) { c.Timeout = time.Minute })
	if got := factory.CreatePendingRequest().BuildClient().Timeout; got != time.Minute {
		t.Fatalf("GlobalOptions left the timeout at %v, and it is the way to change it", got)
	}
}
