package events

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BroadcastNotificationCreated is a notification on its way to a browser that
// is connected right now.
//
// It is Illuminate\Notifications\Events\BroadcastNotificationCreated: the value
// the broadcast channel builds and hands to the broadcaster. In PHP it is an
// event that implements ShouldBroadcast, so dispatching it *is* the broadcast;
// here the channel calls the four methods below and pushes the result, because
// an event on the outbox is a record of something that happened and not the
// mechanism that makes it happen (RULE 9).
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
	// back to the class name Illuminate would use, spelled here as the event
	// name this package publishes.
	Event string
	// Type is what the payload calls the notification. Empty means the Key.
	Type string
}

// BroadcastOn is the channels the notification should be pushed on.
//
// Illuminate wraps each one in a PrivateChannel; there is no channel type here
// yet -- hesape/broadcasting is a later layer -- so what travels is the name,
// and "private" is what the broadcaster makes of a name that belongs to one
// recipient.
func (b BroadcastNotificationCreated) BroadcastOn() []string {
	if len(b.Channels) > 0 {
		return append([]string(nil), b.Channels...)
	}
	if name := b.ChannelName(); name != "" {
		return []string{name}
	}
	return nil
}

// ChannelName is the recipient's own channel: the type and the key, dotted.
//
// Illuminate builds it from the class name with the backslashes turned into
// dots, plus the model's key. Here the type is already a name that was chosen
// -- "user", "team" -- so there is nothing to translate.
func (b BroadcastNotificationCreated) ChannelName() string {
	if b.NotifiableType == "" || b.NotifiableID == "" {
		return ""
	}
	return b.NotifiableType + "." + b.NotifiableID
}

// BroadcastWith is the payload the client receives.
//
// It is the notification's own data with the id and the type added, which is
// what Illuminate merges in: a client that receives two notifications in one
// second has to be able to tell them apart and to know which is which kind.
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

// BroadcastType is what the payload calls the notification.
//
// Illuminate answers with the class name. Here it is the Key, which is the name
// that was chosen to be stable -- a class name changes when somebody moves a
// type between packages, and every client that switched on it stops matching.
func (b BroadcastNotificationCreated) BroadcastType() string {
	if b.Type != "" {
		return b.Type
	}
	return b.Key
}

// BroadcastAs is the event name the client listens for.
//
// Empty falls back to the notification's Key, which is what a client subscribes
// to when it cares about one kind, and to "notification" when there is not even
// a key -- the name Illuminate's own class name reduces to.
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
