package messages

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// Level is the tone of a mail message, and the only styling decision a
// notification gets to make.
//
// Three values rather than a colour, because a notification is written once and
// rendered by whatever the application's layout is: "this is bad news" survives
// a redesign and "#dc2626" does not.
type Level string

// The three tones.
const (
	// LevelInfo is the default: something happened.
	LevelInfo Level = "info"
	// LevelSuccess is something the recipient was waiting for.
	LevelSuccess Level = "success"
	// LevelError is something that went wrong and that they have to know
	// about.
	LevelError Level = "error"
)

// Action is the one button a notification may have.
//
// One, not a list. A message with three buttons is a message with no call to
// action, and the recipient answers it by doing nothing.
//
// It is Illuminate\Notifications\Action, which lives at the root of the
// notifications namespace there and here sits with the messages it belongs to.
type Action struct {
	// Text is what the button says: "Reset password". An imperative verb, not
	// "click here".
	Text string
	// URL is where it goes. Absolute, because it is opened from a mail client
	// that has no idea what host the application is on.
	URL string
}

// NewAction returns the button.
func NewAction(text, url string) Action { return Action{Text: text, URL: url} }

// Empty reports whether there is no action.
func (a Action) Empty() bool { return a.Text == "" || a.URL == "" }

// Address is one recipient of a copy of the message.
type Address struct {
	// Address is the e-mail address. Name is the display name, and may be
	// empty.
	Address string
	Name    string
}

// Attachment is a file sent with the message.
//
// File is a path the mailer can open, or empty when the bytes are in Data --
// Illuminate has attach() and attachData() for the same two cases, and they
// land in one type here because a mailer sends them the same way.
type Attachment struct {
	File string
	Name string
	Data []byte
	// MIME is the content type. Empty lets the mailer decide.
	MIME string
}

// MessageCallback is handed the headers of the outgoing message, so an
// application can add one the message type has no field for.
type MessageCallback func(headers map[string]string)

// Mail is a notification as an e-mail.
//
// It is Illuminate's SimpleMessage and MailMessage in one type: the split there
// exists so that a channel with no view layer can reuse the lines, and there is
// only one mail channel here.
//
// Build it with the chaining helpers, each of which returns a copy:
//
//	messages.NewMail().
//		Subject("Your invoice is paid").
//		Greeting("Hello Ada").
//		Line("We received your payment for invoice 2026-114.").
//		Action("View invoice", url).
//		Line("Thank you for your business.")
//
// A line added after Action lands after the button, which is where a closing
// sentence belongs, and is the rule Illuminate's SimpleMessage::with follows.
//
// # Why the fields are not spelled like the methods
//
// In PHP a class can have a property $subject and a method subject(); in Go the
// fields and the methods of a type share one namespace. Every method here keeps
// Illuminate's name, because the method is what somebody types, and the field it
// writes says what it holds: SubjectLine, GreetingLine, SalutationLine,
// LevelName, MailerName, ViewName, TextView, MarkdownView, ThemeName,
// FromAddress, ReplyToAddresses, CCAddresses, BCCAddresses, MetadataHeaders and
// PriorityLevel. The fields Illuminate has no method for keep its own spelling:
// IntroLines, OutroLines, ActionText, ActionURL, Attachments, RawAttachments,
// Tags, ViewData and Callbacks.
type Mail struct {
	// LevelName is the tone. The zero value reads as LevelInfo.
	LevelName Level
	// SubjectLine is the subject. Required: a message with no subject is filed
	// unread.
	SubjectLine string
	// GreetingLine opens the message: "Hello Ada". Empty means the layout
	// supplies its own.
	GreetingLine string
	// SalutationLine closes it: "Regards, the Arandu team". Empty, likewise.
	SalutationLine string
	// IntroLines is the lines before the action, OutroLines the ones after it.
	IntroLines []string
	OutroLines []string
	// ActionText and ActionURL are the button.
	ActionText string
	ActionURL  string
	// MailerName is which configured mailer sends it. Empty is the default one.
	MailerName string
	// ViewName, TextView and MarkdownView name a template for the body.
	//
	// They are names handed to the view layer, never a second asset pipeline: a
	// notification that names none is rendered from the lines above by Render
	// and PlainText, which is the path RULE 13 leaves open and the one every
	// notification in this collection takes.
	ViewName     string
	TextView     string
	MarkdownView string
	// ThemeName is which of the application's mail themes to render under.
	ThemeName string
	// ViewData is what a named template is rendered with, merged over ToArray.
	ViewData map[string]any
	// FromAddress overrides the application's default sender.
	FromAddress Address
	// ReplyToAddresses, CCAddresses and BCCAddresses are the other people on
	// the message.
	ReplyToAddresses []Address
	CCAddresses      []Address
	BCCAddresses     []Address
	// Attachments is the files sent with it. RawAttachments is the ones whose
	// bytes are in hand rather than on disk.
	Attachments    []Attachment
	RawAttachments []Attachment
	// Tags and MetadataHeaders are carried by the transports that support them,
	// so a provider dashboard can tell password resets from invoices.
	Tags            []string
	MetadataHeaders map[string]string
	// PriorityLevel is 1 (highest) to 5 (lowest). Zero leaves the header off.
	PriorityLevel int
	// Callbacks are handed the outgoing headers just before the message goes.
	Callbacks []MessageCallback
	// Locale is the language to render in, taken from the recipient by the
	// channel. Empty means the application default.
	Locale string
}

// NewMail returns an empty message.
func NewMail() Mail { return Mail{} }

// Level sets the tone.
func (m Mail) Level(level Level) Mail {
	m.LevelName = level
	return m
}

// Success marks the message as good news.
func (m Mail) Success() Mail { return m.Level(LevelSuccess) }

// Error marks the message as bad news.
//
// The name is Illuminate's. It does not make Mail an error value: the method
// takes no arguments and returns a Mail, so nothing satisfies the error
// interface by accident.
func (m Mail) Error() Mail { return m.Level(LevelError) }

// Subject sets the subject line.
func (m Mail) Subject(subject string) Mail {
	m.SubjectLine = subject
	return m
}

// Greeting sets the opening line.
func (m Mail) Greeting(greeting string) Mail {
	m.GreetingLine = greeting
	return m
}

// Salutation sets the closing line.
func (m Mail) Salutation(salutation string) Mail {
	m.SalutationLine = salutation
	return m
}

// Line adds a paragraph -- before the action if there is not one yet, after it
// if there is.
func (m Mail) Line(line string) Mail { return m.With(line) }

// LineIf adds the paragraph when the condition holds.
func (m Mail) LineIf(ok bool, line string) Mail {
	if !ok {
		return m
	}
	return m.Line(line)
}

// Lines adds several paragraphs, in order.
func (m Mail) Lines(lines ...string) Mail {
	for _, line := range lines {
		m = m.Line(line)
	}
	return m
}

// LinesIf adds them when the condition holds.
func (m Mail) LinesIf(ok bool, lines ...string) Mail {
	if !ok {
		return m
	}
	return m.Lines(lines...)
}

// With adds a paragraph, collapsing the whitespace in it.
//
// It is what Line calls, exactly as Illuminate's line() calls with(): a line
// written across three source lines of a Go raw string arrives as one sentence
// rather than as three with the indentation in the middle.
func (m Mail) With(line string) Mail {
	line = formatLine(line)
	if m.ActionText == "" {
		m.IntroLines = append(append([]string(nil), m.IntroLines...), line)
		return m
	}
	m.OutroLines = append(append([]string(nil), m.OutroLines...), line)
	return m
}

// Action sets the button. Calling it twice replaces the first one, because
// there is only ever one.
func (m Mail) Action(text, url string) Mail {
	m.ActionText, m.ActionURL = text, url
	return m
}

// Mailer names which configured mailer sends the message.
func (m Mail) Mailer(mailer string) Mail {
	m.MailerName = mailer
	return m
}

// View names the template the body is rendered from, with the data it is
// rendered with.
//
// It clears the markdown template, as Illuminate's does: a message has one
// body, and two ways to draw it is the thing RULE 9 refuses.
func (m Mail) View(view string, data map[string]any) Mail {
	m.ViewName = view
	m.ViewData = data
	m.MarkdownView = ""
	return m
}

// Text names the template the plain text part is rendered from.
func (m Mail) Text(view string, data map[string]any) Mail {
	m.TextView = view
	if data != nil {
		m.ViewData = data
	}
	m.MarkdownView = ""
	return m
}

// Markdown names the markdown template the body is rendered from, and clears
// the view.
func (m Mail) Markdown(view string, data map[string]any) Mail {
	m.MarkdownView = view
	m.ViewData = data
	m.ViewName = ""
	return m
}

// Template sets the markdown template without touching the data.
func (m Mail) Template(template string) Mail {
	m.MarkdownView = template
	return m
}

// Theme sets which of the application's mail themes to render under.
func (m Mail) Theme(theme string) Mail {
	m.ThemeName = theme
	return m
}

// From sets the sender.
func (m Mail) From(address, name string) Mail {
	m.FromAddress = Address{Address: address, Name: name}
	return m
}

// ReplyTo adds an address replies go to.
func (m Mail) ReplyTo(address, name string) Mail {
	m.ReplyToAddresses = append(append([]Address(nil), m.ReplyToAddresses...), Address{Address: address, Name: name})
	return m
}

// CC adds an address that gets a visible copy.
//
// Illuminate spells it cc(); the initialism goes to upper case because Go says
// so (ADR 0044).
func (m Mail) CC(address, name string) Mail {
	m.CCAddresses = append(append([]Address(nil), m.CCAddresses...), Address{Address: address, Name: name})
	return m
}

// BCC adds an address that gets a copy nobody else sees.
func (m Mail) BCC(address, name string) Mail {
	m.BCCAddresses = append(append([]Address(nil), m.BCCAddresses...), Address{Address: address, Name: name})
	return m
}

// Attach sends a file with the message.
func (m Mail) Attach(file string, options Attachment) Mail {
	options.File = file
	m.Attachments = append(append([]Attachment(nil), m.Attachments...), options)
	return m
}

// AttachMany sends several files.
func (m Mail) AttachMany(files ...Attachment) Mail {
	m.Attachments = append(append([]Attachment(nil), m.Attachments...), files...)
	return m
}

// AttachData sends bytes that are already in hand, under the given name.
func (m Mail) AttachData(data []byte, name string, options Attachment) Mail {
	options.Data, options.Name = data, name
	m.RawAttachments = append(append([]Attachment(nil), m.RawAttachments...), options)
	return m
}

// Tag adds a tag header, for the transports that report on one.
func (m Mail) Tag(value string) Mail {
	m.Tags = append(append([]string(nil), m.Tags...), value)
	return m
}

// Metadata adds a metadata header, for the transports that carry them.
func (m Mail) Metadata(key, value string) Mail {
	out := make(map[string]string, len(m.MetadataHeaders)+1)
	for k, v := range m.MetadataHeaders {
		out[k] = v
	}
	out[key] = value
	m.MetadataHeaders = out
	return m
}

// Priority sets how urgent the message claims to be: 1 is the highest and 5 the
// lowest, which is the scale the header uses and the one Illuminate documents.
func (m Mail) Priority(level int) Mail {
	m.PriorityLevel = level
	return m
}

// WithSymfonyMessage registers a callback handed the outgoing headers.
//
// The name is Illuminate's, and Symfony is the mailer PHP uses. There is no
// Symfony here: what the callback receives is the header map the mail transport
// is about to send, which is what the PHP callback reaches for in practice. The
// name is kept so that the escape hatch is where a Laravel developer looks for
// it (ADR 0044).
func (m Mail) WithSymfonyMessage(fn MessageCallback) Mail {
	m.Callbacks = append(append([]MessageCallback(nil), m.Callbacks...), fn)
	return m
}

// Tone is the level with the zero value resolved.
func (m Mail) Tone() Level {
	if m.LevelName == "" {
		return LevelInfo
	}
	return m.LevelName
}

// ToArray is the message as the data a template is rendered with.
//
// The keys are Illuminate's, in snake_case rather than camelCase because that
// is the case the rest of the collection puts on the wire.
func (m Mail) ToArray() map[string]any {
	return map[string]any{
		"level":       string(m.Tone()),
		"subject":     m.SubjectLine,
		"greeting":    m.GreetingLine,
		"salutation":  m.SalutationLine,
		"intro_lines": append([]string(nil), m.IntroLines...),
		"outro_lines": append([]string(nil), m.OutroLines...),
		"action_text": m.ActionText,
		"action_url":  m.ActionURL,
		// The URL as it should be shown to a person: a mailto: or tel: link
		// reads as an address, not as a scheme.
		"displayable_action_url": displayable(m.ActionURL),
	}
}

// Data is ToArray with the view data merged over it, which is what a template
// is rendered with.
func (m Mail) Data() map[string]any {
	out := m.ToArray()
	for k, v := range m.ViewData {
		out[k] = v
	}
	return out
}

// Validate reports what would make this message useless to receive.
//
// It is checked by the channel before a transport is touched, so a missing
// subject fails at the send rather than as an unread message three days later.
func (m Mail) Validate() error {
	if strings.TrimSpace(m.SubjectLine) == "" {
		return errors.New("notifications: the mail message has no subject")
	}
	if len(m.IntroLines) == 0 && len(m.OutroLines) == 0 && m.ActionText == "" &&
		m.ViewName == "" && m.MarkdownView == "" {
		return errors.New("notifications: the mail message has no body")
	}
	if m.ActionText != "" && !strings.Contains(m.ActionURL, "://") {
		return fmt.Errorf("notifications: the action link %q is not absolute, and a mail client has no host to resolve it against", m.ActionURL)
	}
	return nil
}

// PlainText renders the text part.
//
// It is a function over the fields rather than a template, which is the whole
// argument for the message being structured: there is no second language to
// learn, no theme to publish, and the text part cannot fall out of step with
// the HTML because both are drawn from these fields.
//
// The action becomes a line with the URL on it, because that is the only way a
// button survives in text.
func (m Mail) PlainText() string {
	var b strings.Builder
	write := func(s string) {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s)
	}
	if m.GreetingLine != "" {
		write(m.GreetingLine)
	}
	for _, line := range m.IntroLines {
		write(line)
	}
	if m.ActionText != "" {
		write(m.ActionText + ": " + m.ActionURL)
	}
	for _, line := range m.OutroLines {
		write(line)
	}
	if m.SalutationLine != "" {
		write(m.SalutationLine)
	}
	b.WriteString("\n")
	return b.String()
}

// Render is the HTML part.
//
// Illuminate renders a markdown template under a theme, which is a second
// asset pipeline and a second way to draw a body (RULE 9, RULE 13). Here the
// same fields that PlainText reads are written into a fragment the application's
// mail layout wraps: one source of truth, no theme to publish, and nothing to
// build.
//
// A message that names a ViewName or a MarkdownView is not rendered here: the
// view layer owns those, and Render says so rather than quietly ignoring them.
func (m Mail) Render() (string, error) {
	if m.ViewName != "" || m.MarkdownView != "" {
		return "", fmt.Errorf("notifications: this message names the template %q, and the view layer is what renders it", firstOf(m.ViewName, m.MarkdownView))
	}
	var b strings.Builder
	if err := bodyTemplate.Execute(&b, m); err != nil {
		return "", fmt.Errorf("notifications: rendering the mail body: %w", err)
	}
	return b.String(), nil
}

// bodyTemplate is the fragment Render produces. It is html/template, so every
// line the application put in the message is escaped on the way out.
var bodyTemplate = template.Must(template.New("mail").Parse(
	`{{with .GreetingLine}}<p class="greeting">{{.}}</p>
{{end}}{{range .IntroLines}}<p>{{.}}</p>
{{end}}{{if .ActionText}}<p class="action"><a href="{{.ActionURL}}" class="button button-{{.Tone}}">{{.ActionText}}</a></p>
{{end}}{{range .OutroLines}}<p>{{.}}</p>
{{end}}{{with .SalutationLine}}<p class="salutation">{{.}}</p>
{{end}}`))

// formatLine collapses the whitespace in a line, as Illuminate's formatLine
// does: a paragraph written across several source lines is one paragraph.
func formatLine(line string) string { return strings.Join(strings.Fields(line), " ") }

// displayable strips the scheme from a link that is not a web address, so that
// a mailto: shows as the address and a tel: as the number.
func displayable(url string) string {
	for _, scheme := range []string{"mailto:", "tel:"} {
		url = strings.ReplaceAll(url, scheme, "")
	}
	return url
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Database is a notification as a stored row.
//
// It is Illuminate's DatabaseMessage. Data is any value rather than a map,
// because a bell menu renders fields and a typed struct is how a field that was
// renamed becomes a compile error instead of an empty span.
type Database struct {
	Data any
}

// NewDatabase returns the message.
func NewDatabase(data any) Database { return Database{Data: data} }

// JSON is what goes in the data column.
func (d Database) JSON() (json.RawMessage, error) {
	if d.Data == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(d.Data)
	if err != nil {
		return nil, fmt.Errorf("notifications: encoding the database payload: %w", err)
	}
	return raw, nil
}

// Broadcast is a notification pushed to a browser that is connected right now.
//
// It is Illuminate's BroadcastMessage. Payload is what Illuminate calls $data;
// the field moved aside so that the Data setter could keep its name, for the
// reason Mail's fields give.
type Broadcast struct {
	// Event is the name the client listens for. Empty means the channel uses
	// the notification's Key, which is what most of them want.
	Event string
	// Payload is what goes over the wire, encoded as JSON.
	Payload any
	// Connection and Queue are which queue the broadcast goes out on, for an
	// application that pushes through one. Illuminate gets them from the
	// Queueable trait on the message.
	Connection string
	Queue      string
}

// NewBroadcast returns the message.
func NewBroadcast(data any) Broadcast { return Broadcast{Payload: data} }

// Data sets what goes over the wire.
func (b Broadcast) Data(data any) Broadcast {
	b.Payload = data
	return b
}

// OnConnection and OnQueue say where the broadcast is queued, for an
// application that does not push it inline.
func (b Broadcast) OnConnection(connection string) Broadcast {
	b.Connection = connection
	return b
}

// OnQueue names the queue the broadcast goes out on.
func (b Broadcast) OnQueue(queue string) Broadcast {
	b.Queue = queue
	return b
}

// JSON is what goes over the wire.
func (b Broadcast) JSON() (json.RawMessage, error) {
	if b.Payload == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(b.Payload)
	if err != nil {
		return nil, fmt.Errorf("notifications: encoding the broadcast payload: %w", err)
	}
	return raw, nil
}
