package concerns

import (
	"sort"

	"github.com/arandu-io/hesape/database/query"
)

// The four functions below run a statement built with the base query builder.
//
// They exist because the pivot table is reached with Illuminate\Database\Query\
// Builder and not with an Eloquent one -- newPivotStatement returns the base
// builder in the PHP too -- and the base builder in this collection compiles
// SQL without executing it. Each one is the body of the matching
// Illuminate\Database\Query\Builder method: insert, update, delete and get.
//
// They are unexported and they stay that way. When the query package grows its
// own Insert, Update, Delete and Get, these become three-line calls to them --
// that is the direction the dependency should run, and two public ways to run
// an insert is exactly what RULE 9 refuses.

// insert answers Illuminate\Database\Query\Builder::insert.
//
// The values are key-sorted before they are flattened, which is the PHP's
// ksort. It is not tidiness: the grammar writes the column list from the same
// records, and bindings flattened in a different order than the columns were
// written is every value landing in the wrong column -- a corrupt row rather
// than an error.
func insert(q *query.Builder, values []map[string]any) error {
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

	_, err := q.Connection.Insert(q.Grammar.CompileInsert(q, values), bindings)
	return err
}

// update answers Illuminate\Database\Query\Builder::update.
func update(q *query.Builder, values map[string]any) (int64, error) {
	q.ApplyBeforeQueryCallbacks()

	sql := q.Grammar.CompileUpdate(q, values)
	return q.Connection.Update(sql, q.Grammar.PrepareBindingsForUpdate(q.GetRawBindings(), values))
}

// deleteFrom answers Illuminate\Database\Query\Builder::delete.
//
// The name is the one place a mechanical rename was forced by the language:
// delete is not a keyword in Go, but a function called delete is -- the builtin
// that removes a key from a map -- and shadowing it inside this package would
// be a trap for the next reader.
func deleteFrom(q *query.Builder) (int64, error) {
	q.ApplyBeforeQueryCallbacks()

	sql := q.Grammar.CompileDelete(q)
	return q.Connection.Delete(sql, q.Grammar.PrepareBindingsForDelete(q.GetRawBindings()))
}

// selectRows answers Illuminate\Database\Query\Builder::get for the base
// builder: the rows, unhydrated.
func selectRows(q *query.Builder) ([]query.Record, error) {
	rows, err := q.Connection.Select(q.ToSQL(), q.GetBindings(), false)
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
