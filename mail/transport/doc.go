// Package transport is the delivery half of mail: the eight ways a rendered
// message leaves the process.
//
// A transport is written against [github.com/arandu-io/hesape/mail.Message] and
// nothing else. By the time one is called the addressing, the rendering and the
// validation have all happened, so what is left is a connection and a body --
// which is why writing a new one is small, and why they are a package of their
// own rather than a growing file next to the Mailer.
//
// # The names
//
// The type names drop the Transport suffix, because the package supplies it:
// [SMTP], [Log], [Array], [Resend], [SendGrid], [Postmark], [Meeng] and [Failover] read
// as transport.Log and transport.Array at every call site.
//
// # Registration, not a switch
//
// This package imports mail, so mail cannot import this one and cannot
// construct these types by name. [Register] inverts it, and the inversion is
// worth keeping -- a transport somebody else writes arrives through the same
// door as these eight, so there is one way to add one.
//
//	manager := mail.NewMailManager(config, views, events)
//	transport.Register(manager)
//
// # Why these eight
//
// [SMTP] is the one every provider speaks, and it needs no dependency: net/smtp
// is in the standard library. Whatever somebody buys, it accepts SMTP, so an
// application is never blocked waiting for an adapter to exist.
//
// [Log] is what development uses: an example that needs a mail server is an
// example nobody runs.
//
// [Array] is what tests use. It keeps what was sent so a test can read it, which
// is the difference between proving an e-mail was sent and proving nothing.
//
// [Resend], [SendGrid], [Postmark] and [Meeng] are here, rather than in a module of
// their own, because none of them needs a dependency: each is one POST with a
// JSON body, which net/http already does. Writing them against a vendor SDK
// would have bought nothing and cost a dependency tree in go.sum for a request
// that fits on a screen.
//
// [Meeng] is the HYZIS mail platform's own API, and the way an Arandu
// application sends through it: a Bearer key and one POST to /api/v1/emails.
// SMTP to the platform's relay works too, through [SMTP], and is the second way
// in, for the case where an API call cannot be made. The platform keeps no
// files, so a message with an attachment is refused before it is sent.
//
// Amazon SES is deliberately absent. It is the one provider that needs a
// request-signing implementation, so it is the one that would belong in a
// module of its own, and nobody has asked for it.
//
// [Failover] is not a provider. It is the composition: a list of transports
// tried in order, so that a provider having an afternoon does not become an
// application that cannot send a password reset.
//
// # Failure has two kinds
//
// A 429 or a 5xx is a provider that will work in a minute; a 422 is a message
// that will never be accepted. The provider transports separate the two by
// wrapping the first in mail.ErrRetryable, so a job that sends can reschedule
// instead of burning its attempts, and a bad address fails once instead of
// forever.
package transport
