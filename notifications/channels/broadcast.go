package channels

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// Broadcaster is the little the broadcast channel needs to reach a browser that
// is connected right now.
//
// It is a seam to hesape/broadcasting, which is layer 5 of
// docs/31-reorganizacao-hesape.md and does not exist yet. The signature is the
// one a hub can satisfy with no adaptation: a channel name, an event name and a
// JSON payload.
type Broadcaster interface {
	// Push delivers a payload to everyone subscribed to a channel. Nobody
	// listening is not an error: a live push is a courtesy on top of a stored
	// notification, never the only copy.
	Push(ctx context.Context, channel, event string, payload json.RawMessage) error
}

// BroadcastNotification is what a notification implements to be pushed live.
type BroadcastNotification interface {
	// ToBroadcast is the payload the browser receives.
	ToBroadcast(to notifications.Notifiable) messages.Broadcast
}

// Broadcast pushes a notification to a connected browser.
type Broadcast struct {
	hub Broadcaster
}

// NewBroadcast returns the broadcast channel over a hub.
func NewBroadcast(h Broadcaster) *Broadcast { return &Broadcast{hub: h} }

var _ notifications.Channel = (*Broadcast)(nil)

// Name is "broadcast".
func (*Broadcast) Name() notifications.ChannelName { return notifications.ChannelBroadcast }

// Send pushes the payload to the channel the recipient is subscribed on.
//
// RouteFor answers with that channel name -- "user.42", "team.7" -- and an
// empty answer is notifications.ErrNotAddressed: this recipient has no live
// connection to push to, which is the normal state of most recipients most of
// the time.
func (c *Broadcast) Send(ctx context.Context, _ auth.Grant, to notifications.Notifiable, n notifications.Notification) (string, error) {
	channel := to.RouteFor(notifications.ChannelBroadcast)
	if channel == "" {
		return "", notifications.ErrNotAddressed
	}
	buildable, ok := n.(BroadcastNotification)
	if !ok {
		return "", fmt.Errorf("notifications: %s named the broadcast channel and %T has no ToBroadcast", n.Key(), n)
	}
	if c.hub == nil {
		return "", fmt.Errorf("notifications: the broadcast channel has no hub, and %s was meant to be pushed", n.Key())
	}

	message := buildable.ToBroadcast(to)
	payload, err := message.JSON()
	if err != nil {
		return "", err
	}
	event := message.Event
	if event == "" {
		event = string(n.Key())
	}
	return "", c.hub.Push(ctx, channel, event, payload)
}
