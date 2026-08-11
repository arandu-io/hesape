package mail

import "strings"

// Headers is Illuminate\Mail\Mailables\Headers: the three header fields a
// mailable is allowed to set by hand.
//
// The fluent setters messageId(), references() and text() have no Go twin for
// the reason given on [Content]: PHP lets a property and a method share a name.
// ReferencesString has no property to collide with and is a method.
type Headers struct {
	// MessageID is the Message-Id this message goes out with. Illuminate spells
	// the property messageId; Go spells the initialism in capitals (ADR 0044).
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
