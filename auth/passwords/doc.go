// Package passwords mirrors Illuminate\Auth\Passwords.
//
// The files it answers to, in the clone at laravel_illuminate/Auth/Passwords:
//
//	CacheTokenRepository.php
//	CanResetPassword.php
//	DatabaseTokenRepository.php
//	PasswordBroker.php
//	PasswordBrokerManager.php
//	PasswordResetServiceProvider.php
//	TokenRepositoryInterface.php
//
// A password reset is two requests with a secret between them. The first finds
// an account, mints a token, stores a hash of it and mails the token; the second
// offers the token back, and if it matches an unexpired record the new password
// is written and the token destroyed. [PasswordBroker] is both halves;
// [TokenRepository] is the middle.
//
// # The four things this package is careful about
//
// The token is never stored. What is stored is a hash of it, so a copy of the
// table -- or of the cache -- is a list of addresses that asked, not a set of
// working links. The plain token exists once, in the return of Create.
//
// The answer takes the same time whether or not the account exists. Both public
// methods of the broker run inside support.Timebox, because a form that answers
// faster for an unknown address is a way to enumerate the accounts of an
// application from outside it. See [PasswordBroker].
//
// A token is single use and short lived. Reset deletes it after the callback
// succeeds -- after, so that a store that failed leaves the person holding a link
// that still works -- and Exists refuses a record older than the expiry whatever
// it holds.
//
// Every statement is authorized and scoped by tenant. A reset runs for somebody
// who cannot sign in, so there is no subject and no Policy to ask: the
// repositories take auth.SystemGrant under [ReadToken] or [WriteToken], with the
// tenant that was configured, never the one that arrived in the form (RULE 14).
// The lookups are filtered by it exactly as the writes are (RULE 17) -- an
// unscoped token lookup would accept a token minted for another customer's
// account with the same address.
//
// # What is not here, and what it answers to
//
//   - PasswordResetServiceProvider.php, whole. It registers auth.password and
//     auth.password.broker in the container, and there is no container (ADR 0002).
//     Reason (2).
//   - PasswordBrokerManager::resolve, ::createTokenRepository and ::getConfig.
//     They read auth.passwords.{name} out of the config and new up a repository
//     from the driver key. The wiring builds the brokers here and hands them to
//     the manager already made. Reason (2).
//   - PasswordBrokerManager::__call, which forwards every unknown method to the
//     default broker. It is PHP's magic dispatch, which Go has no counterpart for
//     and needs none: [PasswordBrokerManager.Broker] answers with the broker and
//     the method is called on it. Reason (1).
//   - PasswordBroker::$events and the PasswordResetLinkSent it dispatches. The
//     event class is Illuminate\Auth\Events', which is hesape/auth/events -- a
//     package of its own that this one does not own, and declaring the event here
//     would be the second declaration of it (RULE 9). The broker gains the
//     dispatcher when that package lands; nothing else in sendResetLink depends
//     on it.
//   - CacheTokenRepository::$format. It exists to flatten a Carbon into a string
//     for the cache and to parse it back; the cache codec here is JSON, which
//     writes a time.Time and reads a time.Time. Reason (1).
//
// # Two shapes worth knowing before reading the code
//
// A status is not an error. The methods answer with one of the five constants --
// [ResetLinkSent], [PasswordReset], [InvalidUser], [InvalidToken],
// [ResetThrottled] -- which are translation keys shown to whoever filled in the
// form, and separately with an error, which is a store that did not answer or a
// broker that was wired wrong. The PHP has one return value and uses exceptions
// for the second kind.
//
// $expires and $throttle are time.Duration and not a count of seconds. The unit
// is then written at every call site instead of in the constructor's doc block.
package passwords
