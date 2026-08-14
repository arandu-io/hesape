package channels

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// DatabaseNotification is what a notification implements to be stored.
//
// It is the toDatabase()/toArray() DatabaseChannel::getData looks for by name
// on the notification. Only toDatabase is here: two names for one payload is
// the second way RULE 9 refuses, and toArray is the one PHP falls back to.
type DatabaseNotification interface {
	// ToDatabase is the payload the bell menu renders from.
	ToDatabase(to notifications.Notifiable) messages.Database
}

// Database stores a notification as a row. It is
// Illuminate\Notifications\Channels\DatabaseChannel.
//
// It is the channel behind the bell menu: the copy that is still there when the
// e-mail was filtered, and the one the recipient can mark as read.
type Database struct {
	store notifications.Store
}

// NewDatabase returns the database channel over a Store.
//
// It has no PHP counterpart: DatabaseChannel has no constructor and reaches the
// notifiable's relation directly.
func NewDatabase(s notifications.Store) *Database { return &Database{store: s} }

var _ notifications.Channel = (*Database)(nil)

// Name is "database". It has no PHP counterpart, for the reason
// [Mail.Name] gives.
func (*Database) Name() notifications.ChannelName { return notifications.ChannelDatabase }

// Send is DatabaseChannel::send, and the payload it writes is
// DatabaseChannel::buildPayload.
//
// The Grant goes straight through to the Store, which is where the tenant is
// read off it. Nothing here decides who may be notified; that decision is the
// one the caller made before it had a Grant to pass (RULE 17).
func (c *Database) Send(ctx context.Context, g auth.Grant, to notifications.Notifiable, n notifications.Notification) (string, error) {
	if to.NotifiableID() == "" {
		return "", notifications.ErrAnonymous
	}
	buildable, ok := n.(DatabaseNotification)
	if !ok {
		return "", fmt.Errorf("notifications: %s named the database channel and %T has no ToDatabase", n.Key(), n)
	}
	if c.store == nil {
		return "", fmt.Errorf("notifications: the database channel has no store, and %s was meant to be stored", n.Key())
	}

	payload, err := buildable.ToDatabase(to).JSON()
	if err != nil {
		return "", err
	}
	stored, err := c.store.Save(ctx, g, notifications.Record{
		NotifiableType: to.NotifiableType(),
		NotifiableID:   to.NotifiableID(),
		Key:            n.Key(),
		Data:           payload,
	})
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}
