package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// The contract of AfterCommit, which is a contract about when and not about
// what.
//
// It exists because a callback that runs inside the transaction it is about has
// announced something that a rollback can still take back, and there is no
// message that takes it back. The tests below are the four cases a caller can
// be in: no transaction, one that commits, one that rolls back, and one nested
// inside another.

// afterCommitDB is a handle over the package's fake driver, which is what
// every other test of this file's subject uses: the question here is when a
// callback runs, and that is answered by the transaction and not by an engine.
func afterCommitDB(t *testing.T) *database.DB {
	t.Helper()

	sqldb, _ := newFakeDB()
	t.Cleanup(func() { _ = sqldb.Close() })
	return database.Wrap(sqldb, database.DialectSQLite)
}

func TestAfterCommitRunsOutsideATransactionImmediately(t *testing.T) {
	db := afterCommitDB(t)

	ran := 0
	if err := database.AfterCommit(context.Background(), db, func(context.Context) { ran++ }); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if ran != 1 {
		t.Errorf("ran %d times outside a transaction, want 1: there is nothing left to wait for", ran)
	}
}

func TestAfterCommitWaitsForTheCommit(t *testing.T) {
	db := afterCommitDB(t)

	ran, inside := 0, 0
	err := database.Transaction(context.Background(), db, func(ctx context.Context) error {
		if err := database.AfterCommit(ctx, db, func(after context.Context) {
			ran++
			if database.InTransaction(after, db) {
				inside++
			}
		}); err != nil {
			return err
		}
		if ran != 0 {
			t.Error("the callback ran before the transaction committed")
		}
		_, err := db.ExecContext(ctx, "INSERT INTO customer (id) VALUES (?)", "1")
		return err
	})
	if err != nil {
		t.Fatalf("the transaction: %v", err)
	}
	if ran != 1 {
		t.Errorf("ran %d times after one commit, want 1", ran)
	}
	if inside != 0 {
		t.Errorf("the callback saw itself as inside the transaction %d times, and the transaction had ended", inside)
	}
}

func TestARollbackDiscardsTheCallback(t *testing.T) {
	db := afterCommitDB(t)

	ran := 0
	refused := errors.New("the rule said no")
	err := database.Transaction(context.Background(), db, func(ctx context.Context) error {
		if err := database.AfterCommit(ctx, db, func(context.Context) { ran++ }); err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("the transaction reported %v, want the caller's error", err)
	}
	if ran != 0 {
		t.Errorf("a rolled back transaction ran %d callbacks, and it wrote nothing to announce", ran)
	}
}

// TestANestedRegistrationBelongsToTheOutermostTransaction is the case the
// wallet was wrong about: a package that opens its own transaction, joined to
// one the application already had, and announced its write when its own call
// returned rather than when the application's transaction ended.
func TestANestedRegistrationBelongsToTheOutermostTransaction(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "outer commits"
		if !commit {
			name = "outer rolls back"
		}
		t.Run(name, func(t *testing.T) {
			db := afterCommitDB(t)

			ran := 0
			abort := errors.New("outer rollback")
			err := database.Transaction(context.Background(), db, func(outer context.Context) error {
				// The inner one joins, so it opens nothing and commits nothing.
				if err := database.Transaction(outer, db, func(inner context.Context) error {
					return database.AfterCommit(inner, db, func(context.Context) { ran++ })
				}); err != nil {
					return err
				}
				if ran != 0 {
					t.Error("the callback ran when the inner transaction returned, and the inner one committed nothing")
				}
				if !commit {
					return abort
				}
				return nil
			})

			switch {
			case commit && err != nil:
				t.Fatalf("the outer transaction: %v", err)
			case !commit && !errors.Is(err, abort):
				t.Fatalf("the outer transaction reported %v, want the caller's error", err)
			}

			want := 0
			if commit {
				want = 1
			}
			if ran != want {
				t.Errorf("ran %d times, want %d: a nested registration belongs to the outermost transaction", ran, want)
			}
		})
	}
}

// TestAPanickingCallbackDoesNotStopTheOthers holds that the commit is final.
//
// It already happened by the time any of these run, so a callback cannot undo
// it, and letting the first failure swallow the rest would hide work that had
// nothing to do with it.
func TestAPanickingCallbackDoesNotStopTheOthers(t *testing.T) {
	db := afterCommitDB(t)

	ran := 0
	err := database.Transaction(context.Background(), db, func(ctx context.Context) error {
		if err := database.AfterCommit(ctx, db, func(context.Context) { panic("the listener broke") }); err != nil {
			return err
		}
		return database.AfterCommit(ctx, db, func(context.Context) { ran++ })
	})
	if err != nil {
		t.Fatalf("a callback that panicked was reported as a failed transaction: %v", err)
	}
	if ran != 1 {
		t.Errorf("the second callback ran %d times, want 1", ran)
	}
}

func TestAfterCommitRefusesWhatItCannotRun(t *testing.T) {
	if err := database.AfterCommit(context.Background(), nil, func(context.Context) {}); err == nil {
		t.Error("a nil handle was accepted")
	}
	if err := database.AfterCommit(context.Background(), afterCommitDB(t), nil); err == nil {
		t.Error("a nil callback was accepted")
	}
}
