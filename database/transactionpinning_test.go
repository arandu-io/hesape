package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database"
)

// TestAStatementInsideATransactionRunsOnTheTransactionConnection: a BEGIN
// issued on one pooled connection and a statement sent to another is not a
// transaction. The pool here holds a single connection, which is what the
// SQLite pool holds, so a statement that went to the pool would wait forever
// for the connection the BEGIN is holding.
func TestAStatementInsideATransactionRunsOnTheTransactionConnection(t *testing.T) {
	handle, state := newFakeDB()
	handle.SetMaxOpenConns(1)

	conn := database.NewConnection(handle, "app", "", map[string]any{"driver": "sqlite"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := conn.Transaction(func() error {
		_, err := conn.Statement(ctx, `CREATE TABLE widgets (id VARCHAR(255) PRIMARY KEY)`, nil)
		return err
	}, 1)
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}

	for _, want := range []string{"BEGIN", "CREATE TABLE widgets", "COMMIT"} {
		if !state.sawStatement(want) {
			t.Errorf("the driver never saw %q: %q", want, state.statements())
		}
	}
}

// TestASelectInsideATransactionRunsOnTheTransactionConnection is the read half
// of the same rule: a migration that reads before it writes has to see what the
// transaction it is in has already done.
func TestASelectInsideATransactionRunsOnTheTransactionConnection(t *testing.T) {
	handle, state := newFakeDB()
	handle.SetMaxOpenConns(1)

	conn := database.NewConnection(handle, "app", "", map[string]any{"driver": "sqlite"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := conn.Transaction(func() error {
		_, err := conn.Select(ctx, `SELECT id FROM widgets`, nil, true)
		return err
	}, 1)
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}

	if !state.sawStatement("SELECT id FROM widgets") {
		t.Errorf("the driver never saw the select: %q", state.statements())
	}
}
