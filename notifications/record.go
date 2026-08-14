package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// The actions a Policy decides about. They have no PHP counterpart: Illuminate
// reads the notifications relation straight off the model, which is the read
// path RULE 17 refuses.
// They are the "module.verb" form the rest
// of the collection uses, so `aru doctor` recognises them.
const (
	// ActionSend is asked before anything is delivered, and again before the
	// row is written. It is the one a job or a controller authorizes.
	ActionSend auth.Action = "notification.send"
	// ActionList is reading somebody's notifications back -- the bell menu.
	ActionList auth.Action = "notification.list"
	// ActionRead is marking one, or all of them, as read.
	ActionRead auth.Action = "notification.read"
	// ActionDelete is removing one.
	ActionDelete auth.Action = "notification.delete"
)

// Table is where stored notifications live. It is DatabaseNotification::$table.
const Table = "notifications"

// Record is one stored notification: the row behind the bell menu.
//
// It is Illuminate's DatabaseNotification with the morph columns spelled out
// and a tenant added, because in a SaaS a notification belongs to a customer
// before it belongs to a person, and a query that forgets that reads across
// all of them (RULE 14).
type Record struct {
	// ID is the row's own identifier, a UUIDv7 so the table sorts by time
	// without a second index to do it.
	ID string
	// Tenant comes off the Grant, never from the request.
	Tenant string
	// NotifiableType and NotifiableID are who it is for.
	NotifiableType string
	NotifiableID   string
	// Key is the kind, and is what a query filters on: "billing.invoice-paid".
	Key Key
	// Data is the payload the database channel produced, as JSON. It is what
	// the bell menu renders from.
	Data json.RawMessage
	// ReadAt is when the recipient read it. The zero value means unread, which
	// is why Read and Unread exist rather than a *time.Time nobody remembers
	// to nil-check.
	ReadAt time.Time
	// CreatedAt is when it was stored.
	CreatedAt time.Time
}

// Read is DatabaseNotification::read.
func (r Record) Read() bool { return !r.ReadAt.IsZero() }

// Unread is DatabaseNotification::unread.
func (r Record) Unread() bool { return r.ReadAt.IsZero() }

// MarkAsRead is DatabaseNotification::markAsRead.
//
// The store is an argument because a Record is a row and not an Active Record
// object with a connection inside it (docs/01-arquitetura.md), and the Grant is
// one because stamping somebody's notification read is a write on their data.
//
// Marking a notification that is already read changes nothing and does not move
// the timestamp: the recipient wanted it read and it is.
func (r Record) MarkAsRead(ctx context.Context, g auth.Grant, s Store) error {
	if s == nil {
		return errors.New("notifications: marking a notification read needs a store")
	}
	return s.MarkAsRead(ctx, g, r.ID)
}

// MarkAsUnread is DatabaseNotification::markAsUnread: the stamp is cleared, so
// the notification is back in the bell menu.
//
// It is the undo of MarkAsRead, and Illuminate has it for the same reason: a
// menu that marks everything read on open needs a way to put one back.
func (r Record) MarkAsUnread(ctx context.Context, g auth.Grant, s Store) error {
	if s == nil {
		return errors.New("notifications: marking a notification unread needs a store")
	}
	return s.MarkAsUnread(ctx, g, r.ID)
}

// Notifiable is DatabaseNotification::notifiable.
//
// In Illuminate it is a morphTo relation that loads the model. Here it is the
// two columns that name the row -- the type and the id -- as something a
// channel or a store can be handed. Loading the model is the application's job:
// this package does not know what a "user" is and must not.
func (r Record) Notifiable() Notifiable {
	return recipient{kind: r.NotifiableType, id: r.NotifiableID}
}

// recipient is a Notifiable that is only its identity.
type recipient struct{ kind, id string }

func (r recipient) NotifiableID() string      { return r.id }
func (r recipient) NotifiableType() string    { return r.kind }
func (recipient) RouteFor(ChannelName) string { return "" }

// Records is a page of stored notifications.
//
// It is Illuminate's DatabaseNotificationCollection, which is an Eloquent
// collection with two methods on it. Here the collection is a slice, so what is
// left is the two methods.
type Records []Record

// MarkAsRead is DatabaseNotificationCollection::markAsRead.
//
// It stops at the first error rather than carrying on, because the errors a
// store returns here are "no such row" and "the Grant does not allow it", and
// neither gets better on the next row.
func (rs Records) MarkAsRead(ctx context.Context, g auth.Grant, s Store) error {
	for _, r := range rs {
		if err := r.MarkAsRead(ctx, g, s); err != nil {
			return err
		}
	}
	return nil
}

// MarkAsUnread is DatabaseNotificationCollection::markAsUnread.
func (rs Records) MarkAsUnread(ctx context.Context, g auth.Grant, s Store) error {
	for _, r := range rs {
		if err := r.MarkAsUnread(ctx, g, s); err != nil {
			return err
		}
	}
	return nil
}

// Read is the ones the recipient has read.
//
// It has no PHP counterpart of its own: there the collection is filtered with
// Collection::filter and a closure calling read(). Here the two filters are
// named, because the alternative is the same closure written in every caller.
func (rs Records) Read() Records { return rs.filter(Record.Read) }

// Unread is the ones they have not. It has no PHP counterpart of its own, for
// the reason [Records.Read] gives.
func (rs Records) Unread() Records { return rs.filter(Record.Unread) }

func (rs Records) filter(keep func(Record) bool) Records {
	out := make(Records, 0, len(rs))
	for _, r := range rs {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// ScopeRead is DatabaseNotification::scopeRead, and ScopeUnread is
// DatabaseNotification::scopeUnread: the SQL conditions that keep only the read
// or only the unread notifications.
//
// In Illuminate they are query scopes on the model, which is what a `where`
// looks like when the query builder is the model. There is no query builder
// here (docs/01-arquitetura.md rejects Active Record), so a scope is what it
// always was underneath: a condition, written once, that a statement pastes in.
//
// TableStore uses them, and so does an application writing its own read model
// over the same table.
func ScopeRead() string { return "read_at IS NOT NULL" }

// ScopeUnread is DatabaseNotification::scopeUnread, the condition for the
// notifications still in the bell menu.
func ScopeUnread() string { return "read_at IS NULL" }

// Store is where the database channel puts a notification and where the bell
// menu reads it back.
//
// It has no PHP counterpart: there DatabaseNotification is an Eloquent model
// and the query builder is the model, which docs/01-arquitetura.md rejects.
//
// Every method takes a Grant, reads included. A notification is somebody's
// invoice, somebody's password reset, somebody's mention -- a list endpoint
// without a policy is a list endpoint that hands one tenant another tenant's
// bell menu (RULE 17).
type Store interface {
	// Save writes one and returns it with the id and timestamp filled in.
	Save(ctx context.Context, g auth.Grant, r Record) (Record, error)
	// For returns the most recent notifications for a recipient, newest first.
	For(ctx context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error)
	// Unread is For, restricted to the ones not yet read.
	Unread(ctx context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error)
	// MarkAsRead stamps one. Marking a read notification read again is not an
	// error and does not move the timestamp.
	MarkAsRead(ctx context.Context, g auth.Grant, id string) error
	// MarkAsUnread clears the stamp, putting the notification back in the bell
	// menu. Marking an unread notification unread again is not an error.
	MarkAsUnread(ctx context.Context, g auth.Grant, id string) error
	// MarkAllAsRead stamps every unread one a recipient has.
	MarkAllAsRead(ctx context.Context, g auth.Grant, to Notifiable) error
	// Delete removes one. A missing row is database.ErrNotFound.
	Delete(ctx context.Context, g auth.Grant, id string) error
}

// Policy is the default decision about stored notifications: a subject may read
// and clear their own, and nobody else's. It has no PHP counterpart.
//
// It is here rather than in the skeleton because every application wants this
// same answer and getting it wrong leaks a bell menu. An application with a
// different rule -- a support agent who may read a customer's -- writes its own
// Policy and passes that instead; the type is an argument to auth.Authorize,
// not a registration.
//
// Illuminate has no equivalent: in Laravel the notifications relation is read
// straight off the model, which is exactly the read path RULE 17 refuses.
type Policy struct{}

// Can decides. It has no PHP counterpart, for the reason Policy gives.
//
// ActionSend is about the sender rather than about a stored row, so it asks
// only that the subject carry a tenant: the row that gets written is scoped to
// it, and a send with no tenant has nowhere to be stored.
//
// The three reading actions compare the record against the subject. A
// collection action passes the zero Record -- there is no row yet to compare --
// and the tenant on the Grant is what scopes the query.
func (Policy) Can(_ context.Context, s auth.Subject, a auth.Action, r Record) error {
	if s.Tenant == "" {
		return errors.New("the subject has no tenant")
	}
	switch a {
	case ActionSend:
		return nil
	case ActionList, ActionRead, ActionDelete:
		if r.ID == "" {
			// A collection action: there is no row to compare, and the query
			// is scoped by the tenant on the Grant.
			return nil
		}
		if r.Tenant != s.Tenant {
			return errors.New("the notification belongs to another tenant")
		}
		if r.NotifiableID != s.ID {
			return errors.New("the notification belongs to somebody else")
		}
		return nil
	default:
		return fmt.Errorf("%s is not an action on a notification", a)
	}
}

// Migrations is the notifications table.
//
// It has no PHP method to answer to: it is the stub
// NotificationTableCommand::migrationStubFile names --
// notifications.stub in the illuminate/notifications package -- returned as a
// value. [notifications/console.NotificationTableCommand] is the command that
// writes it to a file for a project that generates rather than imports.
//
// One migration, returned rather than embedded in a file tree, because the
// table belongs to the package that reads it: an application that never uses
// the database channel does not create it, and one that does adds this to the
// list it passes to database.Migrate.
//
// The key column is notification_key and not key: KEY is reserved in MySQL, and
// a table nobody can create on one of the three supported databases is a table
// that fails on the day somebody switches.
func Migrations() []database.Migration {
	return []database.Migration{{
		ID: "2026_08_10_000001_create_notifications_table",
		Up: `CREATE TABLE ` + Table + ` (
			id               ` + database.KeyText + ` PRIMARY KEY,
			tenant           ` + database.KeyText + ` NOT NULL,
			notifiable_type  ` + database.KeyText + ` NOT NULL,
			notifiable_id    ` + database.KeyText + ` NOT NULL,
			notification_key ` + database.KeyText + ` NOT NULL,
			data             TEXT NOT NULL,
			read_at          TIMESTAMP NULL,
			created_at       TIMESTAMP NOT NULL
		);
		CREATE INDEX notifications_recipient_idx
			ON ` + Table + ` (tenant, notifiable_type, notifiable_id, created_at);`,
		Down: `DROP TABLE ` + Table + `;`,
	}}
}
