package providers_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/oauth/providers"
)

// fakeStore is the state store the PHP test mocks.
type fakeStore struct {
	state string
	sets  int
	gets  int
}

func (s *fakeStore) SetState(state string) error {
	s.state = state
	s.sets++
	return nil
}

func (s *fakeStore) GetState() (string, error) {
	s.gets++
	return s.state, nil
}

// fakeClient answers without a network and remembers what it was asked.
type fakeClient struct {
	handler func(*http.Request) (*http.Response, error)
	seen    []*http.Request
	bodies  []string
}

func (c *fakeClient) Do(r *http.Request) (*http.Response, error) {
	body := ""
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		body = string(b)
	}
	c.seen = append(c.seen, r)
	c.bodies = append(c.bodies, body)
	return c.handler(r)
}

func respond(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func answering(status int, body string) *fakeClient {
	return &fakeClient{handler: func(*http.Request) (*http.Response, error) {
		return respond(status, body), nil
	}}
}

func provider(store providers.StateStoreInterface) *providers.Provider {
	return providers.NewProvider(store, "client", "secret",
		"https://auth.test/authorize", "https://auth.test/token", "https://auth.test/me")
}

// TestGettingAuthUrlSetsStateInStorage is the PHP's
// testGettingAuthUrlSetsStateInStorage: the redirect is what writes the state
// down, and everything the callback checks depends on it having happened.
func TestGettingAuthUrlSetsStateInStorage(t *testing.T) {
	store := &fakeStore{}
	if _, err := provider(store).GetAuthURL("https://app.test/callback"); err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	if store.sets != 1 {
		t.Fatalf("the state was stored %d times, want once", store.sets)
	}
	if len(store.state) != 40 {
		t.Fatalf("state = %q, want forty random characters", store.state)
	}
}

// TestAuthUrlQueryStringConstruction is the PHP's
// testAuthUrlQueryStringConstruction.
func TestAuthUrlQueryStringConstruction(t *testing.T) {
	store := &fakeStore{}
	p := provider(store).SetScope("read", "write")

	raw, err := p.GetAuthURL("https://app.test/callback", map[string]string{"boom": "zoom"})
	if err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	base, query, found := strings.Cut(raw, "?")
	if !found {
		t.Fatalf("url = %q, want a query string", raw)
	}
	if base != "https://auth.test/authorize" {
		t.Fatalf("base = %q, want the auth endpoint", base)
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	for key, want := range map[string]string{
		"boom":          "zoom",
		"client_id":     "client",
		"redirect_uri":  "https://app.test/callback",
		"response_type": "code",
		"scope":         "read,write",
		"state":         store.state,
	} {
		if got := values.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestGoogleSeparatesScopesWithASpace(t *testing.T) {
	store := &fakeStore{}
	raw, err := providers.NewGoogleProvider(store, "client", "secret").GetAuthURL("https://app.test/callback")
	if err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	_, query, _ := strings.Cut(raw, "?")
	values, _ := url.ParseQuery(query)
	if got := values.Get("scope"); got != "openid profile email" {
		t.Fatalf("scope = %q, want the three joined by spaces", got)
	}
	if got := providers.NewGoogleProvider(store, "c", "s").GetScopeDelimiter(); got != " " {
		t.Fatalf("delimiter = %q, want a space", got)
	}
}

// TestStateMismatchThrowsException is the PHP's
// testStateMismatchThrowsException, and the reason this package exists in the
// shape it does: a callback carrying a state this browser never asked for is
// refused before anything is exchanged.
func TestStateMismatchThrowsException(t *testing.T) {
	store := &fakeStore{state: "foo"}
	client := answering(http.StatusOK, "access_token=token")
	p := provider(store).SetHTTPClient(client)

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	if _, err := p.GetAccessToken(r); !errors.Is(err, providers.ErrStateMismatch) {
		t.Fatalf("err = %v, want ErrStateMismatch", err)
	}
	if len(client.seen) != 0 {
		t.Fatal("the code was exchanged although the state did not match")
	}
}

// TestAnEmptyStateIsAMismatch: a missing cookie and a missing query parameter
// are both empty, and equal is the wrong answer for two things that are not
// there.
func TestAnEmptyStateIsAMismatch(t *testing.T) {
	for _, tc := range []struct{ stored, returned string }{
		{"", ""},
		{"", "something"},
		{"something", ""},
	} {
		if err := providers.Verify(tc.stored, tc.returned); !errors.Is(err, providers.ErrStateMismatch) {
			t.Errorf("Verify(%q, %q) = %v, want ErrStateMismatch", tc.stored, tc.returned, err)
		}
	}
	if err := providers.Verify("same", "same"); err != nil {
		t.Fatalf("Verify of two equal states = %v, want nil", err)
	}
}

// TestTheStateIsCheckedForEveryProvider is the divergence from the clone,
// stated as a test: Illuminate's GithubProvider overrides stateMismatch() to
// return false, and this does not.
func TestTheStateIsCheckedForEveryProvider(t *testing.T) {
	for name, build := range map[string]func(providers.StateStoreInterface) *providers.Provider{
		"github": func(s providers.StateStoreInterface) *providers.Provider {
			return providers.NewGithubProvider(s, "c", "s")
		},
		"google": func(s providers.StateStoreInterface) *providers.Provider {
			return providers.NewGoogleProvider(s, "c", "s")
		},
		"facebook": func(s providers.StateStoreInterface) *providers.Provider {
			return providers.NewFacebookProvider(s, "c", "s")
		},
		"stripe": func(s providers.StateStoreInterface) *providers.Provider {
			return providers.NewStripeProvider(s, "c", "s")
		},
	} {
		store := &fakeStore{state: "asked-for-this"}
		p := build(store).SetHTTPClient(answering(http.StatusOK, `{"access_token":"t"}`))
		r := httptest.NewRequest(http.MethodGet, "/callback?state=somebody-elses&code=blah", nil)
		if _, err := p.GetAccessToken(r); !errors.Is(err, providers.ErrStateMismatch) {
			t.Errorf("%s: err = %v, want ErrStateMismatch", name, err)
		}
	}
}

// TestAccessRequestCalledWithProperOptions is the PHP's
// testAccessRequestCalledWithProperOptions, with the one difference the package
// comment explains: the fields go in a form body rather than a query string.
func TestAccessRequestCalledWithProperOptions(t *testing.T) {
	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, "access_token=token&expires=100")
	p := provider(store).SetHTTPClient(client).RedirectURL("https://current.test/callback")

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	token, err := p.GetAccessToken(r)
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if got := token.GetValue(); got != "token" {
		t.Fatalf("token = %q, want token", got)
	}
	if got := token.Get("expires"); got != "100" {
		t.Fatalf("expires = %q, want 100", got)
	}

	if len(client.seen) != 1 {
		t.Fatalf("%d requests were made, want one", len(client.seen))
	}
	req := client.seen[0]
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if req.URL.String() != "https://auth.test/token" {
		t.Fatalf("url = %s, want the access endpoint", req.URL)
	}
	if req.URL.RawQuery != "" {
		t.Fatalf("query = %q, want the fields in the body and not the URL", req.URL.RawQuery)
	}
	sent, err := url.ParseQuery(client.bodies[0])
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	for key, want := range map[string]string{
		"client_id":     "client",
		"client_secret": "secret",
		"redirect_uri":  "https://current.test/callback",
		"code":          "blah",
		"grant_type":    "authorization_code",
	} {
		if got := sent.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestTheGrantTypeCanBeReplacedTheWayIlluminateAllows(t *testing.T) {
	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, "access_token=token")
	p := provider(store).SetHTTPClient(client)

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	if _, err := p.GetAccessToken(r, map[string]string{"grant_type": "refresh_token"}); err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	sent, _ := url.ParseQuery(client.bodies[0])
	if got := sent.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q, want the one that was passed", got)
	}
}

func TestAJSONTokenResponseIsReadToo(t *testing.T) {
	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, `{"access_token":"tok","expires_in":3600,"scope":"read"}`)
	p := provider(store).SetHTTPClient(client)

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	token, err := p.GetAccessToken(r)
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if token.GetValue() != "tok" {
		t.Fatalf("token = %q, want tok", token.GetValue())
	}
	// A JSON number and a form-encoded one have to read the same, or every
	// caller writes the conversion twice.
	if got := token.Get("expires_in"); got != "3600" {
		t.Fatalf("expires_in = %q, want 3600", got)
	}
	if !token.Has("scope") {
		t.Fatal("Has says the scope is absent")
	}
	if got := token.Get("nothing", "fallback"); got != "fallback" {
		t.Fatalf("Get with a fallback = %q, want fallback", got)
	}
}

func TestARefusedAuthorizationSaysWhy(t *testing.T) {
	store := &fakeStore{state: "bar"}
	p := provider(store).SetHTTPClient(answering(http.StatusOK, ""))

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&error=access_denied", nil)
	_, err := p.GetAccessToken(r)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v, want the provider's refusal named", err)
	}
}

func TestATokenResponseWithNoTokenIsAnError(t *testing.T) {
	store := &fakeStore{state: "bar"}
	p := provider(store).SetHTTPClient(answering(http.StatusOK, `{"error":"bad_verification_code"}`))

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	if _, err := p.GetAccessToken(r); err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("err = %v, want the provider's answer in the message", err)
	}
}

func TestStatelessSkipsTheStateEntirely(t *testing.T) {
	store := &fakeStore{}
	client := answering(http.StatusOK, "access_token=token")
	p := provider(store).Stateless().SetHTTPClient(client)

	if _, err := p.GetAuthURL("https://app.test/callback"); err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	if store.sets != 0 {
		t.Fatal("a stateless provider stored a state")
	}
	r := httptest.NewRequest(http.MethodGet, "/callback?code=blah", nil)
	if _, err := p.GetAccessToken(r); err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if store.gets != 0 {
		t.Fatal("a stateless provider read a state")
	}
}

func TestAProviderWithNoStateStoreRefusesToStart(t *testing.T) {
	p := provider(nil)
	if _, err := p.GetAuthURL("https://app.test/callback"); err == nil {
		t.Fatal("GetAuthURL with no state store must not succeed")
	}
	r := httptest.NewRequest(http.MethodGet, "/callback?state=x&code=y", nil)
	if _, err := p.GetAccessToken(r); err == nil {
		t.Fatal("GetAccessToken with no state store must not succeed")
	}
}

func TestGetUserDataSendsTheTokenInAHeader(t *testing.T) {
	client := &fakeClient{handler: func(*http.Request) (*http.Response, error) {
		return respond(http.StatusOK, `{"id":1234567,"login":"grace","name":"Grace"}`), nil
	}}
	p := providers.NewGithubProvider(&fakeStore{}, "client", "secret").SetHTTPClient(client)

	user, err := p.GetUserData(context.Background(), providers.AccessToken{"access_token": "tok"})
	if err != nil {
		t.Fatalf("GetUserData: %v", err)
	}
	if got := user.String("login"); got != "grace" {
		t.Fatalf("login = %q, want grace", got)
	}
	// An id arrives as a JSON number, and a number printed carelessly comes out
	// as 1.234567e+06.
	if got := user.String("id"); got != "1234567" {
		t.Fatalf("id = %q, want 1234567", got)
	}

	req := client.seen[0]
	if got := req.Header.Get("Authorization"); got != "token tok" {
		t.Fatalf("Authorization = %q, want GitHub's spelling", got)
	}
	if strings.Contains(req.URL.RawQuery, "tok") {
		t.Fatalf("query = %q, want the token out of the URL", req.URL.RawQuery)
	}
}

func TestUserAnswersWithBothThePersonAndTheToken(t *testing.T) {
	calls := 0
	client := &fakeClient{handler: func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return respond(http.StatusOK, `{"access_token":"tok"}`), nil
		}
		return respond(http.StatusOK, `{"sub":"42","email":"grace@example.test"}`), nil
	}}
	store := &fakeStore{state: "bar"}
	p := providers.NewGoogleProvider(store, "client", "secret").SetHTTPClient(client)

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	user, token, err := p.User(r)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if token.GetValue() != "tok" {
		t.Fatalf("token = %q, want tok", token.GetValue())
	}
	if got := user.String("email"); got != "grace@example.test" {
		t.Fatalf("email = %q, want the address the provider sent", got)
	}
	if !user.Has("sub") {
		t.Fatal("the provider's own keys were not kept")
	}
}

func TestStripeAuthenticatesTheTokenRequestWithAHeader(t *testing.T) {
	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, `{"access_token":"tok","stripe_user_id":"acct_1"}`)
	p := providers.NewStripeProvider(store, "client", "sk_test").SetHTTPClient(client)

	r := httptest.NewRequest(http.MethodGet, "/callback?state=bar&code=blah", nil)
	token, err := p.GetAccessToken(r)
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if got := client.seen[0].Header.Get("Authorization"); got != "Bearer sk_test" {
		t.Fatalf("Authorization = %q, want the secret key", got)
	}
	if got := token.Get("stripe_user_id"); got != "acct_1" {
		t.Fatalf("stripe_user_id = %q, want acct_1", got)
	}
	if _, err := p.GetUserData(context.Background(), token); err == nil {
		t.Fatal("Stripe has no user data endpoint and must say so")
	}
}

func TestScopesMergeAndSetScopesReplace(t *testing.T) {
	p := providers.NewGithubProvider(&fakeStore{}, "c", "s")

	if got := strings.Join(p.GetScope(), ","); got != "user:email" {
		t.Fatalf("default scope = %q, want user:email", got)
	}
	p.Scopes("repo", "repo", "gist")
	if got := strings.Join(p.GetScope(), ","); got != "repo,gist" {
		t.Fatalf("scope = %q, want repo,gist with the duplicate dropped", got)
	}
	p.SetScopes("read:user")
	if got := strings.Join(p.GetScope(), ","); got != "read:user" {
		t.Fatalf("scope = %q, want read:user", got)
	}
	p.AddScope("gist")
	if got := strings.Join(p.GetScope(), ","); got != "read:user,gist" {
		t.Fatalf("scope = %q, want read:user,gist", got)
	}
	if got := strings.Join(p.GetDefaultScope(), ","); got != "user:email" {
		t.Fatalf("default scope = %q, want it left alone", got)
	}
}

// TestSetScopeDelimiterSetsTheDelimiter: the PHP assigns from a variable its
// own signature never declares, so the call silently nulls the delimiter.
func TestSetScopeDelimiterSetsTheDelimiter(t *testing.T) {
	p := provider(&fakeStore{}).SetScope("a", "b").SetScopeDelimiter("|")
	if got := p.GetScopeDelimiter(); got != "|" {
		t.Fatalf("delimiter = %q, want |", got)
	}
	raw, err := p.GetAuthURL("https://app.test/callback")
	if err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	_, query, _ := strings.Cut(raw, "?")
	values, _ := url.ParseQuery(query)
	if got := values.Get("scope"); got != "a|b" {
		t.Fatalf("scope = %q, want a|b", got)
	}
}

func TestWithAddsParametersToTheAuthorizationRequest(t *testing.T) {
	p := provider(&fakeStore{}).With(map[string]string{"prompt": "consent"})
	raw, err := p.GetAuthURL("https://app.test/callback")
	if err != nil {
		t.Fatalf("GetAuthURL: %v", err)
	}
	if !strings.Contains(raw, "prompt=consent") {
		t.Fatalf("url = %q, want the parameter in it", raw)
	}
}

func TestTheStateStoreAndClientCanBeReplaced(t *testing.T) {
	first := &fakeStore{}
	p := provider(first)
	if p.GetStateStore() != first {
		t.Fatal("GetStateStore did not answer with the store it was built with")
	}
	second := &fakeStore{}
	p.SetStateStore(second)
	if p.GetStateStore() != second {
		t.Fatal("SetStateStore did not replace the store")
	}
	if p.GetHTTPClient() == nil {
		t.Fatal("GetHTTPClient must answer with a client even when none was set")
	}
	client := answering(http.StatusOK, "")
	if p.SetHTTPClient(client).GetHTTPClient() != client {
		t.Fatal("SetHTTPClient did not replace the client")
	}
}

func TestRedirectSendsTheBrowserToTheProvider(t *testing.T) {
	store := &fakeStore{}
	p := provider(store).RedirectURL("https://app.test/callback")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	if err := p.Redirect(w, r); err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "https://auth.test/authorize?") {
		t.Fatalf("Location = %q, want the authorization endpoint", location)
	}
	if !strings.Contains(location, "state="+store.state) {
		t.Fatalf("Location = %q, want the state that was stored", location)
	}
}

func TestRedirectNeedsARedirectURL(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	if err := provider(&fakeStore{}).Redirect(w, r); err == nil {
		t.Fatal("Redirect with no redirect URL must not succeed")
	}
}

// TestTheCookieStoreKeepsTheStateForOneCallback: the state is written on the
// way out, read on the way back, and gone afterwards. A state that survives its
// callback can be replayed.
func TestTheCookieStoreKeepsTheStateForOneCallback(t *testing.T) {
	out := httptest.NewRecorder()
	first := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	store := providers.NewCookieStateStore(out, first)
	if err := store.SetState("the-state"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	cookies := out.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%d cookies were set, want one", len(cookies))
	}
	c := cookies[0]
	if c.Name != providers.StateCookieName || c.Value != "the-state" {
		t.Fatalf("cookie = %s=%s, want the state", c.Name, c.Value)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v, want HttpOnly and SameSite=Lax", c)
	}

	back := httptest.NewRecorder()
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	callback.AddCookie(c)

	read := providers.NewCookieStateStore(back, callback)
	got, err := read.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got != "the-state" {
		t.Fatalf("state = %q, want the-state", got)
	}
	cleared := back.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("cookies = %+v, want the state cookie cleared", cleared)
	}
}

func TestTheCookieStoreAnswersEmptyWhenNothingWasStored(t *testing.T) {
	store := providers.NewCookieStateStore(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/callback", nil))
	got, err := store.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got != "" {
		t.Fatalf("state = %q, want empty", got)
	}
	// And an empty state never verifies, which is what makes the missing
	// cookie a refusal rather than a pass.
	if err := providers.Verify(got, "anything"); !errors.Is(err, providers.ErrStateMismatch) {
		t.Fatalf("err = %v, want ErrStateMismatch", err)
	}
}

// TestTheWholeFlow puts the two handlers together, against a provider that
// answers like a real one.
func TestTheWholeFlow(t *testing.T) {
	client := &fakeClient{handler: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			return respond(http.StatusOK, `{"access_token":"tok","token_type":"bearer"}`), nil
		case "/me":
			return respond(http.StatusOK, `{"id":7,"email":"ada@example.test"}`), nil
		}
		return respond(http.StatusNotFound, ""), nil
	}}

	// The redirect.
	out := httptest.NewRecorder()
	start := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	p := provider(providers.NewCookieStateStore(out, start)).
		RedirectURL("https://app.test/callback").
		SetHTTPClient(client)
	if err := p.Redirect(out, start); err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	location, err := url.Parse(out.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	state := location.Query().Get("state")

	// The callback, carrying the same state.
	back := httptest.NewRecorder()
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?code=the-code&state="+state, nil)
	for _, c := range out.Result().Cookies() {
		callback.AddCookie(c)
	}
	q := provider(providers.NewCookieStateStore(back, callback)).
		RedirectURL("https://app.test/callback").
		SetHTTPClient(client)

	user, token, err := q.User(callback)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if token.GetValue() != "tok" {
		t.Fatalf("token = %q, want tok", token.GetValue())
	}
	if got := user.String("email"); got != "ada@example.test" {
		t.Fatalf("email = %q, want ada@example.test", got)
	}

	// And the same callback presented a second time is refused, because the
	// state went with the first one.
	replay := provider(providers.NewCookieStateStore(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/auth/callback?code=the-code&state="+state, nil))).
		SetHTTPClient(client)
	replayed := httptest.NewRequest(http.MethodGet, "/auth/callback?code=the-code&state="+state, nil)
	if _, err := replay.GetAccessToken(replayed); !errors.Is(err, providers.ErrStateMismatch) {
		t.Fatalf("err = %v, want the replay refused", err)
	}
}
