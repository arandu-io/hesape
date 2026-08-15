package providers

// The four providers the clone carries, as four constructors.
//
// Illuminate makes each one a subclass that overrides getAuthEndpoint(),
// getAccessEndpoint(), getUserDataEndpoint() and getDefaultScope(), and two of
// them override executeAccessRequest() and parseAccessResponse() as well. Every
// one of those overrides is data -- three URLs, a delimiter, a list of scopes,
// a header -- so here they are data, and there is one implementation of the
// flow instead of five.
//
// The endpoints are the current ones, from
// reference_laravel/socialite/src/Two. The clone's are thirteen years old and
// three of the four no longer answer: Facebook's Graph API requires a version
// in the path, Google moved its token endpoint to googleapis.com, and the
// userinfo endpoint the clone names was retired with OAuth 1. What came from
// the clone is the shape -- the class names, the method names, the scope
// delimiter of each provider, and the default scopes where they still exist.

// NewFacebookProvider builds a [Provider] wired to Facebook Login: the login
// dialog, the Graph token endpoint and the Graph /me endpoint, all pinned to
// one API version, with scopes separated by commas and "email" asked for by
// default. Graph answers with an id and nothing else unless the fields are
// named, so the user data request carries the field list that fills the
// profile. The caller adds its callback URL and drives the flow from there.
//
// Answers Illuminate\Socialite\OAuthTwo\FacebookProvider.
func NewFacebookProvider(state StateStoreInterface, clientID, secret string) *Provider {
	const version = "v23.0"
	p := NewProvider(state, clientID, secret,
		"https://www.facebook.com/"+version+"/dialog/oauth",
		"https://graph.facebook.com/"+version+"/oauth/access_token",
		"https://graph.facebook.com/"+version+"/me",
	)
	p.scopeDelimiter = ","
	p.defaultScope = []string{"email"}
	// Facebook returns an id and nothing else unless the fields are asked for
	// by name.
	p.userDataQuery = map[string]string{
		"fields": "name,email,gender,verified,link,picture.width(1920)",
	}
	return p
}

// NewGithubProvider answers Illuminate\Socialite\OAuthTwo\GithubProvider.
//
// Illuminate's overrides stateMismatch() to return false, which turns the state
// check off for GitHub alone. That override is not here; the package comment
// says why.
func NewGithubProvider(state StateStoreInterface, clientID, secret string) *Provider {
	p := NewProvider(state, clientID, secret,
		"https://github.com/login/oauth/authorize",
		"https://github.com/login/oauth/access_token",
		"https://api.github.com/user",
	)
	p.scopeDelimiter = ","
	p.defaultScope = []string{"user:email"}
	// GitHub's own documentation spells the scheme "token", and the current
	// Socialite sends it that way.
	p.userDataAuthScheme = "token"
	p.userDataAccept = "application/vnd.github.v3+json"
	return p
}

// NewGoogleProvider builds a [Provider] wired to Sign in with Google: the
// accounts.google.com authorization endpoint, the googleapis.com token and
// userinfo endpoints, scopes separated by spaces, and openid, profile and email
// asked for by default. The caller adds its callback URL and drives the flow
// from there.
//
// Answers Illuminate\Socialite\OAuthTwo\GoogleProvider.
func NewGoogleProvider(state StateStoreInterface, clientID, secret string) *Provider {
	p := NewProvider(state, clientID, secret,
		"https://accounts.google.com/o/oauth2/auth",
		"https://www.googleapis.com/oauth2/v4/token",
		"https://www.googleapis.com/oauth2/v3/userinfo",
	)
	// Google is the one provider that separates scopes with a space, which is
	// what the specification says and what the other three ignore.
	p.scopeDelimiter = " "
	p.defaultScope = []string{"openid", "profile", "email"}
	return p
}

// NewStripeProvider answers Illuminate\Socialite\OAuthTwo\StripeProvider.
//
// Stripe Connect has no user data endpoint -- Illuminate's
// getUserDataEndpoint() returns an empty string -- so [Provider.User]
// and [Provider.GetUserData] fail on it, saying so. What comes back
// from the exchange is the connected account's id, in the token.
func NewStripeProvider(state StateStoreInterface, clientID, secret string) *Provider {
	p := NewProvider(state, clientID, secret,
		"https://connect.stripe.com/oauth/authorize",
		"https://connect.stripe.com/oauth/token",
		"",
	)
	p.scopeDelimiter = ","
	p.defaultScope = []string{"read_write"}
	// Stripe authenticates the token request with the secret key in a header
	// rather than a form field, which is the override its subclass exists for.
	p.accessHeaders = map[string]string{"Authorization": "Bearer " + secret}
	return p
}
