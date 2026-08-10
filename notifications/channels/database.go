package channels

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// DatabaseNotification is what a notification implements to be stored.
type DatabaseNotification interface {
	// ToDatabase is the payload the bell menu renders from.
	ToDatabase(to notifications.Notifiable) messages.Database
}

// Database stores a notification as a row.
//
// It is the channel behind the bell menu: the copy that is still there when the
// e-mail was filtered, and the one the recipient can mark as read.
type Database struct {
	store notifications.Store
}

// NewDatabase returns the database channel over a Store.
func NewDatabase(s notifications.Store) *Database { return &Database{store: s} }

var _ notifications.Channel = (*Database)(nil)

// Name is "database".
func (*Database) Name() notifications.ChannelName { return notifications.ChannelDatabase }

// Send writes the row and answers with its id.
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
