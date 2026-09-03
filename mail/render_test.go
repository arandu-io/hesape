package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
)

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

	head, _, found := strings.Cut(mail.Render(message), "\r\n\r\n")
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

	head, _, _ := strings.Cut(mail.Render(message), "\r\n\r\n")

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
