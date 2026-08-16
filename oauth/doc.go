// Package oauth signs somebody in with an account they already have.
//
// It holds [UserData] -- what a provider says about the person who just signed
// in -- and the providers subpackage, which is the flow that gets it: Google,
// GitHub, Facebook and Twitter today.
//
// # What it is not
//
// It is not an OAuth 2 authorization SERVER. This package is the client half:
// it sends somebody to a provider and reads back who they are. Issuing tokens
// for other applications to consume is a separate thing and is not here.
//
// It is also not golang.org/x/oauth2. That library is the token exchange and
// nothing above it. What this adds is the part every application writes by hand
// otherwise: the state parameter and its verification, the redirect, the
// provider endpoints, and one shape for the user across providers that each
// describe a person differently.
//
// # The state parameter is not optional
//
// A provider calls back with a code, and without a state parameter that the
// application issued and verified, anybody can trigger that callback with a
// code of their own -- which signs the victim into the attacker's account, or
// the reverse. See [github.com/arandu-io/hesape/oauth/providers.Verify].
//
// A provider built without a state store refuses to redirect rather than
// redirecting insecurely. Stateless exists for the case where the caller
// genuinely has nowhere to keep it, and it is a decision made in the open at
// the call site rather than a default.
package oauth
