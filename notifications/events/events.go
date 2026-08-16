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

// Aggregate is what these events happen to, and it is the name the outbox
// groups them under.
const Aggregate = "notification"

// Payload is what a consumer of the three events receives.
//
// It is the four public properties NotificationSending, NotificationSent and
// NotificationFailed each declare -- $notifiable, $notification, $channel and
// $response -- with the notifiable spelled out as its two columns and the
// notification as its Key.
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

// NewSending is NotificationSending::__construct.
func NewSending(p Payload) events.Event { return newEvent(Sending, p) }

// NewSent is NotificationSent::__construct. The receipt is its $response.
func NewSent(p Payload, receipt string) events.Event {
	p.Receipt = receipt
	return newEvent(Sent, p)
}

// NewFailed is NotificationFailed::__construct. The cause is its $data.
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
