package bus_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/database"
)

// A database/sql driver that understands the statements this package issues,
// and nothing else.
//
// It is here for the same reason the one in hesape/database is: the core has
// two dependencies, a test dependency is still a dependency, and a real engine
// would put a SQLite driver in the go.sum of every project that imports the
// collection.
//
// What it proves is that the repository's statements agree with each other --
// that the columns Store writes are the columns Find reads back in the order it
// reads them, that the counter compare-and-set rejects a write against counters
// that moved, that Cancel and Prune touch the rows they claim to. What it does
// not prove is that the SQL is valid for PostgreSQL, MySQL or SQLite; that is
// the conformance suite's job, and it runs against real engines in the adapter
// repositories.

func init() { sql.Register("bus-fake", fakeDriver{}) }

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) { return &fakeConn{db: lookupFake(dsn)}, nil }

var (
	fakeMu  sync.Mutex
	fakeDBs = map[string]*fakeDB{}
	fakeSeq int
)

// fakeRow is one row of job_batches, in the order the columns are declared.
type fakeRow struct {
	id, tenant, name        string
	total, pending, failed  int64
	failedJobIDs, options   string
	createdAt               time.Time
	cancelledAt, finishedAt *time.Time
}

type fakeDB struct {
	mu   sync.Mutex
	rows map[string]*fakeRow // keyed by tenant and id
}

// newFakeStore returns a database repository over a fresh fake database.
func newFakeStore() *bus.DatabaseBatchRepository {
	fakeMu.Lock()
	fakeSeq++
	dsn := fmt.Sprintf("bus-fake-%d", fakeSeq)
	fakeDBs[dsn] = &fakeDB{rows: map[string]*fakeRow{}}
	fakeMu.Unlock()

	handle, err := sql.Open("bus-fake", dsn)
	if err != nil {
		panic(err)
	}
	return bus.NewDatabaseBatchRepository(database.Wrap(handle, database.DialectSQLite))
}

func lookupFake(dsn string) *fakeDB {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fakeDBs[dsn]
}

func fakeKey(tenant, id string) string { return tenant + "\x00" + id }

type fakeConn struct{ db *fakeDB }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeResult int64

func (fakeResult) LastInsertId() (int64, error)   { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

// squash collapses whitespace so a statement can be matched on shape rather
// than on indentation.
func squash(q string) string { return strings.Join(strings.Fields(q), " ") }

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	q := squash(query)
	v := make([]driver.Value, len(args))
	for i, a := range args {
		v[i] = a.Value
	}

	c.db.mu.Lock()
	defer c.db.mu.Unlock()

	switch {
	case strings.HasPrefix(q, "INSERT INTO "+bus.BatchesTable):
		row := &fakeRow{
			id: str(v[0]), tenant: str(v[1]), name: str(v[2]),
			total: num(v[3]), pending: num(v[4]), failed: num(v[5]),
			failedJobIDs: str(v[6]), options: str(v[7]), createdAt: stamp(v[8]),
			cancelledAt: maybe(v[9]), finishedAt: maybe(v[10]),
		}
		c.db.rows[fakeKey(row.tenant, row.id)] = row
		return fakeResult(1), nil

	case strings.HasPrefix(q, "UPDATE "+bus.BatchesTable+" SET total_jobs"):
		row := c.db.rows[fakeKey(str(v[3]), str(v[2]))]
		if row == nil {
			return fakeResult(0), nil
		}
		row.total += num(v[0])
		row.pending += num(v[1])
		row.finishedAt = nil
		return fakeResult(1), nil

	case strings.HasPrefix(q, "UPDATE "+bus.BatchesTable+" SET pending_jobs"):
		row := c.db.rows[fakeKey(str(v[4]), str(v[3]))]
		// The compare-and-set: the write only lands if the counters are still
		// the ones the caller read.
		if row == nil || row.pending != num(v[5]) || row.failed != num(v[6]) {
			return fakeResult(0), nil
		}
		row.pending, row.failed, row.failedJobIDs = num(v[0]), num(v[1]), str(v[2])
		return fakeResult(1), nil

	case strings.HasPrefix(q, "UPDATE "+bus.BatchesTable+" SET finished_at"):
		row := c.db.rows[fakeKey(str(v[2]), str(v[1]))]
		if row == nil || row.finishedAt != nil {
			return fakeResult(0), nil
		}
		row.finishedAt = maybe(v[0])
		return fakeResult(1), nil

	case strings.HasPrefix(q, "UPDATE "+bus.BatchesTable+" SET cancelled_at"):
		row := c.db.rows[fakeKey(str(v[3]), str(v[2]))]
		if row == nil || row.cancelledAt != nil {
			return fakeResult(0), nil
		}
		row.cancelledAt, row.finishedAt = maybe(v[0]), maybe(v[1])
		return fakeResult(1), nil

	case strings.Contains(q, "DELETE FROM "+bus.BatchesTable+" WHERE id ="):
		key := fakeKey(str(v[1]), str(v[0]))
		if _, ok := c.db.rows[key]; !ok {
			return fakeResult(0), nil
		}
		delete(c.db.rows, key)
		return fakeResult(1), nil

	case strings.HasPrefix(q, "DELETE FROM "+bus.BatchesTable+" WHERE tenant_id ="):
		tenant, before := str(v[0]), stamp(v[1])
		takes := func(row *fakeRow) bool {
			switch {
			case strings.Contains(q, "finished_at IS NOT NULL"):
				return row.finishedAt != nil
			case strings.Contains(q, "finished_at IS NULL"):
				return row.finishedAt == nil
			default:
				return row.cancelledAt != nil
			}
		}
		n := int64(0)
		for k, row := range c.db.rows {
			if row.tenant != tenant || !takes(row) || !row.createdAt.Before(before) {
				continue
			}
			delete(c.db.rows, k)
			n++
		}
		return fakeResult(n), nil
	}

	return nil, fmt.Errorf("bus-fake: no statement matched %q", q)
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q := squash(query)
	if !strings.HasPrefix(q, "SELECT id, tenant_id") {
		return nil, fmt.Errorf("bus-fake: no query matched %q", q)
	}

	c.db.mu.Lock()
	defer c.db.mu.Unlock()

	// The list: WHERE tenant_id = ? ... ORDER BY created_at DESC, id DESC LIMIT ?
	if strings.Contains(q, "ORDER BY created_at DESC") {
		tenant := str(args[0].Value)
		limit := num(args[len(args)-1].Value)
		var (
			cursorAt time.Time
			cursorID string
			paging   bool
		)
		if len(args) == 5 {
			cursorAt, cursorID, paging = stamp(args[1].Value), str(args[3].Value), true
		}
		after := func(row *fakeRow) bool {
			if row.createdAt.Equal(cursorAt) {
				return row.id < cursorID
			}
			return row.createdAt.Before(cursorAt)
		}

		var rows []*fakeRow
		for _, row := range c.db.rows {
			if row.tenant != tenant || (paging && !after(row)) {
				continue
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].createdAt.Equal(rows[j].createdAt) {
				return rows[i].id > rows[j].id
			}
			return rows[i].createdAt.After(rows[j].createdAt)
		})
		if int64(len(rows)) > limit {
			rows = rows[:limit]
		}
		out := &fakeRows{}
		for _, row := range rows {
			out.values = append(out.values, fakeValues(row))
		}
		return out, nil
	}

	row := c.db.rows[fakeKey(str(args[1].Value), str(args[0].Value))]
	if row == nil {
		return &fakeRows{}, nil
	}
	return &fakeRows{values: [][]driver.Value{fakeValues(row)}}, nil
}

func fakeValues(row *fakeRow) []driver.Value {
	return []driver.Value{
		row.id, row.tenant, row.name,
		row.total, row.pending, row.failed,
		row.failedJobIDs, row.options, row.createdAt,
		back(row.cancelledAt), back(row.finishedAt),
	}
}

type fakeRows struct {
	values [][]driver.Value
	i      int
}

func (r *fakeRows) Columns() []string {
	return []string{"id", "tenant_id", "name", "total_jobs", "pending_jobs",
		"failed_jobs", "failed_job_ids", "options", "created_at", "cancelled_at", "finished_at"}
}

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.i])
	r.i++
	return nil
}

func str(v driver.Value) string {
	s, _ := v.(string)
	return s
}

func num(v driver.Value) int64 {
	n, _ := v.(int64)
	return n
}

func stamp(v driver.Value) time.Time {
	t, _ := v.(time.Time)
	return t
}

// maybe turns a nullable timestamp argument into what the fake stores.
func maybe(v driver.Value) *time.Time {
	t, ok := v.(time.Time)
	if !ok {
		return nil
	}
	return &t
}

// back turns what the fake stores into what it hands to a scan.
func back(t *time.Time) driver.Value {
	if t == nil {
		return nil
	}
	return *t
}
