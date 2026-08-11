package eloquent

import (
	"fmt"

	"github.com/arandu-io/hesape/database/query"
)

// This file holds the bodies of Illuminate\Database\Query\Builder's select,
// insert, insertGetId, update, upsert, delete and aggregate.
//
// They run here rather than in the query package because running a statement
// takes a Grant and building SQL does not: query.Builder is the SQL, and the
// layer that holds the authorization is the layer that issues it (RULE 17). The
// SQL itself is still the grammar's -- nothing here concatenates a fragment.

// runSelect answers Builder::runSelect.
func (b *Builder[T]) runSelect() ([]query.Record, error) {
	sql := b.query.ToSQL()
	rows, err := b.model.Connection.Select(sql, b.query.GetBindings(), !b.query.UseWritePDO)
	if err != nil {
		return nil, fmt.Errorf("eloquent: selecting from %s: %w", b.model.GetTable(), err)
	}
	if b.model.Processor != nil {
		rows = b.model.Processor.ProcessSelect(b.query, rows)
	}
	return rows, nil
}

// runInsert answers Builder::insert.
//
// The rows are sorted by column name before they are compiled and bound, which
// is the same ksort the PHP applies to a batch insert -- and the only ordering
// available here, since a Go map has none.
func (b *Builder[T]) runInsert(values []map[string]any) (bool, error) {
	if len(values) == 0 {
		return true, nil
	}
	b.query.ApplyBeforeQueryCallbacks()

	sql := b.model.Grammar.CompileInsert(b.query, values)
	bindings := make([]any, 0, len(values)*len(values[0]))
	for _, row := range values {
		for _, column := range sortedKeys(row) {
			bindings = append(bindings, row[column])
		}
	}
	ok, err := b.model.Connection.Insert(sql, cleanBindings(bindings))
	if err != nil {
		return false, fmt.Errorf("eloquent: inserting into %s: %w", b.model.GetTable(), err)
	}
	return ok, nil
}

// runInsertGetID answers Builder::insertGetId. The PHP spells it insertGetId.
func (b *Builder[T]) runInsertGetID(values map[string]any, sequence string) (int64, error) {
	b.query.ApplyBeforeQueryCallbacks()

	sql := b.model.Grammar.CompileInsertGetID(b.query, values, sequence)

	bindings := make([]any, 0, len(values))
	for _, column := range sortedKeys(values) {
		bindings = append(bindings, values[column])
	}

	id, err := b.model.Processor.ProcessInsertGetID(b.query, sql, cleanBindings(bindings), sequence)
	if err != nil {
		return 0, fmt.Errorf("eloquent: inserting into %s: %w", b.model.GetTable(), err)
	}
	return id, nil
}

// runUpdate answers Builder::update.
func (b *Builder[T]) runUpdate(values map[string]any) (int64, error) {
	b.query.ApplyBeforeQueryCallbacks()

	sql := b.model.Grammar.CompileUpdate(b.query, values)
	bindings := b.model.Grammar.PrepareBindingsForUpdate(b.query.GetRawBindings(), values)

	affected, err := b.model.Connection.Update(sql, cleanBindings(bindings))
	if err != nil {
		return 0, fmt.Errorf("eloquent: updating %s: %w", b.model.GetTable(), err)
	}
	return affected, nil
}

// runUpsert answers Builder::upsert.
//
// Illuminate runs it through affectingStatement; query.Connection spells that
// Update -- a statement that reports how many rows it touched.
func (b *Builder[T]) runUpsert(values []map[string]any, uniqueBy, update []string) (int64, error) {
	b.query.ApplyBeforeQueryCallbacks()

	sql := b.model.Grammar.CompileUpsert(b.query, values, uniqueBy, update)
	bindings := make([]any, 0, len(values)*len(values[0]))
	for _, row := range values {
		for _, column := range sortedKeys(row) {
			bindings = append(bindings, row[column])
		}
	}

	affected, err := b.model.Connection.Update(sql, cleanBindings(bindings))
	if err != nil {
		return 0, fmt.Errorf("eloquent: upserting into %s: %w", b.model.GetTable(), err)
	}
	return affected, nil
}

// runDelete answers Builder::delete.
func (b *Builder[T]) runDelete() (int64, error) {
	b.query.ApplyBeforeQueryCallbacks()

	sql := b.model.Grammar.CompileDelete(b.query)
	bindings := b.model.Grammar.PrepareBindingsForDelete(b.query.GetRawBindings())

	affected, err := b.model.Connection.Delete(sql, cleanBindings(bindings))
	if err != nil {
		return 0, fmt.Errorf("eloquent: deleting from %s: %w", b.model.GetTable(), err)
	}
	return affected, nil
}

// runAggregate answers Builder::aggregate: the one row an aggregate select
// returns, read out of the column the grammar aliases as "aggregate".
func (b *Builder[T]) runAggregate(function string, columns []any) (any, error) {
	aggregate := b.clone()
	aggregate.query = aggregate.query.
		CloneWithout("columns", "orders").
		CloneWithoutBindings("select", "order")
	aggregate.query.SetAggregate(function, columns)

	rows, err := aggregate.runSelect()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	for key, value := range rows[0] {
		if lowerASCII(key) == "aggregate" {
			return value, nil
		}
	}
	return nil, fmt.Errorf("eloquent: the %s query on %s came back without an aggregate column", function, b.model.GetTable())
}

// cleanBindings answers Grammar::cleanBindings: an Expression is SQL and never a
// binding, so it is dropped from the list rather than sent as a value.
func cleanBindings(bindings []any) []any {
	out := make([]any, 0, len(bindings))
	for _, binding := range bindings {
		if query.IsExpression(binding) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}
