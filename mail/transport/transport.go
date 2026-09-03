package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
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
// The [mail.SentMessage] it returns carries no identifier: SMTP has no field
// for one, and the id the receiving server assigns is in a reply line no client
// is obliged to be given.
func (t SMTP) Send(ctx context.Context, m mail.Message) (mail.SentMessage, error) {
	sent := mail.SentMessage{Transport: t.Name()}

	// Rendered before the connection is dialled, because a message that cannot
	// be rendered must not reach the server at all: RCPT TO tells it who the
	// recipients are, and a refusal after that has already named them.
	rendered, err := mail.Render(m)
	if err != nil {
		return mail.SentMessage{}, err
	}

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
		return mail.SentMessage{}, mail.Retryable(fmt.Errorf("mail: dialing %s: %w", addr, err))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	c, err := smtp.NewClient(conn, t.Host)
	if err != nil {
		return mail.SentMessage{}, mail.Retryable(fmt.Errorf("mail: %s: %w", addr, err))
	}
	defer c.Close()

	// STARTTLS when the server offers it, and that is not optional when there
	// are credentials: PlainAuth refuses to send a password over a connection
	// that is not encrypted, and it is right to.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: t.Host}); err != nil {
			return mail.SentMessage{}, fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if t.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", t.Username, t.Password, t.Host)); err != nil {
			return mail.SentMessage{}, fmt.Errorf("mail: authenticating: %w", err)
		}
	}

	if err := c.Mail(m.From.Address); err != nil {
		return mail.SentMessage{}, fmt.Errorf("mail: from %s: %w", m.From.Address, err)
	}
	for _, a := range append(append(append([]mail.Address{}, m.To...), m.CC...), m.BCC...) {
		if err := c.Rcpt(a.Address); err != nil {
			return mail.SentMessage{}, fmt.Errorf("mail: to %s: %w", a.Address, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return mail.SentMessage{}, fmt.Errorf("mail: data: %w", err)
	}
	if _, err := w.Write([]byte(rendered)); err != nil {
		return mail.SentMessage{}, fmt.Errorf("mail: writing the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return mail.SentMessage{}, fmt.Errorf("mail: closing the message: %w", err)
	}
	if err := c.Quit(); err != nil {
		return mail.SentMessage{}, fmt.Errorf("mail: closing the session: %w", err)
	}
	return sent, nil
}

// Log writes the message to the log instead of sending it.
//
// It is the development default, and what makes a project run with no mail
// server installed. The whole body is logged, because the reason to read it is
// to follow the link inside.
type Log struct {
	// logger is where the message is written. It is unexported because
	// [Log.Logger] reads it and a Go type cannot carry a method and a field of
	// the same name; [NewLog] is what sets it.
	logger *slog.Logger
}

// NewLog builds a log transport.
//
// A nil logger means the one the context carries, which is what makes the zero
// value useful: transport.Log{} logs wherever the request is logging.
func NewLog(logger *slog.Logger) Log { return Log{logger: logger} }

// Name identifies the transport in a log line.
func (Log) Name() string { return "log" }

// Logger is where this transport writes, and is nil when the transport follows
// the context.
func (t Log) Logger() *slog.Logger { return t.logger }

// Send logs the message.
func (t Log) Send(ctx context.Context, m mail.Message) (mail.SentMessage, error) {
	to := make([]string, 0, len(m.To))
	for _, a := range m.To {
		to = append(to, a.Address)
	}

	logger := t.logger
	if logger == nil {
		logger = log.For(ctx)
	}
	logger.Info("mail: this transport logs instead of sending",
		"to", strings.Join(to, ", "),
		"subject", m.Subject,
		"body", firstNonEmpty(m.Text, m.HTML))
	return mail.SentMessage{Transport: t.Name()}, nil
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
func (a *Array) Send(_ context.Context, m mail.Message) (mail.SentMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, m)
	return mail.SentMessage{Transport: a.Name()}, nil
}

// Messages is everything sent so far, oldest first, as a copy: a test can read
// it while another goroutine is still sending.
func (a *Array) Messages() []mail.Message {
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

// Flush forgets everything and answers what it forgot. A test that shares a
// transport between cases calls it, and one that does not share it does not
// need to.
func (a *Array) Flush() []mail.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	was := a.sent
	a.sent = nil
	return was
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
