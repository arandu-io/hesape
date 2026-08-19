package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/query/processors"
)

// TestEveryDialectCompilesItsOwnSQL is the defect this closes: Query returned a
// builder holding a nil grammar and a nil processor on every connection
// NewConnection made, so the first statement compiled off it dereferenced nil.
// A public function documented as the way to build a query panicked on every
// call, on every dialect.
//
// The statement carries a quoted identifier and a lock clause because those are
// what tell the three apart: SQLite locks the file rather than the row, so its
// lock compiles to nothing, and MySQL quotes with backticks.
func TestEveryDialectCompilesItsOwnSQL(t *testing.T) {
	for driver, want := range map[string]string{
		"sqlite": `select * from "t" where "id" = ?`,
		"pgsql":  `select * from "t" where "id" = ? for update`,
		"mysql":  "select * from `t` where `id` = ? for update",
	} {
		t.Run(driver, func(t *testing.T) {
			c := database.NewConnection(nil, "app", "", map[string]any{"driver": driver})
			got := c.Query(context.Background()).From("t").Where("id", 1).LockForUpdate().ToSQL()
			if got != want {
				t.Errorf("%s compiled %q, want %q", driver, got, want)
			}
		})
	}
}

// TestEveryDialectGetsItsOwnPostProcessor is the half of the same registration
// that no compiled statement would show.
//
// The processors differ by the thing hardest to notice when it is wrong:
// Postgres reads the identifier of an inserted row out of a returning clause
// and MySQL reads it out of band. A connection holding the wrong one, or nil,
// answers with a wrong identifier rather than an error.
func TestEveryDialectGetsItsOwnPostProcessor(t *testing.T) {
	for driver, want := range map[string]any{
		"sqlite": &processors.SQLiteProcessor{},
		"pgsql":  &processors.PostgresProcessor{},
		"mysql":  &processors.MySQLProcessor{},
	} {
		t.Run(driver, func(t *testing.T) {
			c := database.NewConnection(nil, "app", "", map[string]any{"driver": driver})
			got := c.GetPostProcessor()
			if got == nil {
				t.Fatalf("%s has no post processor, so reading back an inserted id has nothing to run", driver)
			}
			if gotType, wantType := typeName(got), typeName(want); gotType != wantType {
				t.Errorf("%s got %s, want %s", driver, gotType, wantType)
			}
		})
	}
}

// typeName is the dynamic type of a value, which is what the assertion above
// compares: the processors carry no state to compare instead.
func typeName(v any) string { return fmt.Sprintf("%T", v) }
