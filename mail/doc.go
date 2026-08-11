// Package mail sends what an application has to say to somebody.
//
// It mirrors Illuminate\Mail, and was written against the clone in
// laravel_illuminate/mail at commit 005b0331 (27/02/2026), which is Laravel's
// 13.x branch. The files it answers to:
//
//	Attachment.php          -> attachment.go
//	MailManager.php         -> manager.go
//	MailServiceProvider.php -> nothing; see "What is deliberately absent"
//	Mailable.php            -> pendingmail.go, assertions.go
//	Mailer.php              -> mailer.go
//	Markdown.php            -> markdown.go
//	Message.php             -> message.go
//	PendingMail.php         -> pendingmail.go
//	SendQueuedMailable.php  -> queued.go
//	SentMessage.php         -> mail.go
//	TextMessage.php         -> textmessage.go
//	Mailables/*.php         -> address.go, attachment.go, content.go,
//	                           envelope.go, headers.go, and the alias package
//	                           github.com/arandu-io/hesape/mail/mailables
//	Events/*.php            -> events.go, and the alias package
//	                           github.com/arandu-io/hesape/mail/events
//	Transport/*.php         -> github.com/arandu-io/hesape/mail/transport
//
// The shape is Laravel's, because a developer coming from there should
// recognise it: a mailable declares an Envelope and a Content, a Mailer sends
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
//		return mail.Content{View: "mail.welcome", With: m}
//	}
//
//	sent, err := mailer.To("you@example.test").Send(ctx, WelcomeEmail{Name: "Ada"})
//
// [SentMessage] is what came back: the provider's identifier for the message,
// which is the only thing that connects a line in the application log to a row
// in the provider's dashboard.
//
// # Where Illuminate's methods went, and why
//
// Three of Illuminate's classes have no one-to-one Go twin, and the reason is
// the same each time: PHP can do something Go cannot.
//
// The Mailable *class* is inheritance. A user class extends it and overrides
// envelope(), content(), attachments() and headers(), and the parent calls back
// into the child. Go has no virtual dispatch through an embedded struct, so
// [Mailable] is the interface the two required methods form -- the role
// Illuminate\Contracts\Mail\Mailable plays -- and the fluent surface the base
// class carries lives on [PendingMail], the value that is doing the sending.
// Every method name is Illuminate's; only the receiver moved. The assertions
// live on [Message], because every one of them renders first and a rendered
// mailable is what a Message is.
//
// The Mailables and Events sub-namespaces reference their parent and are
// referenced by it. Go refuses that cycle, so the types are declared here and
// the two sub-packages are aliases -- the same types under the import path a
// Laravel developer reaches for, not copies of them.
//
// A PHP class may carry a property and a method of the same name, and
// Illuminate's value objects all do: $envelope->tags and $envelope->tags(...).
// A Go struct cannot. Where the two collide the field wins, because a Go struct
// literal is what PHP's named arguments are, and named arguments are the form
// Illuminate's own documentation uses to build an Envelope, a Content or a
// Headers. The five setters with no Go twin are Content::htmlString,
// Envelope::tags, Envelope::using, Headers::messageId and Headers::references;
// each one is a field of that name on the same type.
//
// "Symfony" survives in GetSymfonyMessage, GetSymfonyTransport,
// SetSymfonyTransport, GetSymfonySentMessage, WithSymfonyMessage and
// CreateSymfonyTransport. Symfony is Laravel's MIME and mailer library and
// hesape has none, so each of those answers the equivalent value here. The name
// is kept because ADR 0044 keeps the name: a method a Laravel developer cannot
// find is a method that does not exist for them.
//
// # Renames this package made to its own earlier names
//
// These were hesape's inventions, not Illuminate's, and they were corrected:
//
//	mail.Sent            -> mail.SentMessage      (SentMessage.php)
//	mail.Pending         -> mail.PendingMail      (PendingMail.php)
//	Address.Email        -> Address.Address       (Mailables\Address::$address)
//	Content.Data         -> Content.With          (Mailables\Content::$with)
//	transport.Array.Sent -> ArrayTransport.Messages
//	transport.Array.Reset-> ArrayTransport.Flush
//
// Two of them were changes of meaning and not only of spelling, and they are
// the ones to read twice. Content.HTML used to be a body; in Illuminate it is
// "alternative syntax for view" and it is a view name here too, with the body
// moved to Content.HTMLString. Content.Text used to be a literal body with a
// separate TextView beside it; in Illuminate it is the text view's name, and
// that is what it is now -- a literal text body is [Mailer.Raw], which is where
// Illuminate puts it. A field with the right name and the wrong meaning is
// worse than a missing one, because nobody checks.
//
// A message that names no subject is no longer refused. Illuminate derives one
// from the mailable's class name -- OrderShipped goes out as "Order Shipped" --
// and so does this, which prevents the empty subject the refusal existed to
// prevent without losing the message.
//
// # What is deliberately absent
//
// No facade and no `Mail::` global (ADR 0002), and no service provider: the
// MailServiceProvider register() and provides() methods exist to bind into a
// container, and ADR 0001 has no container. [NewMailManager] takes its
// configuration and its view factory as arguments instead, and MailManager's
// getApplication and setApplication have nothing to answer.
//
// Amazon SES is absent, which is SesTransport::ses, getOptions and setOptions.
// It is the one provider that needs a request-signing implementation, so it is
// the one that would belong in a submodule of its own, and nobody has asked for
// it. Every other provider in this collection is one POST with a JSON body.
//
// # What is still owed
//
// The markdown mail theme -- Illuminate's resources/views/html and
// resources/views/text: layout, header, footer, message, button, panel, subcopy
// and table, in an HTML version and a plain-text one. In Laravel they are Blade
// components under the "mail" view namespace; here they are kyse components
// published into the project by the ui starter kit, one .kyse.go per component,
// under resources/views/mail/html and resources/views/mail/text, plus a theme
// view whose whole output is CSS. [Markdown] already looks them up:
// HTMLComponentPaths and TextComponentPaths are the two lists it resolves
// against, and the theme is the view named mail.themes.<theme>. Nothing in this
// package writes kyse, by RULE 13 and because the components belong to the
// skeleton rather than to the framework (ADR 0021).
//
// True CSS inlining. Illuminate hands the rendered HTML and the theme's
// stylesheet to CssToInlineStyles, which moves each rule onto the element it
// matches, because Outlook drops most of a <style> element and several webmail
// clients drop it entirely. Doing that needs an HTML parser, and the core
// carries no dependency past golang.org/x/crypto, so the default [Inliner] puts
// the stylesheet in a <style> element and an application that needs more
// supplies its own.
//
// Three contracts are declared in this package rather than imported, because
// the packages they belong to are being written in parallel: [FilesystemFactory]
// and [Disk] for hesape/filesystem, and [QueueFactory] and [Queue] for
// hesape/queue. When those land, their own contracts are what these become.
// [Filesystem] is a package-level variable for the same reason Illuminate reads
// the container there, and locale switching -- Illuminate's withLocale around
// every render -- is recorded on [Message.Locale] and applied by whatever
// hesape/translation ends up wiring.
package mail
