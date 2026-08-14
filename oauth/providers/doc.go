// Package providers is the OAuth 2 authorization code flow, out to the provider
// and back, with one type per provider on top of it.
//
// Four are here: Google, GitHub, Facebook and Twitter. Each is the same flow
// with different endpoints and a different shape of answer, mapped into the one
// [github.com/arandu-io/hesape/oauth.UserData] every caller reads.
//
// Two handlers use it. One sends the browser out:
//
//	store := providers.NewCookieStateStore(w, r)
//	provider := providers.NewGithubProvider(store, id, secret).
//		RedirectURL("https://example.com/auth/github/callback")
//	provider.Redirect(w, r)
//
// The other takes the callback:
//
//	store := providers.NewCookieStateStore(w, r)
//	provider := providers.NewGithubProvider(store, id, secret).
//		RedirectURL("https://example.com/auth/github/callback")
//
//	user, token, err := provider.User(r)
//	if errors.Is(err, providers.ErrStateMismatch) {
//		// this callback was not asked for by this browser
//	}
//
// # The state is the security, and it is checked
//
// Everything else in this flow is bookkeeping. The one thing that decides
// whether it is safe is [Verify]: a random string stored before the redirect
// and compared on the way back.
//
// Without it, the callback is a URL like any other, and an attacker can hand a
// victim one carrying a code issued for the attacker's own account. The
// application exchanges it, gets a valid token for the attacker's account, and
// signs the victim into it -- so everything the victim does next, the attacker
// can read. Nothing about the request looks wrong; there is simply no evidence
// that the application asked for it.
//
// Illuminate's GithubProvider overrides stateMismatch() to return false, which
// turns that check off for GitHub. It is not reproduced here. The rule about
// bodies over signatures is what settles it: a method named for a check that
// does not check is worse than one that is missing, because nobody looks at it
// again -- and the current Laravel checks the state for every provider,
// GitHub included. The divergence is one method, and it is this one.
//
// [Provider.Stateless] turns the check off deliberately, for a caller
// with no cookie to keep a state in. That is a decision somebody makes and can
// be found by searching for; a provider that quietly skipped it is not.
//
// # What each provider needs, and why there are no subclasses
//
// Illuminate's four providers are four subclasses. What they override is three
// endpoint strings, a scope delimiter, a list of default scopes, and -- for
// Google and Stripe -- how the token request is sent and read. All of that is
// data, so it is data: [NewGithubProvider] and its three companions build one
// [Provider] each, and [NewProvider] builds one for a service
// this package does not carry.
//
// Two of those overrides are gone because one implementation covers both cases.
// The token request is always a POST with a form body, which is what the
// current Laravel does for every provider and what all four accept -- the
// clone's default GET puts client_secret in a query string, where access logs
// and Referer headers keep it. The token response is read as JSON or as a
// form-encoded body depending on what arrived, which is what the base class and
// its two overrides do between them.
//
// # What is not here
//
// The clone's getCurrentUrl(), getClientId(), getClientSecret(),
// getRedirectUri(), getResponseType(), buildDefaultAuthQuery(),
// buildAuthQuery(), getAccessOptions(), buildDefaultAccessOptions(),
// buildAccessOptions(), getGrantOptions() and snakeToCamel() are protected: in
// PHP they exist to be overridden, and here the thing they varied is a field.
// snakeToCamel() is the one worth naming, because it exists only to turn
// 'client_id' into a call to getClientId() by string -- a method call assembled
// out of a string is what Go does not do, and does not need to.
package providers
