// Package onetime issues, keeps and spends the short code an application mails
// to somebody.
//
// It is the half of a one-time code that has state. The otp package computes a
// code from a secret and an instant and remembers nothing at all; this package
// remembers -- which code is outstanding, for what, for whom, until when, and
// how many times it has been tried. Nothing here computes HOTP or TOTP, and
// nothing there knows what a subject is.
//
// # It is not a second factor
//
// A code that arrives by e-mail proves that somebody reads that mailbox. It
// does not prove that they hold a device, and it travels the same channel a
// password-reset link travels, so whoever controls the mailbox controls both of
// them. That makes it a second proof of one factor rather than a second factor.
//
// Use it to confirm that an address is reachable, to re-confirm a sensitive
// action for somebody already signed in, or to let somebody back in through the
// address they registered. Do not use it as the second step of a sign-in and
// call the result two-factor: the person would be trusting the account more
// because of a mechanism that did not earn it.
//
// # Six digits, and what actually defends them
//
// A code is six decimal digits: a million codes, 19.93 bits, each one exactly
// as likely as any other. Digits and not letters because it arrives in an
// e-mail and is retyped by a person, often on a phone keyboard that a numeric
// field turns into a keypad.
//
// A million guesses is not a lot. What defends the code is therefore not its
// length but [Config.MaxAttempts]: one issued code may be presented that many
// times and then it is dead, whether or not it was ever typed correctly. That
// limit is a security control and not a guard against volume, so it fails
// closed -- a store that cannot be reached refuses the attempt rather than
// letting it past. There is no argument, configuration or store failure on
// which [Codes.Consume] returns nil without the right code.
//
// # One code at a time
//
// Issuing replaces. A second [Codes.Issue] for the same purpose and subject
// overwrites the record, and the record is the only memory there is, so the
// previous code stops being something the store can recognise -- it is refused
// from the instant the new one is written, in flight or not. There is one
// outstanding code per purpose and subject, never two, and [Config.Cooldown] is
// how often that one may be replaced.
//
// # What the store holds
//
// Not the code: a keyed digest of it, under a key that is itself a keyed digest
// of the purpose and the subject. So a dump of the cache yields neither the
// codes nor the addresses they were sent to, and a digest is not worth grinding
// without the application key, which is not in the cache.
//
// The entries are not prefixed by tenant, and that is deliberate rather than an
// omission: a code confirming an address is issued before anybody has signed
// in, so there is no Grant to take a tenant from -- the same reason a rate
// limiter has none. The subject is the whole of the scope. An application whose
// subjects are not already unique across tenants puts the tenant into the
// subject it passes.
//
// # The errors are for branching, not for the screen
//
// [ErrNoCode] and [ErrInvalidCode] are different sentences here so that an
// application can act differently. Showing them as different sentences to
// somebody who has not signed in tells them which addresses have a code waiting
// -- which is to say, which addresses are registered. Answer with one sentence
// and log the difference.
package onetime
