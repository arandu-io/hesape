// Package users mirrors the user providers of Illuminate\Auth.
//
// The files it answers to, in the clone at laravel_illuminate/Auth:
//
//	EloquentUserProvider.php
//	DatabaseUserProvider.php
//	GenericUser.php
//
// A user provider is the seam between a guard and wherever the users are kept.
// A guard knows how a session or a token proves identity; a provider knows how
// to find the row and how to check the password. Both providers here implement
// auth.UserProvider, which is Illuminate\Contracts\Auth\UserProvider, declared in
// the parent package with its other contracts (ADR 0045).
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
// header, not from a subdomain, not from the credentials map (RULE 14). A
// provider whose tenant came in with the request would let an anonymous caller
// choose whose users the sign-in form searches.
//
// Reads are authorized and scoped exactly as writes are (RULE 17). Every
// statement below -- retrieveById, retrieveByToken, retrieveByCredentials as
// much as the two updates -- goes through the same Grant and the same tenant
// filter.
//
// # What each Illuminate method became
//
// The names are the PHP's, with the four mechanical changes of ADR 0044 and the
// context.Context of auth's contracts. Two shapes are worth naming here:
//
//   - The Eloquent model, a class-string in PHP, is a constructor function --
//     func() auth.Authenticatable. Go cannot construct a type named at run time,
//     which is reason (1). The query that model would have opened comes in
//     beside it, for the same reason: an interface value has no newQuery().
//   - A row becomes a user through [Hydratable], which is Eloquent's
//     setRawAttributes. In PHP the step does not exist, because the query builder
//     answers with models already.
//
// # What is not here, and what it answers to
//
//   - GenericUser::__get, __set, __isset and __unset. PHP declares them because
//     a property named at run time is not otherwise reachable; the columns are an
//     exported map here and indexing it is the same operation. Reason (1).
//   - EloquentUserProvider::newModelQuery's optional $model parameter. It exists
//     so retrieveById can open the query on the instance it already made; the
//     query comes from a factory here, not from the instance, so the parameter
//     has nothing to carry.
//   - CreatesUserProviders.php. It is the registry of provider drivers, which is
//     the AuthManager's and lives with it in hesape/auth. Nothing in it belongs
//     to a provider.
package users
