package mail

// MessageSending is Illuminate\Mail\Events\MessageSending: fired before the
// transport is asked to do anything, and the one event a listener can refuse.
//
// A listener that answers false to [Dispatcher.Until] stops the message. That
// is what a "do not e-mail this address" list is, and what a staging
// environment uses to make sure nothing reaches a customer.
type MessageSending struct {
	// Message is what is about to be sent, and a listener may change it.
	Message *Message
	// Data is what the message was built from: the mailable, or the view data.
	Data any
}

// MessageSent is Illuminate\Mail\Events\MessageSent: fired after the transport
// accepted the message. Nobody can refuse it -- it already happened.
type MessageSent struct {
	// Sent is the receipt, including the provider's identifier for the message.
	Sent SentMessage
	// Data is what the message was built from.
	Data any
}

// GetOriginalMessage is what was sent. Illuminate reaches it through a __get
// that turns $event->message into $this->sent->getOriginalMessage(); PHP magic
// accessors have no Go twin, so the method carries the name of the call it
// forwarded to.
func (e MessageSent) GetOriginalMessage() *Message { return e.Sent.Message }
