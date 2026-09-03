package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
)

// mustRender renders a message that is expected to be renderable. A test whose
// subject is the output has nothing to say about a message that was refused,
// and reading the error at every call site would bury what each one is about.
func mustRender(t *testing.T, m mail.Message) string {
	t.Helper()
	out, err := mail.Render(m)
	if err != nil {
		t.Fatalf("Render refused a message this test expected to render: %v", err)
	}
	return out
}

// smuggler is a mailable whose envelope carries a second header line inside a
// field, which is the shape this arrives in: a subject or a reply-to assembled
// from something a person typed.
type smuggler struct {
	subject string
	replyTo string
	headers mail.Headers
}

func (s smuggler) Envelope() mail.Envelope {
	e := mail.Envelope{Subject: "Invoice"}
	if s.subject != "" {
		e.Subject = s.subject
	}
	if s.replyTo != "" {
		e.ReplyTo = []mail.Address{{Address: s.replyTo}}
	}
	return e
}

func (s smuggler) Content() mail.Content { return mail.Content{Text: "the body"} }
func (s smuggler) Headers() mail.Headers { return s.headers }

// TestASmuggledHeaderNeverReachesATransport is the end of the path: the refusal
// has to land before anything is handed to a provider, because a provider that
// received the message has already sent it.
//
// The array transport is what the assertion reads. Nothing arrived there, so
// nothing rendered, and the error names the field that carried the break.
func TestASmuggledHeaderNeverReachesATransport(t *testing.T) {
	for _, c := range []struct {
		name     string
		mailable smuggler
		field    string
	}{
		{
			name:     "a subject that adds a blind copy",
			mailable: smuggler{subject: "Invoice\r\nBcc: attacker@example.test"},
			field:    "Subject",
		},
		{
			name:     "a reply-to that adds one",
			mailable: smuggler{replyTo: "ok@example.test\r\nBcc: attacker@example.test"},
			field:    "Reply-To",
		},
		{
			name:     "a message id that adds one",
			mailable: smuggler{headers: mail.Headers{MessageID: "1@example.test\r\nBcc: attacker@example.test"}},
			field:    "Message-Id",
		},
		{
			name:     "a header name that adds one",
			mailable: smuggler{headers: mail.Headers{Text: map[string]string{"X\r\nBcc: attacker@example.test": "v"}}},
			field:    "Headers.Text name",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, box := mailer(t)

			_, err := m.To("you@example.test").Send(context.Background(), c.mailable)

			var refusal *mail.HeaderError
			if !errors.As(err, &refusal) {
				t.Fatalf("Send = %v, want a *HeaderError naming %s", err, c.field)
			}
			if refusal.Field != c.field {
				t.Errorf("the refusal names %q, want %q", refusal.Field, c.field)
			}
			if _, ok := box.Last(); ok {
				t.Error("the message reached the transport, and a transport that received it has sent it")
			}
		})
	}
}

// partBreaks are the ways a value ends an attachment's header line early.
//
// The first four are the line breaks every other header field is checked for.
// The last two are what a quoted parameter adds: name="..." ends at the closing
// quote, so a quote inside the value ends it, and a backslash escapes whatever
// follows it -- including that closing quote.
var partBreaks = map[string]string{
	"a carriage return":   "ok.txt\rmore",
	"a line feed":         "ok.txt\nmore",
	"a full line break":   "ok.txt\r\nmore",
	"a smuggled envelope": "ok.txt\"\r\nBcc: attacker@example.test\r\nX: \"",
	"a closing quote":     "ok.txt\"; x=\"y",
	"an escaping slash":   "ok.txt\\",
}

// TestASmuggledAttachmentNameNeverRenders is the injection the envelope checks
// did not reach.
//
// The name of an attachment is written into three header lines and its content
// type into a fourth, and an attachment's name is typically the name of a file
// somebody uploaded. A name carrying a line break is a header of its own, and
// the renderer wrote it out.
func TestASmuggledAttachmentNameNeverRenders(t *testing.T) {
	for shape, value := range partBreaks {
		t.Run(shape, func(t *testing.T) {
			var m mail.Message
			m.Subject = "hello"
			m.Text = "the body"
			m.AttachData([]byte("data"), value, mail.AttachOptions{Mime: "text/plain"})

			out, err := mail.Render(m)

			if strings.Contains(out, "Bcc: attacker@example.test") {
				t.Errorf("the rendered message carries %d headers the sender never wrote:\n%s",
					strings.Count(out, "Bcc: attacker@example.test"), out)
			}
			var refusal *mail.HeaderError
			if !errors.As(err, &refusal) {
				t.Fatalf("Render = %v, want a *HeaderError", err)
			}
			if refusal.Field != "Content-Disposition filename" {
				t.Errorf("the refusal names %q", refusal.Field)
			}
			if out != "" {
				t.Errorf("a refused message rendered %d bytes, and a caller writing them "+
					"to a socket sends half a message:\n%s", len(out), out)
			}
		})
	}
}

// TestASmuggledAttachmentContentTypeNeverRenders is the same defect one field
// over: the content type is interpolated into the same line the name is.
func TestASmuggledAttachmentContentTypeNeverRenders(t *testing.T) {
	var m mail.Message
	m.Subject = "hello"
	m.Text = "the body"
	m.AttachData([]byte("data"), "ok.txt", mail.AttachOptions{
		Mime: "text/plain\r\nBcc: attacker@example.test",
	})

	out, err := mail.Render(m)
	if strings.Contains(out, "Bcc: attacker@example.test") {
		t.Errorf("the rendered message carries a header the sender never wrote:\n%s", out)
	}
	var refusal *mail.HeaderError
	if !errors.As(err, &refusal) {
		t.Fatalf("Render = %v, want a *HeaderError", err)
	}
	if refusal.Field != "Content-Type" {
		t.Errorf("the refusal names %q, want Content-Type", refusal.Field)
	}
}

// TestASmuggledAttachmentNameNeverReachesATransport is the other half, and the
// half that matters more: a provider that received the message has sent it, so
// the refusal has to land before the transport, not inside the renderer.
func TestASmuggledAttachmentNameNeverReachesATransport(t *testing.T) {
	for shape, value := range partBreaks {
		t.Run(shape, func(t *testing.T) {
			m, box := mailer(t)

			_, err := m.To("you@example.test").Send(context.Background(), attacher{name: value})

			var refusal *mail.HeaderError
			if !errors.As(err, &refusal) {
				t.Fatalf("Send = %v, want a *HeaderError", err)
			}
			if !errors.Is(err, mail.ErrHeaderInjection) {
				t.Errorf("the refusal does not wrap ErrHeaderInjection: %v", err)
			}
			if _, ok := box.Last(); ok {
				t.Error("the message reached the transport, and a transport that received it has sent it")
			}
		})
	}
}

// attacher is a mailable that attaches one named file, which is how a name a
// person uploaded gets into a message.
type attacher struct {
	name string
	mime string
}

func (attacher) Envelope() mail.Envelope { return mail.Envelope{Subject: "Invoice"} }
func (attacher) Content() mail.Content   { return mail.Content{Text: "the body"} }
func (attacher) Headers() mail.Headers   { return mail.Headers{} }

func (a attacher) Attachments() []*mail.Attachment {
	return []*mail.Attachment{
		mail.FromData(func() ([]byte, error) { return []byte("data"), nil }, a.name).
			WithMime(a.mime),
	}
}

// smugglingDisk answers every question with a value that ends a header line
// early, which is the half of a part nobody set: a stored attachment takes its
// content type from the disk that holds it.
type smugglingDisk struct{ mime string }

func (d smugglingDisk) Disk(string) mail.Disk           { return d }
func (smugglingDisk) Get(string) ([]byte, error)        { return []byte("data"), nil }
func (d smugglingDisk) MimeType(string) (string, error) { return d.mime, nil }

// TestASmuggledContentTypeFromADiskNeverRenders is why the renderer checks a
// part again after resolving it.
//
// The check before the send reads what a caller set, and a stored attachment's
// content type is not that: it is what the disk answered with, minutes later,
// on a worker. Nothing between the two checks would have caught this one.
func TestASmuggledContentTypeFromADiskNeverRenders(t *testing.T) {
	previous := mail.Filesystem
	mail.Filesystem = smugglingDisk{mime: "text/plain\r\nBcc: attacker@example.test"}
	t.Cleanup(func() { mail.Filesystem = previous })

	var m mail.Message
	m.Subject = "hello"
	m.Text = "the body"
	m.AttachFromStorage("invoices/1.txt", "invoice.txt")

	out, err := mail.Render(m)

	if strings.Contains(out, "Bcc: attacker@example.test") {
		t.Errorf("the disk wrote a header of its own:\n%s", out)
	}
	var refusal *mail.HeaderError
	if !errors.As(err, &refusal) {
		t.Fatalf("Render = %v, want a *HeaderError", err)
	}
	if refusal.Field != "Content-Type" {
		t.Errorf("the refusal names %q, want Content-Type", refusal.Field)
	}
}

// TestAnOrdinaryAttachmentStillRenders is the half that says the refusal did
// not become "refuse every attachment". A name with a space, a comma and an
// accent in it is a name people actually upload.
func TestAnOrdinaryAttachmentStillRenders(t *testing.T) {
	var m mail.Message
	m.Subject = "hello"
	m.Text = "the body"
	m.AttachData([]byte("data"), "Fatura 2026, vencida.pdf", mail.AttachOptions{Mime: "application/pdf"})

	out := mustRender(t, m)
	if !strings.Contains(out, `Content-Disposition: attachment; filename="Fatura 2026, vencida.pdf"`) {
		t.Errorf("the attachment did not render:\n%s", out)
	}
}

// TestAValidMessageRendersExactlyAsItDid fixes the output the refusal must not
// have changed.
//
// The renderer was not touched, and this is what says so: every header line a
// message without attachments produces, in order, with the one line that cannot
// be fixed -- the date -- left to be checked for presence only. A message that
// renders differently after a change to the checks is a message whose clients
// see something different, and nobody would find that from the checks' own
// tests.
func TestAValidMessageRendersExactlyAsItDid(t *testing.T) {
	message := mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test", Name: "App"},
			To:      []mail.Address{{Address: "you@example.test"}},
			CC:      []mail.Address{{Address: "cc@example.test"}},
			ReplyTo: []mail.Address{{Address: "reply@example.test"}},
			Subject: "Invoice",
		},
		Text: "the body",
		Headers: mail.Headers{
			MessageID:  "1@example.test",
			References: []string{"0@example.test"},
			Text:       map[string]string{"X-Campaign": "invoices"},
		},
	}

	head, _, found := strings.Cut(mustRender(t, message), "\r\n\r\n")
	if !found {
		t.Fatal("the rendered message has no header block")
	}

	lines := strings.Split(head, "\r\n")
	var dated bool
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "Date: ") {
			dated = true
			continue
		}
		kept = append(kept, line)
	}
	if !dated {
		t.Error("the message carries no Date header")
	}

	want := []string{
		`From: "App" <app@example.test>`,
		"To: you@example.test",
		"Cc: cc@example.test",
		"Reply-To: reply@example.test",
		"Subject: Invoice",
		"Message-Id: <1@example.test>",
		"References: <0@example.test>",
		"X-Campaign: invoices",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
	}
	if strings.Join(kept, "\n") != strings.Join(want, "\n") {
		t.Errorf("the header block changed:\n got %q\nwant %q", kept, want)
	}
}

// TestALongSubjectIsStillSplitIntoEncodedWords fixes what the renderer does
// with a subject too long for one encoded word, which is the case the refusal
// must not have disturbed.
//
// It splits it into two encoded words separated by a space, on one line. It
// does not fold onto a continuation line, and that is worth writing down: a
// header value here never contains a line break, so "refuse every CR and LF"
// costs this renderer nothing. A change that starts folding makes this test the
// place the refusal has to be reconsidered.
func TestALongSubjectIsStillSplitIntoEncodedWords(t *testing.T) {
	subject := "Você tem uma fatura em aberto, e ela vence hoje — confira os detalhes agora mesmo"
	message := mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			Subject: subject,
		},
		Text: "the body",
	}
	if err := message.Envelope.Check(); err != nil {
		t.Fatalf("Check refused a subject with no line break in it: %v", err)
	}

	head, _, _ := strings.Cut(mustRender(t, message), "\r\n\r\n")

	var line string
	for _, l := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(l, "Subject: ") {
			line = l
		}
	}
	if !strings.HasPrefix(line, "Subject: =?utf-8?q?") {
		t.Fatalf("the subject was not encoded:\n%s", head)
	}
	if got := strings.Count(line, "=?utf-8?q?"); got != 2 {
		t.Errorf("the subject came out as %d encoded words, want 2:\n%s", got, line)
	}
	if strings.ContainsAny(line, "\r\n") {
		t.Errorf("the subject line carries a line break, which is what the refusal is about:\n%q", line)
	}
}
