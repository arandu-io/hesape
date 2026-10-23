package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/support"
)

// DatabaseHandler keeps the sessions of a [RecordStore] in a table, so they
// survive a restart and are shared by every instance that reads the same
// database -- the durable answer for a deployment with more than one replica
// and no RESP server.
//
// It stores [Record] whole, as JSON, for the reason the RESP handler does: a
// handler that mapped the record onto columns of its own would drop any field
// it did not name, and the loss would show only in the deployment that uses
// it. Three facts are also written to columns, because the table is asked by
// them: tenant and subject_id are the index [DatabaseHandler.DestroyIndex]
// deletes by, and expires_at is the lifetime each write carries, so a
// remembered session and a plain one in the same table each expire on their
// own clock.
//
// The table is [RecordTable], created by [CreateSessionRecordsTable], which
// [RecordMigrations] lists. Nothing in the read or the write is scoped by a
// tenant on the way in, and that is deliberate: the session id is what says
// which tenant a request belongs to, so the id is the whole key, as [Handler]
// requires.
type DatabaseHandler[T any] struct {
	conn  DatabaseConnection
	table string
}

// DatabaseConnection is the part of a database [DatabaseHandler] uses.
//
// It is declared here rather than imported from hesape/database, so this
// package stays below the database package instead of carrying it into
// everything that reads a session. The application's *database.DB satisfies
// it unchanged.
type DatabaseConnection interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewDatabaseHandler returns the handler over conn, reading and writing
// table. An empty table is [RecordTable].
//
// The statements use the portable "?" placeholder, which the application's
// *database.DB rewrites for PostgreSQL, so conn is that value and not a bare
// *sql.DB on PostgreSQL.
//
// table is part of the statement rather than a parameter, so it must be a
// plain identifier -- letters, digits and underscores -- and anything else is
// refused with [ErrBadRecordTable].
func NewDatabaseHandler[T any](conn DatabaseConnection, table string) (*DatabaseHandler[T], error) {
	if table == "" {
		table = RecordTable
	}
	if !validTableName(table) {
		return nil, fmt.Errorf("%w: %q", ErrBadRecordTable, table)
	}
	return &DatabaseHandler[T]{conn: conn, table: table}, nil
}

// ErrBadRecordTable is returned by [NewDatabaseHandler] for a table name that
// is not a plain identifier. A table name cannot be a parameter in SQL, so
// refusing anything else is what keeps a configured value from becoming an
// injection.
var ErrBadRecordTable = errors.New("session: the record table name must be letters, digits and underscores")

var _ Handler[any] = (*DatabaseHandler[any])(nil)

// Read returns the record, or [ErrExpired] when the table holds no live row
// for the id: missing, or past its expiry.
func (h *DatabaseHandler[T]) Read(ctx context.Context, id string) (Record[T], error) {
	var payload string
	var expires int64
	err := h.conn.QueryRowContext(ctx,
		`SELECT payload, expires_at FROM `+h.table+` WHERE id = ?`, id).
		Scan(&payload, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Record[T]{}, ErrExpired
	}
	if err != nil {
		return Record[T]{}, fmt.Errorf("session: reading the session row: %w", err)
	}
	if expires <= support.Now().UnixMilli() {
		return Record[T]{}, ErrExpired
	}

	var rec Record[T]
	if err := json.Unmarshal([]byte(payload), &rec); err != nil {
		return Record[T]{}, fmt.Errorf("session: decoding the session record: %w", err)
	}
	return rec, nil
}

// Write stores the record under id until ttl from now.
//
// It updates the row and inserts it when there was none, and a second insert
// racing the first -- two requests of one new session -- falls back to the
// update rather than failing either request. A ttl of zero or less is
// refused: a row that never expires is a session that outlives every logout
// that does not reach it.
func (h *DatabaseHandler[T]) Write(ctx context.Context, id string, rec Record[T], ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("session: a session needs a lifetime, or its row outlives every sign-out")
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("session: encoding the session record: %w", err)
	}
	args := []any{string(raw), nullable(rec.Tenant), nullable(rec.SubjectID), support.Now().Add(ttl).UnixMilli(), id}

	updated, err := h.update(ctx, args)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}
	_, insertErr := h.conn.ExecContext(ctx,
		`INSERT INTO `+h.table+` (payload, tenant, subject_id, expires_at, id) VALUES (?, ?, ?, ?, ?)`,
		args...)
	if insertErr == nil {
		return nil
	}
	if _, err := h.update(ctx, args); err != nil {
		return fmt.Errorf("session: writing the session row: %w", insertErr)
	}
	return nil
}

// update writes the row if it exists and reports whether it did.
//
// MySQL counts the rows an UPDATE changed rather than the rows it matched, so
// a write of identical values reports none; the insert that follows then
// meets the key and falls back here, which is why that path exists.
func (h *DatabaseHandler[T]) update(ctx context.Context, args []any) (bool, error) {
	result, err := h.conn.ExecContext(ctx,
		`UPDATE `+h.table+` SET payload = ?, tenant = ?, subject_id = ?, expires_at = ? WHERE id = ?`,
		args...)
	if err != nil {
		return false, fmt.Errorf("session: updating the session row: %w", err)
	}
	n, err := result.RowsAffected()
	return err == nil && n > 0, nil
}

// Destroy removes the session, if present.
func (h *DatabaseHandler[T]) Destroy(ctx context.Context, id string) error {
	if _, err := h.conn.ExecContext(ctx, `DELETE FROM `+h.table+` WHERE id = ?`, id); err != nil {
		return fmt.Errorf("session: deleting the session row: %w", err)
	}
	return nil
}

// DestroyIndex removes every session of one subject of one tenant except
// keepID, in one statement over the index the write keeps.
//
// An empty tenant or subject id is refused, as [ArrayHandler.DestroyIndex]
// refuses it: neither names a subject, and the empty subject id of a tenant is
// every guest of it.
func (h *DatabaseHandler[T]) DestroyIndex(ctx context.Context, tenant, subjectID, keepID string) error {
	if tenant == "" || subjectID == "" {
		return errNoSubjectScope
	}
	if _, err := h.conn.ExecContext(ctx,
		`DELETE FROM `+h.table+` WHERE tenant = ? AND subject_id = ? AND id <> ?`,
		tenant, subjectID, keepID); err != nil {
		return fmt.Errorf("session: deleting the sessions of a subject: %w", err)
	}
	return nil
}

// Prune deletes the rows whose lifetime has passed and reports how many went.
//
// A read already refuses an expired row; this is what keeps the table from
// growing by one row per visit. Run it from the scheduler. A driver that does
// not count rows reports zero rather than an error.
func (h *DatabaseHandler[T]) Prune(ctx context.Context) (int, error) {
	result, err := h.conn.ExecContext(ctx,
		`DELETE FROM `+h.table+` WHERE expires_at <= ?`, support.Now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("session: pruning expired sessions: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(n), nil
}

// nullable is a string column that is NULL when empty: a guest has no subject,
// and an empty string in subject_id would be a subject called "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RecordTable is the table [DatabaseHandler] reads and writes unless it is
// given another.
const RecordTable = "session_records"

// CreateSessionRecordsTable creates [RecordTable]: the session id as the key,
// the record as JSON, the tenant and subject a sign-out of every other session
// deletes by, and the end of the row's lifetime in Unix milliseconds.
//
// It is code rather than a file in a tree, because a migration written as SQL
// text describes one engine and this collection runs on three.
type CreateSessionRecordsTable struct{ migrations.BaseMigration }

// GetName returns the migration's name.
func (CreateSessionRecordsTable) GetName() string {
	return "2026_10_21_000001_create_session_records_table"
}

// Up creates the table and the two indexes it is deleted by: one for a
// subject's sessions and one for the expired rows Prune removes.
func (CreateSessionRecordsTable) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, RecordTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant").Nullable()
		table.String("subject_id").Nullable()
		table.Text("payload")
		table.BigInteger("expires_at")

		table.Index([]string{"tenant", "subject_id"}, "session_records_tenant_subject_id_index")
		table.Index([]string{"expires_at"}, "session_records_expires_at_index")
	})
}

// Down drops the table, and its indexes with it.
func (CreateSessionRecordsTable) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, RecordTable)
}

// RecordMigrations is the table [DatabaseHandler] keeps its sessions in. An
// application that keeps sessions in memory or over RESP does not create it,
// and one using [DatabaseHandler] adds this to the list it hands the migrator.
func RecordMigrations() []migrations.Migration {
	return []migrations.Migration{CreateSessionRecordsTable{}}
}
