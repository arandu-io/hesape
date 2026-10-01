package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database/connectors/mysql"
	gomysql "github.com/go-sql-driver/mysql"
)

// TestTheNumberDecidesNotTheMessage holds the classification to the error
// number without a server. The SQLSTATE cannot decide it: MySQL sends 23000 for
// a duplicate and for a NULL in a NOT NULL column alike.
func TestTheNumberDecidesNotTheMessage(t *testing.T) {
	integrity := [5]byte{'2', '3', '0', '0', '0'}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"ER_DUP_ENTRY", &gomysql.MySQLError{Number: 1062, SQLState: integrity}, true},
		{"ER_DUP_ENTRY_WITH_KEY_NAME", &gomysql.MySQLError{Number: 1586, SQLState: integrity}, true},
		{"ER_DUP_ENTRY, wrapped", fmt.Errorf("creating the account: %w", &gomysql.MySQLError{Number: 1062}), true},
		{"ER_BAD_NULL_ERROR", &gomysql.MySQLError{Number: 1048, SQLState: integrity}, false},
		{"a message and no number", errors.New("Error 1062 (23000): Duplicate entry 'ana@example.com' for key 'email'"), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (mysql.MySqlConnector{}).CausedByUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("CausedByUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestADuplicateOnTheServerIsAUniqueViolation reads the error a real server
// sends. It skips without ARANDU_TEST_MYSQL_DSN, like the conformance suite.
func TestADuplicateOnTheServerIsAUniqueViolation(t *testing.T) {
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_MYSQL_DSN to run this against a real server")
	}
	sqldb, err := sql.Open(driverName(t), dsn())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A temporary table lives on one session, so the statements are pinned to
	// one connection rather than spread over the pool.
	conn, err := sqldb.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.ExecContext(ctx, `CREATE TEMPORARY TABLE arandu_unique (id VARCHAR(255) PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	insert := `INSERT INTO arandu_unique (id, email) VALUES (?, ?)`
	if _, err := conn.ExecContext(ctx, insert, "1", "ana@example.com"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	for _, tc := range []struct {
		name  string
		args  []any
		match bool
	}{
		{"a unique index", []any{"2", "ana@example.com"}, true},
		{"the primary key", []any{"1", "bia@example.com"}, true},
		{"a NOT NULL column", []any{"3", nil}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, insert, tc.args...)
			if err == nil {
				t.Fatal("the server accepted the row")
			}
			if got := (mysql.MySqlConnector{}).CausedByUniqueViolation(err); got != tc.match {
				t.Fatalf("CausedByUniqueViolation = %v, want %v, for %v", got, tc.match, err)
			}
		})
	}
}
