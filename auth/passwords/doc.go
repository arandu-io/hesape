// Package passwords is the password reset flow: the token, the store that holds
// a hash of it, and the broker that runs both halves.
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
// tenant that was configured, never the one that arrived in the form. The
// lookups are filtered by it exactly as the writes are -- an unscoped token
// lookup would accept a token minted for another customer's account with the
// same address.
//
// # Three shapes worth knowing before reading the code
//
// A status is not an error. The methods answer with one of the five constants --
// [ResetLinkSent], [PasswordReset], [InvalidUser], [InvalidToken],
// [ResetThrottled] -- which are translation keys shown to whoever filled in the
// form, and separately with an error, which is a store that did not answer or a
// broker that was wired wrong.
//
// The expiry and the throttle are a time.Duration rather than a count of
// seconds, so the unit is written at every call site instead of in a doc
// comment.
//
// [PasswordBroker] takes no event dispatcher, and nothing here announces that a
// reset link went out.
package passwords
