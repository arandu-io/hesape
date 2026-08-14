package queue_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
)

// A minimal database/sql driver, so the DatabaseQueue can be tested without a
// server. It is written here rather than pulled from a mock library because the
// collection has one dependency and a test dependency is still a dependency --
// the same reason database and events each have one of these.
//
// What it proves is the statements: which SQL the driver issues, in which
// order, with which arguments. What it cannot prove is what an engine does with
// them -- that a claim is atomic, that an expired lease reappears -- and those
// belong to the conformance suite that runs against the three real engines.

func init() { sql.Register("arandu-fake-jobs", fakeDriver{}) }

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	return &fakeConn{db: lookupFake(dsn)}, nil
}

var (
	fakeMu  sync.Mutex
	fakeDBs = map[string]*fakeDB{}
	fakeSeq int
)

// answer is what the fake replies with to a query whose text contains pattern.
type answer struct {
	pattern string
	columns []string
	rows    [][]driver.Value
}

// fakeDB is the state a test sets up and asserts on.
type fakeDB struct {
	mu       sync.Mutex
	stmts    []string
	args     [][]driver.NamedValue
	answers  []answer
	affected int64
}

// newFakeDB registers a fake and returns the handle plus its state.
func newFakeDB() (*sql.DB, *fakeDB) {
	fakeMu.Lock()
	fakeSeq++
	dsn := fmt.Sprintf("fake-jobs-%d", fakeSeq)
	state := &fakeDB{affected: 1}
	fakeDBs[dsn] = state
	fakeMu.Unlock()

	db, err := sql.Open("arandu-fake-jobs", dsn)
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

// answerWith makes every query containing pattern return these rows.
func (f *fakeDB) answerWith(pattern string, columns []string, rows ...[]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, answer{pattern: pattern, columns: columns, rows: rows})
}

func (f *fakeDB) record(query string, args []driver.NamedValue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stmts = append(f.stmts, query)
	f.args = append(f.args, args)
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

// argsFor returns the arguments of the first statement containing substring.
func (f *fakeDB) argsFor(substring string) []driver.NamedValue {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, s := range f.stmts {
		if strings.Contains(strings.Join(strings.Fields(s), " "), substring) {
			return f.args[i]
		}
	}
	return nil
}

// argsForAll returns the arguments of every statement containing substring, in
// the order they were issued. It is what argsFor cannot answer: whether two
// pushes wrote what distinguishes them.
func (f *fakeDB) argsForAll(substring string) [][]driver.NamedValue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]driver.NamedValue
	for i, s := range f.stmts {
		if strings.Contains(strings.Join(strings.Fields(s), " "), substring) {
			out = append(out, f.args[i])
		}
	}
	return out
}

func (f *fakeDB) answerFor(query string) (answer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.answers {
		if strings.Contains(query, a.pattern) {
			return a, true
		}
	}
	return answer{}, false
}

func (f *fakeDB) rowsAffected() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.affected
}

func (f *fakeDB) setRowsAffected(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.affected = n
}

type fakeConn struct{ db *fakeDB }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.db.record(query, args)
	return fakeResult{affected: c.db.rowsAffected()}, nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.db.record(query, args)
	a, known := c.db.answerFor(query)
	if !known {
		return &fakeRows{columns: []string{"empty"}}, nil
	}
	return &fakeRows{columns: a.columns, values: a.rows}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeResult struct{ affected int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeRows struct {
	columns []string
	values  [][]driver.Value
	i       int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.i])
	r.i++
	return nil
}
