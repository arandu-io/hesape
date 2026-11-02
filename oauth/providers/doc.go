// Package providers is the OAuth 2 authorization code flow, out to the provider
// and back, with one type per provider on top of it.
//
// Five are here: Google, GitHub, Facebook, Apple and Stripe Connect. Each is
// the same flow with different endpoints and a different shape of answer,
// mapped into the one [github.com/arandu-io/hesape/oauth.UserData] every caller
// reads. Apple differs most, and has a section of its own below.
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
// read. All of that is data, so it is data: [NewGithubProvider] and its
// companions build one [Provider] each, and [NewProvider] builds one for a
// service this package does not carry.
//
// One implementation covers the request and the response for all of them. The
// token request is always a POST with a form body, which every provider here
// accepts -- a GET would put client_secret in a query string, where access logs
// and Referer headers keep it. The token response is read as JSON or as a
// form-encoded body depending on what arrived.
//
// # Sign in with Apple
//
// [NewAppleProvider] is a login, for people who already have an account: it
// answers who the person is at Apple and creates nothing. The answer carries
// id (the subject, stable, and the one thing to link an account by), email,
// email_verified and is_private_email. The last two are booleans, whichever way
// Apple wrote them, so the application decides on them: an address that is not
// verified, or that is Apple's relay, says nothing about which account is the
// person's, and an application that links accounts by e-mail should link only by
// a verified one that is not a relay.
//
// Apple answers the redirect with a POST from its own site (response_mode
// form_post), which asks three things of the application.
//
// The callback route accepts POST:
//
//	r.Post("/auth/apple/callback", http.HandlerFunc(callback))
//
// The callback route is exempt from the CSRF check, because the request is
// cross-site by construction and carries no token. The state check takes the
// place of it and is not optional: the exemption is written where CSRFProtect
// is wired, for that one path and no other, with
// [github.com/arandu-io/hesape/http/middleware.CSRFExcept]:
//
//	middleware.CSRFProtect(csrf, sessions.IDFromRequest,
//		middleware.CSRFExcept("/auth/apple/callback"))
//
// And the state must survive a cross-site POST, which a SameSite=Lax cookie
// does not. [CookieStateStore] is switched to SameSite=None; Secure by
// [NewAppleProvider] (see [CookieStateStore.CrossSite]), so the site must be
// served over HTTPS -- Apple accepts no other redirect address. A state kept
// anywhere else has to be readable on that POST as well.
//
// The callback then reads the person from the id_token the exchange returns:
//
//	func callback(w http.ResponseWriter, r *http.Request) {
//		store := providers.NewCookieStateStore(w, r)
//		provider := providers.NewAppleProvider(store, servicesID, teamID, keyID, p8).
//			RedirectURL("https://example.com/auth/apple/callback")
//
//		user, _, err := provider.User(r)
//		switch {
//		case errors.Is(err, providers.ErrStateMismatch):
//			// not a sign-in this browser asked for
//		case errors.Is(err, providers.ErrInvalidIDToken):
//			// Apple's answer did not verify
//		case err != nil:
//			// the exchange failed
//		}
//		if !user.Bool("email_verified") || user.Bool("is_private_email") {
//			// do not link by this address
//		}
//		// look up the account linked to user.String("sub")
//	}
//
// The client secret is made for each exchange: a JWT signed with the key (the
// .p8 file) using ES256, naming the team as issuer, the Services ID as subject
// and Apple as audience, valid for five minutes. The id_token is verified with
// [VerifyIDToken] against Apple's published keys, and its nonce is derived from
// the state, so a token is accepted only for the sign-in that asked for it.
// Apple sends the person's name once, with the first authorization and never
// again; [Provider.User] adds it as name, given_name and family_name when it
// came, and ignores the e-mail posted alongside, which only the browser vouches
// for.
//
// # A token a native app already holds
//
// On Android and iOS the platform signs the person in and hands the app an ID
// token, with no redirect, state or code. The app sends it to the server, and
// [VerifyIDToken] is what the server runs on it, for Google and for Apple:
//
//	claims, err := providers.VerifyIDToken(r.Context(), body.IDToken,
//		providers.AppleIssuer(), []string{"com.example.app"},
//		providers.WithNonce(body.Nonce))
//	if errors.Is(err, providers.ErrInvalidIDToken) {
//		// 401
//	}
//	user := claims.UserData()
//
// The audiences are the client ids this server accepts, and the call refuses to
// run without one. The signing keys are fetched from the provider, cached and
// renewed by a [KeySet]; [GoogleIssuer] and [AppleIssuer] share one each for
// the process, because a [Provider] lives for one request and its keys do not.
// The algorithm is RS256 and nothing else, whatever the token claims.
package providers
