package pgx_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database/connectors/pgx"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestTheCodeDecidesNotTheMessage holds the classification to the SQLSTATE
// without a server: the message is localised by lc_messages, so the two errors
// below carry the same sentence and only the code tells them apart.
func TestTheCodeDecidesNotTheMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"unique_violation", &pgconn.PgError{Code: "23505", Message: "constraint failed"}, true},
		{"unique_violation, wrapped", fmt.Errorf("creating the account: %w", &pgconn.PgError{Code: "23505"}), true},
		{"not_null_violation", &pgconn.PgError{Code: "23502", Message: "constraint failed"}, false},
		{"a message and no code", errors.New(`duplicate key value violates unique constraint "account_email_key" (SQLSTATE 23505)`), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (pgx.PostgresConnector{}).CausedByUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("CausedByUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestADuplicateOnTheServerIsAUniqueViolation reads the error a real server
// sends, which is the only proof that the code is read from the field pgx fills.
// It skips without ARANDU_TEST_POSTGRES_DSN, like the conformance suite.
func TestADuplicateOnTheServerIsAUniqueViolation(t *testing.T) {
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_POSTGRES_DSN to run this against a real server")
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
	insert := `INSERT INTO arandu_unique (id, email) VALUES ($1, $2)`
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
			if got := (pgx.PostgresConnector{}).CausedByUniqueViolation(err); got != tc.match {
				t.Fatalf("CausedByUniqueViolation = %v, want %v, for %v", got, tc.match, err)
			}
		})
	}
}
