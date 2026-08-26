package concerns

import (
	"context"
	"sort"

	"github.com/arandu-io/hesape/database/query"
)

// The four functions below run a statement built with the base query builder.
//
// They exist because the pivot table is reached with the base query builder
// rather than an Eloquent one, and the base builder compiles SQL without
// executing it.
//
// They are unexported and they stay that way. When the query package grows its
// own Insert, Update, Delete and Get, these become three-line calls to them --
// that is the direction the dependency should run, and two public ways to run
// an insert is one too many.

// insert runs the insert the builder compiled.
//
// The values are key-sorted before they are flattened, and that is not tidiness:
// the grammar writes the column list from the same records, and bindings
// flattened in a different order than the columns were written is every value
// landing in the wrong column -- a corrupt row rather than an error.
func insert(ctx context.Context, q *query.Builder, values []map[string]any) error {
	if len(values) == 0 {
		return nil
	}

	q.ApplyBeforeQueryCallbacks()

	bindings := make([]any, 0, len(values)*len(values[0]))
	for _, record := range values {
		for _, column := range sortedKeys(record) {
			if value := record[column]; !query.IsExpression(value) {
				bindings = append(bindings, value)
			}
		}
	}

	_, err := q.Connection.Insert(ctx, q.Grammar.CompileInsert(q, values), bindings)
	return err
}

// update runs the update the builder compiled.
func update(ctx context.Context, q *query.Builder, values map[string]any) (int64, error) {
	q.ApplyBeforeQueryCallbacks()

	sql := q.Grammar.CompileUpdate(q, values)
	return q.Connection.Update(ctx, sql, q.Grammar.PrepareBindingsForUpdate(q.GetRawBindings(), values))
}

// deleteFrom runs the delete the builder compiled.
//
// It is not called delete: that is the builtin removing a key from a map, and
// shadowing it inside this package would be a trap for the next reader.
func deleteFrom(ctx context.Context, q *query.Builder) (int64, error) {
	q.ApplyBeforeQueryCallbacks()

	sql := q.Grammar.CompileDelete(q)
	return q.Connection.Delete(ctx, sql, q.Grammar.PrepareBindingsForDelete(q.GetRawBindings()))
}

// selectRows runs the select the builder compiled: the rows, unhydrated.
func selectRows(ctx context.Context, q *query.Builder) ([]query.Record, error) {
	rows, err := q.Connection.Select(ctx, q.ToSQL(), q.GetBindings(), false)
	if err != nil {
		return nil, err
	}
	if q.Processor != nil {
		rows = q.Processor.ProcessSelect(q, rows)
	}
	return rows, nil
}

func sortedKeys(record map[string]any) []string {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
