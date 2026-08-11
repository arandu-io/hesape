package notifications

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/auth"
)

// NotificationBase is the state every notification carries whatever channel it
// takes: the id of the delivery and the language to render it in.
//
// It is Illuminate's Notification base class, which a notification extends. The
// Go equivalent of extending is embedding:
//
//	type InvoicePaid struct {
//		notifications.NotificationBase
//		Number string
//	}
//
// The name carries "Base" because Notification is already the interface a
// notification satisfies. Every method on it keeps Illuminate's spelling.
type NotificationBase struct {
	// ID is the identifier of this delivery. It is written to the stored row
	// and travels to every channel, so the same notification sent over mail and
	// over the database can be matched up in a support ticket.
	ID string
	// LocaleName is the language to render in: "pt-BR", "en".
	//
	// Illuminate calls the property $locale and the setter locale(). Go has one
	// namespace for the fields and the methods of a type, so the setter keeps
	// the name -- locale() is what somebody types -- and the field says what it
	// holds. PreferredLocale is what reads it.
	LocaleName string
}

// Locale sets the language this notification is sent in, and returns a copy.
//
// It beats the recipient's own preference, which is Illuminate's order: a
// notification that has been told which language to use has been told for a
// reason, usually because it is about something the sender chose the words for.
func (n NotificationBase) Locale(locale string) NotificationBase {
	n.LocaleName = locale
	return n
}

// NotificationID is the id of this delivery.
//
// Illuminate reads `$notification->id` as a property; a property read in Go is
// a method, and this is it. It is what ties the copy pushed to a browser to the
// copy stored in the bell menu.
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
// It satisfies Localized, which is the one question the Notifier asks about a
// language whether it is asking a notification or a recipient.
func (n NotificationBase) PreferredLocale() string { return n.LocaleName }

// BroadcastOn is the channels the notification should broadcast on.
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
// It is Illuminate's RoutesNotifications trait, and a model embeds it the way a
// PHP model uses it:
//
//	type User struct {
//		notifications.RoutesNotifications
//		ID    string
//		Email string
//	}
//
// The Notifier is a field rather than something reached through a container
// (ADR 0001): `$user->notify(...)` in PHP finds the dispatcher globally, and
// here the model is handed the one it should use, which is also what lets a
// test hand it a Capture.
type RoutesNotifications struct {
	// Notifier is who does the sending.
	Notifier *Notifier
	// Routes is the address on each channel: an e-mail address for
	// ChannelMail, a channel name for ChannelBroadcast.
	Routes map[ChannelName]string
}

// RouteFor is the address on a channel, or the empty string when the recipient
// cannot be reached there. It is what satisfies Notifiable.
//
// Illuminate looks for a `routeNotificationForMail` method by string and falls
// back to the model's `email` attribute. Here it is a map, because a route
// found by string is a route that silently becomes null the day somebody
// renames a method.
func (r RoutesNotifications) RouteFor(c ChannelName) string { return r.Routes[c] }

// RouteNotificationFor is RouteFor under Illuminate's spelling, so that a model
// ported from PHP keeps compiling.
func (r RoutesNotifications) RouteNotificationFor(c ChannelName) string { return r.RouteFor(c) }

// Notify sends a notification to the given recipient.
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

// NotifyNow sends a notification immediately, optionally over the given
// channels rather than the ones the notification names.
//
// In Illuminate the two differ because `notify` queues a notification that
// implements ShouldQueue. Nothing here queues on its own -- sending on the
// queue is a job that calls Send, and it is visible at the call site -- so what
// NotifyNow adds is the channel override.
func (r RoutesNotifications) NotifyNow(ctx context.Context, g auth.Grant, to Notifiable, n Notification, channels ...ChannelName) error {
	if r.Notifier == nil {
		return errors.New("notifications: this recipient has no notifier to send with")
	}
	return r.Notifier.SendNow(ctx, g, to, n, channels...)
}

// HasDatabaseNotifications is a recipient's bell menu.
//
// It is Illuminate's trait of the same name, whose three methods are Eloquent
// relations there and three reads here. Every one of them takes a Grant and is
// scoped by its tenant, because a bell menu is somebody's invoices and somebody
// else's mentions (RULE 17).
type HasDatabaseNotifications struct {
	// Store is where the rows are.
	Store Store
}

// Notifications is the recipient's notifications, newest first.
func (h HasDatabaseNotifications) Notifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	if h.Store == nil {
		return nil, errors.New("notifications: this recipient has no store to read from")
	}
	return h.Store.For(ctx, g, to, limit)
}

// UnreadNotifications is the ones they have not read.
func (h HasDatabaseNotifications) UnreadNotifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	if h.Store == nil {
		return nil, errors.New("notifications: this recipient has no store to read from")
	}
	return h.Store.Unread(ctx, g, to, limit)
}

// ReadNotifications is the ones they have read.
//
// Illuminate has a `read` query scope for it; there is no scope here because
// there is no query builder, so the filter runs over what Notifications
// returned. It reads a page at a time like the other two, and a recipient with
// ten thousand read notifications is a recipient whose caller should be paging
// rather than asking for all of them.
func (h HasDatabaseNotifications) ReadNotifications(ctx context.Context, g auth.Grant, to Notifiable, limit int) (Records, error) {
	all, err := h.Notifications(ctx, g, to, limit)
	if err != nil {
		return nil, err
	}
	return all.Read(), nil
}
