package mail

// Envelope is Illuminate\Mail\Mailables\Envelope: who a message is from, who it
// is to, and what it says it is.
//
// # Why the fields and not the fluent setters
//
// Illuminate's Envelope carries both a public property and a fluent setter of
// the same name -- $envelope->to and $envelope->to(...) -- which PHP allows and
// Go does not. A Go struct literal is PHP's named arguments, which is the form
// Illuminate's own documentation uses to build an envelope, so the property
// wins: from, to, cc, bcc, replyTo, subject, tags and using are fields here.
// Tag, and every has* query, have no field to collide with and are methods.
type Envelope struct {
	// From is the address sending the message. The zero value means the
	// Mailer's alwaysFrom.
	From Address

	To      []Address
	CC      []Address
	BCC     []Address
	ReplyTo []Address

	Subject string

	// Tags and Metadata are carried by the transports that support them and
	// dropped by the ones that do not. They are how a provider's dashboard
	// groups "password resets" apart from "invoices".
	Tags     []string
	Metadata map[string]string

	// Using is Illuminate's $using: the callbacks that get the message after it
	// is built and before it is sent, for the header no field covers.
	Using []func(*Message)
}

// NewEnvelope is Illuminate's Envelope::__construct. Every argument there is
// optional and named, which in Go is a struct literal, so this exists only for
// the string-or-Address polymorphism of the recipient arguments.
func NewEnvelope(from any, to, cc, bcc, replyTo any, subject string) Envelope {
	e := Envelope{Subject: subject}
	if list := addressesOf(from); len(list) > 0 {
		e.From = list[0]
	}
	e.To = addressesOf(to)
	e.CC = addressesOf(cc)
	e.BCC = addressesOf(bcc)
	e.ReplyTo = addressesOf(replyTo)
	return e
}

// Tag adds one tag to the message, and is Illuminate's Envelope::tag().
func (e *Envelope) Tag(tag string) *Envelope {
	e.Tags = append(e.Tags, tag)
	return e
}

// IsFrom reports whether the message is from the given address. A name given
// has to match too.
//
// IsFrom is Envelope::isFrom.
func (e Envelope) IsFrom(address string, name ...string) bool {
	if e.From.Address != address {
		return false
	}
	if len(name) == 0 {
		return true
	}
	return e.From.Name == name[0]
}

// HasTo reports whether the address is a recipient.
//
// HasTo is Envelope::hasTo.
func (e Envelope) HasTo(address string, name ...string) bool {
	return hasAddress(e.To, address, name...)
}

// HasCC reports whether the address is a carbon copy recipient.
//
// Illuminate spells it hasCc; Go spells an initialism in capitals (ADR 0044).
//
// HasCC is Envelope::hasCc.
func (e Envelope) HasCC(address string, name ...string) bool {
	return hasAddress(e.CC, address, name...)
}

// HasBCC reports whether the address is a blind carbon copy recipient.
//
// Illuminate spells it hasBcc; Go spells an initialism in capitals (ADR 0044).
//
// HasBCC is Envelope::hasBcc.
func (e Envelope) HasBCC(address string, name ...string) bool {
	return hasAddress(e.BCC, address, name...)
}

// HasReplyTo reports whether the address is a "reply to" recipient.
//
// HasReplyTo is Envelope::hasReplyTo.
func (e Envelope) HasReplyTo(address string, name ...string) bool {
	return hasAddress(e.ReplyTo, address, name...)
}

// HasSubject reports whether the message has exactly this subject.
//
// HasSubject is Envelope::hasSubject.
func (e Envelope) HasSubject(subject string) bool { return e.Subject == subject }

// HasMetadata reports whether the message carries this key with this value.
//
// HasMetadata is Envelope::hasMetadata.
func (e Envelope) HasMetadata(key, value string) bool {
	got, ok := e.Metadata[key]
	return ok && got == value
}

// HasTag reports whether the message carries this tag. Illuminate reads
// $envelope->tags directly at the one place it needs this; the query is here so
// that Message can ask an envelope it did not build.
//
// HasTag is Mailable::hasTag, which is the one place PHP asks this and which
// reads $envelope->tags directly.
func (e Envelope) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
