package redis

import (
	"crypto/tls"
	"testing"

	"github.com/arandu-io/hesape/cache"
)

// These are in the package because the client is unexported, and the claim is
// about what reaches it: an option that stops at Options is an option that
// silently does nothing, and with encryption the difference is invisible from
// outside -- the queue works, in the clear.

// TestNewDialsInTheClearByDefault: TLS is a field nobody has to set, and an
// option nobody set may not change how the client dials.
func TestNewDialsInTheClearByDefault(t *testing.T) {
	q := New(Options{Address: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = q.Close() })

	if config := q.client.Options().TLSConfig; config != nil {
		t.Errorf("the client was handed a TLS configuration (%v) for a queue that asked for none", config)
	}
}

// TestNewHandsTheTLSConfigurationToTheClient: the field is worth nothing if it
// stops at the struct.
func TestNewHandsTheTLSConfigurationToTheClient(t *testing.T) {
	encryption := &tls.Config{MinVersion: tls.VersionTLS13}
	q := New(Options{Address: "127.0.0.1:6379", TLS: encryption})
	t.Cleanup(func() { _ = q.Close() })

	if got := q.client.Options().TLSConfig; got != encryption {
		t.Errorf("the client was handed %v, want the configuration New received", got)
	}
}

// TestOpenHandsTheEndpointToTheClient: every field of the endpoint is a
// setting somebody wrote down.
func TestOpenHandsTheEndpointToTheClient(t *testing.T) {
	encryption := &tls.Config{MinVersion: tls.VersionTLS13}
	opened, err := connector{}.Open(cache.Endpoint{
		Address:  "127.0.0.1:1",
		Password: "secret",
		Database: 4,
		Prefix:   "app",
		TLS:      encryption,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	q := opened.(*RedisQueue)
	t.Cleanup(func() { _ = q.Close() })

	options := q.client.Options()
	if options.Addr != "127.0.0.1:1" || options.Password != "secret" || options.DB != 4 {
		t.Errorf("the client was handed addr=%q password=%q db=%d", options.Addr, options.Password, options.DB)
	}
	if options.TLSConfig != encryption {
		t.Errorf("the client was handed TLS %v, want the configuration in the endpoint", options.TLSConfig)
	}
	if q.prefix != "app" {
		t.Errorf("prefix = %q, want the endpoint's", q.prefix)
	}
}
