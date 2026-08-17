package migrations

import (
	"context"
	"errors"
	"testing"
)

// sqlMigration is a migration whose Up and Down send statements, which is what
// UpStatements and DownStatements are there to read back.
type sqlMigration struct {
	BaseMigration
	upErr error
}

func (sqlMigration) GetName() string { return "2026_01_01_000000_create_widgets_table" }

func (m sqlMigration) Up(ctx context.Context, conn Connection) error {
	if m.upErr != nil {
		return m.upErr
	}
	for _, statement := range []string{
		`CREATE TABLE widgets (id VARCHAR(255) PRIMARY KEY)`,
		`CREATE INDEX widgets_id_idx ON widgets (id)`,
	} {
		if _, err := conn.Statement(ctx, statement, nil); err != nil {
			return err
		}
	}
	return nil
}

func (sqlMigration) Down(ctx context.Context, conn Connection) error {
	_, err := conn.Statement(ctx, `DROP TABLE widgets`, nil)
	return err
}

// irreversibleMigration has no Down, which is the case DownStatements answers
// with nothing rather than an error.
type irreversibleMigration struct{ BaseMigration }

func (irreversibleMigration) GetName() string { return "2026_01_02_000000_backfill_widgets" }

func (irreversibleMigration) Up(ctx context.Context, conn Connection) error {
	_, err := conn.Statement(ctx, `UPDATE widgets SET id = id`, nil)
	return err
}

func TestUpStatementsReturnsWhatTheMigrationWouldSend(t *testing.T) {
	got, err := UpStatements(context.Background(), sqlMigration{})
	if err != nil {
		t.Fatalf("UpStatements: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("%d statements, want 2: %q", len(got), got)
	}
	if got[0] != `CREATE TABLE widgets (id VARCHAR(255) PRIMARY KEY)` {
		t.Errorf("the first statement is %q", got[0])
	}
	if got[1] != `CREATE INDEX widgets_id_idx ON widgets (id)` {
		t.Errorf("the second statement is %q", got[1])
	}
}

func TestUpStatementsRunsNothing(t *testing.T) {
	// The migration above would fail against any server, because no table
	// named widgets exists. It cannot fail here, which is the whole point.
	if _, err := UpStatements(context.Background(), irreversibleMigration{}); err != nil {
		t.Fatalf("UpStatements: %v", err)
	}
}

func TestUpStatementsReturnsTheMigrationsError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := UpStatements(context.Background(), sqlMigration{upErr: boom}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestDownStatementsReturnsWhatTheRollbackWouldSend(t *testing.T) {
	got, err := DownStatements(context.Background(), sqlMigration{})
	if err != nil {
		t.Fatalf("DownStatements: %v", err)
	}
	if len(got) != 1 || got[0] != `DROP TABLE widgets` {
		t.Fatalf("statements = %q", got)
	}
}

func TestDownStatementsOfAnIrreversibleMigrationIsEmpty(t *testing.T) {
	got, err := DownStatements(context.Background(), irreversibleMigration{})
	if err != nil {
		t.Fatalf("DownStatements: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("statements = %q, want none", got)
	}
}
