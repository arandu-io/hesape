package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/mail"
)

// SMTP sends over SMTP, with STARTTLS.
type SMTP struct {
	// Host and Port are the server. 587 is submission with STARTTLS, which is
	// what a provider gives you; 25 is server-to-server and is usually blocked.
	Host string
	Port string

	// Username and Password authenticate. Both empty sends unauthenticated,
	// which is right for a local relay and wrong for anything reachable.
	Username string
	Password string

	// Timeout bounds the whole exchange. Without one a hung server holds the
	// request that triggered it until the client gives up -- and net/smtp has no
	// deadline of its own.
	Timeout time.Duration
}

// Name identifies the transport in a log line.
func (SMTP) Name() string { return "smtp" }

// Send delivers the message.
//
// The [mail.Sent] it returns carries no identifier: SMTP has no field for one,
// and the id the receiving server assigns is in a reply line no client is
// obliged to be given.
func (t SMTP) Send(ctx context.Context, m mail.Message) (mail.Sent, error) {
	sent := mail.Sent{Transport: t.Name()}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	addr := net.JoinHostPort(t.Host, t.Port)
	dialer := &net.Dialer{Timeout: timeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		// The server was not reachable, which is the same kind of event as a
		// provider answering 502: nothing about the message was refused.
		return mail.Sent{}, mail.Retryable(fmt.Errorf("mail: dialing %s: %w", addr, err))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	c, err := smtp.NewClient(conn, t.Host)
	if err != nil {
		return mail.Sent{}, mail.Retryable(fmt.Errorf("mail: %s: %w", addr, err))
	}
	defer c.Close()

	// STARTTLS when the server offers it, and that is not optional when there
	// are credentials: PlainAuth refuses to send a password over a connection
	// that is not encrypted, and it is right to.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: t.Host}); err != nil {
			return mail.Sent{}, fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if t.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", t.Username, t.Password, t.Host)); err != nil {
			return mail.Sent{}, fmt.Errorf("mail: authenticating: %w", err)
		}
	}

	if err := c.Mail(m.From.Email); err != nil {
		return mail.Sent{}, fmt.Errorf("mail: from %s: %w", m.From.Email, err)
	}
	for _, a := range append(append(append([]mail.Address{}, m.To...), m.CC...), m.BCC...) {
		if err := c.Rcpt(a.Email); err != nil {
			return mail.Sent{}, fmt.Errorf("mail: to %s: %w", a.Email, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return mail.Sent{}, fmt.Errorf("mail: data: %w", err)
	}
	if _, err := w.Write([]byte(mail.Render(m))); err != nil {
		return mail.Sent{}, fmt.Errorf("mail: writing the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return mail.Sent{}, fmt.Errorf("mail: closing the message: %w", err)
	}
	if err := c.Quit(); err != nil {
		return mail.Sent{}, fmt.Errorf("mail: closing the session: %w", err)
	}
	return sent, nil
}

// Log writes the message to the log instead of sending it.
//
// It is the development default, and what makes `aru dev` work with nothing
// installed. The whole body is logged, because the reason to read it is to
// follow the link inside.
type Log struct{}

// Name identifies the transport in a log line.
func (Log) Name() string { return "log" }

// Send logs the message.
func (t Log) Send(ctx context.Context, m mail.Message) (mail.Sent, error) {
	to := make([]string, 0, len(m.To))
	for _, a := range m.To {
		to = append(to, a.Email)
	}

	log.For(ctx).Info("mail: this transport logs instead of sending",
		"to", strings.Join(to, ", "),
		"subject", m.Subject,
		"body", firstNonEmpty(m.Text, m.HTML))
	return mail.Sent{Transport: t.Name()}, nil
}

// Array keeps what was sent, for a test to read.
//
// It is safe for concurrent use, because a test that sends from two goroutines
// and reads from a third is a test that would otherwise fail under -race for a
// reason that has nothing to do with what it is proving.
type Array struct {
	mu   sync.Mutex
	sent []mail.Message
}

// Name identifies the transport in a log line.
func (*Array) Name() string { return "array" }

// Send records the message.
func (a *Array) Send(_ context.Context, m mail.Message) (mail.Sent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, m)
	return mail.Sent{Transport: a.Name()}, nil
}

// Sent is everything sent so far, oldest first.
func (a *Array) Sent() []mail.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]mail.Message(nil), a.sent...)
}

// Last is the most recent message, and whether there was one.
func (a *Array) Last() (mail.Message, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sent) == 0 {
		return mail.Message{}, false
	}
	return a.sent[len(a.sent)-1], true
}

// Reset forgets everything. A test that shares a transport between cases calls
// it, and one that does not share it does not need to.
func (a *Array) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var (
	_ mail.Transport = SMTP{}
	_ mail.Transport = Log{}
	_ mail.Transport = (*Array)(nil)
)
