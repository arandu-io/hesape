package failed_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/queue/failed"
)

// A database/sql driver small enough to answer one question: what the primary
// key does to a second record of one failure. It keeps the ids it accepted,
// refuses a repeat the way an engine does, and counts.
//
// Written here rather than pulled from a mock library because the collection
// has one dependency and a test dependency is still a dependency.

func init() { sql.Register("arandu-fake-failed", keyedDriver{}) }

type keyedDriver struct{}

func (keyedDriver) Open(dsn string) (driver.Conn, error) {
	return &keyedConn{rows: lookupTable(dsn)}, nil
}

var (
	tablesMu sync.Mutex
	tables   = map[string]*keyedTable{}
	tableSeq int
)

// keyedTable is the rows, by id, with the tenant each belongs to.
//
// stored keeps each row as it was inserted, in the projection's own column
// order, so a read can be answered with what a write actually wrote. That is
// what makes an insert and a select that disagree about the columns visible
// here rather than at the first engine that runs them.
type keyedTable struct {
	mu     sync.Mutex
	byID   map[string]string
	stored map[string][]driver.Value
	order  []string
	insert int
}

func newKeyedTable() (*sql.DB, *keyedTable) {
	tablesMu.Lock()
	tableSeq++
	dsn := fmt.Sprintf("fake-failed-%d", tableSeq)
	table := &keyedTable{byID: map[string]string{}, stored: map[string][]driver.Value{}}
	tables[dsn] = table
	tablesMu.Unlock()

	db, err := sql.Open("arandu-fake-failed", dsn)
	if err != nil {
		panic(err)
	}
	return db, table
}

func lookupTable(dsn string) *keyedTable {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	return tables[dsn]
}

func (tb *keyedTable) rows() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return len(tb.byID)
}

func (tb *keyedTable) inserts() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.insert
}

type keyedConn struct{ rows *keyedTable }

func (c *keyedConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *keyedConn) Close() error                        { return nil }
func (c *keyedConn) Begin() (driver.Tx, error)           { return keyedTx{}, nil }

func (c *keyedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "INSERT INTO") {
		return keyedResult{}, nil
	}
	// An engine binds by position, so a statement naming one more column than
	// it carries values for is refused before it is run. Refusing it here too
	// is what keeps a column added to the insert and forgotten in the values
	// from passing as a working write.
	if placeholders := strings.Count(query, "?"); placeholders != len(args) {
		return nil, fmt.Errorf("the insert has %d placeholders and %d values", placeholders, len(args))
	}
	id, _ := args[0].Value.(string)
	tenantID, _ := args[2].Value.(string)

	c.rows.mu.Lock()
	defer c.rows.mu.Unlock()
	c.rows.insert++
	if _, taken := c.rows.byID[id]; taken {
		return nil, errors.New("UNIQUE constraint failed: failed_jobs.id")
	}
	c.rows.byID[id] = tenantID

	row := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		row = append(row, arg.Value)
	}
	c.rows.stored[id] = row
	c.rows.order = append([]string{id}, c.rows.order...)
	return keyedResult{}, nil
}

func (c *keyedConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT id, uuid") {
		return c.projection(query)
	}
	if !strings.Contains(query, "count(*)") {
		return &keyedRows{columns: []string{"empty"}}, nil
	}
	wantTenant, _ := args[0].Value.(string)
	wantID, _ := args[1].Value.(string)

	c.rows.mu.Lock()
	defer c.rows.mu.Unlock()
	found := int64(0)
	if owner, stored := c.rows.byID[wantID]; stored && owner == wantTenant {
		found = 1
	}
	return &keyedRows{columns: []string{"count"}, values: [][]driver.Value{{found}}}, nil
}

// projection answers a read with the rows as they were written.
//
// The insert names its columns in the projection's order, so a stored row is
// already the row this hands back. The tenant filter is the provider's and not
// this fake's -- what is under test here is the shape of the two statements.
func (c *keyedConn) projection(query string) (driver.Rows, error) {
	list := query[strings.Index(query, "SELECT ")+len("SELECT ") : strings.Index(query, "FROM")]
	columns := strings.Split(list, ",")
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
	}

	c.rows.mu.Lock()
	defer c.rows.mu.Unlock()
	values := make([][]driver.Value, 0, len(c.rows.order))
	for _, id := range c.rows.order {
		row := c.rows.stored[id]
		if len(row) != len(columns) {
			return nil, fmt.Errorf("the read names %d columns and the write stored %d",
				len(columns), len(row))
		}
		values = append(values, row)
	}
	return &keyedRows{columns: columns, values: values}, nil
}

type keyedTx struct{}

func (keyedTx) Commit() error   { return nil }
func (keyedTx) Rollback() error { return nil }

type keyedResult struct{}

func (keyedResult) LastInsertId() (int64, error) { return 0, nil }
func (keyedResult) RowsAffected() (int64, error) { return 1, nil }

type keyedRows struct {
	columns []string
	values  [][]driver.Value
	i       int
}

func (r *keyedRows) Columns() []string { return r.columns }
func (r *keyedRows) Close() error      { return nil }

func (r *keyedRows) Next(dest []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.i])
	r.i++
	return nil
}

// TestTheDatabaseProviderRecordsOneFailureOnce: the id is the job's own uuid
// and it is the primary key, so the engine refuses the second record. That
// refusal is the answer the caller wanted -- the failure is listed -- and
// returning it as an error would make a worker report a broken dead letter list
// every time one job was recorded twice.
func TestTheDatabaseProviderRecordsOneFailureOnce(t *testing.T) {
	ctx := context.Background()
	sqldb, table := newKeyedTable()
	t.Cleanup(func() { _ = sqldb.Close() })

	p := failed.NewDatabaseFailedJobProvider(database.Wrap(sqldb, database.DialectSQLite), "")
	record := failed.FailedJob{
		UUID:       "job-1",
		Connection: "database",
		Queue:      "default",
		Name:       "invoice.send",
		Payload:    []byte(`{"id":"i-1"}`),
		Exception:  "the payment gateway is down",
	}

	first, err := p.Log(ctx, grantFor(tenant), record)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	second, err := p.Log(ctx, grantFor(tenant), record)
	if err != nil {
		t.Fatalf("recording the same failure again: %v", err)
	}

	if first != second {
		t.Errorf("the two records came back as %q and %q", first, second)
	}
	if table.inserts() != 2 {
		t.Fatalf("the provider issued %d inserts, so the test proved nothing", table.inserts())
	}
	if rows := table.rows(); rows != 1 {
		t.Errorf("the table holds %d rows for one failure", rows)
	}
}

// TestTheDatabaseProviderKeepsThePermissionTheJobWasPushedUnder: the record has
// to carry the action, because a retry rebuilds the job's Grant from it.
//
// Written and read back rather than only written: the insert and the projection
// name their columns independently, and a column added to one and not the other
// is a provider that stores the action and answers without it.
func TestTheDatabaseProviderKeepsThePermissionTheJobWasPushedUnder(t *testing.T) {
	ctx := context.Background()
	sqldb, _ := newKeyedTable()
	t.Cleanup(func() { _ = sqldb.Close() })

	p := failed.NewDatabaseFailedJobProvider(database.Wrap(sqldb, database.DialectSQLite), "")
	if _, err := p.Log(ctx, grantFor(tenant), failed.FailedJob{
		UUID:       "job-1",
		Connection: "database",
		Queue:      "default",
		Name:       "invoice.send",
		Action:     "invoice.send",
		Payload:    []byte(`{"id":"i-1"}`),
		Exception:  "the payment gateway is down",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	all, err := p.All(ctx, grantFor(tenant))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("the table answered with %d records", len(all))
	}
	if all[0].Action != "invoice.send" {
		t.Errorf("the record came back under the action %q, want the one it was logged with",
			all[0].Action)
	}
}

// TestTheDatabaseProviderStillReportsAnInsertItCannotExplain: the read-back is
// narrow on purpose. An insert that failed for any other reason -- and one that
// collided with another tenant's row, which is not this tenant's failure -- is
// still an error.
func TestTheDatabaseProviderStillReportsAnInsertItCannotExplain(t *testing.T) {
	ctx := context.Background()
	sqldb, _ := newKeyedTable()
	t.Cleanup(func() { _ = sqldb.Close() })

	p := failed.NewDatabaseFailedJobProvider(database.Wrap(sqldb, database.DialectSQLite), "")
	record := failed.FailedJob{UUID: "job-1", Name: "invoice.send"}

	if _, err := p.Log(ctx, grantFor(tenant), record); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := p.Log(ctx, grantFor(other), record); err == nil {
		t.Error("an id already held by another tenant was reported as this tenant's failure")
	}
}
