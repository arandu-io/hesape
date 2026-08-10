package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// TableStore is the Store backed by the notifications table.
//
// The SQL is written out and the values go in placeholders. There is no query
// builder and no ORM behind it: five statements, each of them readable, each of
// them carrying the tenant in the WHERE clause because that is the clause the
// whole design rests on (RULE 14).
type TableStore struct {
	db  *database.DB
	now func() time.Time
}

// NewTableStore returns a Store over an open connection.
func NewTableStore(db *database.DB) *TableStore {
	return &TableStore{db: db, now: func() time.Time { return time.Now().UTC() }}
}

var _ Store = (*TableStore)(nil)

const columns = `id, tenant, notifiable_type, notifiable_id, notification_key, data, read_at, created_at`

// Save writes one.
func (s *TableStore) Save(ctx context.Context, g auth.Grant, r Record) (Record, error) {
	tenant, err := scope(g, ActionSend)
	if err != nil {
		return Record{}, err
	}
	row, err := prepare(r, tenant, s.now())
	if err != nil {
		return Record{}, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO `+Table+` (`+columns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Tenant, row.NotifiableType, row.NotifiableID, string(row.Key), string(row.Data), nil, row.CreatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("notifications: storing %s: %w", row.Key, err)
	}
	return row, nil
}

// For returns the most recent notifications for a recipient, newest first.
func (s *TableStore) For(ctx context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error) {
	return s.list(ctx, g, to, limit, "")
}

// Unread is For, restricted to the ones not yet read.
func (s *TableStore) Unread(ctx context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error) {
	return s.list(ctx, g, to, limit, ` AND read_at IS NULL`)
}

func (s *TableStore) list(ctx context.Context, g auth.Grant, to Notifiable, limit int, extra string) ([]Record, error) {
	tenant, err := scope(g, ActionList)
	if err != nil {
		return nil, err
	}
	if to == nil {
		return nil, errors.New("notifications: no recipient")
	}
	// The limit is an integer this package clamped, never a caller's string:
	// it is the one value here that cannot be a placeholder on every driver,
	// so it is the one that has to be provably a number.
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+columns+` FROM `+Table+`
		 WHERE tenant = ? AND notifiable_type = ? AND notifiable_id = ?`+extra+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT `+fmt.Sprint(sane(limit)),
		tenant, to.NotifiableType(), to.NotifiableID())
	if err != nil {
		return nil, fmt.Errorf("notifications: listing for %s %s: %w", to.NotifiableType(), to.NotifiableID(), err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifications: reading rows: %w", err)
	}
	return out, nil
}

// MarkAsRead stamps one.
//
// Which row the Grant was issued for is decided by the Authorize call that
// produced it; what this statement guarantees is the tenant. A row belonging to
// another tenant answers database.ErrNotFound rather than "forbidden", because
// the difference between the two tells the caller the row exists.
func (s *TableStore) MarkAsRead(ctx context.Context, g auth.Grant, id string) error {
	tenant, err := scope(g, ActionRead)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE `+Table+` SET read_at = ? WHERE id = ? AND tenant = ? AND read_at IS NULL`,
		s.now(), id, tenant)
	if err != nil {
		return fmt.Errorf("notifications: marking %s read: %w", id, err)
	}
	// Zero rows means either "already read" or "no such row", and the two have
	// to be told apart: the first is fine and the second is a bug in the
	// caller.
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return nil
	}
	return s.mustExist(ctx, id, tenant)
}

// MarkAllAsRead stamps every unread notification a recipient has.
func (s *TableStore) MarkAllAsRead(ctx context.Context, g auth.Grant, to Notifiable) error {
	tenant, err := scope(g, ActionRead)
	if err != nil {
		return err
	}
	if to == nil {
		return errors.New("notifications: no recipient")
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE `+Table+` SET read_at = ?
		 WHERE tenant = ? AND notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL`,
		s.now(), tenant, to.NotifiableType(), to.NotifiableID())
	if err != nil {
		return fmt.Errorf("notifications: marking all read for %s %s: %w", to.NotifiableType(), to.NotifiableID(), err)
	}
	return nil
}

// Delete removes one.
func (s *TableStore) Delete(ctx context.Context, g auth.Grant, id string) error {
	tenant, err := scope(g, ActionDelete)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM `+Table+` WHERE id = ? AND tenant = ?`, id, tenant)
	if err != nil {
		return fmt.Errorf("notifications: deleting %s: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w: notification %s", database.ErrNotFound, id)
	}
	return nil
}

// mustExist answers whether a row this tenant owns is there, so an update that
// changed nothing can say which of the two reasons it was.
func (s *TableStore) mustExist(ctx context.Context, id, tenant string) error {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM `+Table+` WHERE id = ? AND tenant = ?`, id, tenant).Scan(&found)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: notification %s", database.ErrNotFound, id)
	case err != nil:
		return fmt.Errorf("notifications: reading %s: %w", id, err)
	default:
		return nil
	}
}

// scanRecord reads one row in the order columns names.
func scanRecord(rows *sql.Rows) (Record, error) {
	var (
		r      Record
		key    string
		data   string
		readAt sql.NullTime
	)
	if err := rows.Scan(&r.ID, &r.Tenant, &r.NotifiableType, &r.NotifiableID, &key, &data, &readAt, &r.CreatedAt); err != nil {
		return Record{}, fmt.Errorf("notifications: scanning a row: %w", err)
	}
	r.Key = Key(key)
	r.Data = json.RawMessage(data)
	if readAt.Valid {
		r.ReadAt = readAt.Time
	}
	return r, nil
}
