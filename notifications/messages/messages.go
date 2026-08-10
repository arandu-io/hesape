// Package messages is what a notification looks like on a channel.
//
// It mirrors Illuminate\Notifications\Messages. The files it answers to, in the
// clone at laravel_illuminate/notifications/Messages:
//
//	SimpleMessage.php     -> Mail (the lines, the action, the greeting)
//	MailMessage.php       -> Mail
//	DatabaseMessage.php   -> Database
//	BroadcastMessage.php  -> Broadcast
//	Action.php            -> Action
//
// A message is data. It does not know how to reach anybody and holds no
// transport, which is what lets a test build one and read it, and what lets the
// same Mail be rendered to HTML by the view layer and to text by PlainText
// without either of them being the authority.
//
// # Why Mail is structured rather than a body
//
// Laravel builds the HTML from a markdown template and a theme. RULE 13 rules
// the theme out -- it is a second asset pipeline -- and RULE 9 rules out having
// both a markdown path and a view path to draw the same message. So a mail
// notification carries a greeting, some lines, at most one action and a
// salutation: the view draws the HTML from those fields, PlainText renders the
// text part from the same fields, and the two cannot disagree about what the
// message said.
//
// A message that needs a layout of its own is not a notification. It is a
// Mailable, and hesape/mail is where those live.
package messages

import (
	"encoding/json"
	"errors"
	"fmt"
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
type Action struct {
	// Text is what the button says: "Reset password". An imperative verb, not
	// "click here".
	Text string
	// URL is where it goes. Absolute, because it is opened from a mail client
	// that has no idea what host the application is on.
	URL string
}

// Empty reports whether there is no action.
func (a Action) Empty() bool { return a.Text == "" || a.URL == "" }

// Mail is a notification as an e-mail.
//
// Build it with the chaining helpers, which return a copy:
//
//	messages.Mail{Subject: "Your invoice is paid"}.
//		Greet("Hello Ada").
//		Line("We received your payment for invoice 2026-114.").
//		Act("View invoice", url).
//		Line("Thank you for your business.")
//
// A line added after Act lands after the button, which is where a closing
// sentence belongs, and is the same rule Illuminate's SimpleMessage follows.
type Mail struct {
	// Level is the tone. The zero value reads as LevelInfo.
	Level Level
	// Subject is the subject line. Required: a message with no subject is
	// filed unread.
	Subject string
	// Greeting opens the message: "Hello Ada". Empty means the layout supplies
	// its own.
	Greeting string
	// Salutation closes it: "Regards, the Arandu team". Empty, likewise.
	Salutation string
	// Intro is the lines before the action.
	Intro []string
	// Action is the button, if there is one.
	Action Action
	// Outro is the lines after it.
	Outro []string
	// Locale is the language to render in, taken from the recipient by the
	// channel. Empty means the application default.
	Locale string
	// Tags and Metadata are carried by the transports that support them, so a
	// provider dashboard can tell password resets from invoices.
	Tags     []string
	Metadata map[string]string
}

// Greet sets the opening line.
func (m Mail) Greet(greeting string) Mail {
	m.Greeting = greeting
	return m
}

// Close sets the closing line.
func (m Mail) Close(salutation string) Mail {
	m.Salutation = salutation
	return m
}

// Line adds a paragraph -- before the action if there is not one yet, after it
// if there is.
func (m Mail) Line(text string) Mail {
	if m.Action.Empty() {
		m.Intro = append(append([]string(nil), m.Intro...), text)
		return m
	}
	m.Outro = append(append([]string(nil), m.Outro...), text)
	return m
}

// Act sets the button. Calling it twice replaces the first one, because there
// is only ever one.
func (m Mail) Act(text, url string) Mail {
	m.Action = Action{Text: text, URL: url}
	return m
}

// Success and Failure set the tone.
func (m Mail) Success() Mail { m.Level = LevelSuccess; return m }

// Failure marks the message as bad news. It is not called Error, because a
// method called Error on a value makes it look like it implements the error
// interface.
func (m Mail) Failure() Mail { m.Level = LevelError; return m }

// Tone is the level with the zero value resolved.
func (m Mail) Tone() Level {
	if m.Level == "" {
		return LevelInfo
	}
	return m.Level
}

// Validate reports what would make this message useless to receive.
//
// It is checked by the channel before a transport is touched, so a missing
// subject fails at the send rather than as an unread message three days later.
func (m Mail) Validate() error {
	if strings.TrimSpace(m.Subject) == "" {
		return errors.New("notifications: the mail message has no subject")
	}
	if len(m.Intro) == 0 && len(m.Outro) == 0 && m.Action.Empty() {
		return errors.New("notifications: the mail message has no body")
	}
	if !m.Action.Empty() && !strings.Contains(m.Action.URL, "://") {
		return fmt.Errorf("notifications: the action link %q is not absolute, and a mail client has no host to resolve it against", m.Action.URL)
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
	if m.Greeting != "" {
		write(m.Greeting)
	}
	for _, line := range m.Intro {
		write(line)
	}
	if !m.Action.Empty() {
		write(m.Action.Text + ": " + m.Action.URL)
	}
	for _, line := range m.Outro {
		write(line)
	}
	if m.Salutation != "" {
		write(m.Salutation)
	}
	b.WriteString("\n")
	return b.String()
}

// Database is a notification as a stored row.
//
// Data is any value rather than a map, because a bell menu renders fields and a
// typed struct is how a field that was renamed becomes a compile error instead
// of an empty span.
type Database struct {
	Data any
}

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
type Broadcast struct {
	// Event is the name the client listens for. Empty means the channel uses
	// the notification's Key, which is what most of them want.
	Event string
	// Data is the payload, encoded as JSON on the way out.
	Data any
}

// JSON is what goes over the wire.
func (b Broadcast) JSON() (json.RawMessage, error) {
	if b.Data == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(b.Data)
	if err != nil {
		return nil, fmt.Errorf("notifications: encoding the broadcast payload: %w", err)
	}
	return raw, nil
}
