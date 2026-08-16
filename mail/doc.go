// Package mail sends what an application has to say to somebody.
//
// A mailable declares an [Envelope] and a [Content], a [Mailer] sends it, and
// the transport behind the Mailer is configuration rather than a decision the
// calling code makes.
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
// # Where the methods live
//
// [Mailable] is an interface of two methods, and a mailable is the caller's own
// struct. There is nowhere to hang To, CC, Subject, Attach or Tag on somebody
// else's type, so the fluent surface lives on [PendingMail], which is the value
// that is doing the sending. The assertions live on [Message], because every
// one of them reads the mail as it will go out and a rendered mailable is what
// a Message is.
//
// [Envelope], [Content] and [Headers] carry fields where a setter would collide
// with one, because a Go struct cannot declare a method and a field of the same
// name. The field wins, since a struct literal is how these values are built,
// and each of those setters has a twin on [PendingMail].
//
// The two sub-packages are aliases rather than declarations. Each would have to
// import this package for the types it names, and this package would have to
// import them for the values it builds -- a cycle the compiler refuses. So the
// types are declared here, and
// github.com/arandu-io/hesape/mail/mailables and
// github.com/arandu-io/hesape/mail/events name them again under import paths of
// their own. An alias is not a second type: a mailable written against either
// name interoperates with one written against the other.
//
// A message that names no subject is not refused. The subject falls back to the
// mailable's type name, so an OrderShipped goes out as "Order Shipped", which
// prevents the empty subject without losing the message.
//
// # Testing
//
// A test hands the mailer the transport that records, the way every other
// collaborator is handed over:
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
// What it holds is [Message] values, checked against the mail as it would have
// gone out. Nothing is installed and nothing is shared: the transport is a
// local variable, and two tests calling t.Parallel each hold their own.
//
// # The markdown theme
//
// A markdown mailable is drawn from one template into both parts by [Markdown],
// which resolves its components against [Markdown.HTMLComponentPaths] and
// [Markdown.TextComponentPaths] and takes its stylesheet from the view named
// mail.themes.<theme>. Those views belong to the application: nothing in this
// package writes them, and the default [Inliner] leaves the stylesheet in a
// <style> element rather than moving each rule onto the element it matches.
//
// # What is declared here rather than imported
//
// [FilesystemFactory] and [Disk] are the two questions an attachment asks of a
// stored file -- its bytes and its content type. [QueueFactory] and [Queue] are
// the two calls a background send makes. Both pairs are declared in this
// package so that a module which only sends mail does not pull a storage or a
// queue registry in behind it; anything an application wires in has to satisfy
// them and nothing more. [Filesystem] is the package-level variable the
// attachment constructors read, set once at boot.
//
// Locale switching is recorded rather than performed. [Message.Locale] carries
// the locale a message was rendered in, and the translator that acts on it is
// wired by the application.
package mail
