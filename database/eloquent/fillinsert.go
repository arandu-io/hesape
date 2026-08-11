package eloquent

import (
	"github.com/arandu-io/hesape/auth"
)

// FillForInsert answers Builder::fillForInsert: the rows, enriched with whatever
// the model would have put on them -- its defaults and its timestamps -- without
// making a model per row on the way to the database.
//
// It is the path a seeder and an importer take: one statement for a thousand
// rows, with the columns a save would have written.
func (b *Builder[T]) FillForInsert(values []map[string]any) ([]map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, row := range values {
		instance, err := b.NewModelInstance(row)
		if err != nil {
			return nil, err
		}
		if instance.UsesTimestamps() {
			instance.UpdateTimestamps()
		}
		out = append(out, instance.getAttributesForInsert())
	}
	return out, nil
}

// FillAndInsert answers Builder::fillAndInsert.
func (b *Builder[T]) FillAndInsert(g auth.Grant, values []map[string]any) (bool, error) {
	rows, err := b.FillForInsert(values)
	if err != nil {
		return false, err
	}
	return b.Insert(g, rows...)
}

// FillAndInsertOrIgnore answers Builder::fillAndInsertOrIgnore.
func (b *Builder[T]) FillAndInsertOrIgnore(g auth.Grant, values []map[string]any) (bool, error) {
	rows, err := b.FillForInsert(values)
	if err != nil {
		return false, err
	}
	return b.InsertOrIgnore(g, rows...)
}

// FillAndInsertGetID answers Builder::fillAndInsertGetId. The PHP spells the
// last word Id.
func (b *Builder[T]) FillAndInsertGetID(g auth.Grant, values map[string]any) (int64, error) {
	rows, err := b.FillForInsert([]map[string]any{values})
	if err != nil {
		return 0, err
	}
	return b.InsertGetID(g, rows[0], b.model.GetKeyName())
}

// InsertOrIgnore answers the base builder's insertOrIgnore, reached through the
// Eloquent one: the rows that violate a unique index are dropped rather than
// failing the statement.
func (b *Builder[T]) InsertOrIgnore(g auth.Grant, values ...map[string]any) (bool, error) {
	prepared, rows, err := b.prepareWrite(g, values)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return true, nil
	}

	prepared.query.ApplyBeforeQueryCallbacks()
	sql := b.model.Grammar.CompileInsertOrIgnore(prepared.query, rows)

	bindings := make([]any, 0, len(rows)*len(rows[0]))
	for _, row := range rows {
		for _, column := range sortedKeys(row) {
			bindings = append(bindings, row[column])
		}
	}
	return b.model.Connection.Insert(sql, cleanBindings(bindings))
}

// IncrementOrCreate answers Builder::incrementOrCreate: the row, or one more on
// the counter of the row that was already there.
func (b *Builder[T]) IncrementOrCreate(g auth.Grant, attributes map[string]any, column string, def, step any) (*Model[T], error) {
	if column == "" {
		column = "count"
	}
	if def == nil {
		def = 1
	}
	if step == nil {
		step = 1
	}

	instance, err := b.FirstOrCreate(g, attributes, map[string]any{column: def})
	if err != nil {
		return nil, err
	}
	if instance.WasRecentlyCreated {
		return instance, nil
	}

	q := instance.NewModelQuery()
	instance.setKeysForSaveQuery(q)
	if _, err := q.Increment(g, column, step, nil); err != nil {
		return nil, err
	}
	return instance, instance.Refresh(g)
}

// UseWritePDO answers Builder::useWritePdo: the statement goes to the writer,
// even though it reads. The PHP spells it useWritePdo.
//
// It is how a read that has to see what was just written avoids the replica lag
// that would otherwise make a fresh row look missing.
func (b *Builder[T]) UseWritePDO() *Builder[T] {
	b.query.UseWritePDO = true
	return b
}

// OnWriteConnection answers Model::onWriteConnection.
func (m *Model[T]) OnWriteConnection() *Builder[T] { return m.NewQuery().UseWritePDO() }

// OnClone answers Builder::onClone: a callback that runs on every copy this
// builder makes.
func (b *Builder[T]) OnClone(callback func(*Builder[T])) *Builder[T] {
	b.onCloneCallbacks = append(b.onCloneCallbacks, callback)
	return b
}

// NewQueryForRestoration answers Model::newQueryForRestoration: the query a
// route model binding uses to find a soft deleted row.
func (m *Model[T]) NewQueryForRestoration(ids ...any) *Builder[T] {
	q := m.NewQueryWithoutScopes()
	if len(ids) == 1 {
		return q.WhereKey(ids[0])
	}
	return q.WhereKey(ids)
}

// IsSoftDeletable answers Model::isSoftDeletable.
//
// PHP asks whether the class uses the SoftDeletes trait; here the model says so
// itself, which is the same answer without the reflection.
func (m *Model[T]) IsSoftDeletable() bool { return m.SoftDeletes }

// SoftDeleted answers SoftDeletes::softDeleted: a callback for the moment a row
// is marked deleted.
func (m *Model[T]) SoftDeleted(callback func(*Model[T]) error) *Model[T] {
	return m.RegisterModelEvent(Trashed, callback)
}

// Restoring answers SoftDeletes::restoring.
func (m *Model[T]) Restoring(callback func(*Model[T]) error) *Model[T] {
	return m.RegisterModelEvent(Restoring, callback)
}

// Restored answers SoftDeletes::restored.
func (m *Model[T]) Restored(callback func(*Model[T]) error) *Model[T] {
	return m.RegisterModelEvent(Restored, callback)
}

// ForceDeleting answers SoftDeletes::forceDeleting.
func (m *Model[T]) ForceDeleting(callback func(*Model[T]) error) *Model[T] {
	return m.RegisterModelEvent(ForceDeleting, callback)
}

// ForceDeleted answers SoftDeletes::forceDeleted.
func (m *Model[T]) ForceDeleted(callback func(*Model[T]) error) *Model[T] {
	return m.RegisterModelEvent(ForceDeleted, callback)
}
