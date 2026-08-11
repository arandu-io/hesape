// Package events mirrors Illuminate\Mail\Events.
//
// The files it answers to, in the clone at laravel_illuminate/mail/Events:
//
//	MessageSending.php
//	MessageSent.php
//
// # Why both names here are aliases
//
// MessageSent carries the SentMessage it announces, and the Mailer that fires
// both events is what builds them. That is a cycle -- events would import mail
// for the type it carries, and mail would import events for the values it
// dispatches -- and Go refuses it where PHP namespaces do not.
//
// So the two events are declared in [github.com/arandu-io/hesape/mail], next to
// the Mailer that fires them, and this package is the import path a Laravel
// developer reaches for. They are the same types under a second name, so a
// listener written against either one receives the other.
//
//	func (l *Listener) Handle(ctx context.Context, e events.MessageSending) bool {
//		return !l.suppressed(e.Message.To)
//	}
//
// A listener for MessageSending can refuse the send; a listener for MessageSent
// is being told what already happened. See [github.com/arandu-io/hesape/mail.Dispatcher].
package events

import "github.com/arandu-io/hesape/mail"

type (
	// MessageSending is fired before the transport is asked to do anything, and
	// is the one event a listener can refuse.
	MessageSending = mail.MessageSending
	// MessageSent is fired after the transport accepted the message.
	MessageSent = mail.MessageSent
)
