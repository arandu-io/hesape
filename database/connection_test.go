package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	dbevents "github.com/arandu-io/hesape/database/events"
)

// countingDriver is the smallest driver that reports an identifier: each Exec
// returns the next integer. It is here rather than in fakedb_test.go because
// that file is package database_test and these tests reach boundConnection,
// which is unexported.
type countingDriver struct{ n atomic.Int64 }

var counting = &countingDriver{}

func init() { sql.Register("arandu-counting", counting) }

func (d *countingDriver) Open(string) (driver.Conn, error) { return &countingConn{d: d}, nil }

type countingConn struct{ d *countingDriver }

func (c *countingConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *countingConn) Close() error                        { return nil }
func (c *countingConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c *countingConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return countingResult{id: c.d.n.Add(1)}, nil
}

type countingResult struct{ id int64 }

func (r countingResult) LastInsertId() (int64, error) { return r.id, nil }
func (countingResult) RowsAffected() (int64, error)   { return 1, nil }

// openCountingPool opens a pool over that driver.
func openCountingPool(t *testing.T) *sql.DB {
	t.Helper()
	pool, err := sql.Open("arandu-counting", "counting")
	if err != nil {
		t.Fatalf("opening the pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func TestQueryExceptionMessageCarriesTheStatementAndValues(t *testing.T) {
	err := NewQueryException(
		"pgsql",
		"select * from invoice where tenant_id = ? and id = ?",
		[]any{"acme", 41},
		errors.New(`ERROR: relation "invoice" does not exist`),
		map[string]any{"driver": "pgsql", "host": "127.0.0.1", "port": "5432", "database": "arandu"},
		"write",
	)

	message := err.Error()
	for _, want := range []string{
		`relation "invoice" does not exist`,
		"Connection: pgsql",
		"Host: 127.0.0.1",
		"Port: 5432",
		"Database: arandu",
		"'acme'",
		"41",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("the message does not name %q:\n%s", want, message)
		}
	}
}

func TestQueryExceptionLeavesHostAndPortOutForSQLite(t *testing.T) {
	err := NewQueryException("sqlite", "select 1", nil, errors.New("boom"),
		map[string]any{"driver": "sqlite", "database": "database/database.sqlite"}, "")

	if strings.Contains(err.Error(), "Host:") {
		t.Fatalf("a SQLite connection has no host, and the message names one:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "Database: database/database.sqlite") {
		t.Fatalf("the message does not name the file:\n%s", err.Error())
	}
}

func TestUniqueConstraintViolationUnwrapsToTheDriverError(t *testing.T) {
	driverErr := errors.New("duplicate key value violates unique constraint")
	err := NewUniqueConstraintViolationException("pgsql", "insert into x", nil, driverErr, nil, "")

	if !errors.Is(err, driverErr) {
		t.Fatal("errors.Is cannot reach the driver error through the unique-constraint exception")
	}
}

func TestCausedByLostConnection(t *testing.T) {
	if !CausedByLostConnection(errors.New("write tcp: broken pipe")) {
		t.Fatal("a broken pipe is a lost connection")
	}
	if !CausedByLostConnection(errors.New("MySQL server has gone away")) {
		t.Fatal("\"server has gone away\" is the canonical lost connection")
	}
	if CausedByLostConnection(errors.New("syntax error at or near \"slect\"")) {
		t.Fatal("a syntax error was read as a lost connection, so the query would be retried")
	}
	if CausedByLostConnection(nil) {
		t.Fatal("no error is not a lost connection")
	}
}

func TestCausedByConcurrencyError(t *testing.T) {
	for _, message := range []string{
		"ERROR: deadlock detected (SQLSTATE 40P01)",
		"Error 1213: Deadlock found when trying to get lock",
		"database is locked",
		"SQLSTATE 40001: could not serialize access",
	} {
		if !CausedByConcurrencyError(errors.New(message)) {
			t.Fatalf("%q was not read as a concurrency error, so it would not be retried", message)
		}
	}
	if CausedByConcurrencyError(errors.New("column \"total\" does not exist")) {
		t.Fatal("a missing column was read as a concurrency error, so it would be retried forever")
	}
}

func TestSubstituteBindingsQuotesAndEscapes(t *testing.T) {
	got := substituteBindings("select * from x where a = ? and b = ? and c = ?",
		[]any{"o'brien", 3, nil})

	want := "select * from x where a = 'o''brien' and b = 3 and c = null"
	if got != want {
		t.Fatalf("substituteBindings answered\n%s\nwant\n%s", got, want)
	}
}

func TestSubstituteBindingsLeavesExtraPlaceholdersAlone(t *testing.T) {
	got := substituteBindings("select ?, ?", []any{1})
	if got != "select 1, ?" {
		t.Fatalf("substituteBindings answered %q", got)
	}
}

func TestConnectionReadsItsConfiguration(t *testing.T) {
	connection := NewConnection(nil, "arandu", "acme_", map[string]any{
		"driver": "pgsql", "name": "primary", "host": "db.internal",
	})

	if connection.GetName() != "primary" {
		t.Fatalf("GetName = %q", connection.GetName())
	}
	if connection.GetDriverName() != "pgsql" {
		t.Fatalf("GetDriverName = %q", connection.GetDriverName())
	}
	if connection.GetDatabaseName() != "arandu" {
		t.Fatalf("GetDatabaseName = %q", connection.GetDatabaseName())
	}
	if connection.GetTablePrefix() != "acme_" {
		t.Fatalf("GetTablePrefix = %q", connection.GetTablePrefix())
	}

	connection.SetReadWriteType("read")
	if connection.GetNameWithReadWriteType() != "primary::read" {
		t.Fatalf("GetNameWithReadWriteType = %q", connection.GetNameWithReadWriteType())
	}
}

func TestWithoutTablePrefixPutsItBack(t *testing.T) {
	connection := NewConnection(nil, "arandu", "acme_", nil)

	err := connection.WithoutTablePrefix(func(c *Connection) error {
		if c.GetTablePrefix() != "" {
			t.Fatalf("the prefix is still %q inside WithoutTablePrefix", c.GetTablePrefix())
		}
		return errors.New("the callback failed")
	})
	if err == nil {
		t.Fatal("WithoutTablePrefix swallowed the callback's error")
	}
	if connection.GetTablePrefix() != "acme_" {
		t.Fatalf("the prefix is %q after a failed callback, and it must be put back", connection.GetTablePrefix())
	}
}

func TestRecordsHaveBeenModifiedOnlyEverSets(t *testing.T) {
	connection := NewConnection(nil, "", "", nil)

	connection.RecordsHaveBeenModified(true)
	connection.RecordsHaveBeenModified(false)

	if !connection.HasModifiedRecords() {
		t.Fatal("RecordsHaveBeenModified(false) cleared the flag, and the PHP's guard says it never does")
	}

	connection.ForgetRecordModificationState()
	if connection.HasModifiedRecords() {
		t.Fatal("ForgetRecordModificationState did not clear it")
	}
}

func TestPrepareBindingsConvertsBoolsAndTimes(t *testing.T) {
	connection := NewConnection(nil, "", "", nil)

	got := connection.PrepareBindings([]any{true, false, "x", 3})
	if got[0] != 1 || got[1] != 0 {
		t.Fatalf("PrepareBindings answered %v; a bool becomes 0 or 1 because the engines disagree about the wire form", got)
	}
	if got[2] != "x" || got[3] != 3 {
		t.Fatalf("PrepareBindings changed a value it should not have: %v", got)
	}
}

func TestEscape(t *testing.T) {
	connection := NewConnection(nil, "", "", nil)

	for _, tc := range []struct{ in, want any }{
		{nil, "null"},
		{true, "1"},
		{false, "0"},
		{7, "7"},
		{"o'brien", "'o''brien'"},
	} {
		got, err := connection.Escape(tc.in, false)
		if err != nil {
			t.Fatalf("Escape(%v): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Escape(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := connection.Escape("a\x00b", false); err == nil {
		t.Fatal("a string with a null byte was escaped, and it cannot be safely")
	}
}

func TestConnectionWithNoReconnectorSaysSo(t *testing.T) {
	connection := NewConnection(nil, "", "", nil)

	err := connection.ReconnectIfMissingConnection()

	var lost *LostConnectionException
	if !errors.As(err, &lost) {
		t.Fatalf("a connection with no pool and no reconnector answered %v", err)
	}
}

func TestQueryLog(t *testing.T) {
	connection := NewConnection(nil, "", "", map[string]any{"name": "primary"})

	connection.LogQuery("select 1", nil, 1.5)
	if len(connection.GetQueryLog()) != 0 {
		t.Fatal("the log recorded a query while logging was off")
	}

	connection.EnableQueryLog()
	connection.LogQuery("select ?", []any{2}, 1.5)

	log := connection.GetQueryLog()
	if len(log) != 1 || log[0].Query != "select ?" || log[0].Time != 1.5 {
		t.Fatalf("the log holds %+v", log)
	}
	if connection.TotalQueryDuration() != 3.0 {
		t.Fatalf("TotalQueryDuration = %v, and it counts every query whether or not it was logged", connection.TotalQueryDuration())
	}

	raw := connection.GetRawQueryLog()
	if raw[0].Query != "select 2" {
		t.Fatalf("GetRawQueryLog answered %q", raw[0].Query)
	}

	connection.FlushQueryLog()
	if len(connection.GetQueryLog()) != 0 {
		t.Fatal("FlushQueryLog left entries behind")
	}

	connection.ResetTotalQueryDuration()
	if connection.TotalQueryDuration() != 0 {
		t.Fatal("ResetTotalQueryDuration left the total behind")
	}
}

func TestListenSeesEveryQuery(t *testing.T) {
	connection := NewConnection(nil, "", "", map[string]any{"name": "primary"})

	seen := 0
	connection.Listen(func(*dbevents.QueryExecuted) { seen++ })

	connection.LogQuery("select 1", nil, 0.1)
	connection.LogQuery("select 2", nil, 0.1)

	if seen != 2 {
		t.Fatalf("the listener saw %d queries, want 2", seen)
	}
}

// TestInsertGetIDReadsTheIdentifierFromTheStatementThatCausedIt covers the path
// that was unreachable: a query builder over a real Connection asking for the
// identifier of the row it just inserted.
//
// It could not work before. The processor asks the connection through
// LastInsertIDConnection, and the type that makes a Connection usable as a
// query.GetConnection() did not implement it -- so InsertGetID answered "this
// connection cannot report the identifier it assigned" against every engine,
// while every test that exercised it used a fake that could.
//
// The identifier is read from sql.Result rather than by asking the engine
// afterwards. On a pooled connection the second question is answered by
// whichever statement ran most recently, which is how one request comes to be
// handed another request's row.
func TestInsertGetIDReadsTheIdentifierFromTheStatementThatCausedIt(t *testing.T) {
	connection := NewConnection(openCountingPool(t), "arandu", "", nil)

	first, err := connection.InsertReturningID(context.Background(),
		"insert into invoices (total) values (?)", []any{100})
	if err != nil {
		t.Fatalf("InsertReturningID: %v", err)
	}
	second, err := connection.InsertReturningID(context.Background(),
		"insert into invoices (total) values (?)", []any{200})
	if err != nil {
		t.Fatalf("InsertReturningID: %v", err)
	}

	if first == 0 || second == 0 {
		t.Fatalf("identifiers were %d and %d; a zero is indistinguishable from none", first, second)
	}
	if first == second {
		t.Fatalf("both inserts reported %d, so the identifier is not the one the statement assigned", first)
	}
}

// TestTheBoundConnectionAnswersTheProcessor proves the seam the processor uses.
func TestTheBoundConnectionAnswersTheProcessor(t *testing.T) {
	connection := NewConnection(openCountingPool(t), "arandu", "", nil)
	bound := &boundConnection{connection: connection}

	// Nothing inserted yet: it says so rather than answering zero, because a
	// zero identifier reads like a row.
	if _, err := bound.GetLastInsertID(""); err == nil {
		t.Fatal("GetLastInsertID answered before any insert; a zero would read like a row")
	}

	if _, err := bound.Insert(context.Background(), "insert into invoices (total) values (?)", []any{100}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	id, err := bound.GetLastInsertID("")
	if err != nil {
		t.Fatalf("GetLastInsertID: %v", err)
	}
	if id == 0 {
		t.Fatal("GetLastInsertID answered 0 after an insert that reported one")
	}
}
