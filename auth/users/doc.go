// Package users holds the user providers that read accounts out of a database.
//
// A user provider is the seam between a guard and wherever the users are kept.
// A guard knows how a session or a token proves identity; a provider knows how
// to find the row and how to check the password. Both providers here implement
// auth.UserProvider, which is declared in the parent package with its other
// contracts.
//
// # Authorization, which is the question a reviewer asks first
//
// A provider runs at sign-in, where there is no subject yet -- establishing one
// is what is about to happen -- so there is no Policy to run and nothing to
// inherit a Grant from. Every statement in this package is therefore taken under
// auth.SystemGrant, under [RetrieveUser] or [UpdateUser].
//
// The tenant that grant carries is the provider's, from configuration, fixed
// when the application was wired. It is never read off the request: not from a
// header, not from a subdomain, not from the credentials map. A provider whose
// tenant came in with the request would let an anonymous caller choose whose
// users the sign-in form searches.
//
// Reads are authorized and scoped exactly as writes are. Every statement below
// -- RetrieveByID, RetrieveByToken and RetrieveByCredentials as much as the two
// updates -- goes through the same Grant and the same tenant filter.
//
// # Two shapes worth naming
//
//   - The model is a constructor function, func() auth.Authenticatable, rather
//     than a type named in a string: Go cannot construct a type from a name at
//     run time. The query that model would have opened comes in beside it, for
//     the same reason: an interface value has no query of its own.
//   - A row becomes a user through [Hydratable], which is where the columns are
//     written onto the value the constructor made.
package users
