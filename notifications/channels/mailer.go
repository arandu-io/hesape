package channels

import (
	"context"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/notifications/messages"
)

// MailVia returns a mail channel that sends through the application's
// *mail.Mailer, the one bootstrap/app.go already builds over its transport:
//
//	notifier := notifications.New([]notifications.Channel{
//		channels.NewDatabase(notifications.NewTableStore(db)),
//		channels.MailVia(mailer),
//	})
//
// The message a notification builds is handed to the mailer as a mailable, so
// it goes out the way every other message of the application does: the
// mailer's default sender and reply-to, its global "to" in development, the
// sending and sent events, and the transport's message id as the receipt.
//
// What the message names decides how its body is drawn. A ViewName or a
// MarkdownView is rendered by the mailer, with the message's Data; anything
// else is drawn from the message's own lines by its Render and PlainText, so
// a notification that names no template needs no view registry at all. A
// TextView is rendered by the mailer in either case. The sender, the copies,
// the attachments, the tags, the metadata, the priority, the theme, the
// mailer name, the locale and the header callbacks are carried across.
//
// A nil mailer gives the channel NewMail(nil) gives: one that refuses each
// send with an error naming the notification, rather than one that panics.
func MailVia(m *mail.Mailer) *Mail {
	if m == nil {
		return NewMail(nil)
	}
	return NewMail(mailerSender{mailer: m})
}

// mailerSender is a *mail.Mailer seen through the Mailer this package asks for.
type mailerSender struct {
	mailer *mail.Mailer
}

// Send builds the message into a mailable and sends it to one address,
// answering the id the transport gave it.
func (s mailerSender) Send(ctx context.Context, to string, msg messages.Mail) (string, error) {
	pending := s.mailer.To(to).Locale(msg.Locale)
	if msg.PriorityLevel != 0 {
		pending.Priority(msg.PriorityLevel)
	}
	if msg.ThemeName != "" {
		pending.Theme(msg.ThemeName)
	}
	if msg.MailerName != "" {
		pending.Mailer(msg.MailerName)
	}
	for _, a := range append(append([]messages.Attachment(nil), msg.Attachments...), msg.RawAttachments...) {
		if a.File == "" {
			pending.AttachData(a.Data, a.Name, mail.AttachOptions{Mime: a.MIME})
			continue
		}
		pending.Attach(a.File, mail.AttachOptions{As: a.Name, Mime: a.MIME})
	}

	content := mail.Content{Text: msg.TextView}
	switch {
	case msg.ViewName != "":
		content.View, content.With = msg.ViewName, msg.Data()
	case msg.MarkdownView != "":
		content.Markdown, content.With = msg.MarkdownView, msg.Data()
	default:
		html, err := msg.Render()
		if err != nil {
			return "", err
		}
		content.HTMLString = html
		if msg.TextView != "" {
			content.With = msg.Data()
		}
		// The text part is drawn from the same lines, and a literal body has
		// no field on Content -- a text view is a name, never a body -- so it
		// is set on the built message, where a text view that rendered wins.
		plain := msg.PlainText()
		pending.WithMessage(func(m *mail.Message) {
			if m.Text == "" {
				m.Text = plain
			}
		})
	}

	for _, callback := range msg.Callbacks {
		if callback == nil {
			continue
		}
		pending.WithMessage(func(m *mail.Message) {
			if m.Headers.Text == nil {
				m.Headers.Text = map[string]string{}
			}
			callback(m.Headers.Text)
		})
	}

	sent, err := pending.Send(ctx, notificationMail{envelope: envelopeOf(msg), content: content})
	if err != nil {
		return "", err
	}
	return sent.ID, nil
}

// envelopeOf is who the message is from and to besides the routed address,
// and what it says it is.
func envelopeOf(msg messages.Mail) mail.Envelope {
	return mail.Envelope{
		Subject:  msg.SubjectLine,
		From:     mail.Address(msg.FromAddress),
		CC:       addressesOf(msg.CCAddresses),
		BCC:      addressesOf(msg.BCCAddresses),
		ReplyTo:  addressesOf(msg.ReplyToAddresses),
		Tags:     append([]string(nil), msg.Tags...),
		Metadata: msg.MetadataHeaders,
	}
}

func addressesOf(list []messages.Address) []mail.Address {
	if len(list) == 0 {
		return nil
	}
	out := make([]mail.Address, len(list))
	for i, a := range list {
		out[i] = mail.Address(a)
	}
	return out
}

// notificationMail is a notification's message as the mailable the mailer
// sends.
type notificationMail struct {
	envelope mail.Envelope
	content  mail.Content
}

// Envelope is the message's addressing and subject.
func (n notificationMail) Envelope() mail.Envelope { return n.envelope }

// Content is how the body is drawn.
func (n notificationMail) Content() mail.Content { return n.content }
