package events

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BroadcastNotificationCreated is a notification on its way to a browser that
// is connected right now.
//
// It is the value the broadcast channel builds and hands to the broadcaster:
// the channel calls the four methods below and pushes the result. Recording it
// on the outbox is not what makes the push happen -- an event there is a record
// of something that happened, never the mechanism.
type BroadcastNotificationCreated struct {
	// NotifiableType and NotifiableID say who received it.
	NotifiableType string
	NotifiableID   string
	// Key is the kind of notification: "billing.invoice-paid".
	Key string
	// ID identifies this delivery, so the same notification pushed live and
	// stored in the bell menu can be matched up.
	ID string
	// Data is what the notification's ToBroadcast produced.
	Data json.RawMessage
	// Channels is where it goes. Empty means the recipient's own private
	// channel, which ChannelName derives.
	Channels []string
	// Event is the name the client listens for. Empty means BroadcastAs falls
	// back to the Key.
	Event string
	// Type is what the payload calls the notification. Empty means the Key.
	Type string
}

// BroadcastOn is the channels this goes to: the ones named on the value, or the
// recipient's own channel when none were.
//
// What travels is the name and not a channel value: whether a name that belongs
// to one recipient is private is what the broadcaster makes of it.
func (b BroadcastNotificationCreated) BroadcastOn() []string {
	if len(b.Channels) > 0 {
		return append([]string(nil), b.Channels...)
	}
	if name := b.ChannelName(); name != "" {
		return []string{name}
	}
	return nil
}

// ChannelName is the recipient's own channel: the type and the id dotted,
// "user.42". It is the empty string when either half is missing.
//
// It is exported because a channel derived from a notifiable is the one thing a
// caller has to be able to check.
func (b BroadcastNotificationCreated) ChannelName() string {
	if b.NotifiableType == "" || b.NotifiableID == "" {
		return ""
	}
	return b.NotifiableType + "." + b.NotifiableID
}

// BroadcastWith is the payload that goes over the wire: the notification's own
// data with the id and the type added, because a client that receives two
// notifications in one second has to be able to tell them apart and to know
// which is which kind.
func (b BroadcastNotificationCreated) BroadcastWith() (json.RawMessage, error) {
	out := map[string]any{}
	if len(b.Data) > 0 {
		if err := json.Unmarshal(b.Data, &out); err != nil {
			return nil, fmt.Errorf("notifications: reading the broadcast payload of %s: %w", b.Key, err)
		}
	}
	// The id is left out when there is none rather than sent empty: a client
	// that keys on it would key everything on "".
	if b.ID != "" {
		out["id"] = b.ID
	}
	out["type"] = b.BroadcastType()

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("notifications: encoding the broadcast payload of %s: %w", b.Key, err)
	}
	return raw, nil
}

// BroadcastType is what the payload calls the notification: the Type when one
// was set, and otherwise the Key.
//
// It is a name that was chosen to be stable rather than a type name, which
// changes when somebody moves a type between packages and leaves every client
// that switched on it matching nothing.
func (b BroadcastNotificationCreated) BroadcastType() string {
	if b.Type != "" {
		return b.Type
	}
	return b.Key
}

// BroadcastAs is the event name the client listens for.
//
// An empty Event falls back to the notification's Key, which is what a client
// subscribes to when it cares about one kind, and to "notification" when there
// is not even a key.
func (b BroadcastNotificationCreated) BroadcastAs() string {
	switch {
	case b.Event != "":
		return b.Event
	case strings.TrimSpace(b.Key) != "":
		return b.Key
	default:
		return "notification"
	}
}
