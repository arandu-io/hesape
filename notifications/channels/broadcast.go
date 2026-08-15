package channels

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/events"
	"github.com/arandu-io/hesape/notifications/messages"
)

// Broadcaster is the little the broadcast channel needs to reach a browser that
// is connected right now.
//
// It is the Illuminate\Contracts\Events\Dispatcher that
// BroadcastChannel::__construct takes, narrowed: there the dispatch of a
// ShouldBroadcast event is what makes the push happen, and here the push is a
// call.
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
//
// It is the toBroadcast()/toArray() BroadcastChannel::getData looks for by name
// on the notification. Only toBroadcast is here, for the reason
// [DatabaseNotification] gives.
type BroadcastNotification interface {
	// ToBroadcast is the payload the browser receives.
	ToBroadcast(to notifications.Notifiable) messages.Broadcast
}

// Broadcast is the channel that pushes a notification to a browser connected
// right now. It asks the notification for its payload through
// [BroadcastNotification], works out which channel names to push to -- the ones
// the notification names itself, or the one the recipient routes to -- and
// hands each of them to the [Broadcaster] it was built with.
//
// A recipient with no live connection is notifications.ErrNotAddressed rather
// than a failure, which is the normal state of most recipients most of the
// time: the push is a courtesy on top of a stored notification, never the only
// copy.
//
// Answers Illuminate\Notifications\Channels\BroadcastChannel.
type Broadcast struct {
	hub Broadcaster
}

// NewBroadcast is BroadcastChannel::__construct.
func NewBroadcast(h Broadcaster) *Broadcast { return &Broadcast{hub: h} }

var _ notifications.Channel = (*Broadcast)(nil)

// Name is "broadcast". It has no PHP counterpart, for the reason [Mail.Name]
// gives.
func (*Broadcast) Name() notifications.ChannelName { return notifications.ChannelBroadcast }

// Send is BroadcastChannel::send.
//
// RouteFor answers with a channel name -- "user.42", "team.7" -- and an empty
// answer is notifications.ErrNotAddressed: this recipient has no live
// connection to push to, which is the normal state of most recipients most of
// the time. A notification that names its own channels through Broadcastable
// overrides the route, which is Illuminate's BroadcastNotificationCreated
// preferring the notification's broadcastOn().
func (c *Broadcast) Send(ctx context.Context, _ auth.Grant, to notifications.Notifiable, n notifications.Notification) (string, error) {
	buildable, ok := n.(BroadcastNotification)
	if !ok {
		return "", fmt.Errorf("notifications: %s named the broadcast channel and %T has no ToBroadcast", n.Key(), n)
	}

	message := buildable.ToBroadcast(to)
	data, err := message.JSON()
	if err != nil {
		return "", err
	}

	created := events.BroadcastNotificationCreated{
		NotifiableType: to.NotifiableType(),
		NotifiableID:   to.NotifiableID(),
		Key:            string(n.Key()),
		Data:           data,
		Event:          message.Event,
	}
	if id, ok := n.(notifications.Identified); ok {
		created.ID = id.NotificationID()
	}
	if b, ok := n.(notifications.Broadcastable); ok {
		created.Channels = b.BroadcastOn()
	}
	if len(created.Channels) == 0 {
		if route := to.RouteFor(notifications.ChannelBroadcast); route != "" {
			created.Channels = []string{route}
		}
	}

	channels := created.BroadcastOn()
	if len(channels) == 0 {
		return "", notifications.ErrNotAddressed
	}
	if c.hub == nil {
		return "", fmt.Errorf("notifications: the broadcast channel has no hub, and %s was meant to be pushed", n.Key())
	}

	payload, err := created.BroadcastWith()
	if err != nil {
		return "", err
	}
	for _, channel := range channels {
		if err := c.hub.Push(ctx, channel, created.BroadcastAs(), payload); err != nil {
			return "", err
		}
	}
	return "", nil
}
