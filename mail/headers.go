package mail

import "strings"

// Headers is the three header fields a mailable is allowed to set by hand.
//
// There are no fluent setters here, for the reason given on [Content].
// [Headers.ReferencesString] has no field to collide with and is a method.
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
