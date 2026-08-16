package providers

// The four providers this package carries, as four constructors.
//
// What separates them is data -- three URLs, a scope delimiter, a list of
// default scopes, a header -- so it is data, and there is one implementation of
// the flow rather than one per provider.
//
// The endpoints are the current published ones. Facebook's Graph API takes a
// version in the path, which is why [NewFacebookProvider] pins one, and Google
// serves its token endpoint from googleapis.com.

// NewFacebookProvider builds a [Provider] wired to Facebook Login: the login
// dialog, the Graph token endpoint and the Graph /me endpoint, all pinned to
// one API version, with scopes separated by commas and "email" asked for by
// default. Graph answers with an id and nothing else unless the fields are
// named, so the user data request carries the field list that fills the
// profile. The caller adds its callback URL and drives the flow from there.
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

// NewGithubProvider builds the GitHub provider. The state check is on for it,
// as it is for every provider here.
func NewGithubProvider(state StateStoreInterface, clientID, secret string) *Provider {
	p := NewProvider(state, clientID, secret,
		"https://github.com/login/oauth/authorize",
		"https://github.com/login/oauth/access_token",
		"https://api.github.com/user",
	)
	p.scopeDelimiter = ","
	p.defaultScope = []string{"user:email"}
	// GitHub's own documentation spells the scheme "token".
	p.userDataAuthScheme = "token"
	p.userDataAccept = "application/vnd.github.v3+json"
	return p
}

// NewGoogleProvider builds a [Provider] wired to Sign in with Google: the
// accounts.google.com authorization endpoint, the googleapis.com token and
// userinfo endpoints, scopes separated by spaces, and openid, profile and email
// asked for by default. The caller adds its callback URL and drives the flow
// from there.
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

// NewStripeProvider builds the Stripe Connect provider.
//
// Stripe Connect has no user data endpoint, so [Provider.User] and
// [Provider.GetUserData] fail on it, saying so. What comes back from the
// exchange is the connected account's id, in the token.
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
