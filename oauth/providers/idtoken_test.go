package providers_test

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/oauth/providers"
)

const testAudience = "com.example.app"

// verifier is an Apple issuer reading its keys from a test server that
// publishes keyA as "k1".
func verifier(t *testing.T) (providers.TokenIssuer, *jwksServer) {
	t.Helper()
	a, _ := rsaKeys(t)
	server := newJWKSServer(t, jwkOf("k1", a))
	keys := providers.NewKeySet(server.URL + "/keys").SetHTTPClient(http.DefaultClient)
	return providers.AppleIssuer().WithKeys(keys), server
}

func verify(t *testing.T, issuer providers.TokenIssuer, token string, options ...providers.IDTokenOption) (providers.IDTokenClaims, error) {
	t.Helper()
	return providers.VerifyIDToken(context.Background(), token, issuer, []string{testAudience}, options...)
}

func TestAValidAppleTokenIsAccepted(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)

	claims, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, nil)))
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if claims.Subject != "001234.abcdef.5678" || claims.Email != "ada@example.test" {
		t.Fatalf("claims = %+v", claims)
	}
	if !claims.EmailVerified || claims.IsPrivateEmail {
		t.Errorf("EmailVerified = %v, IsPrivateEmail = %v, want true and false", claims.EmailVerified, claims.IsPrivateEmail)
	}
	user := claims.UserData()
	if user.String("id") != claims.Subject || !user.Bool("email_verified") || user.Bool("is_private_email") {
		t.Errorf("UserData = %v", user.All())
	}
}

func TestAFlagIsReadAsABooleanOrAsText(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)

	for _, tc := range []struct {
		name            string
		verified, relay any
		wantVerified    bool
		wantRelay       bool
	}{
		{"text", "true", "true", true, true},
		{"boolean", true, true, true, true},
		{"text false", "false", "false", false, false},
		{"boolean false", false, false, false, false},
		{"absent", nil, nil, false, false},
		{"a number is no flag", float64(1), float64(1), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience,
				map[string]any{"email_verified": tc.verified, "is_private_email": tc.relay})))
			if err != nil {
				t.Fatalf("VerifyIDToken: %v", err)
			}
			if claims.EmailVerified != tc.wantVerified || claims.IsPrivateEmail != tc.wantRelay {
				t.Errorf("verified = %v, relay = %v, want %v and %v", claims.EmailVerified, claims.IsPrivateEmail, tc.wantVerified, tc.wantRelay)
			}
		})
	}
}

func TestGoogleTokensAreVerifiedWithGoogleIssuers(t *testing.T) {
	a, _ := rsaKeys(t)
	server := newJWKSServer(t, jwkOf("g1", a))
	issuer := providers.GoogleIssuer().WithKeys(providers.NewKeySet(server.URL + "/keys").SetHTTPClient(http.DefaultClient))

	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		claims, err := verify(t, issuer, rs256(t, a, "g1", claimsFor(testAudience, map[string]any{
			"iss": iss, "email_verified": true, "name": "Ada Lovelace", "picture": "https://example.test/a.png",
			"aud": []string{"other", testAudience}, "azp": testAudience,
		})))
		if err != nil {
			t.Fatalf("iss %q: %v", iss, err)
		}
		if claims.Name != "Ada Lovelace" || claims.AuthorizedParty != testAudience || len(claims.Audience) != 2 {
			t.Errorf("claims = %+v", claims)
		}
	}
	// Apple's issuer is not Google's.
	if _, err := verify(t, issuer, rs256(t, a, "g1", claimsFor(testAudience, nil))); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Errorf("an Apple iss on a Google issuer: err = %v, want ErrInvalidIDToken", err)
	}
}

func TestATokenIsRefused(t *testing.T) {
	a, b := rsaKeys(t)
	issuer, _ := verifier(t)
	now := time.Now()

	good := func(extra map[string]any) string { return rs256(t, a, "k1", claimsFor(testAudience, extra)) }
	valid := good(nil)
	parts := strings.Split(valid, ".")
	otherPayload := strings.Split(good(map[string]any{"sub": "someone-else"}), ".")[1]

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"expired", good(map[string]any{"exp": now.Add(-10 * time.Minute).Unix()})},
		{"another audience", good(map[string]any{"aud": "com.example.other"})},
		{"no audience", good(map[string]any{"aud": nil})},
		{"another issuer", good(map[string]any{"iss": "https://evil.test"})},
		{"no issuer", good(map[string]any{"iss": nil})},
		{"no subject", good(map[string]any{"sub": nil})},
		{"no expiry", good(map[string]any{"exp": nil})},
		{"expiry as text", good(map[string]any{"exp": "9999999999"})},
		{"not valid yet", good(map[string]any{"nbf": now.Add(time.Hour).Unix()})},
		{"issued in the future", good(map[string]any{"iat": now.Add(time.Hour).Unix()})},
		{"signed by another key", rs256(t, b, "k1", claimsFor(testAudience, nil))},
		{"a key id nobody published", rs256(t, a, "k9", claimsFor(testAudience, nil))},
		{"no key id", mint(t, map[string]any{"alg": "RS256"}, claimsFor(testAudience, nil), signRS256(t, a))},
		{"alg none", mint(t, map[string]any{"alg": "none", "kid": "k1"}, claimsFor(testAudience, nil), func(string) []byte { return nil })},
		{"alg none with a signature", mint(t, map[string]any{"alg": "none", "kid": "k1"}, claimsFor(testAudience, nil), func(string) []byte { return []byte("x") })},
		{"alg HS256 keyed with the public modulus", mint(t, map[string]any{"alg": "HS256", "kid": "k1"}, claimsFor(testAudience, nil), hmacHS256(a.N.Bytes()))},
		{"alg lower case", mint(t, map[string]any{"alg": "rs256", "kid": "k1"}, claimsFor(testAudience, nil), signRS256(t, a))},
		{"alg ES256", mint(t, map[string]any{"alg": "ES256", "kid": "k1"}, claimsFor(testAudience, nil), signRS256(t, a))},
		{"a crit header", mint(t, map[string]any{"alg": "RS256", "kid": "k1", "crit": []string{"exp"}}, claimsFor(testAudience, nil), signRS256(t, a))},
		{"a payload swapped under a valid signature", parts[0] + "." + otherPayload + "." + parts[2]},
		{"an empty signature", parts[0] + "." + parts[1] + "."},
		{"a signature cut short", valid[:len(valid)-8]},
		{"two segments", parts[0] + "." + parts[1]},
		{"four segments", valid + ".x"},
		{"empty", ""},
		{"padding", parts[0] + "=." + parts[1] + "." + parts[2]},
		{"a line break", parts[0] + "\n." + parts[1] + "." + parts[2]},
		{"claims that are not an object", mint(t, map[string]any{"alg": "RS256", "kid": "k1"}, nil, signRS256(t, a))},
		{"larger than the limit", valid + strings.Repeat("A", 20<<10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verify(t, issuer, tc.token)
			if !errors.Is(err, providers.ErrInvalidIDToken) {
				t.Fatalf("err = %v, want ErrInvalidIDToken", err)
			}
			if errors.Is(err, providers.ErrKeysUnavailable) {
				t.Errorf("err = %v, a bad token is not a missing key set", err)
			}
		})
	}
}

func TestAnExpiredTokenSaysSo(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)
	_, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})))
	if !errors.Is(err, providers.ErrExpiredIDToken) || !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Fatalf("err = %v, want ErrExpiredIDToken, which is also ErrInvalidIDToken", err)
	}
}

func TestAClockAMinuteOffIsForgiven(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)
	now := time.Now()
	for _, extra := range []map[string]any{
		{"exp": now.Add(-30 * time.Second).Unix()},
		{"iat": now.Add(30 * time.Second).Unix(), "nbf": now.Add(30 * time.Second).Unix()},
	} {
		if _, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, extra))); err != nil {
			t.Errorf("%v: %v", extra, err)
		}
	}
}

func TestTheNonceIsCheckedWhenAsked(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)
	withNonce := rs256(t, a, "k1", claimsFor(testAudience, map[string]any{"nonce": "n-123"}))
	without := rs256(t, a, "k1", claimsFor(testAudience, nil))

	if _, err := verify(t, issuer, withNonce, providers.WithNonce("n-123")); err != nil {
		t.Errorf("the right nonce: %v", err)
	}
	if _, err := verify(t, issuer, withNonce, providers.WithNonce("n-124")); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Errorf("the wrong nonce: err = %v, want ErrInvalidIDToken", err)
	}
	if _, err := verify(t, issuer, without, providers.WithNonce("n-123")); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Errorf("no nonce in the token: err = %v, want ErrInvalidIDToken", err)
	}
	// Without the option the claim is not required: a native flow that has no
	// nonce is the caller's decision.
	if _, err := verify(t, issuer, without); err != nil {
		t.Errorf("no nonce asked for: %v", err)
	}
	// An empty nonce would turn the check into a pass.
	if _, err := verify(t, issuer, without, providers.WithNonce("")); err == nil || errors.Is(err, providers.ErrInvalidIDToken) {
		t.Errorf("WithNonce(\"\"): err = %v, want a configuration error", err)
	}
}

func TestAMisconfiguredVerificationFailsClosed(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)
	token := rs256(t, a, "k1", claimsFor(testAudience, nil))

	if _, err := providers.VerifyIDToken(context.Background(), token, issuer, nil); err == nil {
		t.Error("no audience was accepted")
	}
	if _, err := providers.VerifyIDToken(context.Background(), token, issuer, []string{}); err == nil {
		t.Error("an empty audience list was accepted")
	}
	if _, err := providers.VerifyIDToken(context.Background(), token, providers.TokenIssuer{}, []string{testAudience}); err == nil {
		t.Error("an issuer with no keys was accepted")
	}
	if _, err := providers.VerifyIDToken(context.Background(), token, providers.NewTokenIssuer(providers.NewKeySet("x")), []string{testAudience}); err == nil {
		t.Error("an issuer with no iss value was accepted")
	}
}

func TestAnyOfSeveralAudiencesMatches(t *testing.T) {
	a, _ := rsaKeys(t)
	issuer, _ := verifier(t)
	token := rs256(t, a, "k1", claimsFor("com.example.ios", nil))
	if _, err := providers.VerifyIDToken(context.Background(), token, issuer, []string{"com.example.android", "com.example.ios"}); err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if _, err := providers.VerifyIDToken(context.Background(), token, issuer, []string{"com.example.android", "com.example"}); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Fatalf("err = %v, want a prefix of the audience refused", err)
	}
}

func TestNoErrorRepeatsTheToken(t *testing.T) {
	a, b := rsaKeys(t)
	issuer, _ := verifier(t)
	for _, token := range []string{
		rs256(t, b, "k1", claimsFor(testAudience, map[string]any{"sub": "secret-subject-value"})),
		rs256(t, a, "k1", claimsFor("audience-that-is-wrong", nil)),
		rs256(t, a, "k1", claimsFor(testAudience, map[string]any{"iss": "issuer-that-is-wrong"})),
	} {
		_, err := verify(t, issuer, token)
		if err == nil {
			t.Fatal("a bad token was accepted")
		}
		for _, leak := range []string{token, "secret-subject-value", "audience-that-is-wrong", "issuer-that-is-wrong"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("the error repeats %q: %v", leak, err)
			}
		}
	}
}

func TestKeysThatCannotBeFetchedAreNotATokenFault(t *testing.T) {
	a, _ := rsaKeys(t)
	server := newJWKSServer(t, jwkOf("k1", a))
	server.status = http.StatusInternalServerError
	issuer := providers.AppleIssuer().WithKeys(providers.NewKeySet(server.URL + "/keys").SetHTTPClient(http.DefaultClient))

	_, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, nil)))
	if !errors.Is(err, providers.ErrKeysUnavailable) {
		t.Fatalf("err = %v, want ErrKeysUnavailable", err)
	}
	if errors.Is(err, providers.ErrInvalidIDToken) {
		t.Errorf("err = %v, an outage is not an invalid token", err)
	}
	// And the next call, straight after, does not ask the provider again
	// while it is down, and still says the keys are the problem.
	hits := server.hits.Load()
	if _, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, nil))); !errors.Is(err, providers.ErrKeysUnavailable) {
		t.Errorf("second call: err = %v, want ErrKeysUnavailable", err)
	}
	if server.hits.Load() != hits {
		t.Errorf("the provider was asked again straight after a failure")
	}
}

func TestOnlyUsableRSAKeysAreKept(t *testing.T) {
	a, _ := rsaKeys(t)
	with := func(change func(map[string]any)) map[string]any {
		k := jwkOf("x", a)
		change(k)
		return k
	}
	smallModulus := new(big.Int).Rsh(a.N, 1500) // a few hundred bits

	// Each of these would verify the token if it were kept; the server
	// publishes them all under the id the token names, one at a time.
	for name, entry := range map[string]map[string]any{
		"an elliptic curve key": {"kty": "EC", "kid": "k1", "crv": "P-256", "x": "AA", "y": "AA"},
		"a key for encryption":  with(func(k map[string]any) { k["kid"] = "k1"; k["use"] = "enc" }),
		"a key for RS512":       with(func(k map[string]any) { k["kid"] = "k1"; k["alg"] = "RS512" }),
		"a small modulus":       with(func(k map[string]any) { k["kid"] = "k1"; k["n"] = b64(smallModulus.Bytes()) }),
		"exponent 3":            with(func(k map[string]any) { k["kid"] = "k1"; k["e"] = b64([]byte{3}) }),
		"an even exponent":      with(func(k map[string]any) { k["kid"] = "k1"; k["e"] = b64([]byte{1, 0, 0}) }),
	} {
		t.Run(name, func(t *testing.T) {
			// A usable key beside it so the document itself is accepted.
			server := newJWKSServer(t, entry, jwkOf("other", a))
			issuer := providers.AppleIssuer().WithKeys(providers.NewKeySet(server.URL + "/keys").SetHTTPClient(http.DefaultClient))
			_, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, nil)))
			if !errors.Is(err, providers.ErrInvalidIDToken) {
				t.Fatalf("err = %v, want the key skipped and the token refused", err)
			}
		})
	}
}

func TestADocumentWithNoUsableKeyIsAnOutage(t *testing.T) {
	a, _ := rsaKeys(t)
	server := newJWKSServer(t, map[string]any{"kty": "EC", "kid": "ec"})
	issuer := providers.AppleIssuer().WithKeys(providers.NewKeySet(server.URL + "/keys").SetHTTPClient(http.DefaultClient))
	_, err := verify(t, issuer, rs256(t, a, "ec", claimsFor(testAudience, nil)))
	if !errors.Is(err, providers.ErrKeysUnavailable) {
		t.Fatalf("err = %v, want ErrKeysUnavailable", err)
	}
}

func TestTheDefaultKeySetRefusesAnAddressInsideTheNetwork(t *testing.T) {
	a, _ := rsaKeys(t)
	server := newJWKSServer(t, jwkOf("k1", a))
	issuer := providers.AppleIssuer().WithKeys(providers.NewKeySet(server.URL + "/keys"))
	_, err := verify(t, issuer, rs256(t, a, "k1", claimsFor(testAudience, nil)))
	if !errors.Is(err, providers.ErrKeysUnavailable) {
		t.Fatalf("err = %v, want ErrKeysUnavailable", err)
	}
	if server.hits.Load() != 0 {
		t.Error("the default client reached a server on loopback")
	}
}
