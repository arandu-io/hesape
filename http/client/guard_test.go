package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
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

	_, err := NewFactory(nil).CreatePendingRequest().Get(server.URL, nil)
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
	resp, err := f.CreatePendingRequest().Get(server.URL, nil)
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

	_, err := NewFactory(nil).CreatePendingRequest().Get(server.URL, nil)
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
// host the factory declared; the answer sends the client to the metadata
// address, which it did not. Following the redirect must be checked again
// rather than inherit the first hop's permission.
func TestARedirectDoesNotEscapeTheCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()

	f := NewFactory(nil).AllowInternalHosts(loopbackHost)
	_, err := f.CreatePendingRequest().Get(server.URL, nil)
	if !errors.Is(err, ErrInternalAddress) {
		t.Fatalf("the redirect should be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Fatalf("the refusal should name the address redirected to: %v", err)
	}
}

// TestASchemeThatDoesNotLeaveIsRefused. Only http and https go out, so a URL
// that reached the client from somewhere else cannot ask it to read a file.
func TestASchemeThatDoesNotLeaveIsRefused(t *testing.T) {
	_, err := NewFactory(nil).CreatePendingRequest().Get("file:///etc/passwd", nil)
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
	_, err := f.CreatePendingRequest().Get(server.URL, nil)
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
	_, err := f.CreatePendingRequest().Get(server.URL, nil)
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
	resp, err := f.CreatePendingRequest().Get(server.URL, nil)
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

// TestAnAddressIsInternalOrItIsNot walks the ranges the guard names, and the
// mapped form of a loopback address, which is the same address written so that
// a check on the IPv6 form alone misses it.
func TestAnAddressIsInternalOrItIsNot(t *testing.T) {
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
		addr, err := netip.ParseAddr(c.address)
		if err != nil {
			t.Fatalf("parsing %s: %v", c.address, err)
		}
		if got := isInternalAddress(addr); got != c.internal {
			t.Errorf("isInternalAddress(%s) = %v, want %v", c.address, got, c.internal)
		}
	}
}
