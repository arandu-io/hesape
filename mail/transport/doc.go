// Package transport is the delivery half of mail: the seven ways a rendered
// message leaves the process.
//
// It mirrors Illuminate\Mail\Transport. The files it answers to, in the clone
// at laravel_illuminate/mail/Transport:
//
//	ArrayTransport.php
//	LogTransport.php
//	ResendTransport.php
//	SesTransport.php
//	SesV2Transport.php
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
// [SMTP], [Log], [Array], [Resend], [SendGrid], [Postmark] and [Failover] read
// as transport.Log and transport.Array at every call site. Two of the seven
// have an Illuminate class behind them -- ArrayTransport and LogTransport --
// and their *methods* carry Illuminate's names exactly: [Array.Messages] and
// [Array.Flush] were called Sent and Reset here until 11/08/2026, which are
// names Illuminate does not have, and [Log.Logger] was missing. ADR 0044 is
// about the name somebody types, and it is the method they type.
//
// # Registration, not a switch
//
// Illuminate's MailManager::createSymfonyTransport is a switch that constructs
// these classes directly. Go cannot do that here: this package imports mail, so
// mail cannot import this one. [Register] inverts it, and the inversion is worth
// keeping -- a transport somebody else writes arrives through the same door as
// these seven, so there is one way to add one (RULE 9).
//
//	manager := mail.NewMailManager(config, views, events)
//	transport.Register(manager)
//
// # Why these seven
//
// [SMTP] is the one every provider speaks, and it needs no dependency: net/smtp
// is in the standard library. Whatever somebody buys, it accepts SMTP, so an
// application is never blocked waiting for an adapter to exist.
//
// [Log] is what development uses. Laravel ships the same thing, and for the same
// reason: an example that needs a mail server is an example nobody runs.
//
// [Array] is what tests use. It keeps what was sent so a test can read it, which
// is the difference between proving an e-mail was sent and proving nothing.
//
// [Resend], [SendGrid] and [Postmark] are here, rather than in a submodule,
// because none of them needs a dependency: each is one POST with a JSON body,
// which net/http already does. RULE 11 sends an adapter to a submodule when it
// drags an SDK in behind it, and these three drag nothing. Writing them against
// the vendor SDK would have bought nothing and cost a dependency tree in
// go.sum -- resend-go alone brings its own transitive set -- for a request that
// fits on a screen. See ADR 0031.
//
// Amazon SES is deliberately absent, and that is what SesTransport.php and
// SesV2Transport.php above answer to: it is the one provider that does need a
// signing implementation, so it is the one that would belong in a submodule of
// its own, and nobody has asked for it.
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
