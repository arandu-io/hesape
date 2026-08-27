package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// A minimal database/sql driver, so the data package can be tested without a
// server. It is here rather than pulled from a mock library because the core has
// two dependencies and a test dependency is still a dependency.

func init() { sql.Register("arandu-fake", fakeDriver{}) }

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	return &fakeConn{db: lookupFake(dsn)}, nil
}

var (
	fakeMu   sync.Mutex
	fakeDBs  = map[string]*fakeDB{}
	fakeSeq  int
	fakeName = "fake-%d"
)

// fakeDB is the state a fixture inspects: every statement that arrived, and the
// rows to answer with.
type fakeDB struct {
	mu      sync.Mutex
	stmts   []string
	args    [][]driver.NamedValue
	onTx    []bool
	rows    map[string][]string // sql substring -> single column rows
	failOn  string
	failErr error
	lastID  int64

	// answerColumns and answerRows are the result set every query gets when
	// one was set, for a fixture whose shape is not the migrations table.
	answerColumns []string
	answerRows    [][]driver.Value
}

// answer sets the result set every query on this fake comes back with.
func (f *fakeDB) answer(columns []string, rows ...[]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answerColumns = columns
	f.answerRows = rows
}

// statementsOnTransaction counts the statements that ran on an open
// transaction and the ones that ran beside it, ignoring the commit and the
// rollback themselves.
//
// It is what tells a statement that joined a transaction from one that merely
// ran while a transaction was open: database/sql hands the transaction one
// connection out of the pool and gives every other statement a different one,
// so the two are indistinguishable from the SQL alone.
func (f *fakeDB) statementsOnTransaction() (joined, beside int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, statement := range f.stmts {
		if statement == "COMMIT" || statement == "ROLLBACK" {
			continue
		}
		if f.onTx[i] {
			joined++
			continue
		}
		beside++
	}
	return joined, beside
}

// newFakeDB registers a fake and returns the handle plus its state.
func newFakeDB() (*sql.DB, *fakeDB) {
	fakeMu.Lock()
	fakeSeq++
	dsn := fmt.Sprintf(fakeName, fakeSeq)
	state := &fakeDB{rows: map[string][]string{}}
	fakeDBs[dsn] = state
	fakeMu.Unlock()

	db, err := sql.Open("arandu-fake", dsn)
	if err != nil {
		panic(err)
	}
	return db, state
}

func lookupFake(dsn string) *fakeDB {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fakeDBs[dsn]
}

func (f *fakeDB) record(query string, args []driver.NamedValue, onTx bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stmts = append(f.stmts, query)
	f.args = append(f.args, args)
	f.onTx = append(f.onTx, onTx)
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return f.failErr
	}
	return nil
}

// statements returns the statements seen so far, whitespace collapsed so a test
// can match on shape without matching on indentation.
func (f *fakeDB) statements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.stmts))
	for i, s := range f.stmts {
		out[i] = strings.Join(strings.Fields(s), " ")
	}
	return out
}

func (f *fakeDB) sawStatement(substring string) bool {
	for _, s := range f.statements() {
		if strings.Contains(s, substring) {
			return true
		}
	}
	return false
}

// answerFor returns the result set set with answer, or nil columns when none
// was.
func (f *fakeDB) answerFor() ([]string, [][]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answerColumns, f.answerRows
}

func (f *fakeDB) rowsFor(query string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for pattern, rows := range f.rows {
		if strings.Contains(query, pattern) {
			return rows
		}
	}
	return nil
}

// fakeConn is one connection out of the pool. It knows whether a transaction
// is open on itself, which is the only place that can be known: the pool hands
// a transaction one connection and every other statement a different one.
type fakeConn struct {
	db   *fakeDB
	inTx bool
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *fakeConn) Close() error                        { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) {
	c.inTx = true
	return &fakeTx{conn: c}, nil
}

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.inTx = true
	return &fakeTx{conn: c}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.db.record(query, args, c.inTx); err != nil {
		return nil, err
	}
	c.db.lastID++
	return fakeResult{id: c.db.lastID}, nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.db.record(query, args, c.inTx); err != nil {
		return nil, err
	}
	if columns, rows := c.db.answerFor(); columns != nil {
		return &fakeRows{columns: columns, rows: rows}, nil
	}
	return &fakeRows{values: c.db.rowsFor(query)}, nil
}

type fakeTx struct{ conn *fakeConn }

func (t *fakeTx) Commit() error {
	err := t.conn.db.record("COMMIT", nil, t.conn.inTx)
	t.conn.inTx = false
	return err
}

func (t *fakeTx) Rollback() error {
	err := t.conn.db.record("ROLLBACK", nil, t.conn.inTx)
	t.conn.inTx = false
	return err
}

// fakeResult reports an identifier, because a driver that does is what
// InsertGetID needs and a zero would be indistinguishable from "none".
type fakeResult struct{ id int64 }

func (r fakeResult) LastInsertId() (int64, error) { return r.id, nil }
func (fakeResult) RowsAffected() (int64, error)   { return 1, nil }

// fakeRows mimics the shape of the migrations table -- id, batch, applied_at --
// unless the fixture set one of its own with answer, which a query whose shape
// is a module's table needs.
type fakeRows struct {
	values []string
	i      int

	columns []string
	rows    [][]driver.Value
}

func (r *fakeRows) Columns() []string {
	if r.columns != nil {
		return r.columns
	}
	return []string{"id", "batch", "applied_at"}
}

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.columns != nil {
		if r.i >= len(r.rows) {
			return io.EOF
		}
		copy(dest, r.rows[r.i])
		r.i++
		return nil
	}

	if r.i >= len(r.values) {
		return io.EOF
	}
	dest[0] = r.values[r.i]
	if len(dest) > 1 {
		dest[1] = int64(1)
		dest[2] = time.Unix(0, 0).UTC()
	}
	r.i++
	return nil
}
