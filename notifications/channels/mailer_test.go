package channels_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/channels"
	"github.com/arandu-io/hesape/notifications/messages"
)

// sentBox is a transport that keeps what it was handed.
type sentBox struct{ sent []mail.Message }

func (b *sentBox) Name() string { return "box" }

func (b *sentBox) Send(_ context.Context, m mail.Message) (mail.SentMessage, error) {
	b.sent = append(b.sent, m)
	return mail.SentMessage{ID: "box-1", Transport: "box"}, nil
}

// views renders a named view as its name and the data it was given.
type views struct {
	names []string
	data  []any
}

func (v *views) RenderToString(name string, data any) (string, error) {
	v.names = append(v.names, name)
	v.data = append(v.data, data)
	return "<rendered " + name + ">", nil
}

func newMailer(v mail.Renderer) (*mail.Mailer, *sentBox) {
	box := &sentBox{}
	m := mail.New("default", v, box, nil)
	m.AlwaysFrom("app@example.com", "The App")
	return m, box
}

// withMail is a notification whose message the test writes.
type withMail struct {
	full
	message messages.Mail
}

func (n withMail) ToMail(notifications.Notifiable) messages.Mail { return n.message }

// TestMailViaSendsThroughTheApplicationMailer: the channel built over the
// application's mailer delivers the message the notification built, drawn from
// its own lines, to the routed address, from the mailer's sender, in the
// recipient's language, and answers the transport's id.
func TestMailViaSendsThroughTheApplicationMailer(t *testing.T) {
	mailer, box := newMailer(nil)
	ada := person{id: "u1", email: "ada@example.com", locale: "pt-BR"}

	receipt, err := channels.MailVia(mailer).Send(context.Background(), sendGrant(t), ada, withMail{message: messages.NewMail().
		Subject("Your invoice is paid").
		Greeting("Hello Ada").
		Line("Invoice <2026-114> is paid.").
		Action("View invoice", "https://example.com/invoices/2026-114")})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt != "box-1" {
		t.Errorf("receipt = %q, want the transport's id", receipt)
	}
	if len(box.sent) != 1 {
		t.Fatalf("the transport was handed %d messages, want 1", len(box.sent))
	}
	got := box.sent[0]
	if len(got.To) != 1 || got.To[0].Address != "ada@example.com" {
		t.Errorf("to = %+v, want the routed address", got.To)
	}
	if got.From.Address != "app@example.com" {
		t.Errorf("from = %+v, want the mailer's sender", got.From)
	}
	if got.Subject != "Your invoice is paid" || got.Locale != "pt-BR" {
		t.Errorf("subject %q, locale %q", got.Subject, got.Locale)
	}
	if !strings.Contains(got.HTML, "Invoice &lt;2026-114&gt; is paid.") || !strings.Contains(got.HTML, "https://example.com/invoices/2026-114") {
		t.Errorf("the HTML part is not the message's lines, escaped: %q", got.HTML)
	}
	if !strings.Contains(got.Text, "Invoice <2026-114> is paid.") || !strings.Contains(got.Text, "View invoice: https://example.com/invoices/2026-114") {
		t.Errorf("the text part is not the message's lines: %q", got.Text)
	}
}

// TestMailViaCarriesTheEnvelope: everything a notification's message says
// about who else receives it and how it travels reaches the transport.
func TestMailViaCarriesTheEnvelope(t *testing.T) {
	mailer, box := newMailer(nil)
	message := messages.NewMail().
		Subject("Quarterly report").
		Line("Attached.").
		From("reports@example.com", "Reports").
		CC("boss@example.com", "Boss").
		BCC("archive@example.com", "").
		ReplyTo("help@example.com", "Help").
		Tag("report").
		Metadata("quarter", "q3").
		Priority(1).
		AttachData([]byte("a,b\n"), "report.csv", messages.Attachment{MIME: "text/csv"}).
		WithMessage(func(headers map[string]string) { headers["X-Report"] = "q3" })

	_, err := channels.MailVia(mailer).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com"}, withMail{message: message})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := box.sent[0]
	if got.From.Address != "reports@example.com" || got.From.Name != "Reports" {
		t.Errorf("from = %+v, want the message's own sender", got.From)
	}
	if !got.HasCC("boss@example.com", "Boss") || !got.HasBCC("archive@example.com") || !got.HasReplyTo("help@example.com", "Help") {
		t.Errorf("copies and reply-to lost: cc %+v bcc %+v reply-to %+v", got.CC, got.BCC, got.ReplyTo)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "report" || got.Metadata["quarter"] != "q3" {
		t.Errorf("tags %v, metadata %v", got.Tags, got.Metadata)
	}
	if got.GetPriority() != 1 {
		t.Errorf("priority = %d, want 1", got.GetPriority())
	}
	if len(got.RawAttachments) != 1 || got.RawAttachments[0].Name != "report.csv" || got.RawAttachments[0].Options.Mime != "text/csv" {
		t.Errorf("attachments = %+v, want the csv", got.RawAttachments)
	}
	if got.Headers.Text["X-Report"] != "q3" {
		t.Errorf("headers = %v, want the callback's header", got.Headers.Text)
	}
}

// TestMailViaRendersANamedViewThroughTheMailer: a message that names its
// template is drawn by the mailer's views, with the message's data.
func TestMailViaRendersANamedViewThroughTheMailer(t *testing.T) {
	v := &views{}
	mailer, box := newMailer(v)

	_, err := channels.MailVia(mailer).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com"}, withMail{message: messages.NewMail().
			Subject("Welcome").
			View("mail.welcome", map[string]any{"plan": "pro"}).
			Text("mail.welcome-text", nil)})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := box.sent[0]
	if got.HTML != "<rendered mail.welcome>" || got.Text != "<rendered mail.welcome-text>" {
		t.Fatalf("html %q, text %q: the named views were not rendered by the mailer", got.HTML, got.Text)
	}
	data, ok := v.data[0].(map[string]any)
	if !ok || data["plan"] != "pro" {
		t.Errorf("the view was rendered with %#v, want the message's data", v.data[0])
	}
}

// TestMailViaWithNoMailerRefuses: the channel says what is missing rather than
// dereferencing nil inside a send.
func TestMailViaWithNoMailerRefuses(t *testing.T) {
	_, err := channels.MailVia(nil).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com"}, full{})
	if err == nil || !strings.Contains(err.Error(), "the mail channel has no mailer") {
		t.Fatalf("err = %v, want the refusal naming the missing mailer", err)
	}
}
