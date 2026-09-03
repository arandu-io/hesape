package mail

import (
	"errors"
	"fmt"
	"strings"
)

// Headers is the three header fields a mailable is allowed to set by hand.
//
// There are no fluent setters here, for the reason given on [Content].
// [Headers.ReferencesString] has no field to collide with and is a method.
//
// A name or a value carrying a carriage return or a line feed is refused before
// the message is rendered: a header line ends at CRLF, so such a value is a
// second header written by whoever supplied it. See [Headers.Check].
type Headers struct {
	// MessageID is the Message-Id this message goes out with.
	MessageID string

	// References are the message IDs this message answers, which is what makes
	// a mail client thread it under the original.
	References []string

	// Text is every other header, by name.
	Text map[string]string
}

// ReferencesString is the References header as one field value: each id wrapped
// in angle brackets, separated by spaces.
func (h Headers) ReferencesString() string {
	out := make([]string, 0, len(h.References))
	for _, id := range h.References {
		if !strings.HasPrefix(id, "<") {
			id = "<" + id
		}
		if !strings.HasSuffix(id, ">") {
			id += ">"
		}
		out = append(out, id)
	}
	return strings.Join(out, " ")
}

// ErrHeaderInjection is what every refusal from a header check wraps, so a
// caller can tell a smuggled line apart from a missing recipient with errors.Is
// rather than by reading the message.
var ErrHeaderInjection = errors.New("mail: a header value carries a character that ends it early")

// HeaderError is the refusal: which field carried the character, and what it
// held.
//
// The field is a separate string because the message this ends up in is a log
// line somebody greps, and "Reply-To" is the part they grep for.
type HeaderError struct {
	// Field is the name of the header, or of the envelope field, that is unsafe.
	Field string

	// Value is what it held, quoted so that a line break in it stays inside one
	// log line.
	Value string
}

// Error renders the refusal, naming the field first.
func (e *HeaderError) Error() string {
	return fmt.Sprintf("%s: %s = %q", ErrHeaderInjection.Error(), e.Field, e.Value)
}

// Unwrap returns [ErrHeaderInjection].
func (e *HeaderError) Unwrap() error { return ErrHeaderInjection }

// Check refuses headers whose names or values carry a carriage return or a line
// feed.
//
// A header line ends at CRLF, so a value holding one is two headers. The second
// is written by whoever supplied the value -- a Bcc to an address nobody chose,
// a Content-Type that turns the body into something else -- and the message
// goes out looking exactly as it should. There is no escaping to fall back on:
// the field is refused, because a value that had to be rewritten to be safe is
// not the value the caller meant.
//
// The subject and the addresses are on the envelope and are refused by
// [Envelope.Check].
func (h Headers) Check() error {
	if err := checkHeaderValue("Message-Id", h.MessageID); err != nil {
		return err
	}
	for _, id := range h.References {
		if err := checkHeaderValue("References", id); err != nil {
			return err
		}
	}
	for name, value := range h.Text {
		// The name as well as the value: it is written to the line before the
		// colon and nothing encodes it on the way.
		if err := checkHeaderValue("Headers.Text name", name); err != nil {
			return err
		}
		if err := checkHeaderValue(name, value); err != nil {
			return err
		}
	}
	return nil
}

// checkHeaderValue refuses one field whose value would end the header line
// early.
//
// It checks for the two characters rather than for the pair, because a bare
// line feed ends a line for most receivers and a bare carriage return ends one
// for some -- and a value that carries either was not written to be a header
// value.
func checkHeaderValue(field, value string) error {
	if !strings.ContainsAny(value, "\r\n") {
		return nil
	}
	return &HeaderError{Field: field, Value: value}
}

// checkHeaderParameter refuses one field written into a header line that
// carries quoted parameters, which is the line an attachment's name and content
// type share.
//
// It refuses two characters more than [checkHeaderValue] does, because two more
// end a value early here. A double quote closes the parameter it sits inside,
// so a name holding one is a name followed by whatever the sender wrote after
// it -- reason enough on its own, with no line break needed. A backslash
// escapes the character after it, so a name ending in one escapes the closing
// quote and swallows the rest of the line.
//
// Neither is in a filename anybody meant to send: a quote and a backslash are
// already refused by the filesystems these names come from.
func checkHeaderParameter(field, value string) error {
	if err := checkHeaderValue(field, value); err != nil {
		return err
	}
	if !strings.ContainsAny(value, `"\`) {
		return nil
	}
	return &HeaderError{Field: field, Value: value}
}
