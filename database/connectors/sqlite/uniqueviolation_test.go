package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/connectors/sqlite"
)

// openWithAccounts opens a fresh database holding one row in a table with a
// primary key, a unique index and a NOT NULL column -- the three constraints
// the classification has to tell apart.
func openWithAccounts(t *testing.T) *database.DB {
	t.Helper()
	db, closeDB, err := database.Open(database.Config{
		Connection: database.DialectSQLite,
		Database:   filepath.Join(t.TempDir(), "app.sqlite"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(closeDB)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE account (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO account (id, email) VALUES (?, ?)`, "a-1", "ana@example.com"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	return db
}

// TestADuplicateIsAUniqueViolation reads the code the driver really returns,
// for both kinds of key: SQLite reports a duplicate primary key and a duplicate
// unique index under two different extended codes, and a detector that knew
// only one of them would miss half the collisions.
func TestADuplicateIsAUniqueViolation(t *testing.T) {
	db := openWithAccounts(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		id, email string
	}{
		{"a unique index", "a-2", "ana@example.com"},
		{"the primary key", "a-1", "bia@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, `INSERT INTO account (id, email) VALUES (?, ?)`, tc.id, tc.email)
			if err == nil {
				t.Fatal("the duplicate was accepted")
			}
			if !(sqlite.SQLiteConnector{}).CausedByUniqueViolation(err) {
				t.Fatalf("a duplicate on %s was not recognised: %v", tc.name, err)
			}
			wrapped := fmt.Errorf("creating the account: %w", err)
			if !(sqlite.SQLiteConnector{}).CausedByUniqueViolation(wrapped) {
				t.Fatal("the code is lost once the error is wrapped, so a repository that adds context hides the violation")
			}
		})
	}
}

// TestAnotherConstraintIsNotAUniqueViolation: SQLite reports a NOT NULL failure
// with the same primary code as a duplicate, and answering it as one turns a
// blank field into "that already exists".
func TestAnotherConstraintIsNotAUniqueViolation(t *testing.T) {
	db := openWithAccounts(t)

	_, err := db.ExecContext(context.Background(), `INSERT INTO account (id, email) VALUES (?, ?)`, "a-2", nil)
	if err == nil {
		t.Fatal("a NULL in a NOT NULL column was accepted")
	}
	if (sqlite.SQLiteConnector{}).CausedByUniqueViolation(err) {
		t.Fatalf("a NOT NULL violation was recognised as a duplicate: %v", err)
	}
}

func TestAnErrorFromElsewhereIsNotAUniqueViolation(t *testing.T) {
	for _, err := range []error{nil, errors.New("UNIQUE constraint failed: account.email")} {
		if (sqlite.SQLiteConnector{}).CausedByUniqueViolation(err) {
			t.Fatalf("%v was recognised as a duplicate; only the driver's code may say so, never a message", err)
		}
	}
}
