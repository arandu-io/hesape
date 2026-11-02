package providers_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/oauth/providers"
)

const (
	appleServicesID = "com.example.web"
	appleTeamID     = "TEAM123456"
	appleKeyID      = "KEY1234567"
)

// apple imitates the three endpoints of Sign in with Apple that the flow
// touches: authorize is never called, a browser would be, so it is the token
// endpoint and the published keys.
type apple struct {
	t        *testing.T
	server   *httptest.Server
	clientEC *ecdsa.PrivateKey
	pem      string
	issuer   providers.TokenIssuer

	mu sync.Mutex
	// idToken makes the id_token the next exchange answers with.
	idToken func() string
	// status, when not 200, is what the token endpoint answers.
	status int
	// exchanges are the forms the token endpoint received.
	exchanges []url.Values
}

func newApple(t *testing.T) *apple {
	t.Helper()
	a, _ := rsaKeys(t)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	f := &apple{
		t:        t,
		clientEC: ec,
		pem:      string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		status:   http.StatusOK,
	}
	f.idToken = func() string { return rs256(t, a, "k1", claimsFor(appleServicesID, nil)) }

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwkOf("k1", a)}})
		case "/auth/token":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if err := r.ParseForm(); err != nil {
				http.Error(w, "form", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.exchanges = append(f.exchanges, r.PostForm)
			status, idToken := f.status, f.idToken
			f.mu.Unlock()
			if status != http.StatusOK {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-1", "token_type": "Bearer", "expires_in": 3600,
				"refresh_token": "rt-1", "id_token": idToken(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)

	target, _ := url.Parse(f.server.URL)
	keys := providers.NewKeySet("https://appleid.apple.com/auth/keys").SetHTTPClient(rewriteClient{target})
	f.issuer = providers.AppleIssuer().WithKeys(keys)
	return f
}

// provider is Apple's provider for one request pair, pointed at the imitation.
func (f *apple) provider(store providers.StateStoreInterface) *providers.Provider {
	target, _ := url.Parse(f.server.URL)
	return providers.NewAppleProvider(store, appleServicesID, appleTeamID, appleKeyID, f.pem).
		RedirectURL("https://app.test/auth/apple/callback").
		SetHTTPClient(rewriteClient{target}).
		UserFromIDToken(f.issuer)
}

// start runs the redirect half and returns the state, the nonce and the
// cookies the browser keeps.
func (f *apple) start(t *testing.T) (state, nonce string, cookies []*http.Cookie, location *url.URL) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://app.test/auth/apple", nil)
	if err := f.provider(providers.NewCookieStateStore(w, r)).Redirect(w, r); err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := location.Query()
	return q.Get("state"), q.Get("nonce"), w.Result().Cookies(), location
}

// callback is what Apple's form_post sends the browser to do.
func callback(form url.Values, cookies []*http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "https://app.test/auth/apple/callback", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://appleid.apple.com")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

// useNonce makes the token endpoint issue the id_token for the nonce the
// redirect carried, as Apple does.
func (f *apple) useNonce(nonce string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idToken = func() string {
		a, _ := rsaKeys(f.t)
		return rs256(f.t, a, "k1", claimsFor(appleServicesID, map[string]any{"nonce": nonce}))
	}
}

func (f *apple) exchangeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.exchanges)
}

func TestAppleSendsTheBrowserWithFormPostAStateAndANonce(t *testing.T) {
	f := newApple(t)
	state, nonce, cookies, location := f.start(t)

	if got := location.Scheme + "://" + location.Host + location.Path; got != "https://appleid.apple.com/auth/authorize" {
		t.Errorf("endpoint = %q", got)
	}
	q := location.Query()
	for key, want := range map[string]string{
		"client_id":     appleServicesID,
		"redirect_uri":  "https://app.test/auth/apple/callback",
		"response_type": "code",
		"response_mode": "form_post",
		"scope":         "name email",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if len(state) != 40 || nonce == "" || nonce == state || strings.Contains(nonce, state) {
		t.Errorf("state = %q, nonce = %q, want a state and a different nonce that does not contain it", state, nonce)
	}

	// The cookie must come back on the POST Apple makes from another site.
	if len(cookies) != 1 {
		t.Fatalf("%d cookies, want the state's", len(cookies))
	}
	c := cookies[0]
	if c.SameSite != http.SameSiteNoneMode || !c.Secure || !c.HttpOnly {
		t.Errorf("cookie = SameSite %v Secure %v HttpOnly %v, want None, Secure, HttpOnly", c.SameSite, c.Secure, c.HttpOnly)
	}
}

func TestAppleResponseModeIsNotSentToTheTokenEndpoint(t *testing.T) {
	f := newApple(t)
	state, nonce, cookies, _ := f.start(t)
	f.useNonce(nonce)

	r := callback(url.Values{"code": {"c-1"}, "state": {state}}, cookies)
	w := httptest.NewRecorder()
	if _, _, err := f.provider(providers.NewCookieStateStore(w, r)).User(r); err != nil {
		t.Fatalf("User: %v", err)
	}
	if got := f.exchanges[0].Get("response_mode"); got != "" {
		t.Errorf("the token request carries response_mode = %q", got)
	}
}

func TestAppleWholeFlow(t *testing.T) {
	f := newApple(t)
	state, nonce, cookies, _ := f.start(t)
	f.useNonce(nonce)

	r := callback(url.Values{
		"code":  {"c-1"},
		"state": {state},
		// Apple's one-time field. The e-mail in it is the browser's word.
		"user": {`{"name":{"firstName":"Ada","lastName":"Lovelace"},"email":"attacker@example.test"}`},
	}, cookies)
	w := httptest.NewRecorder()
	user, token, err := f.provider(providers.NewCookieStateStore(w, r)).User(r)
	if err != nil {
		t.Fatalf("User: %v", err)
	}

	if token.GetValue() != "at-1" || token.Get("refresh_token") != "rt-1" || token.Get("id_token") == "" {
		t.Errorf("token = %v", token)
	}
	if user.String("id") != "001234.abcdef.5678" || user.String("sub") != user.String("id") {
		t.Errorf("id = %q, sub = %q", user.String("id"), user.String("sub"))
	}
	if user.String("email") != "ada@example.test" {
		t.Errorf("email = %q, want the claim and not the posted field", user.String("email"))
	}
	if v, ok := user.Get("email_verified").(bool); !ok || !v {
		t.Errorf("email_verified = %#v, want the boolean true", user.Get("email_verified"))
	}
	if v, ok := user.Get("is_private_email").(bool); !ok || v {
		t.Errorf("is_private_email = %#v, want the boolean false", user.Get("is_private_email"))
	}
	if user.String("name") != "Ada Lovelace" || user.String("given_name") != "Ada" || user.String("family_name") != "Lovelace" {
		t.Errorf("name = %q %q %q", user.String("name"), user.String("given_name"), user.String("family_name"))
	}

	// The state went with this callback.
	clear := false
	for _, c := range w.Result().Cookies() {
		if c.Name == providers.StateCookieName && c.MaxAge < 0 {
			clear = true
		}
	}
	if !clear {
		t.Error("the state cookie was not cleared")
	}
}

func TestTheNameIsOptionalAndNeverBreaksTheLogin(t *testing.T) {
	for name, field := range map[string]string{
		"absent":    "",
		"not JSON":  "{broken",
		"no name":   `{"email":"x@example.test"}`,
		"too large": `{"name":{"firstName":"` + strings.Repeat("A", 5000) + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newApple(t)
			state, nonce, cookies, _ := f.start(t)
			f.useNonce(nonce)
			form := url.Values{"code": {"c-1"}, "state": {state}}
			if field != "" {
				form.Set("user", field)
			}
			r := callback(form, cookies)
			user, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
			if err != nil {
				t.Fatalf("User: %v", err)
			}
			if user.Has("name") || user.Has("given_name") {
				t.Errorf("name = %v, want none", user.All())
			}
			if user.String("id") == "" {
				t.Error("the person is missing")
			}
		})
	}
}

func TestTheClientSecretIsASignedES256Token(t *testing.T) {
	f := newApple(t)
	state, nonce, cookies, _ := f.start(t)
	f.useNonce(nonce)

	r := callback(url.Values{"code": {"c-1"}, "state": {state}}, cookies)
	before := time.Now().Unix()
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); err != nil {
		t.Fatalf("User: %v", err)
	}
	form := f.exchanges[0]

	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "c-1" || form.Get("client_id") != appleServicesID ||
		form.Get("redirect_uri") != "https://app.test/auth/apple/callback" {
		t.Errorf("exchange = %v", form)
	}

	parts := strings.Split(form.Get("client_secret"), ".")
	if len(parts) != 3 {
		t.Fatalf("client_secret = %d segments, want a JWT", len(parts))
	}
	decode := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var header map[string]string
	if err := json.Unmarshal(decode(parts[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "ES256" || header["kid"] != appleKeyID {
		t.Errorf("header = %v", header)
	}
	var claims map[string]any
	if err := json.Unmarshal(decode(parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != appleTeamID || claims["sub"] != appleServicesID || claims["aud"] != "https://appleid.apple.com" {
		t.Errorf("claims = %v", claims)
	}
	iat, exp := int64(claims["iat"].(float64)), int64(claims["exp"].(float64))
	if iat < before-1 || iat > time.Now().Unix()+1 {
		t.Errorf("iat = %d, want now", iat)
	}
	if lifetime := time.Duration(exp-iat) * time.Second; lifetime <= 0 || lifetime > 10*time.Minute {
		t.Errorf("lifetime = %v, want a few minutes", lifetime)
	}

	// The signature is the fixed-length r||s that JWS wants, and it verifies
	// with the public half of the key.
	sig := decode(parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature = %d bytes, want 64", len(sig))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&f.clientEC.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the client secret does not verify with the key")
	}
}

func TestEveryExchangeGetsAFreshClientSecret(t *testing.T) {
	f := newApple(t)
	var secrets []string
	for i := 0; i < 2; i++ {
		state, nonce, cookies, _ := f.start(t)
		f.useNonce(nonce)
		r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
		if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, f.exchanges[i].Get("client_secret"))
	}
	if secrets[0] == secrets[1] {
		t.Error("two exchanges used one client secret")
	}
}

func TestACallbackWithTheWrongStateIsRefusedBeforeAnyRequest(t *testing.T) {
	f := newApple(t)
	_, _, cookies, _ := f.start(t)

	for name, form := range map[string]url.Values{
		"another state": {"code": {"c"}, "state": {"attacker-chosen"}},
		"no state":      {"code": {"c"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := callback(form, cookies)
			_, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
			if !errors.Is(err, providers.ErrStateMismatch) {
				t.Fatalf("err = %v, want ErrStateMismatch", err)
			}
		})
	}
	t.Run("no cookie", func(t *testing.T) {
		r := callback(url.Values{"code": {"c"}, "state": {"anything"}}, nil)
		_, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
		if !errors.Is(err, providers.ErrStateMismatch) {
			t.Fatalf("err = %v, want ErrStateMismatch", err)
		}
	})
	if n := f.exchangeCount(); n != 0 {
		t.Errorf("%d exchanges happened for callbacks nobody asked for", n)
	}
}

func TestAStateInTheQueryOfAPostIsNotRead(t *testing.T) {
	f := newApple(t)
	state, _, cookies, _ := f.start(t)
	r := httptest.NewRequest(http.MethodPost, "https://app.test/auth/apple/callback?code=c&state="+state, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); err == nil {
		t.Fatal("a POST that carried its state in the query was accepted")
	}
}

func TestAnIDTokenForAnotherSignInIsRefused(t *testing.T) {
	f := newApple(t)
	state, _, cookies, _ := f.start(t)
	// The token was issued for a different redirect: its nonce is another's.
	_, otherNonce, _, _ := f.start(t)
	f.useNonce(otherNonce)

	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	_, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
	if !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Fatalf("err = %v, want ErrInvalidIDToken", err)
	}
}

func TestAnIDTokenWithNoNonceIsRefused(t *testing.T) {
	f := newApple(t)
	state, _, cookies, _ := f.start(t)
	a, _ := rsaKeys(t)
	f.idToken = func() string { return rs256(t, a, "k1", claimsFor(appleServicesID, nil)) }

	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Fatalf("err = %v, want ErrInvalidIDToken", err)
	}
}

func TestAnIDTokenIssuedForAnotherServiceIsRefused(t *testing.T) {
	f := newApple(t)
	state, nonce, cookies, _ := f.start(t)
	a, _ := rsaKeys(t)
	f.idToken = func() string {
		return rs256(t, a, "k1", claimsFor("com.example.someone.else", map[string]any{"nonce": nonce}))
	}
	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); !errors.Is(err, providers.ErrInvalidIDToken) {
		t.Fatalf("err = %v, want ErrInvalidIDToken", err)
	}
}

func TestAnUnsignedOrExpiredIDTokenFromTheTokenEndpointIsRefused(t *testing.T) {
	a, b := rsaKeys(t)
	for name, make := range map[string]func(nonce string) string{
		"forged": func(nonce string) string {
			return rs256(t, b, "k1", claimsFor(appleServicesID, map[string]any{"nonce": nonce}))
		},
		"alg none": func(nonce string) string {
			return mint(t, map[string]any{"alg": "none", "kid": "k1"}, claimsFor(appleServicesID, map[string]any{"nonce": nonce}), func(string) []byte { return nil })
		},
		"expired": func(nonce string) string {
			return rs256(t, a, "k1", claimsFor(appleServicesID, map[string]any{"nonce": nonce, "exp": time.Now().Add(-time.Hour).Unix()}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newApple(t)
			state, nonce, cookies, _ := f.start(t)
			f.idToken = func() string { return make(nonce) }
			r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
			if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); !errors.Is(err, providers.ErrInvalidIDToken) {
				t.Fatalf("err = %v, want ErrInvalidIDToken", err)
			}
		})
	}
}

func TestAResponseWithoutAnIDTokenIsRefused(t *testing.T) {
	f := newApple(t)
	state, _, cookies, _ := f.start(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at"}`))
	}))
	defer plain.Close()

	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	purl, _ := url.Parse(plain.URL)
	p := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).SetHTTPClient(rewriteClient{purl})
	if _, _, err := p.User(r); err == nil || !strings.Contains(err.Error(), "id_token") {
		t.Fatalf("err = %v, want a refusal naming the missing id_token", err)
	}
}

func TestUserDataCannotBeReadWithATokenThatSkippedTheCallback(t *testing.T) {
	f := newApple(t)
	a, _ := rsaKeys(t)
	token := providers.AccessToken{"id_token": rs256(t, a, "k1", claimsFor(appleServicesID, map[string]any{"nonce": "anything"}))}

	p := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)))
	if _, err := p.GetUserData(t.Context(), token); err == nil {
		t.Fatal("a token that did not come through a verified callback was read")
	}
}

func TestAStatelessAppleProviderStillVerifiesTheToken(t *testing.T) {
	f := newApple(t)
	a, _ := rsaKeys(t)
	f.idToken = func() string { return rs256(t, a, "k1", claimsFor(appleServicesID, nil)) }

	r := httptest.NewRequest(http.MethodPost, "https://app.test/cb", strings.NewReader("code=c"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	user, _, err := f.provider(nil).Stateless().User(r)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if user.String("id") == "" {
		t.Error("no person")
	}
}

func TestAppleCancelledByThePersonSaysWhy(t *testing.T) {
	f := newApple(t)
	state, _, cookies, _ := f.start(t)
	r := callback(url.Values{"error": {"user_cancelled_authorize"}, "state": {state}}, cookies)
	_, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
	if err == nil || !strings.Contains(err.Error(), "user_cancelled_authorize") {
		t.Fatalf("err = %v, want the provider's reason", err)
	}
	if f.exchangeCount() != 0 {
		t.Error("an exchange was attempted for a refusal")
	}
}

func TestNothingSecretReachesAnError(t *testing.T) {
	f := newApple(t)
	f.status = http.StatusBadRequest
	state, _, cookies, _ := f.start(t)

	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	_, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r)
	if err == nil {
		t.Fatal("a refused exchange was accepted")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want the status", err)
	}
	secret := f.exchanges[0].Get("client_secret")
	body := strings.Join(strings.Split(strings.TrimSpace(f.pem), "\n")[1:3], "")
	for _, leak := range []string{secret, secret[:20], body[:30], "-----BEGIN"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error repeats %q", leak)
		}
	}
}

func TestACredentialMistakeIsReportedWhereItMatters(t *testing.T) {
	f := newApple(t)
	for name, tc := range map[string]struct{ services, team, key, pem string }{
		"not PEM":          {appleServicesID, appleTeamID, appleKeyID, "this is not a key"},
		"empty key":        {appleServicesID, appleTeamID, appleKeyID, ""},
		"no team":          {appleServicesID, "", appleKeyID, f.pem},
		"no key id":        {appleServicesID, appleTeamID, "", f.pem},
		"no services id":   {"", appleTeamID, appleKeyID, f.pem},
		"a certificate":    {appleServicesID, appleTeamID, appleKeyID, "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"},
		"garbage in a PEM": {appleServicesID, appleTeamID, appleKeyID, "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----"},
		"an RSA key":       {appleServicesID, appleTeamID, appleKeyID, rsaPEM(t)},
		"the wrong curve":  {appleServicesID, appleTeamID, appleKeyID, p384PEM(t)},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/auth/apple", nil)
			p := providers.NewAppleProvider(providers.NewCookieStateStore(w, r), tc.services, tc.team, tc.key, tc.pem).
				RedirectURL("https://app.test/cb")
			err := p.Redirect(w, r)
			if err == nil {
				t.Fatal("Redirect accepted a provider that cannot exchange")
			}
			if w.Code == http.StatusFound {
				t.Error("the browser was sent to Apple with a provider that cannot exchange")
			}
			if strings.Contains(err.Error(), "BEGIN") || strings.Contains(err.Error(), "AAAA") {
				t.Errorf("the error repeats the key: %v", err)
			}
		})
	}
}

func TestThePEMMayCarryEscapedLineBreaks(t *testing.T) {
	f := newApple(t)
	f.pem = strings.ReplaceAll(strings.TrimSpace(f.pem), "\n", `\n`)
	state, nonce, cookies, _ := f.start(t)
	f.useNonce(nonce)
	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); err != nil {
		t.Fatalf("User: %v", err)
	}
}

func TestAnECPrivateKeyBlockIsAccepted(t *testing.T) {
	f := newApple(t)
	der, err := x509.MarshalECPrivateKey(f.clientEC)
	if err != nil {
		t.Fatal(err)
	}
	f.pem = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	state, nonce, cookies, _ := f.start(t)
	f.useNonce(nonce)
	r := callback(url.Values{"code": {"c"}, "state": {state}}, cookies)
	if _, _, err := f.provider(providers.NewCookieStateStore(httptest.NewRecorder(), r)).User(r); err != nil {
		t.Fatalf("User: %v", err)
	}
}

func rsaPEM(t *testing.T) string {
	a, _ := rsaKeys(t)
	der, err := x509.MarshalPKCS8PrivateKey(a)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func p384PEM(t *testing.T) string {
	k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestTheStateCookieOfACrossSiteStoreIsAlwaysSecure(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	store := providers.NewCookieStateStore(w, r).Secure(false).CrossSite()
	if err := store.SetState("s"); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	if c.SameSite != http.SameSiteNoneMode || !c.Secure {
		t.Errorf("cookie = SameSite %v Secure %v, want None and Secure: a browser drops None without Secure", c.SameSite, c.Secure)
	}

	// Reading clears it with the same attributes.
	back := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(c)
	got, err := providers.NewCookieStateStore(back, req).Secure(false).CrossSite().GetState()
	if err != nil || got != "s" {
		t.Fatalf("GetState = %q, %v", got, err)
	}
	cleared := back.Result().Cookies()[0]
	if cleared.MaxAge >= 0 || cleared.SameSite != http.SameSiteNoneMode || !cleared.Secure {
		t.Errorf("cleared = %+v", cleared)
	}
}

func TestOtherStoresStayLax(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := providers.NewCookieStateStore(w, r).SetState("s"); err != nil {
		t.Fatal(err)
	}
	if c := w.Result().Cookies()[0]; c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
}
