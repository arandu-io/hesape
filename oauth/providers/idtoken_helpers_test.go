package providers_test

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The key pairs are made once: generating an RSA key is the slowest thing in
// this package's tests.
var (
	keyOnce sync.Once
	keyA    *rsa.PrivateKey
	keyB    *rsa.PrivateKey
)

func rsaKeys(t testing.TB) (a, b *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if keyA, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if keyB, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return keyA, keyB
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwkOf is the JWK document entry of a public key.
func jwkOf(kid string, key *rsa.PrivateKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": b64(key.N.Bytes()),
		"e": b64(big.NewInt(int64(key.E)).Bytes()),
	}
}

// mint builds a compact JWT. header and claims are used as given, so a test
// can write a header the verifier must refuse. sign decides the signature
// from the signing input.
func mint(t testing.TB, header, claims map[string]any, sign func(input string) []byte) string {
	t.Helper()
	h, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	c, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := b64(h) + "." + b64(c)
	return input + "." + b64(sign(input))
}

func signRS256(t testing.TB, key *rsa.PrivateKey) func(string) []byte {
	return func(input string) []byte {
		digest := sha256.Sum256([]byte(input))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
}

// rs256 is a token signed the way the provider signs it.
func rs256(t testing.TB, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	return mint(t, map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}, claims, signRS256(t, key))
}

// claimsFor are the claims of a good Apple token for this audience.
func claimsFor(aud string, extra map[string]any) map[string]any {
	claims := map[string]any{
		"iss":              "https://appleid.apple.com",
		"aud":              aud,
		"sub":              "001234.abcdef.5678",
		"iat":              time.Now().Unix(),
		"exp":              time.Now().Add(10 * time.Minute).Unix(),
		"email":            "ada@example.test",
		"email_verified":   "true",
		"is_private_email": "false",
	}
	for k, v := range extra {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

// hmacHS256 signs with the given secret, the attack on a verifier that reads
// the algorithm from the token and then uses the public key as the secret.
func hmacHS256(secret []byte) func(string) []byte {
	return func(input string) []byte {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte(input))
		return m.Sum(nil)
	}
}

// jwksServer serves a JWKS document at /keys and counts the requests.
type jwksServer struct {
	*httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	keys   []map[string]any
	status int
	header http.Header
}

func newJWKSServer(t testing.TB, keys ...map[string]any) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys, status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/keys" {
			http.NotFound(w, r)
			return
		}
		s.hits.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range s.header {
			w.Header()[k] = v
		}
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) set(keys ...map[string]any) {
	s.mu.Lock()
	s.keys = keys
	s.mu.Unlock()
}

// rewriteClient sends every request to the test server whatever host it names,
// so the real Apple URLs are the ones exercised and none of them is reached.
type rewriteClient struct{ target *url.URL }

func (c rewriteClient) Do(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.URL.Scheme = c.target.Scheme
	clone.URL.Host = c.target.Host
	clone.Host = c.target.Host
	return http.DefaultClient.Do(clone)
}
