package migrations

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/database/schema"
)

// WidenTimePrecision is a migration that raises the date-time columns of
// Tables to schema.MaxTimePrecision, through Builder.WidenTimePrecision.
//
// It is how a table created while the default time precision was whole
// seconds catches up with the default that is microseconds now: the columns
// are read off the database, only those below the precision are altered, one
// statement per table, and a table already there is left alone. A table that
// does not exist is skipped, since a project may never have created one a
// package offers. SQLite has nothing to widen and runs nothing.
//
// Its Down changes nothing: see Down.
type WidenTimePrecision struct {
	BaseMigration
	// Name is the migration's identity, dated after the migrations that
	// created the tables.
	Name string
	// Tables are the tables whose columns are widened, in order.
	Tables []string
}

// GetName returns Name.
func (m WidenTimePrecision) GetName() string { return m.Name }

// Up widens every table in Tables that exists.
func (m WidenTimePrecision) Up(ctx context.Context, conn Connection) error {
	builder := conn.Schema()
	for _, table := range m.Tables {
		exists, err := builder.HasTable(ctx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := builder.WidenTimePrecision(ctx, table, schema.MaxTimePrecision); err != nil {
			return fmt.Errorf("migrations: widening the time precision of %s: %w", table, err)
		}
	}
	return nil
}

// Down leaves the columns as Up made them. A rollback returns the schema to
// what the code before this migration reads and writes, and that code reads
// and writes a wider column exactly as it did the narrow one; narrowing it
// again would round away the fraction of every row written since.
func (m WidenTimePrecision) Down(ctx context.Context, conn Connection) error { return nil }

// Compile-time proof that a widening is reversible, so a rollback past it
// reaches the migrations before it.
var _ ReversibleMigration = WidenTimePrecision{}
