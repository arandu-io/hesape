package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/mail/transport"
)

// welcome is the shape a person writes: an envelope, a content, and nothing else.
type welcome struct {
	Name string
	Body string
}

func (w welcome) Envelope() mail.Envelope { return mail.Envelope{Subject: "Welcome, " + w.Name} }
func (w welcome) Content() mail.Content   { return mail.Content{Text: w.Body} }

// unnamed is a mailable somebody forgot to give a subject.
type unnamed struct{}

func (unnamed) Envelope() mail.Envelope { return mail.Envelope{} }
func (unnamed) Content() mail.Content   { return mail.Content{Text: "hi"} }

// renderer is a Renderer that returns what it was given, so a test can send a
// message with an HTML part without registering a view.
type renderer struct{ html string }

func (r renderer) RenderToString(string, any) (string, error) { return r.html, nil }

func mailer(t *testing.T) (*mail.Mailer, *transport.Array) {
	t.Helper()
	box := &transport.Array{}
	return mail.New(box, renderer{html: "<p>hello</p>"}, mail.Address{Email: "app@example.test", Name: "App"}), box
}

// TestAMessageIsSentToWhoItWasAddressedTo is the happy path, and it checks the
// default sender too: an application sends from one address, and a mailable that
// does not name one should not have to.
func TestAMessageIsSentToWhoItWasAddressedTo(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, ok := box.Last()
	if !ok {
		t.Fatal("nothing was sent")
	}
	if got := sent.To[0].Email; got != "you@example.test" {
		t.Errorf("to = %q", got)
	}
	if sent.From.Email != "app@example.test" {
		t.Errorf("from = %q, want the configured default", sent.From.Email)
	}
	if sent.Subject != "Welcome, Ada" {
		t.Errorf("subject = %q", sent.Subject)
	}
	if sent.Text != "hi" {
		t.Errorf("text = %q", sent.Text)
	}
}

// TestTheTransportsReceiptReachesTheCaller.
//
// The provider's identifier is the only thing joining a line in the application
// log to a row in the provider's dashboard, and it used to be dropped by the
// Mailer: Transport.Send returned an error and nothing else.
func TestTheTransportsReceiptReachesTheCaller(t *testing.T) {
	m := mail.New(receipt{id: "prov-42"}, nil, mail.Address{Email: "app@example.test"})

	sent, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.ID != "prov-42" {
		t.Errorf("ID = %q, want the provider's identifier", sent.ID)
	}
	if sent.Transport != "receipt" {
		t.Errorf("Transport = %q", sent.Transport)
	}
}

// receipt is a transport that answers with an identifier, the way every provider
// transport does.
type receipt struct{ id string }

func (receipt) Name() string { return "receipt" }
func (r receipt) Send(context.Context, mail.Message) (mail.Sent, error) {
	return mail.Sent{ID: r.id, Transport: r.Name()}, nil
}

// TestNothingIsSentWithoutARecipient.
//
// An error rather than a silent no-op. A message with nobody to send it to is a
// message somebody meant to send, and returning nil here is how a password reset
// goes missing with a green log line.
func TestNothingIsSentWithoutARecipient(t *testing.T) {
	m, box := mailer(t)

	_, err := m.To().Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if !errors.Is(err, mail.ErrNoRecipient) {
		t.Fatalf("err = %v, want ErrNoRecipient", err)
	}
	if len(box.Sent()) != 0 {
		t.Error("a message with no recipient reached the transport")
	}
}

// TestABadAddressIsRefusedBeforeTheTransport: a typo fails at the call rather
// than as a bounce three minutes later, in a log nobody is reading.
func TestABadAddressIsRefusedBeforeTheTransport(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("not an address").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err == nil {
		t.Fatal("an unparseable address was accepted")
	}
	if len(box.Sent()) != 0 {
		t.Error("it reached the transport anyway")
	}
}

// TestAMessageWithNoSubjectIsRefused. Every provider files one as spam, and it
// is the field most easily left empty by a struct literal.
func TestAMessageWithNoSubjectIsRefused(t *testing.T) {
	m, _ := mailer(t)
	_, err := m.To("you@example.test").Send(context.Background(), unnamed{})
	if err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("err = %v, want something about the subject", err)
	}
}

// TestALiteralHTMLBodyIsSentAsIs.
//
// Content.HTML is for a body that is already HTML by the time it arrives. Before
// it existed the only way in was a view, so a module holding a composed body had
// to register one to send it.
func TestALiteralHTMLBodyIsSentAsIs(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), literal{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, _ := box.Last()
	if sent.HTML != "<p>composed elsewhere</p>" {
		t.Errorf("HTML = %q", sent.HTML)
	}
}

// TestAViewBeatsALiteralBody, the way TextView beats Text: one message has one
// HTML part, and a mailable that names both means the view.
func TestAViewBeatsALiteralBody(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), literalAndView{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, _ := box.Last()
	if sent.HTML != "<p>hello</p>" {
		t.Errorf("HTML = %q, want what the renderer drew", sent.HTML)
	}
}

// literal is a mailable whose body is HTML it already has.
type literal struct{}

func (literal) Envelope() mail.Envelope { return mail.Envelope{Subject: "Composed"} }
func (literal) Content() mail.Content {
	return mail.Content{HTML: "<p>composed elsewhere</p>"}
}

// literalAndView names both, which is the case somebody gets wrong.
type literalAndView struct{}

func (literalAndView) Envelope() mail.Envelope { return mail.Envelope{Subject: "Both"} }
func (literalAndView) Content() mail.Content {
	return mail.Content{View: "mail.note", HTML: "<p>composed elsewhere</p>"}
}

// TestBccIsNotAHeader is the one that leaks if it is wrong.
//
// Blind copy is on the envelope, in RCPT TO. Writing it into the message is how
// every recipient learns who else was copied, and it is one line to get wrong.
func TestBccIsNotAHeader(t *testing.T) {
	rendered := mail.Render(mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Email: "app@example.test"},
			To:      []mail.Address{{Email: "you@example.test"}},
			BCC:     []mail.Address{{Email: "secret@example.test"}},
			Subject: "Hello",
		},
		Text: "hi",
	})

	if strings.Contains(rendered, "secret@example.test") {
		t.Errorf("the blind copy is in the message:\n%s", rendered)
	}
	if !strings.Contains(rendered, "you@example.test") {
		t.Error("the visible recipient is missing")
	}
}

// TestANonASCIISubjectIsEncoded: "Você tem uma fatura" is the first subject
// anybody writes here, and a raw one arrives as mojibake.
func TestANonASCIISubjectIsEncoded(t *testing.T) {
	rendered := mail.Render(mail.Message{
		Envelope: mail.Envelope{
			From: mail.Address{Email: "app@example.test"},
			To:   []mail.Address{{Email: "you@example.test"}},

			Subject: "Você tem uma fatura",
		},
		Text: "hi",
	})

	if strings.Contains(rendered, "Você tem uma fatura") {
		t.Errorf("the subject went out raw:\n%s", rendered)
	}
	if !strings.Contains(rendered, "=?utf-8?q?") {
		t.Errorf("the subject was not encoded:\n%s", rendered)
	}
}

// TestBothPartsAreSentWhenBothExist, in the order the standard asks for: least
// preferred first, so a client picks the last one it understands.
func TestBothPartsAreSentWhenBothExist(t *testing.T) {
	rendered := mail.Render(mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Email: "app@example.test"},
			To:      []mail.Address{{Email: "you@example.test"}},
			Subject: "Hello",
		},
		HTML: "<p>hello</p>",
		Text: "hello",
	})

	if !strings.Contains(rendered, "multipart/alternative") {
		t.Fatalf("not multipart:\n%s", rendered)
	}
	plain, html := strings.Index(rendered, "text/plain"), strings.Index(rendered, "text/html")
	if plain < 0 || html < 0 {
		t.Fatalf("a part is missing:\n%s", rendered)
	}
	if plain > html {
		t.Error("the HTML part comes first: a client that renders both picks the last it understands, " +
			"so this arrangement shows plain text to everybody")
	}
}

// TestALongLineIsFolded. A body line over 998 bytes is refused by the protocol,
// and an HTML document is one long line often enough.
func TestALongLineIsFolded(t *testing.T) {
	rendered := mail.Render(mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Email: "app@example.test"},
			To:      []mail.Address{{Email: "you@example.test"}},
			Subject: "Hello",
		},
		HTML: "<p>" + strings.Repeat("x", 2000) + "</p>",
	})

	for _, line := range strings.Split(rendered, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line is %d bytes: the server refuses the message", len(line))
		}
	}
}

// TestRetryableSurvivesWrapping. The mark is what a job reads to decide between
// rescheduling and giving up, and it has to survive being wrapped on the way
// out of a transport.
func TestRetryableSurvivesWrapping(t *testing.T) {
	cause := errors.New("the provider answered 503")
	wrapped := errors.Join(mail.Retryable(cause), errors.New("and so did the other one"))

	var mark mail.ErrRetryable
	if !errors.As(wrapped, &mark) {
		t.Fatal("the mark was lost")
	}
	if !errors.Is(wrapped, cause) {
		t.Error("the cause was lost")
	}
	if mail.Retryable(nil) != nil {
		t.Error("Retryable(nil) is an error, so a caller has to guard the call it exists to remove")
	}
}
