// Package mail sends what an application has to say to somebody.
//
// It mirrors Illuminate\Mail. The files it answers to, in the clone at
// laravel_illuminate/mail:
//
//	Attachment.php
//	MailManager.php
//	MailServiceProvider.php
//	Mailable.php
//	Mailer.php
//	Markdown.php
//	Message.php
//	PendingMail.php
//	SendQueuedMailable.php
//	SentMessage.php
//	TextMessage.php
//
// Mailables\Address, Mailables\Content and Mailables\Envelope are here as well,
// and not in a package of their own: in PHP they are three files because a file
// is a class, and in Go they are three types in the package that is the only
// thing that reads them.
//
// The shape is Laravel's, because a developer coming from there should
// recognise it: a Mailable declares an Envelope and a Content, a Mailer sends
// it, and the transport behind the Mailer is configuration rather than a
// decision the calling code makes.
//
//	type WelcomeEmail struct{ Name, Link string }
//
//	func (m WelcomeEmail) Envelope() mail.Envelope {
//		return mail.Envelope{Subject: "Welcome"}
//	}
//
//	func (m WelcomeEmail) Content() mail.Content {
//		return mail.Content{View: "mail.welcome", Data: m}
//	}
//
//	sent, err := mailer.To("you@example.com").Send(ctx, WelcomeEmail{Name: "Ada"})
//
// [Sent] is what came back: the provider's identifier for the message, which is
// the only thing that connects a line in the application log to a row in the
// provider's dashboard.
//
// The transports live in [github.com/arandu-io/hesape/mail/transport], one
// import below this one, because a transport is written against Message and
// Message alone -- and because the split is what stops the contract from
// growing a field every time a provider does.
//
// # What is deliberately absent
//
// No facade, no `Mail::` global, and no queue by default. Laravel queues a
// mailable when it implements ShouldQueue, which is an interface the class
// opts into at a distance; here sending on the queue is queue.Dispatch with a
// job that sends, and the difference is visible at the call site.
//
// No markdown mailables. Laravel has them because Blade cannot easily produce
// both an HTML and a text part; kyse produces whatever the view produces, and a
// second templating language for e-mail is the second way to draw a page that
// RULE 9 refuses.
package mail
