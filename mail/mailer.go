package mail

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/arandu-io/hesape/str"
)

// ViewParts is the several-parts form of the view argument to [Mailer.Send]:
// one field per meaning, so that a view name and a literal body cannot be
// confused for one another. The body form is HTMLString, which is the name
// [Content] uses for the same thing.
type ViewParts struct {
	// HTML is the HTML part by view name.
	HTML string
	// Text is the plain-text part by view name.
	Text string
	// Raw is the plain-text part as a literal body.
	Raw string
	// HTMLString is the HTML part as a literal body.
	HTMLString string
}

// Mailer is one configured way to send.
type Mailer struct {
	name      string
	views     Renderer
	transport Transport
	events    Dispatcher
	queue     QueueFactory
	markdown  *Markdown

	from       Address
	replyTo    Address
	returnPath string
	to         Address
}

// New builds a mailer. The dispatcher is optional: an application that does not
// listen for [MessageSending] should not have to wire one, and nil fires
// nothing.
func New(name string, views Renderer, transport Transport, events Dispatcher) *Mailer {
	return &Mailer{name: name, views: views, transport: transport, events: events}
}

// Name is which mailer this is, as configured.
func (m *Mailer) Name() string { return m.name }

// AlwaysFrom sets the sender every message goes out with unless it names one.
func (m *Mailer) AlwaysFrom(address string, name ...string) {
	m.from = NewAddress(address, name...)
}

// AlwaysReplyTo sets the reply-to every message goes out with.
func (m *Mailer) AlwaysReplyTo(address string, name ...string) {
	m.replyTo = NewAddress(address, name...)
}

// AlwaysReturnPath sets the bounce address every message goes out with.
func (m *Mailer) AlwaysReturnPath(address string) { m.returnPath = address }

// AlwaysTo redirects every message to one address, dropping cc and bcc.
//
// It is what a development or staging environment sets so that a test run
// cannot reach a customer. The addresses it replaced are recorded in X-To, X-Cc
// and X-Bcc headers, so the message that arrives still says who it was for.
func (m *Mailer) AlwaysTo(address string, name ...string) {
	m.to = NewAddress(address, name...)
}

// To begins a message to one or more recipients.
//
//	mailer.To("you@example.test").Send(ctx, WelcomeEmail{})
//
// It returns a pending message rather than sending, so cc and bcc chain in the
// order they are read.
func (m *Mailer) To(users any, name ...string) *PendingMail {
	return (&PendingMail{mailer: m}).To(users, name...)
}

// CC begins a message with a carbon copy recipient.
func (m *Mailer) CC(users any, name ...string) *PendingMail {
	return (&PendingMail{mailer: m}).CC(users, name...)
}

// BCC begins a message with a blind carbon copy recipient.
func (m *Mailer) BCC(users any, name ...string) *PendingMail {
	return (&PendingMail{mailer: m}).BCC(users, name...)
}

// HTML sends a message whose only part is the HTML given.
func (m *Mailer) HTML(ctx context.Context, html string, callback func(*Message)) (SentMessage, error) {
	return m.Send(ctx, ViewParts{HTMLString: html}, nil, callback)
}

// Raw sends a message whose only part is the text given.
func (m *Mailer) Raw(ctx context.Context, text string, callback func(*Message)) (SentMessage, error) {
	return m.Send(ctx, ViewParts{Raw: text}, nil, callback)
}

// Plain sends a message whose only part is the named text view.
func (m *Mailer) Plain(ctx context.Context, view string, data any, callback func(*Message)) (SentMessage, error) {
	return m.Send(ctx, ViewParts{Text: view}, data, callback)
}

// Render draws the view and answers the HTML, without sending anything, which
// is what a preview route calls.
func (m *Mailer) Render(view any, data any) (string, error) {
	parts, err := parseView(view)
	if err != nil {
		return "", err
	}
	if parts.HTMLString != "" {
		return parts.HTMLString, nil
	}
	name := firstNonEmpty(parts.HTML, parts.Text)
	if name == "" {
		return parts.Raw, nil
	}
	return m.renderView(name, data)
}

// Send sends a message.
//
// view is a [Mailable], a view name, or [ViewParts]; data is what the view
// renders from; callback gets the message after it is built and before it is
// sent. All three are spelled out rather than optional, because they are of
// different types and a variadic tail cannot tell them apart.
func (m *Mailer) Send(ctx context.Context, view any, data any, callback func(*Message)) (SentMessage, error) {
	if mailable, ok := view.(Mailable); ok {
		return m.sendMailable(ctx, mailable, nil)
	}

	msg, err := m.buildFromView(view, data)
	if err != nil {
		return SentMessage{}, err
	}
	if callback != nil {
		callback(msg)
	}
	return m.sendMessage(ctx, msg, data)
}

// SendNow sends a message without ever putting it on the queue. Nothing here
// queues by itself, so it is [Mailer.Send] under a name that says so.
func (m *Mailer) SendNow(ctx context.Context, view any, data any, callback func(*Message)) (SentMessage, error) {
	return m.Send(ctx, view, data, callback)
}

// Build turns a mailable into the message it will go out as: envelope filled
// in, views rendered, attachments recorded, headers applied.
//
// It is the render half of a send, exported so that a caller can look at a
// rendered message without sending it -- which is what every assertion on
// [Message] needs.
func (m *Mailer) Build(ctx context.Context, mailable Mailable) (*Message, error) {
	return (&PendingMail{mailer: m}).Build(ctx, mailable)
}

// GetSymfonyTransport is the [Transport] this mailer sends through.
func (m *Mailer) GetSymfonyTransport() Transport { return m.transport }

// SetSymfonyTransport swaps the transport, which is what a test that wants the
// array transport for one case does.
func (m *Mailer) SetSymfonyTransport(t Transport) { m.transport = t }

// GetViewFactory is the [Renderer] this mailer draws its views with.
func (m *Mailer) GetViewFactory() Renderer { return m.views }

// SetQueue wires the queue that Queue and Later push onto.
func (m *Mailer) SetQueue(q QueueFactory) *Mailer {
	m.queue = q
	return m
}

// SetMarkdown wires the renderer that a mailable with a markdown content uses.
// A mailable that names a markdown view fails to send until one is set.
func (m *Mailer) SetMarkdown(md *Markdown) *Mailer {
	m.markdown = md
	return m
}

// Queue pushes a mailable onto the queue instead of sending it. The queue name
// is optional.
func (m *Mailer) Queue(ctx context.Context, mailable Mailable, queue ...string) (string, error) {
	return (&PendingMail{mailer: m}).Queue(ctx, mailable, queue...)
}

// OnQueue pushes a mailable onto the named queue.
func (m *Mailer) OnQueue(ctx context.Context, queue string, mailable Mailable) (string, error) {
	return m.Queue(ctx, mailable, queue)
}

// QueueOn pushes a mailable onto the named queue, and is [Mailer.OnQueue] under
// the other spelling.
func (m *Mailer) QueueOn(ctx context.Context, queue string, mailable Mailable) (string, error) {
	return m.OnQueue(ctx, queue, mailable)
}

// Later pushes a mailable onto the queue, to be sent after the delay.
func (m *Mailer) Later(ctx context.Context, delay time.Duration, mailable Mailable, queue ...string) (string, error) {
	return (&PendingMail{mailer: m}).Later(ctx, delay, mailable, queue...)
}

// LaterOn pushes a mailable onto the named queue, to be sent after the delay.
func (m *Mailer) LaterOn(ctx context.Context, queue string, delay time.Duration, mailable Mailable) (string, error) {
	return m.Later(ctx, delay, mailable, queue)
}

// sendMailable builds the mailable and hands the message to the transport.
func (m *Mailer) sendMailable(ctx context.Context, mailable Mailable, pending *PendingMail) (SentMessage, error) {
	if pending == nil {
		pending = &PendingMail{mailer: m}
	}
	msg, err := pending.Build(ctx, mailable)
	if err != nil {
		return SentMessage{}, err
	}
	return m.sendMessage(ctx, msg, mailable)
}

// sendMessage is the tail of a send: the global "to", the MessageSending veto,
// the transport, and the MessageSent announcement.
func (m *Mailer) sendMessage(ctx context.Context, msg *Message, data any) (SentMessage, error) {
	if m.to.Address != "" {
		m.setGlobalToAndRemoveCcAndBcc(msg)
	}
	if err := validate(msg); err != nil {
		return SentMessage{}, err
	}

	if !m.shouldSendMessage(ctx, msg, data) {
		return SentMessage{}, nil
	}

	sent, err := m.transport.Send(ctx, *msg)
	if err != nil {
		return SentMessage{}, err
	}
	sent.Message = msg
	m.dispatchSentEvent(ctx, sent, data)
	return sent, nil
}

func (m *Mailer) setGlobalToAndRemoveCcAndBcc(msg *Message) {
	msg.ForgetTo()
	msg.To = []Address{m.to}
	msg.ForgetCC()
	msg.ForgetBCC()
}

func (m *Mailer) shouldSendMessage(ctx context.Context, msg *Message, data any) bool {
	if m.events == nil {
		return true
	}
	return m.events.Until(ctx, MessageSending{Message: msg, Data: data})
}

func (m *Mailer) dispatchSentEvent(ctx context.Context, sent SentMessage, data any) {
	if m.events == nil {
		return
	}
	m.events.Dispatch(ctx, MessageSent{Sent: sent, Data: data})
}

func (m *Mailer) buildFromView(view any, data any) (*Message, error) {
	parts, err := parseView(view)
	if err != nil {
		return nil, err
	}

	msg := m.createMessage()
	if err := m.addContent(msg, parts, data); err != nil {
		return nil, err
	}
	return msg, nil
}

// createMessage is a message that already carries the mailer's global sender,
// reply-to and return path.
func (m *Mailer) createMessage() *Message {
	msg := &Message{mailerName: m.name}
	if m.from.Address != "" {
		msg.From = m.from
	}
	if m.replyTo.Address != "" {
		msg.ReplyTo = append(msg.ReplyTo, m.replyTo)
	}
	if m.returnPath != "" {
		msg.ReturnPath(m.returnPath)
	}
	return msg
}

func (m *Mailer) addContent(msg *Message, parts ViewParts, data any) error {
	if parts.HTMLString != "" {
		msg.HTML = parts.HTMLString
	} else if parts.HTML != "" {
		html, err := m.renderView(parts.HTML, data)
		if err != nil {
			return err
		}
		msg.HTML = html
	}
	if parts.Text != "" {
		text, err := m.renderView(parts.Text, data)
		if err != nil {
			return err
		}
		msg.Text = text
	}
	if parts.Raw != "" {
		msg.Text = parts.Raw
	}
	return nil
}

func (m *Mailer) renderView(name string, data any) (string, error) {
	if m.views == nil {
		return "", fmt.Errorf("mail: no view factory is wired, cannot render %q", name)
	}
	out, err := m.views.RenderToString(name, data)
	if err != nil {
		return "", fmt.Errorf("mail: rendering %s: %w", name, err)
	}
	return out, nil
}

// parseView reads whatever Send accepts for a view into [ViewParts].
func parseView(view any) (ViewParts, error) {
	switch v := view.(type) {
	case string:
		return ViewParts{HTML: v}, nil
	case ViewParts:
		return v, nil
	case *ViewParts:
		if v == nil {
			return ViewParts{}, errors.New("mail: invalid view")
		}
		return *v, nil
	case []string:
		// The positional form: the HTML view, then the text one.
		out := ViewParts{}
		if len(v) > 0 {
			out.HTML = v[0]
		}
		if len(v) > 1 {
			out.Text = v[1]
		}
		return out, nil
	default:
		return ViewParts{}, errors.New("mail: invalid view")
	}
}

// validate is what stops a message that cannot arrive from reaching a provider.
//
// Left to the transport, all three answer with a bounce or a 422 minutes later,
// in a log nobody is reading. They are checked here because every one of them
// is a typo somebody makes at the call site.
func validate(msg *Message) error {
	if msg.err != nil {
		return msg.err
	}
	if len(msg.To)+len(msg.CC)+len(msg.BCC) == 0 {
		return ErrNoRecipient
	}
	for _, a := range append(append(append([]Address{msg.From}, msg.To...), msg.CC...), msg.BCC...) {
		if !a.Valid() {
			return fmt.Errorf("mail: %q is not an address", a.Address)
		}
	}
	if strings.TrimSpace(msg.Subject) == "" {
		return errors.New("mail: the envelope has no subject")
	}
	if msg.HTML == "" && msg.Text == "" {
		return errors.New("mail: the message has no body")
	}
	return nil
}

// subjectFor is the subject a mailable that names none goes out with: its type
// name, humanised. An OrderShipped goes out as "Order Shipped".
func subjectFor(mailable Mailable) string {
	t := reflect.TypeOf(mailable)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Name() == "" {
		return ""
	}
	return str.Title(str.Snake(t.Name(), " "))
}
