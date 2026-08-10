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

// The actions a Policy decides about. They are the "module.verb" form the rest
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

// Table is where stored notifications live.
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

// Read reports whether the recipient has read it.
func (r Record) Read() bool { return !r.ReadAt.IsZero() }

// Unread reports whether they have not.
func (r Record) Unread() bool { return r.ReadAt.IsZero() }

// Store is where the database channel puts a notification and where the bell
// menu reads it back.
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
	// MarkAllAsRead stamps every unread one a recipient has.
	MarkAllAsRead(ctx context.Context, g auth.Grant, to Notifiable) error
	// Delete removes one. A missing row is database.ErrNotFound.
	Delete(ctx context.Context, g auth.Grant, id string) error
}

// Policy is the default decision about stored notifications: a subject may read
// and clear their own, and nobody else's.
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

// Can decides.
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
