// Package socialite is Illuminate\Socialite: signing in with somebody else's
// account.
//
// It holds [UserData], which is what a provider says about the person who just
// signed in, and the oauthtwo subpackage, which is the flow that gets it.
//
// # Where it came from
//
// The clone at laravel_illuminate/socialite is the source of truth for the
// shape: the class names, the method names, the interfaces, and what each call
// does. It is illuminate/socialite as it was in 2013, at commit ff4050a, and it
// is laid out as PSR-0 -- src/Illuminate/Socialite/OAuthTwo. That nesting is
// PHP's autoloader arrangement and carries no meaning here, so
// Illuminate\Socialite is this package and Illuminate\Socialite\OAuthTwo is
// socialite/oauthtwo, one directory down, the way every other component in
// hesape mirrors its namespace.
//
// The endpoints and the newer method names -- Redirect, User, Stateless,
// Scopes, SetScopes, With, RedirectUrl -- come from
// reference_laravel/socialite, which is laravel/socialite as it is now. Each
// place they differ is named where it happens.
//
// # What is not here
//
// The clone has no OAuth 1, no provider manager and no facade, so neither has
// this. The current Socialite's One/ (Twitter's OAuth 1), SocialiteManager,
// Facades/ and Testing/ are outside what the clone covers; the manager and the
// facade would be a container and a facade besides (ADR 0001, ADR 0002).
package socialite
