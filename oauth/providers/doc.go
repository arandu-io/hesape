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
// Every provider here checks it, GitHub included. A method named for a check
// that does not check is worse than one that is missing, because nobody looks
// at it again.
//
// [Provider.Stateless] turns the check off deliberately, for a caller with no
// cookie to keep a state in. That is a decision somebody makes and can be found
// by searching for; a provider that quietly skipped it is not.
//
// # There are no subclasses
//
// What separates one provider from another is three endpoint strings, a scope
// delimiter, a list of default scopes, and how the token request is sent and
// read. All of that is data, so it is data: [NewGithubProvider] and its three
// companions build one [Provider] each, and [NewProvider] builds one for a
// service this package does not carry.
//
// One implementation covers the request and the response for all of them. The
// token request is always a POST with a form body, which every provider here
// accepts -- a GET would put client_secret in a query string, where access logs
// and Referer headers keep it. The token response is read as JSON or as a
// form-encoded body depending on what arrived.
package providers
