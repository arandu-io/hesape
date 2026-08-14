package mail

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// Render turns a message into the bytes an SMTP server receives.
//
// It is exported because a transport that speaks a provider's HTTP API does not
// need it and a transport that speaks SMTP does, and both live outside this
// package -- in mail/transport, and in whatever adapter somebody writes next.
//
// # What it gets right that a Sprintf does not
//
// A header carrying a non-ASCII subject has to be encoded, or the client shows
// mojibake -- and "Você tem uma fatura" is the first subject anybody writes here.
// mime.QEncoding does it, and does nothing when the text is plain ASCII.
//
// A body line longer than 998 bytes is refused by the protocol, and an HTML
// document is one long line often enough. quoted-printable folds it.
//
// A line consisting of a single dot ends the message: a body containing one is
// a body that is silently truncated, and the transfer encoding removes that too.
//
// # The nesting
//
// An attachment wraps everything in multipart/mixed, an embedded image wraps
// the body in multipart/related, and two body parts are multipart/alternative
// inside that. The order matters: a client that finds the inline image outside
// the related part shows it as a second attachment instead of in the message.
//
// Render is Mailable::render.
func Render(m Message) string {
	var b strings.Builder

	header(&b, "From", m.From.String())
	header(&b, "To", addresses(m.To))
	if len(m.CC) > 0 {
		header(&b, "Cc", addresses(m.CC))
	}
	if len(m.ReplyTo) > 0 {
		header(&b, "Reply-To", addresses(m.ReplyTo))
	}
	// Bcc is deliberately not a header. It is on the envelope, in RCPT TO, and
	// writing it here is how every recipient learns who else was blind-copied.

	if m.sender.Address != "" {
		header(&b, "Sender", m.sender.String())
	}
	if m.returnPath != "" {
		header(&b, "Return-Path", "<"+m.returnPath+">")
	}

	header(&b, "Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header(&b, "Date", time.Now().Format(time.RFC1123Z))

	if m.Headers.MessageID != "" {
		header(&b, "Message-Id", angled(m.Headers.MessageID))
	}
	if len(m.Headers.References) > 0 {
		header(&b, "References", m.Headers.ReferencesString())
	}
	for name, value := range m.Headers.Text {
		header(&b, name, mime.QEncoding.Encode("utf-8", value))
	}
	if m.priority > 0 {
		header(&b, "X-Priority", fmt.Sprintf("%d", m.priority))
	}

	header(&b, "MIME-Version", "1.0")

	body(&b, m)
	return b.String()
}

// body writes the content headers and the parts, from the outside in.
func body(b *strings.Builder, m Message) {
	attachments := m.parts()

	if len(attachments) > 0 {
		boundary := newBoundary()
		header(b, "Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
		b.WriteString("\r\n")

		b.WriteString("--" + boundary + "\r\n")
		var inner strings.Builder
		related(&inner, m)
		b.WriteString(inner.String())
		b.WriteString("\r\n")

		for _, part := range attachments {
			attachmentPart(b, boundary, part, false)
		}
		b.WriteString("--" + boundary + "--\r\n")
		return
	}

	related(b, m)
}

// related wraps the body in multipart/related when something is embedded in it,
// so that a client shows the image inside the message rather than beside it.
func related(b *strings.Builder, m Message) {
	if len(m.embeds) == 0 {
		alternative(b, m)
		return
	}

	boundary := newBoundary()
	header(b, "Content-Type", `multipart/related; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	var inner strings.Builder
	alternative(&inner, m)
	b.WriteString(inner.String())
	b.WriteString("\r\n")

	for _, part := range m.embeds {
		attachmentPart(b, boundary, part, true)
	}
	b.WriteString("--" + boundary + "--\r\n")
}

// alternative writes the readable body: both parts when both exist, and one
// otherwise.
func alternative(b *strings.Builder, m Message) {
	switch {
	case m.HTML != "" && m.Text != "":
		// Both parts, in the order the standard asks for: least preferred first,
		// so a client that renders HTML picks the last one it understands.
		boundary := newBoundary()
		header(b, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n")

		part(b, boundary, "text/plain; charset=utf-8", m.Text)
		part(b, boundary, "text/html; charset=utf-8", m.HTML)
		b.WriteString("--" + boundary + "--\r\n")

	case m.HTML != "":
		header(b, "Content-Type", "text/html; charset=utf-8")
		header(b, "Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		b.WriteString(encode(m.HTML))

	default:
		header(b, "Content-Type", "text/plain; charset=utf-8")
		header(b, "Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		b.WriteString(encode(m.Text))
	}
}

func attachmentPart(b *strings.Builder, boundary string, p embedded, inline bool) {
	b.WriteString("--" + boundary + "\r\n")

	contentType := p.Mime
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header(b, "Content-Type", contentType+`; name="`+p.Name+`"`)
	header(b, "Content-Transfer-Encoding", "base64")
	if inline {
		header(b, "Content-Disposition", `inline; filename="`+p.Name+`"`)
		header(b, "Content-ID", "<"+p.CID+">")
	} else {
		header(b, "Content-Disposition", `attachment; filename="`+p.Name+`"`)
	}
	b.WriteString("\r\n")
	b.WriteString(base64Lines(p.Data))
	b.WriteString("\r\n")
}

// base64Lines folds base64 at 76 characters, which is what the transfer
// encoding requires and what keeps every line under the protocol's limit.
func base64Lines(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var out strings.Builder
	for len(encoded) > 76 {
		out.WriteString(encoded[:76])
		out.WriteString("\r\n")
		encoded = encoded[76:]
	}
	out.WriteString(encoded)
	out.WriteString("\r\n")
	return out.String()
}

func newBoundary() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "arandu-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "arandu-" + hex.EncodeToString(b[:])
}

func angled(id string) string {
	if !strings.HasPrefix(id, "<") {
		id = "<" + id
	}
	if !strings.HasSuffix(id, ">") {
		id += ">"
	}
	return id
}

func header(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func part(b *strings.Builder, boundary, contentType, body string) {
	b.WriteString("--" + boundary + "\r\n")
	header(b, "Content-Type", contentType)
	header(b, "Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	b.WriteString(encode(body))
	b.WriteString("\r\n")
}

func encode(body string) string {
	var out strings.Builder
	w := quotedprintable.NewWriter(&out)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return out.String()
}

func addresses(list []Address) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.String())
	}
	return strings.Join(out, ", ")
}
