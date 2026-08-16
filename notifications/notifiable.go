package notifications

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/auth"
)

// NotificationBase is the state every notification carries whatever channel it
// takes: the id of the delivery and the language to render it in.
//
// A notification gets it by embedding it:
//
//	type InvoicePaid struct {
//		notifications.NotificationBase
//		Number string
//	}
//
// The name carries "Base" because [Notification] is already the interface a
// notification satisfies.
type NotificationBase struct {
	// ID is the identifier of this delivery. It is written to the stored row
	// and travels to every channel, so the same notification sent over mail and
	// over the database can be matched up in a support ticket.
	ID string
	// LocaleName is the language to render in: "pt-BR", "en".
	//
	// The fields and the methods of a Go type share one namespace, so the
	// setter keeps the short name -- Locale is what somebody types -- and the
	// field says what it holds. PreferredLocale is what reads it.
	LocaleName string
}

// Locale sets the language this notification is sent in, and returns a copy.
//
// It beats the recipient's own preference: a notification that has been told
// which language to use has been told for a reason, usually because it is about
// something the sender chose the words for.
func (n NotificationBase) Locale(locale string) NotificationBase {
	n.LocaleName = locale
	return n
}

// NotificationID is the id of this delivery, and it is what ties the copy
// pushed to a browser to the copy stored in the bell menu.
func (n NotificationBase) NotificationID() string { return n.ID }

// Identified is a notification that carries the id of its delivery.
//
// It is the optional half of NotificationBase: a notification that embeds the
// base satisfies it, and one that does not is delivered without an id.
type Identified interface {
	NotificationID() string
}

// PreferredLocale is the language the notification asked for, or the empty
// string when it asked for none.
//
// It is the read of Notification::$locale, which
// NotificationSender::preferredLocale does inline.
//
// It satisfies Localized, which is the one question the Notifier asks about a
// language whether it is asking a notification or a recipient.
func (n NotificationBase) PreferredLocale() string { return n.LocaleName }

// BroadcastOn is Notification::broadcastOn.
//
// Empty is the default and means "the recipient's own private channel", which
// the broadcast channel derives from the notifiable. A notification that must
// go somewhere else -- a shared team channel, a public status feed -- overrides
// it.
func (NotificationBase) BroadcastOn() []string { return nil }

// Broadcastable is a notification that names its own broadcast channels.
//
// It is the optional half of NotificationBase: a notification that embeds the
// base satisfies it, and one that does not is broadcast on the recipient's own
// channel.
type Broadcastable interface {
	BroadcastOn() []string
}

// RoutesNotifications is how a recipient is reached and how it is notified.
//
// A model embeds it:
//
//	type User struct {
//		notifications.RoutesNotifications
//		ID    string
//		Email string
//	}
//
// The Notifier is a field rather than something found globally: the model is
// handed the one it should use, which is also what lets a test hand it a
// Capture.
type RoutesNotifications struct {
	// Notifier is who does the sending.
	Notifier *Notifier
	// Routes is the address on each channel: an e-mail address for
	// ChannelMail, a channel name for ChannelBroadcast.
	Routes map[ChannelName]string
}

// RouteFor is the address on a channel, or the empty string when the recipient
// cannot be reached there. It is what satisfies [Notifiable].
//
// The routes are a map rather than a method found by name per channel, because
// a route found by name is a route that silently answers nothing the day
// somebody renames a method.
func (r RoutesNotifications) RouteFor(c ChannelName) string { return r.Routes[c] }

// RouteNotificationFor is [RoutesNotifications.RouteFor] under its other name,
// so that a recipient written against either one works.
func (r RoutesNotifications) RouteNotificationFor(c ChannelName) string { return r.RouteFor(c) }

// Notify sends n to to through this recipient's own Notifier, and fails when it
// has none.
//
// The recipient is an argument rather than the receiver because the receiver is
// the embedded struct and not the model that embeds it: Go has no `$this` that
// reaches the outer value. `user.Notify(ctx, g, user, InvoicePaid{})` reads
// oddly once and is honest about it.
func (r RoutesNotifications) Notify(ctx context.Context, g auth.Grant, to Notifiable, n Notification) error {
	if r.Notifier == nil {
		return errors.New("notifications: this recipient has no notifier to send with")
	}
	return r.Notifier.Send(ctx, g, to, n)
}

// NotifyNow is [RoutesNotifications.Notify] with a channel override: the
// channels named here are the ones used, in place of whatever the
// notification's Via answers.
func (r RoutesNotifications) NotifyNow(ctx context.Context, g auth.Grant, to Notifiable, n Notification, channels ...ChannelName) error {
	if r.Notifier == nil {
		return errors.New("notifications: this recipient has no notifier to send with")
	}
	return r.Notifier.SendNow(ctx, g, to, n, channels...)
}

// HasDatabaseNotifications is a recipient's bell menu.
//
// Its three reads each take a Grant and are scoped by its tenant, because a
// bell menu is somebody's invoices and somebody else's mentions.
type HasDatabaseNotifications struct {
	// Store is where the rows are.
	Store Store
}

// Notifications is the recipient's notifications, newest first.
//
// The page size is an argument rather than the caller's to decide afterwards,
// because a read nobody limited is the query that pages in four years of rows.
func (h HasDatabaseNotifications) Notifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	if h.Store == nil {
		return nil, errors.New("notifications: this recipient has no store to read from")
	}
	return h.Store.For(ctx, g, to, limit)
}

// UnreadNotifications is the ones still in the bell menu, newest first.
func (h HasDatabaseNotifications) UnreadNotifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	if h.Store == nil {
		return nil, errors.New("notifications: this recipient has no store to read from")
	}
	return h.Store.Unread(ctx, g, to, limit)
}

// ReadNotifications is the ones the recipient has already read.
//
// The filter runs over what Notifications returned, so it reads a page at a
// time like the other two: a recipient with ten thousand read notifications is
// a recipient whose caller should be paging rather than asking for all of them.
func (h HasDatabaseNotifications) ReadNotifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	all, err := h.Notifications(ctx, g, to, limit)
	if err != nil {
		return nil, err
	}
	return all.Read(), nil
}
