package mail_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/mail"
)

// breaks are the ways a value ends a header line early. A bare line feed ends
// one for most receivers and a bare carriage return for some, so all four shapes
// are the same defect written differently.
var breaks = map[string]string{
	"a carriage return":   "invoice\rmore",
	"a line feed":         "invoice\nmore",
	"a full line break":   "invoice\r\nmore",
	"a trailing newline":  "invoice\n",
	"a smuggled envelope": "invoice\r\nBcc: attacker@example.test",
}

// TestEveryHeaderCarrierRefusesALineBreak walks each field that becomes a
// header line, with each shape of line break.
//
// Nine of these injected before the refusal existed and three did not, and the
// three were contained by an encoder rather than by a decision: a subject is
// quoted-printable encoded on the way out, which happens to encode a control
// character too. That is not a defence anybody chose for that field, and it
// changes when the encoder does -- so the check covers the encoded fields as
// well, and the table says so by treating all of them alike.
func TestEveryHeaderCarrierRefusesALineBreak(t *testing.T) {
	for shape, value := range breaks {
		t.Run(shape, func(t *testing.T) {
			for _, c := range []struct {
				field string
				build func(string) error
			}{
				{"Subject", func(v string) error {
					return mail.Envelope{Subject: v}.Check()
				}},
				{"From", func(v string) error {
					return mail.Envelope{From: mail.Address{Address: v}}.Check()
				}},
				{"To", func(v string) error {
					return mail.Envelope{To: []mail.Address{{Address: v}}}.Check()
				}},
				{"To", func(v string) error {
					return mail.Envelope{To: []mail.Address{{Address: "a@b.test", Name: v}}}.Check()
				}},
				{"Cc", func(v string) error {
					return mail.Envelope{CC: []mail.Address{{Address: v}}}.Check()
				}},
				{"Bcc", func(v string) error {
					return mail.Envelope{BCC: []mail.Address{{Address: v}}}.Check()
				}},
				{"Reply-To", func(v string) error {
					return mail.Envelope{ReplyTo: []mail.Address{{Address: v}}}.Check()
				}},
				{"Message-Id", func(v string) error {
					return mail.Headers{MessageID: v}.Check()
				}},
				{"References", func(v string) error {
					return mail.Headers{References: []string{v}}.Check()
				}},
				{"X-Campaign", func(v string) error {
					return mail.Headers{Text: map[string]string{"X-Campaign": v}}.Check()
				}},
				{"Headers.Text name", func(v string) error {
					return mail.Headers{Text: map[string]string{v: "value"}}.Check()
				}},
			} {
				err := c.build(value)

				var refusal *mail.HeaderError
				if !errors.As(err, &refusal) {
					t.Errorf("%s with %s: Check() = %v, want a *HeaderError", c.field, shape, err)
					continue
				}
				if !errors.Is(err, mail.ErrHeaderInjection) {
					t.Errorf("%s with %s: the refusal does not wrap ErrHeaderInjection: %v", c.field, shape, err)
				}
				if refusal.Field != c.field {
					t.Errorf("%s with %s: the refusal names %q", c.field, shape, refusal.Field)
				}
			}
		})
	}
}

// TestAnOrdinaryHeaderIsStillAccepted is the other half. A check that refused
// every value would pass every assertion above and send nothing.
func TestAnOrdinaryHeaderIsStillAccepted(t *testing.T) {
	envelope := mail.Envelope{
		From:    mail.Address{Address: "app@example.test", Name: "App, Inc."},
		To:      []mail.Address{{Address: "you@example.test", Name: "Ada Lovelace"}},
		CC:      []mail.Address{{Address: "cc@example.test"}},
		BCC:     []mail.Address{{Address: "bcc@example.test"}},
		ReplyTo: []mail.Address{{Address: "reply@example.test"}},
		// A subject long enough that the encoder folds it, and one that is not
		// ASCII so that it is encoded at all: the folding the renderer produces
		// is legitimate and must keep working.
		Subject: "Você tem uma fatura em aberto, e ela vence hoje — confira os detalhes agora",
	}
	if err := envelope.Check(); err != nil {
		t.Errorf("Envelope.Check() = %v, want nil", err)
	}

	headers := mail.Headers{
		MessageID:  "20260903.1@example.test",
		References: []string{"<20260902.1@example.test>", "20260901.1@example.test"},
		Text:       map[string]string{"X-Campaign": "invoices", "X-Entity-Ref-Id": "abc-123"},
	}
	if err := headers.Check(); err != nil {
		t.Errorf("Headers.Check() = %v, want nil", err)
	}
}

// TestATabIsNotALineBreak fixes the boundary. A header value may be folded onto
// a continuation line, and what continues it is whitespace after a line break --
// the whitespace on its own is an ordinary character and refusing it would
// refuse a References list somebody aligned.
func TestATabIsNotALineBreak(t *testing.T) {
	if err := (mail.Headers{Text: map[string]string{"X-Thing": "a\tb"}}).Check(); err != nil {
		t.Errorf("a tab was refused: %v", err)
	}
}
