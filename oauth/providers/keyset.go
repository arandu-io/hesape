package providers

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// keySetMaxBytes bounds what is read from the network for one key set.
	// Apple's is under 2 KiB and Google's under 3 KiB; this is a ceiling for a
	// peer that never stops sending, not a size anyone is expected to reach.
	keySetMaxBytes = 256 << 10
	// keySetMaxKeys bounds how many keys one document may make us parse.
	keySetMaxKeys = 64
	// keySetDefaultTTL is how long keys are trusted when the provider says
	// nothing about it.
	keySetDefaultTTL = time.Hour
	// keySetMinTTL and keySetMaxTTL clamp what the provider says: a max-age of
	// zero would turn every verification into a request to the provider, and
	// one of a year would keep a key that was withdrawn for a year.
	keySetMinTTL = 5 * time.Minute
	keySetMaxTTL = 24 * time.Hour
	// keySetMinRefresh is the least time between two fetches. A key id nobody
	// has seen is the one input an attacker chooses, so without this a stream
	// of tokens with random ids is a stream of requests to the provider.
	keySetMinRefresh = time.Minute
	// keySetMaxStale is how long keys that could not be renewed keep serving.
	// A provider that is down for an hour should not sign everybody out, and
	// one that is down for a week should not be trusted from memory.
	keySetMaxStale = 24 * time.Hour
	// keySetMinModulusBits refuses an RSA key too small to trust.
	keySetMinModulusBits = 2048
)

// Where the providers publish the keys their ID tokens are signed with.
const (
	googleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"
	appleKeysURL  = "https://appleid.apple.com/auth/keys"
)

// ErrKeysUnavailable is wrapped by the error [VerifyIDToken] returns when the
// provider's signing keys could not be fetched and none were kept.
//
// It is not [ErrInvalidIDToken], because the two are answered differently: a
// token that does not verify is the caller's to refuse with a 401, and keys
// that could not be fetched are this server's problem and the person should be
// told to try again.
var ErrKeysUnavailable = errors.New("oauth: the provider's signing keys are unavailable")

// KeySet is the public keys a provider signs its ID tokens with, fetched from
// the address it publishes them at and kept.
//
// The set is cached because the alternative is a request to the provider on
// every sign-in, and it is refreshed in three cases: the cache has outlived
// the time the provider asked for, a token names a key id the cache does not
// hold (providers rotate keys, and a token signed with the new one arrives
// before the cache expires), and never more often than once a minute. If the
// refresh fails the old keys keep serving for up to a day, because a provider
// outage should not sign everybody out.
//
// A KeySet is safe for concurrent use, and one is meant to be shared: a
// [Provider] lives for one request pair and its keys do not, which is why
// [AppleKeys] and [GoogleKeys] return the same set every time.
type KeySet struct {
	url    string
	client HTTPClient
	now    func() time.Time

	// fetch serialises refreshes, so a burst of sign-ins after the cache
	// expires is one request and not a hundred.
	fetch sync.Mutex

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	expiresAt time.Time
	// retryAt is set when a refresh fails, so that an outage is not met with
	// a request per sign-in.
	retryAt time.Time
}

// NewKeySet builds the set for a provider's JWKS address. Only RSA keys that
// may sign with RS256 are kept; the address is fetched with the guarded
// client unless [KeySet.SetHTTPClient] says otherwise.
func NewKeySet(url string) *KeySet {
	return &KeySet{url: url, now: time.Now}
}

var (
	googleKeys = NewKeySet(googleKeysURL)
	appleKeys  = NewKeySet(appleKeysURL)
)

// GoogleKeys is the shared set of Google's ID token signing keys.
func GoogleKeys() *KeySet { return googleKeys }

// AppleKeys is the shared set of Apple's ID token signing keys.
func AppleKeys() *KeySet { return appleKeys }

// SetHTTPClient sets the client the keys are fetched with, and returns k. The
// default is the guarded client the providers use, which refuses an address
// inside the network.
func (k *KeySet) SetHTTPClient(client HTTPClient) *KeySet {
	k.client = client
	return k
}

func (k *KeySet) httpClient() HTTPClient {
	if k.client != nil {
		return k.client
	}
	return defaultClient
}

// lookup is the key with this id, fetching the set when the cache cannot
// answer. A key the provider does not publish is (nil, nil): the token is
// refused, and nothing is wrong with the provider.
func (k *KeySet) lookup(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if key, ok := k.cached(kid); ok {
		return key, nil
	}

	k.fetch.Lock()
	defer k.fetch.Unlock()

	// Somebody else may have refreshed while this call waited its turn.
	if key, ok := k.cached(kid); ok {
		return key, nil
	}
	if !k.mayFetch() {
		if key := k.stale(kid); key != nil {
			return key, nil
		}
		if k.empty() {
			return nil, fmt.Errorf("%w: the last attempt to fetch them failed", ErrKeysUnavailable)
		}
		return nil, nil
	}

	keys, ttl, err := k.download(ctx)
	now := k.now()
	if err != nil {
		k.mu.Lock()
		k.retryAt = now.Add(keySetMinRefresh / 2)
		k.mu.Unlock()
		if key := k.stale(kid); key != nil {
			return key, nil
		}
		return nil, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}

	k.mu.Lock()
	k.keys = keys
	k.fetchedAt = now
	k.expiresAt = now.Add(ttl)
	k.retryAt = time.Time{}
	k.mu.Unlock()

	return keys[kid], nil
}

// cached answers from the cache alone: the key, when the cache is fresh and
// holds it.
func (k *KeySet) cached(kid string) (*rsa.PublicKey, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.keys == nil || !k.now().Before(k.expiresAt) {
		return nil, false
	}
	key, ok := k.keys[kid]
	return key, ok
}

// mayFetch reports whether a fetch is allowed now: the cache is expired or the
// key is unknown, never twice inside the minimum interval, and not straight
// after a failure.
func (k *KeySet) mayFetch() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	now := k.now()
	if now.Before(k.retryAt) {
		return false
	}
	return k.fetchedAt.IsZero() || now.Sub(k.fetchedAt) >= keySetMinRefresh
}

// stale is a key from keys that are past their time but not past
// [keySetMaxStale]: what answers when the provider cannot.
func (k *KeySet) stale(kid string) *rsa.PublicKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.keys == nil || k.now().Sub(k.fetchedAt) > keySetMaxStale {
		return nil
	}
	return k.keys[kid]
}

// empty reports whether no key has ever been fetched.
func (k *KeySet) empty() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.keys == nil
}

// jwk is the part of a JSON Web Key this package reads.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// download fetches the document and returns the usable keys and how long to
// trust them.
func (k *KeySet) download(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := k.httpClient().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("requesting the keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("the key request answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, keySetMaxBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("reading the keys: %w", err)
	}
	if len(body) > keySetMaxBytes {
		return nil, 0, errors.New("the key document is larger than the limit")
	}

	var document struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, 0, errors.New("the key document is not valid JSON")
	}
	if len(document.Keys) > keySetMaxKeys {
		document.Keys = document.Keys[:keySetMaxKeys]
	}

	keys := make(map[string]*rsa.PublicKey, len(document.Keys))
	for _, raw := range document.Keys {
		if key, ok := raw.publicKey(); ok {
			keys[raw.Kid] = key
		}
	}
	if len(keys) == 0 {
		return nil, 0, errors.New("the key document holds no usable RSA signing key")
	}
	return keys, ttlFrom(resp.Header.Get("Cache-Control")), nil
}

// publicKey is the RSA key this entry describes, when it is one that may sign
// ID tokens with RS256. Anything else -- an elliptic curve key, an encryption
// key, a key with no id, a modulus under 2048 bits -- is skipped and not an
// error, because a provider may publish keys for other purposes beside these.
func (j jwk) publicKey() (*rsa.PublicKey, bool) {
	if j.Kty != "RSA" || j.Kid == "" || (j.Use != "" && j.Use != "sig") || (j.Alg != "" && j.Alg != "RS256") {
		return nil, false
	}
	n, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, false
	}
	e, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil || len(e) == 0 || len(e) > 4 {
		return nil, false
	}
	modulus := new(big.Int).SetBytes(n)
	if modulus.BitLen() < keySetMinModulusBits {
		return nil, false
	}
	exponent := int(new(big.Int).SetBytes(e).Int64())
	if exponent < 65537 || exponent%2 == 0 {
		return nil, false
	}
	return &rsa.PublicKey{N: modulus, E: exponent}, true
}

// ttlFrom reads max-age out of a Cache-Control header and clamps it.
func ttlFrom(header string) time.Duration {
	for _, directive := range strings.Split(header, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		if !found || !strings.EqualFold(name, "max-age") {
			continue
		}
		seconds, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64)
		if err != nil || seconds < 0 {
			break
		}
		// Compared as seconds first: a year of seconds times a second
		// overflows the duration it is about to become.
		switch {
		case seconds > int64(keySetMaxTTL/time.Second):
			return keySetMaxTTL
		case time.Duration(seconds)*time.Second < keySetMinTTL:
			return keySetMinTTL
		}
		return time.Duration(seconds) * time.Second
	}
	return keySetDefaultTTL
}
