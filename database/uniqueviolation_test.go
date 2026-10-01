package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// The tests are in the package rather than beside it for the reason the
// registry tests are: the connector a statement error is classified by is
// package state, and reset is the only way to keep these independent of the
// order the suite runs in.

// codedDriverError stands for the error type a real driver returns: a code the
// connector reads, and a message nothing reads.
type codedDriverError struct {
	code    string
	message string
}

func (e *codedDriverError) Error() string { return e.message }

// errDuplicate and errNotNull are the two failures the fake driver answers
// with, chosen by DSN. They differ in code only by design: a classifier that
// read the message would tell them apart by accident.
var (
	errDuplicate = &codedDriverError{code: "unique", message: "constraint failed"}
	errNotNull   = &codedDriverError{code: "not-null", message: "constraint failed"}
)

// classifyingConnector is a Connector that is also a UniqueViolationDetector,
// recognising the fake driver's code the way a real connector recognises an
// SQLSTATE.
type classifyingConnector struct{ testConnector }

func (classifyingConnector) CausedByUniqueViolation(err error) bool {
	var coded *codedDriverError
	return errors.As(err, &coded) && coded.code == "unique"
}

// failingDriver answers every statement with the error its DSN names.
type failingDriver struct{}

func init() { sql.Register("arandu-unique-test", failingDriver{}) }

func (failingDriver) Open(dsn string) (driver.Conn, error) {
	failure := error(errNotNull)
	if dsn == "duplicate" {
		failure = errDuplicate
	}
	return &failingConn{failure: failure}, nil
}

type failingConn struct{ failure error }

func (c *failingConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (c *failingConn) Close() error                        { return nil }
func (c *failingConn) Begin() (driver.Tx, error)           { return failingTx{}, nil }

func (c *failingConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, c.failure
}

func (c *failingConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, c.failure
}

type failingTx struct{}

func (failingTx) Commit() error   { return nil }
func (failingTx) Rollback() error { return nil }

// openFailing returns a DB on the fake driver, with the classifying connector
// registered for SQLite.
func openFailing(t *testing.T, dsn string) *DB {
	t.Helper()
	reset(t)
	Register(classifyingConnector{testConnector{DialectSQLite, "arandu-unique-test"}})

	sqldb, err := sql.Open("arandu-unique-test", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return Wrap(sqldb, DialectSQLite)
}

// assertUniqueViolation is the whole contract: errors.Is answers the sentinel,
// errors.As still reaches the driver's own type, and the message is the
// driver's.
func assertUniqueViolation(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUniqueViolation) {
		t.Fatalf("error = %v, want errors.Is(err, ErrUniqueViolation)", err)
	}
	var coded *codedDriverError
	if !errors.As(err, &coded) || coded != errDuplicate {
		t.Fatalf("errors.As does not reach the driver error through %T, so a caller loses the constraint the driver named", err)
	}
	if err.Error() != errDuplicate.Error() {
		t.Fatalf("message = %q, want the driver's own %q", err.Error(), errDuplicate.Error())
	}
}

func TestAUniqueViolationOnExecSatisfiesErrUniqueViolation(t *testing.T) {
	db := openFailing(t, "duplicate")
	_, err := db.ExecContext(context.Background(), "INSERT INTO users (email) VALUES (?)", "a@example.com")
	assertUniqueViolation(t, err)
}

// TestAUniqueViolationOnQuerySatisfiesErrUniqueViolation: an insert that
// returns its key goes through QueryContext, which is how a Postgres model
// insert reads its identifier back.
func TestAUniqueViolationOnQuerySatisfiesErrUniqueViolation(t *testing.T) {
	db := openFailing(t, "duplicate")
	_, err := db.QueryContext(context.Background(), "INSERT INTO users (email) VALUES (?) RETURNING id", "a@example.com")
	assertUniqueViolation(t, err)
}

// TestTheModelVerbsCarryTheClassification: Insert, Update and Select are what
// a model writes through, so they are what Create and Save return.
func TestTheModelVerbsCarryTheClassification(t *testing.T) {
	db := openFailing(t, "duplicate")
	ctx := context.Background()

	_, err := db.Insert(ctx, "INSERT INTO users (email) VALUES (?)", []any{"a@example.com"})
	assertUniqueViolation(t, err)

	_, err = db.Update(ctx, "UPDATE users SET email = ? WHERE id = ?", []any{"a@example.com", 1})
	assertUniqueViolation(t, err)

	_, err = db.Select(ctx, "INSERT INTO users (email) VALUES (?) RETURNING id", []any{"a@example.com"}, false)
	assertUniqueViolation(t, err)
}

// TestAUniqueViolationInsideATransactionIsClassified: a statement in a
// transaction runs on the transaction, not the pool, and the classification
// must not depend on which.
func TestAUniqueViolationInsideATransactionIsClassified(t *testing.T) {
	db := openFailing(t, "duplicate")

	err := Transaction(context.Background(), db, func(ctx context.Context) error {
		_, err := db.ExecContext(ctx, "INSERT INTO users (email) VALUES (?)", "a@example.com")
		return err
	})
	assertUniqueViolation(t, err)
}

// TestAnotherConstraintIsNotAUniqueViolation: a NOT NULL failure answered as a
// duplicate turns into a 409 and a "that already exists" for a field that was
// simply left blank.
func TestAnotherConstraintIsNotAUniqueViolation(t *testing.T) {
	db := openFailing(t, "not-null")
	_, err := db.ExecContext(context.Background(), "INSERT INTO users (email) VALUES (?)", nil)

	if errors.Is(err, ErrUniqueViolation) {
		t.Fatal("a NOT NULL violation was classified as a unique violation")
	}
	if err != error(errNotNull) {
		t.Fatalf("error = %#v, want the driver error returned untouched", err)
	}
}

// TestAConnectorThatDoesNotDetectLeavesTheErrorAlone: the detector is the
// connector's to offer, and one that offers none must see exactly what its
// driver returned.
func TestAConnectorThatDoesNotDetectLeavesTheErrorAlone(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "arandu-unique-test"})

	sqldb, err := sql.Open("arandu-unique-test", "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqldb.Close() }()

	_, err = Wrap(sqldb, DialectSQLite).ExecContext(context.Background(), "INSERT INTO users (email) VALUES (?)", "a@example.com")
	if err != error(errDuplicate) {
		t.Fatalf("error = %#v, want the driver error untouched when the connector does not detect", err)
	}
}

// TestTheClassificationFollowsTheDialectOfTheHandle: two engines linked into
// one binary each classify their own errors, so a handle on one dialect never
// consults the other's connector.
func TestTheClassificationFollowsTheDialectOfTheHandle(t *testing.T) {
	db := openFailing(t, "duplicate")
	Register(testConnector{DialectPostgres, "pgx"})

	sqldb := db.Unwrap()
	_, err := Wrap(sqldb, DialectPostgres).ExecContext(context.Background(), "INSERT INTO users (email) VALUES (?)", "a@example.com")
	if errors.Is(err, ErrUniqueViolation) {
		t.Fatal("a Postgres handle was classified by the SQLite connector")
	}
}

// TestAUniqueViolationOnAConnectionSatisfiesErrUniqueViolation: the
// Connection keeps its own exception type for the same failure, and a caller
// asks the one question either way.
func TestAUniqueViolationOnAConnectionSatisfiesErrUniqueViolation(t *testing.T) {
	db := openFailing(t, "duplicate")
	connection := NewConnection(db.Unwrap(), "app", "", map[string]any{"driver": "sqlite"})

	_, err := connection.Insert(context.Background(), "INSERT INTO users (email) VALUES (?)", []any{"a@example.com"})
	if !errors.Is(err, ErrUniqueViolation) {
		t.Fatalf("error = %v, want errors.Is(err, ErrUniqueViolation)", err)
	}
	var exception *UniqueConstraintViolationException
	if !errors.As(err, &exception) {
		t.Fatalf("error = %T, want the UniqueConstraintViolationException that carries the statement", err)
	}
	var coded *codedDriverError
	if !errors.As(err, &coded) || coded != errDuplicate {
		t.Fatal("errors.As does not reach the driver error through the exception")
	}
}

func TestAnotherConstraintOnAConnectionIsAPlainQueryException(t *testing.T) {
	db := openFailing(t, "not-null")
	connection := NewConnection(db.Unwrap(), "app", "", map[string]any{"driver": "sqlite"})

	_, err := connection.Insert(context.Background(), "INSERT INTO users (email) VALUES (?)", []any{nil})
	if errors.Is(err, ErrUniqueViolation) {
		t.Fatal("a NOT NULL violation on a Connection was classified as a unique violation")
	}
	var exception *UniqueConstraintViolationException
	if errors.As(err, &exception) {
		t.Fatal("a NOT NULL violation came back as a UniqueConstraintViolationException")
	}
}
