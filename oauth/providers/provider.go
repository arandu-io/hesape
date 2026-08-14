package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arandu-io/hesape/oauth"
	"github.com/arandu-io/hesape/str"
)

// HTTPClient is the slice of Guzzle's ClientInterface this package needs.
//
// *http.Client is one. It is an interface so that a test can answer without a
// network, which is what Illuminate's own test does with a mock.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultClient stands in for Illuminate's `?: new Guzzle\Http\Client`.
//
// The timeout is the difference between the two: a client with none will wait
// for a provider that has stopped answering until the request that started it
// is the last thing the process ever does.
var defaultClient = &http.Client{Timeout: 30 * time.Second}

// Provider answers Illuminate\Socialite\OAuthTwo\Provider: the
// authorization code flow, from the redirect out to the user data back.
//
// Illuminate's is abstract, and its four subclasses override three endpoints
// and, in two cases, how the token request is sent. In Go a subclass that
// changes three strings is three strings, so the four are constructors --
// [NewGithubProvider], [NewGoogleProvider], [NewFacebookProvider],
// [NewStripeProvider] -- and [NewProvider] is the fifth, for a service
// this package does not carry.
//
// The flow is two handlers:
//
//	// GET /auth/github
//	store := providers.NewCookieStateStore(w, r)
//	provider := providers.NewGithubProvider(store, id, secret).RedirectURL(callback)
//	provider.Redirect(w, r)
//
//	// GET /auth/github/callback
//	store := providers.NewCookieStateStore(w, r)
//	provider := providers.NewGithubProvider(store, id, secret).RedirectURL(callback)
//	user, token, err := provider.User(r)
//
// A provider carries the state store for one request pair and is not safe to
// share between requests: two people signing in at once would be two goroutines
// writing one cookie.
type Provider struct {
	state  StateStoreInterface
	client HTTPClient

	clientID string
	secret   string

	scope          []string
	scopeDelimiter string
	defaultScope   []string

	redirectURL string
	stateless   bool
	parameters  map[string]string

	authEndpoint     string
	accessEndpoint   string
	userDataEndpoint string

	// accessHeaders are headers the token request carries beyond the two every
	// provider gets. Stripe is why the field exists: it authenticates the token
	// request with a bearer header instead of a form field.
	accessHeaders map[string]string
	// userDataAuthScheme is the word in front of the token in the
	// Authorization header of the user data request. Every provider but GitHub
	// says "Bearer".
	userDataAuthScheme string
	// userDataAccept is the Accept header of the user data request.
	userDataAccept string
	// userDataQuery is what Facebook needs: the fields to return.
	userDataQuery map[string]string
}

// NewProvider answers Provider::__construct(), with the three
// endpoints Illuminate's subclasses supply by overriding a method each.
//
// It is the way to reach a provider this package does not carry. The four it
// does carry have constructors of their own, and those set the scope delimiter
// and the default scope as well.
func NewProvider(state StateStoreInterface, clientID, secret, authEndpoint, accessEndpoint, userDataEndpoint string) *Provider {
	return &Provider{
		state:              state,
		clientID:           clientID,
		secret:             secret,
		scopeDelimiter:     ",",
		authEndpoint:       authEndpoint,
		accessEndpoint:     accessEndpoint,
		userDataEndpoint:   userDataEndpoint,
		parameters:         map[string]string{},
		userDataAuthScheme: "Bearer",
		userDataAccept:     "application/json",
	}
}

// GetAuthURL answers Provider::getAuthUrl(): where to send the browser,
// with the state stored on the way past.
//
// Storing the state is the point of the method, not a detail of it. Everything
// else here is assembling a query string; the one line that matters is the one
// that writes down what will have to come back.
//
// options is Illuminate's optional array, and it is applied last, so a provider
// asking for prompt=consent or a login_hint can say so.
func (p *Provider) GetAuthURL(callbackURL string, options ...map[string]string) (string, error) {
	query := url.Values{}

	if p.usesState() {
		if p.state == nil {
			return "", errors.New("oauth: this provider has no state store, and the callback cannot be verified without one (pass one, or call Stateless)")
		}
		state := str.Random(40)
		if err := p.state.SetState(state); err != nil {
			return "", fmt.Errorf("oauth: storing the state: %w", err)
		}
		query.Set("state", state)
	}

	query.Set("client_id", p.clientID)
	query.Set("redirect_uri", p.redirectTarget(callbackURL))
	query.Set("response_type", "code")
	query.Set("scope", p.formattedScope())

	for k, v := range p.parameters {
		query.Set(k, v)
	}
	for k, v := range firstMap(options) {
		query.Set(k, v)
	}

	separator := "?"
	if strings.Contains(p.authEndpoint, "?") {
		separator = "&"
	}
	return p.authEndpoint + separator + query.Encode(), nil
}

// Redirect answers the current Laravel's redirect(): [GetAuthURL] and the 302
// that goes with it.
//
// It sends the browser to the URL set by [Provider.RedirectURL], which
// is also the redirect_uri the provider is told to come back to -- they are one
// value because a provider that was told one address and sent to another
// refuses the exchange.
func (p *Provider) Redirect(w http.ResponseWriter, r *http.Request) error {
	if p.redirectURL == "" {
		return errors.New("oauth: this provider has no redirect URL (call RedirectURL with the address the provider will call back)")
	}
	target, err := p.GetAuthURL(p.redirectURL)
	if err != nil {
		return err
	}
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// GetAccessToken answers Provider::getAccessToken(): the code in the
// callback, exchanged for a token.
//
// The state is verified first, and a mismatch is [ErrStateMismatch] before any
// request is made. Illuminate's GithubProvider overrides that check to always
// pass; this does not, and the difference is deliberate -- see the package
// comment.
//
// options is Illuminate's optional array and is applied last, so grant_type can
// be replaced the way the PHP allows.
func (p *Provider) GetAccessToken(r *http.Request, options ...map[string]string) (AccessToken, error) {
	if p.usesState() {
		if p.state == nil {
			return nil, errors.New("oauth: this provider has no state store, and the callback cannot be verified without one (pass one, or call Stateless)")
		}
		stored, err := p.state.GetState()
		if err != nil {
			return nil, fmt.Errorf("oauth: reading the stored state: %w", err)
		}
		if err := Verify(stored, r.FormValue("state")); err != nil {
			return nil, err
		}
	}

	if code := r.FormValue("code"); code == "" {
		// The provider sends error=access_denied when the person says no, and
		// saying so is more useful than an empty exchange that fails later.
		if reason := r.FormValue("error"); reason != "" {
			return nil, fmt.Errorf("oauth: the provider refused the authorization: %s", reason)
		}
		return nil, errors.New("oauth: the callback carries no authorization code")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.secret)
	form.Set("redirect_uri", p.redirectTarget(currentURL(r)))
	form.Set("code", r.FormValue("code"))
	for k, v := range p.parameters {
		form.Set(k, v)
	}
	for k, v := range firstMap(options) {
		form.Set(k, v)
	}

	body, err := p.executeAccessRequest(r.Context(), form)
	if err != nil {
		return nil, err
	}
	token, err := parseAccessResponse(body)
	if err != nil {
		return nil, err
	}
	if token.GetValue() == "" {
		return nil, fmt.Errorf("oauth: the provider returned no access token: %s", firstLine(string(body)))
	}
	return token, nil
}

// executeAccessRequest answers Provider::executeAccessRequest().
//
// Illuminate's base sends a GET with every field in the query string, and two
// of its four subclasses override it to POST instead. This posts for all of
// them, which is what the current Laravel does and what all four providers
// document: client_secret in a query string is client_secret in an access log,
// in a proxy's history and in a Referer header, and no provider requires it
// there.
func (p *Provider) executeAccessRequest(ctx context.Context, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.accessEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: building the access token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range p.accessHeaders {
		req.Header.Set(k, v)
	}

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: requesting the access token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: reading the access token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("oauth: the access token request answered %d: %s", resp.StatusCode, firstLine(string(body)))
	}
	return body, nil
}

// GetUserData answers Provider::getUserData(): who the token belongs
// to, as the provider describes them.
//
// Illuminate puts the token in the query string. This puts it in the
// Authorization header, because GitHub and Google both stopped accepting it in
// the query string, and a token in a URL is logged everywhere a secret in a URL
// is logged.
func (p *Provider) GetUserData(ctx context.Context, token AccessToken) (oauth.UserData, error) {
	if p.userDataEndpoint == "" {
		return oauth.UserData{}, errors.New("oauth: this provider has no user data endpoint")
	}

	endpoint := p.userDataEndpoint
	if len(p.userDataQuery) > 0 {
		query := url.Values{}
		for k, v := range p.userDataQuery {
			query.Set(k, v)
		}
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint += separator + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return oauth.UserData{}, fmt.Errorf("oauth: building the user data request: %w", err)
	}
	req.Header.Set("Authorization", p.userDataAuthScheme+" "+token.GetValue())
	req.Header.Set("Accept", p.userDataAccept)

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		return oauth.UserData{}, fmt.Errorf("oauth: requesting the user data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return oauth.UserData{}, fmt.Errorf("oauth: reading the user data: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return oauth.UserData{}, fmt.Errorf("oauth: the user data request answered %d: %s", resp.StatusCode, firstLine(string(body)))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return oauth.UserData{}, fmt.Errorf("oauth: the user data is not valid JSON: %w", err)
	}
	return oauth.NewUserData(raw), nil
}

// User answers the current Laravel's user(): the callback, verified, exchanged
// and resolved, in one call.
//
// It answers the token as well as the person. Illuminate hangs the token on its
// User object; a [oauth.UserData] is the provider's own map, and putting
// this package's data among the provider's is how a caller ends up reading
// "token" out of a bag where GitHub could have put one of its own.
func (p *Provider) User(r *http.Request) (oauth.UserData, AccessToken, error) {
	token, err := p.GetAccessToken(r)
	if err != nil {
		return oauth.UserData{}, nil, err
	}
	user, err := p.GetUserData(r.Context(), token)
	if err != nil {
		return oauth.UserData{}, token, err
	}
	return user, token, nil
}

// GetStateStore answers Provider::getStateStore().
func (p *Provider) GetStateStore() StateStoreInterface { return p.state }

// SetStateStore answers Provider::setStateStore().
func (p *Provider) SetStateStore(state StateStoreInterface) *Provider {
	p.state = state
	return p
}

// GetHTTPClient answers Provider::getHttpClient(), including its
// fallback: a client nobody supplied is a default one.
func (p *Provider) GetHTTPClient() HTTPClient {
	if p.client != nil {
		return p.client
	}
	return defaultClient
}

// SetHTTPClient answers Provider::setHttpClient().
func (p *Provider) SetHTTPClient(client HTTPClient) *Provider {
	p.client = client
	return p
}

// GetScope answers Provider::getScope(): what was asked for, or the
// provider's default when nothing was.
func (p *Provider) GetScope() []string {
	if len(p.scope) > 0 {
		return p.scope
	}
	return p.GetDefaultScope()
}

// GetDefaultScope answers Provider::getDefaultScope(), which each
// subclass overrides and each constructor here sets.
func (p *Provider) GetDefaultScope() []string { return p.defaultScope }

// SetScope answers Provider::setScope(): the scopes, replacing whatever
// was there.
//
// Illuminate takes a string or an array and casts; a variadic tail is that cast.
func (p *Provider) SetScope(scope ...string) *Provider {
	return p.SetScopes(scope...)
}

// SetScopes answers the current Laravel's setScopes(), which is
// [Provider.SetScope] under the name the newer Socialite gave it.
// Duplicates are dropped, as they are there.
func (p *Provider) SetScopes(scopes ...string) *Provider {
	p.scope = unique(scopes)
	return p
}

// Scopes answers the current Laravel's scopes(): the given scopes merged with
// the ones already asked for.
func (p *Provider) Scopes(scopes ...string) *Provider {
	p.scope = unique(append(append([]string{}, p.scope...), scopes...))
	return p
}

// AddScope answers Provider::addScope(): one more scope.
//
// Illuminate appends to a list that starts empty, so the first call to this
// replaces the provider's default rather than adding to it. That is the PHP's
// behaviour and it is kept: [Provider.Scopes] is the call that adds to
// what is already there.
func (p *Provider) AddScope(scope string) *Provider {
	p.scope = unique(append(p.scope, scope))
	return p
}

// GetScopeDelimiter answers Provider::getScopeDelimiter().
func (p *Provider) GetScopeDelimiter() string { return p.scopeDelimiter }

// SetScopeDelimiter answers Provider::setScopeDelimiter().
//
// The PHP assigns from $scopeDelimiter, a variable its own signature does not
// declare, so the call silently sets the delimiter to null and every scope
// after it is joined by nothing. This assigns the argument, which is what the
// method is named for; mirroring the typo would be a method with the right name
// and the wrong behaviour, and nobody checks those.
func (p *Provider) SetScopeDelimiter(delimiter string) *Provider {
	p.scopeDelimiter = delimiter
	return p
}

// RedirectURL answers the current Laravel's redirectUrl(): where the provider
// sends the browser back to.
func (p *Provider) RedirectURL(url string) *Provider {
	p.redirectURL = url
	return p
}

// With answers the current Laravel's with(): extra parameters on the
// authorization request, such as prompt or login_hint.
func (p *Provider) With(parameters map[string]string) *Provider {
	p.parameters = map[string]string{}
	for k, v := range parameters {
		p.parameters[k] = v
	}
	return p
}

// Stateless answers the current Laravel's stateless(): no state stored and none
// verified.
//
// It is for an API client that has no cookie to keep a state in, and it is not
// a convenience. Without the state there is nothing tying the callback to the
// request that caused it, which is the attack [ErrStateMismatch] describes. Use
// it where there is genuinely no session, and nowhere else.
func (p *Provider) Stateless() *Provider {
	p.stateless = true
	return p
}

// usesState answers Provider::usesState().
func (p *Provider) usesState() bool { return !p.stateless }

// formattedScope answers Provider::getFormattedScope().
func (p *Provider) formattedScope() string {
	return strings.Join(p.GetScope(), p.scopeDelimiter)
}

// redirectTarget is the redirect_uri sent to the provider: the one that was
// configured, and the callback that was passed only when none was.
func (p *Provider) redirectTarget(fallback string) string {
	if p.redirectURL != "" {
		return p.redirectURL
	}
	return fallback
}

// currentURL answers Provider::getCurrentUrl().
//
// It is a fallback and not the way this should work. The host comes off the
// request, which is a header a client wrote, so behind a proxy it is whatever
// the proxy passed through -- [Provider.RedirectURL] is the value the
// provider is checking against and the one to set.
func currentURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + r.URL.Path
}

func firstMap(values []map[string]string) map[string]string {
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// firstLine trims a provider's error body to something that fits in a log line
// without carrying an HTML page into it.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
