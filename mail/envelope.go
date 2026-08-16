package mail

// Envelope is who a message is from, who it is to, and what it says it is.
//
// # Why the fields and not the fluent setters
//
// A Go type cannot carry a method and a field of the same name, and a struct
// literal is how an envelope is built, so the field wins: From, To, CC, BCC,
// ReplyTo, Subject, Tags and Using are fields. [Envelope.Tag] and every Has
// query have no field to collide with and are methods.
type Envelope struct {
	// From is the address sending the message. The zero value means whatever
	// [Mailer.AlwaysFrom] set.
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

	// Using are the callbacks that get the message after it is built and before
	// it is sent, for the header no field covers.
	Using []func(*Message)
}

// NewEnvelope builds an envelope whose addresses may be given as strings, as
// [Address] values, or as slices of either. A struct literal does everything
// else this would; the polymorphism is the reason it exists.
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

// Tag adds one tag to the message.
func (e *Envelope) Tag(tag string) *Envelope {
	e.Tags = append(e.Tags, tag)
	return e
}

// IsFrom reports whether the message is from the given address. A name given
// has to match too.
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
func (e Envelope) HasTo(address string, name ...string) bool {
	return hasAddress(e.To, address, name...)
}

// HasCC reports whether the address is a carbon copy recipient.
func (e Envelope) HasCC(address string, name ...string) bool {
	return hasAddress(e.CC, address, name...)
}

// HasBCC reports whether the address is a blind carbon copy recipient.
func (e Envelope) HasBCC(address string, name ...string) bool {
	return hasAddress(e.BCC, address, name...)
}

// HasReplyTo reports whether the address is a "reply to" recipient.
func (e Envelope) HasReplyTo(address string, name ...string) bool {
	return hasAddress(e.ReplyTo, address, name...)
}

// HasSubject reports whether the message has exactly this subject.
func (e Envelope) HasSubject(subject string) bool { return e.Subject == subject }

// HasMetadata reports whether the message carries this key with this value.
func (e Envelope) HasMetadata(key, value string) bool {
	got, ok := e.Metadata[key]
	return ok && got == value
}

// HasTag reports whether the message carries this tag. It is a method rather
// than a read of Tags so that a [Message] can ask an envelope it did not build.
func (e Envelope) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
