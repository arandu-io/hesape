package notifications_test

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A database/sql driver holding a notifications table in memory, so the SQL the
// TableStore issues can be exercised without a server. It is written here
// rather than pulled from a mock library because the collection has one
// dependency and a test dependency is still a dependency -- the same reason the
// events package writes its own.
//
// It answers the six statements the store issues and nothing else. What it
// gives a test is the ability to say "there are two tenants with the same
// notifiable id" and watch which rows come back.

func init() { sql.Register("arandu-fake-notifications", fakeDriver{}) }

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	return &fakeConn{table: lookupTable(dsn)}, nil
}

var (
	tablesMu sync.Mutex
	tables   = map[string]*fakeTable{}
	tableSeq int
)

// newTable returns a DSN nobody else is using and the table behind it.
func newTable() (string, *fakeTable) {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	tableSeq++
	dsn := fmt.Sprintf("table-%d", tableSeq)
	t := &fakeTable{}
	tables[dsn] = t
	return dsn, t
}

func lookupTable(dsn string) *fakeTable {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	return tables[dsn]
}

// fakeRow is one stored notification, in the column order the store selects.
type fakeRow struct {
	id, tenant, notifiableType, notifiableID, key, data string
	readAt                                              *time.Time
	createdAt                                           time.Time
}

type fakeTable struct {
	mu   sync.Mutex
	rows []fakeRow
	// seen is every statement the store issued, normalised, for a test that
	// asserts on the shape of the SQL rather than on its effect.
	seen []string
}

func (t *fakeTable) statements() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.seen))
	copy(out, t.seen)
	return out
}

type fakeConn struct{ table *fakeTable }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{table: c.table, query: normalise(query)}, nil
}

func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

func normalise(query string) string { return strings.Join(strings.Fields(query), " ") }

type fakeStmt struct {
	table *fakeTable
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.table.mu.Lock()
	defer s.table.mu.Unlock()
	s.table.seen = append(s.table.seen, s.query)

	switch {
	case strings.HasPrefix(s.query, "INSERT"):
		s.table.rows = append(s.table.rows, fakeRow{
			id:             text(args[0]),
			tenant:         text(args[1]),
			notifiableType: text(args[2]),
			notifiableID:   text(args[3]),
			key:            text(args[4]),
			data:           text(args[5]),
			readAt:         stamp(args[6]),
			createdAt:      args[7].(time.Time),
		})
		return fakeResult(1), nil

	case strings.HasPrefix(s.query, "UPDATE") && strings.Contains(s.query, "WHERE id = ?"):
		now, id, tenant := args[0].(time.Time), text(args[1]), text(args[2])
		for i, r := range s.table.rows {
			if r.id == id && r.tenant == tenant && r.readAt == nil {
				s.table.rows[i].readAt = &now
				return fakeResult(1), nil
			}
		}
		return fakeResult(0), nil

	case strings.HasPrefix(s.query, "UPDATE"):
		now, tenant, kind, id := args[0].(time.Time), text(args[1]), text(args[2]), text(args[3])
		var n int64
		for i, r := range s.table.rows {
			if r.tenant == tenant && r.notifiableType == kind && r.notifiableID == id && r.readAt == nil {
				s.table.rows[i].readAt = &now
				n++
			}
		}
		return fakeResult(n), nil

	case strings.HasPrefix(s.query, "DELETE"):
		id, tenant := text(args[0]), text(args[1])
		for i, r := range s.table.rows {
			if r.id == id && r.tenant == tenant {
				s.table.rows = append(s.table.rows[:i], s.table.rows[i+1:]...)
				return fakeResult(1), nil
			}
		}
		return fakeResult(0), nil
	}
	return nil, fmt.Errorf("the fake table was not taught this statement: %s", s.query)
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.table.mu.Lock()
	defer s.table.mu.Unlock()
	s.table.seen = append(s.table.seen, s.query)

	// SELECT id FROM ... is the existence check behind MarkAsRead.
	if strings.HasPrefix(s.query, "SELECT id FROM") {
		id, tenant := text(args[0]), text(args[1])
		for _, r := range s.table.rows {
			if r.id == id && r.tenant == tenant {
				return &fakeRows{cols: []string{"id"}, values: [][]driver.Value{{r.id}}}, nil
			}
		}
		return &fakeRows{cols: []string{"id"}}, nil
	}
	if !strings.HasPrefix(s.query, "SELECT") {
		return nil, fmt.Errorf("the fake table was not taught this statement: %s", s.query)
	}

	tenant, kind, id := text(args[0]), text(args[1]), text(args[2])
	unreadOnly := strings.Contains(s.query, "AND read_at IS NULL ORDER BY")
	limit := limitOf(s.query)

	var out [][]driver.Value
	// Newest first, which is what the ORDER BY says and what a bell menu shows.
	for i := len(s.table.rows) - 1; i >= 0; i-- {
		r := s.table.rows[i]
		if r.tenant != tenant || r.notifiableType != kind || r.notifiableID != id {
			continue
		}
		if unreadOnly && r.readAt != nil {
			continue
		}
		var readAt driver.Value
		if r.readAt != nil {
			readAt = *r.readAt
		}
		out = append(out, []driver.Value{r.id, r.tenant, r.notifiableType, r.notifiableID, r.key, r.data, readAt, r.createdAt})
		if len(out) == limit {
			break
		}
	}
	cols := []string{"id", "tenant", "notifiable_type", "notifiable_id", "notification_key", "data", "read_at", "created_at"}
	return &fakeRows{cols: cols, values: out}, nil
}

func limitOf(query string) int {
	_, tail, found := strings.Cut(query, "LIMIT ")
	if !found {
		return 0
	}
	n, err := strconv.Atoi(strings.Fields(tail)[0])
	if err != nil {
		return 0
	}
	return n
}

func text(v driver.Value) string {
	switch value := v.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func stamp(v driver.Value) *time.Time {
	if t, ok := v.(time.Time); ok {
		return &t
	}
	return nil
}

type fakeResult int64

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

type fakeRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}
