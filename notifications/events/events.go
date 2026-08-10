// Package events names the three things that can happen to a notification on
// its way out, and builds the hesape/events value that records each of them.
//
// It mirrors Illuminate\Notifications\Events. The files it answers to, in the
// clone at laravel_illuminate/notifications/Events:
//
//	NotificationSending.php            -> Sending, NewSending
//	NotificationSent.php               -> Sent, NewSent
//	NotificationFailed.php             -> Failed, NewFailed
//	BroadcastNotificationCreated.php   -> nothing: broadcasting a notification
//	                                      is the broadcast channel doing its
//	                                      job, not a second event about it
//
// In Laravel these are classes a listener type-hints. Here they are event
// names on the outbox, because that is the one way an application learns that
// something happened (hesape/events) -- a second listener registry next to it
// is the second way RULE 9 refuses.
//
// The constructors take strings rather than the notifications types so that
// hesape/notifications can import this package to emit them. The payload is
// what a consumer needs and nothing that would drag a row along.
package events

import (
	"time"

	"github.com/arandu-io/hesape/events"
)

// The event names. They are the vocabulary a listener subscribes to.
const (
	// Sending is recorded before a channel is asked to deliver.
	Sending = "notification.sending"
	// Sent is recorded after a channel delivered without error.
	Sent = "notification.sent"
	// Failed is recorded when a channel returned an error. The notification
	// was not delivered on that channel; the others are unaffected.
	Failed = "notification.failed"
)

// Aggregate is what these events happen to, for the outbox.
const Aggregate = "notification"

// Payload is what a consumer of the three events receives.
//
// It carries names and ids, never the notification value itself: the body of a
// notification is somebody's password reset link, and an outbox row is read by
// everything downstream.
type Payload struct {
	// Key is the notification kind: "auth.password-reset".
	Key string `json:"key"`
	// Channel is which way it travelled: "mail", "database".
	Channel string `json:"channel"`
	// NotifiableType and NotifiableID say who it was for.
	NotifiableType string `json:"notifiable_type"`
	NotifiableID   string `json:"notifiable_id"`
	// Tenant is the tenant off the Grant that authorized the send.
	Tenant string `json:"tenant"`
	// Receipt is what the channel answered with: a provider message id, the id
	// of the stored row. Empty on Sending and on a channel that has nothing to
	// identify a delivery by.
	Receipt string `json:"receipt,omitempty"`
	// Error is why it failed, on Failed only.
	Error string `json:"error,omitempty"`
}

// NewSending returns the event recorded before a channel is asked to deliver.
func NewSending(p Payload) events.Event { return newEvent(Sending, p) }

// NewSent returns the event recorded after a successful delivery.
func NewSent(p Payload, receipt string) events.Event {
	p.Receipt = receipt
	return newEvent(Sent, p)
}

// NewFailed returns the event recorded when a channel refused or broke.
//
// The cause is flattened to its message: an outbox row is JSON, and an error
// value is not.
func NewFailed(p Payload, cause error) events.Event {
	if cause != nil {
		p.Error = cause.Error()
	}
	return newEvent(Failed, p)
}

func newEvent(name string, p Payload) events.Event {
	return events.Event{
		Name:        name,
		Aggregate:   Aggregate,
		AggregateID: p.NotifiableType + ":" + p.NotifiableID,
		Payload:     p,
		OccurredAt:  time.Now().UTC(),
	}
}
