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
// Headers. Every one of those setters is listed under reason 1 below, and each
// is a field of that name on the same type.
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
// # The methods with no answer here
//
// Every one of them, with the numbered reason from ADR 0056: (1) a PHP language
// feature Go does not have, (2) a method that only serves the container, a
// facade or a service provider, (3) a driver this ecosystem does not carry.
//
// Reason 1, a property and a method sharing a name. Each of these is a field of
// that name on the same type, and each has a twin on [PendingMail], which is
// what a Laravel developer is holding when they call the setter:
//
//   - Mailables\Envelope::from, ::to, ::cc, ::bcc, ::replyTo, ::subject,
//     ::tags, ::metadata and ::using are [Envelope].From, .To, .CC, .BCC,
//     .ReplyTo, .Subject, .Tags, .Metadata and .Using. Envelope::tag has no
//     field to collide with and is [Envelope.Tag].
//   - Mailables\Content::view, ::html, ::text, ::markdown, ::htmlString and
//     ::with are [Content].View, .HTML, .Text, .Markdown, .HTMLString and .With.
//   - Mailables\Headers::messageId, ::references and ::text are [Headers].
//     MessageID, .References and .Text. Headers::referencesString has no field
//     to collide with and is [Headers.ReferencesString].
//   - Message::from, ::to, ::cc, ::bcc, ::replyTo and ::subject are the fields
//     [Message] promotes from its embedded [Envelope]. Every other Message
//     method is here under Illuminate's name.
//
// Reason 1, the rest of the PHP language:
//
//   - Mailable::__call, Message::__call, TextMessage::__call,
//     SentMessage::__call and MailManager::__call forward an unknown method
//     somewhere else. That is the magic-method family.
//   - SendQueuedMailable::__clone is PHP's clone hook, which deep-copies the
//     wrapped mailable. A Go value is copied by assignment.
//   - SentMessage::__serialize, ::__unserialize, MessageSent::__serialize and
//     ::__unserialize rebuild an object graph after a queue round trip.
//     encoding/json is what does that here.
//   - The Mailable *class* is inheritance: a user class extends it and the
//     parent calls back into the child. Go has no virtual dispatch through an
//     embedded struct, so [Mailable] is the interface its two required methods
//     form, and the fluent surface lives on [PendingMail].
//   - The traits -- Queueable, SerializesModels, InteractsWithQueue,
//     Localizable, Macroable, Conditionable, ForwardsCalls -- are PHP trait
//     composition, which is horizontal reuse for a language with single
//     inheritance.
//
// Reason 2, the container and the service provider:
//
//   - MailServiceProvider::register, MailServiceProvider::registerIlluminateMailer
//     and MailServiceProvider::registerMarkdownRenderer bind into a container,
//     and ADR 0001 has no container. [NewMailManager] takes its configuration
//     and its view factory as arguments instead.
//   - MailServiceProvider::provides lists the three bindings -- 'mail.manager',
//     'mailer' and Markdown -- that registration is deferred for. Nothing is
//     deferred when nothing is resolved by name: the application builds the
//     manager it wants and holds it.
//   - MailManager::getApplication and ::setApplication are the application
//     itself.
//   - There is no facade and no `Mail::` global (ADR 0002), which is what
//     Mail::fake needs to exist: it is Facade::swap putting a MailFake where
//     the mail manager was bound, so that every send in the application lands
//     in the fake. With no binding there is nothing to swap, and a
//     package-level mailer a test could swap would be shared mutable state
//     that two tests calling t.Parallel would fight over (ADR 0045).
//
// What a test writes in place of Mail::fake is the transport that records,
// handed to the mailer the way every other collaborator is:
//
//	sent := &transport.Array{}
//	mailer := mail.New("array", views, sent, nil)
//
//	shipOrder(ctx, g, mailer)
//
//	got := sent.Messages()
//	if len(got) != 1 {
//		t.Fatalf("sent %d messages, want 1", len(got))
//	}
//	got[0].AssertHasTo(t, "you@example.test").
//		AssertSeeInHTML(t, "Your order has shipped")
//
// transport.Array is Illuminate's own ArrayTransport, so the name is one a
// Laravel developer already has, and what it holds is [Message] values --
// which is why the assertions MailFake carries are the ones on Message here,
// checked against the mail as it would have gone out. Nothing is installed and
// nothing is shared: the transport is a local variable, and two tests calling
// t.Parallel each hold their own.
//
// Reason 3, drivers this ecosystem does not carry:
//
//   - SesTransport::ses, ::getOptions, ::setOptions and the whole of
//     SesV2Transport, plus MailManager::createSesTransport,
//     ::createSesV2Transport and ::addSesCredentials. Amazon SES is the one
//     provider that needs a request-signing implementation, so it is the one
//     that would belong in a submodule of its own, and nobody has asked for it.
//     Every other provider in this collection is one POST with a JSON body.
//   - MailManager::createMailTransport is PHP's mail() function.
//   - MailManager::createSendmailTransport shells out to a local sendmail
//     binary.
//
// The protected members answer to nothing on purpose. Mailable::buildView,
// ::buildMarkdownView, ::additionalMessageData, ::buildMarkdownHtml,
// ::buildMarkdownText, ::markdownRenderer, ::buildFrom, ::buildRecipients,
// ::buildSubject, ::buildAttachments, ::buildDiskAttachments, ::buildTags,
// ::buildMetadata, ::runCallbacks, ::setAddress, ::addressesToArray,
// ::normalizeRecipient, ::hasRecipient, ::renderForAssertions and
// ::ensureHeadersAreHydrated and its three siblings; Mailer::sendMailable,
// ::parseView, ::addContent, ::renderView, ::setGlobalToAndRemoveCcAndBcc,
// ::createMessage, ::sendSymfonyMessage, ::shouldSendMessage,
// ::dispatchSentEvent, ::replaceEmbeddedAttachments; Message::addAddresses,
// ::addAddressDebugHeader; PendingMail::fill; Envelope::normalizeAddresses,
// ::hasRecipient; Markdown::componentPaths; MailManager::get, ::resolve,
// ::configureSmtpTransport, ::getHttpClient, ::setGlobalAddress, ::getConfig
// and the createXTransport family; LogTransport::decodeQuotedPrintableContent.
// Each is the body of an exported method above it, and each is unexported here
// for the reason it is protected there -- except MailManager::getConfig, which
// is [MailManager.ConfigFor], because a caller with no container has nowhere
// else to read a mailer's configuration from.
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
