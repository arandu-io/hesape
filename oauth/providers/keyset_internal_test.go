package providers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file is internal because the cache is a matter of time, and the clock
// is the one thing the package keeps to itself.

type cacheServer struct {
	*httptest.Server
	hits    atomic.Int32
	mu      sync.Mutex
	keys    []map[string]any
	fail    bool
	control string
}

func newCacheServer(t *testing.T, keys ...map[string]any) *cacheServer {
	t.Helper()
	s := &cacheServer{keys: keys}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if s.control != "" {
			w.Header().Set("Cache-Control", s.control)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *cacheServer) set(fail bool, keys ...map[string]any) {
	s.mu.Lock()
	s.fail, s.keys = fail, keys
	s.mu.Unlock()
}

func jwkFor(t *testing.T, kid string) map[string]any {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

// clocked is a key set whose clock the test moves.
func clocked(server *cacheServer) (*KeySet, *time.Time) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	k := NewKeySet(server.URL).SetHTTPClient(http.DefaultClient)
	k.now = func() time.Time { return now }
	return k, &now
}

func TestKeysAreFetchedOnceAndKept(t *testing.T) {
	server := newCacheServer(t, jwkFor(t, "k1"))
	keys, now := clocked(server)

	for i := 0; i < 5; i++ {
		key, err := keys.lookup(context.Background(), "k1")
		if err != nil || key == nil {
			t.Fatalf("lookup %d = %v, %v", i, key, err)
		}
		*now = now.Add(time.Minute)
	}
	if got := server.hits.Load(); got != 1 {
		t.Fatalf("the provider was asked %d times, want once", got)
	}
}

func TestAKeyNobodyHasSeenIsFetchedForOnceAMinute(t *testing.T) {
	server := newCacheServer(t, jwkFor(t, "k1"))
	keys, now := clocked(server)
	if _, err := keys.lookup(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}

	// A flood of unknown ids straight after the fetch costs the provider
	// nothing.
	for i := 0; i < 20; i++ {
		if key, err := keys.lookup(context.Background(), "random"); key != nil || err != nil {
			t.Fatalf("lookup = %v, %v, want an unknown key and no error", key, err)
		}
	}
	if got := server.hits.Load(); got != 1 {
		t.Fatalf("%d fetches inside the minimum interval, want 1", got)
	}

	// Rotation: the provider publishes a new key, and the first token signed
	// with it arrives a minute later.
	server.set(false, jwkFor(t, "k1"), jwkFor(t, "k2"))
	*now = now.Add(keySetMinRefresh)
	key, err := keys.lookup(context.Background(), "k2")
	if err != nil || key == nil {
		t.Fatalf("the rotated key = %v, %v", key, err)
	}
	if got := server.hits.Load(); got != 2 {
		t.Fatalf("%d fetches, want 2", got)
	}
}

func TestKeysExpireAfterTheTimeTheProviderAskedFor(t *testing.T) {
	server := newCacheServer(t, jwkFor(t, "k1"))
	server.control = "public, max-age=600"
	keys, now := clocked(server)
	if _, err := keys.lookup(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}

	*now = now.Add(9 * time.Minute)
	if _, err := keys.lookup(context.Background(), "k1"); err != nil || server.hits.Load() != 1 {
		t.Fatalf("inside max-age: hits = %d, err = %v", server.hits.Load(), err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := keys.lookup(context.Background(), "k1"); err != nil || server.hits.Load() != 2 {
		t.Fatalf("past max-age: hits = %d, err = %v", server.hits.Load(), err)
	}
}

func TestExpiredKeysKeepServingWhileTheProviderIsDown(t *testing.T) {
	server := newCacheServer(t, jwkFor(t, "k1"))
	keys, now := clocked(server)
	if _, err := keys.lookup(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	server.set(true)

	*now = now.Add(2 * time.Hour) // past the default hour
	key, err := keys.lookup(context.Background(), "k1")
	if err != nil || key == nil {
		t.Fatalf("lookup during an outage = %v, %v, want the kept key", key, err)
	}

	// But not forever.
	*now = now.Add(keySetMaxStale)
	if _, err := keys.lookup(context.Background(), "k1"); err == nil {
		t.Fatal("keys older than a day still served")
	}
}

func TestTheTimeTheProviderAsksForIsClamped(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"":                         keySetDefaultTTL,
		"no-store":                 keySetDefaultTTL,
		"max-age=0":                keySetMinTTL,
		"max-age=600":              10 * time.Minute,
		"public, max-age=7200, x":  2 * time.Hour,
		"max-age=99999999999999":   keySetMaxTTL,
		"max-age=-5":               keySetDefaultTTL,
		"max-age=soon":             keySetDefaultTTL,
		`public, MAX-AGE="1800"`:   30 * time.Minute,
		"s-maxage=60":              keySetDefaultTTL,
		"max-age=9223372036854775": keySetMaxTTL,
	} {
		if got := ttlFrom(header); got != want {
			t.Errorf("ttlFrom(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestABurstAfterExpiryIsOneRequest(t *testing.T) {
	server := newCacheServer(t, jwkFor(t, "k1"))
	keys := NewKeySet(server.URL).SetHTTPClient(http.DefaultClient)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if key, err := keys.lookup(context.Background(), "k1"); err != nil || key == nil {
				t.Errorf("lookup = %v, %v", key, err)
			}
		}()
	}
	wg.Wait()
	if got := server.hits.Load(); got != 1 {
		t.Fatalf("%d requests for a burst, want 1", got)
	}
}
