package eloquent

import (
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// SoftDeletingScopeName is the identifier the SoftDeletingScope is registered
// under.
//
// PHP keys a global scope by the scope's class name; here the key is given, so
// it is a constant -- and WithTrashed has to name the same thing the model
// registered.
const SoftDeletingScopeName = "SoftDeletingScope"

// SoftDeletingScope answers Illuminate\Database\Eloquent\SoftDeletingScope.
//
// It filters the deleted rows out of every query, and it replaces the delete
// with an update, which is the whole of the feature. The model turns it on by
// setting SoftDeletes, and NewQuery registers it -- what bootSoftDeletes does at
// class boot there.
//
// The deleted_at field on the entity has to be able to hold a null: a
// *time.Time, or another type that writes NULL when it is empty. A plain
// time.Time has no null, so restoring would write the zero date and the row
// would read as deleted at the year one.
type SoftDeletingScope[T any] struct{}

// Apply answers SoftDeletingScope::apply.
func (s *SoftDeletingScope[T]) Apply(builder *Builder[T], model *Model[T]) {
	builder.query.WhereNull(model.GetQualifiedDeletedAtColumn())
}

// Extend answers SoftDeletingScope::extend.
//
// The PHP adds withTrashed, onlyTrashed and restore to the builder as macros,
// because a PHP Builder has no such methods. Go has methods, so those are
// methods -- and what is left for extend to do is the part that is not a name:
// pointing the builder's delete at an update.
func (s *SoftDeletingScope[T]) Extend(builder *Builder[T]) {
	builder.OnDelete(func(b *Builder[T], g auth.Grant) (int64, error) {
		column := s.deletedAtColumn(b)
		return b.Update(g, map[string]any{column: b.GetModel().FreshTimestamp()})
	})
}

// deletedAtColumn answers SoftDeletingScope::getDeletedAtColumn: qualified when
// the query joins, bare otherwise, because a bare name is ambiguous across a
// join and a qualified one is refused on the left of a SET by some engines.
func (s *SoftDeletingScope[T]) deletedAtColumn(b *Builder[T]) string {
	if len(b.query.Joins) > 0 {
		return b.GetModel().GetQualifiedDeletedAtColumn()
	}
	return b.GetModel().GetDeletedAtColumn()
}

// GetDeletedAtColumn answers SoftDeletes::getDeletedAtColumn.
func (m *Model[T]) GetDeletedAtColumn() string {
	if m.DeletedAtColumn == "" {
		return "deleted_at"
	}
	return m.DeletedAtColumn
}

// GetQualifiedDeletedAtColumn answers SoftDeletes::getQualifiedDeletedAtColumn.
func (m *Model[T]) GetQualifiedDeletedAtColumn() string {
	return m.QualifyColumn(m.GetDeletedAtColumn())
}

// Trashed answers SoftDeletes::trashed.
func (m *Model[T]) Trashed() bool {
	value := m.GetAttribute(m.GetDeletedAtColumn())
	return value != nil && !isZero(value)
}

// IsForceDeleting answers SoftDeletes::isForceDeleting.
func (m *Model[T]) IsForceDeleting() bool { return m.forceDeleting }

// performDeleteOnModel answers Model::performDeleteOnModel together with the
// override SoftDeletes puts over it.
//
// Go has no method to override, so the branch the trait adds is written here:
// a model that soft deletes and is not force deleting marks the row instead of
// removing it.
func (m *Model[T]) performDeleteOnModel(g auth.Grant) error {
	if m.SoftDeletes && !m.forceDeleting {
		return m.runSoftDelete(g)
	}

	q := m.NewModelQuery()
	m.setKeysForSaveQuery(q)
	if _, err := q.ForceDelete(g); err != nil {
		return err
	}
	m.Exists = false
	return nil
}

// runSoftDelete answers SoftDeletes::runSoftDelete.
func (m *Model[T]) runSoftDelete(g auth.Grant) error {
	now := m.FreshTimestamp()
	column := m.GetDeletedAtColumn()

	columns := map[string]any{column: now}
	if err := m.SetAttribute(column, now); err != nil {
		return err
	}

	if m.UsesTimestamps() && m.GetUpdatedAtColumn() != "" {
		columns[m.GetUpdatedAtColumn()] = now
		if err := m.SetAttribute(m.GetUpdatedAtColumn(), now); err != nil {
			return err
		}
	}

	q := m.NewModelQuery()
	m.setKeysForSaveQuery(q)
	if _, err := q.Update(g, columns); err != nil {
		return err
	}

	m.SyncOriginalAttributes(sortedKeys(columns)...)
	return m.fireModelEvent(Trashed)
}

// ForceDelete answers SoftDeletes::forceDelete, and Model::forceDelete on a
// model that does not soft delete -- where it is a plain delete, which is what
// the PHP's placeholder does.
func (m *Model[T]) ForceDelete(g auth.Grant) (bool, error) {
	if !m.SoftDeletes {
		return m.Delete(g)
	}
	if err := m.fireModelEvent(ForceDeleting); err != nil {
		return false, err
	}

	m.forceDeleting = true
	deleted, err := m.Delete(g)
	m.forceDeleting = false
	if err != nil {
		return false, err
	}
	if deleted {
		if err := m.fireModelEvent(ForceDeleted); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// ForceDeleteQuietly answers SoftDeletes::forceDeleteQuietly.
func (m *Model[T]) ForceDeleteQuietly(g auth.Grant) (deleted bool, err error) {
	return deleted, m.WithoutEvents(func() error {
		deleted, err = m.ForceDelete(g)
		return err
	})
}

// ForceDestroy answers SoftDeletes::forceDestroy.
func (m *Model[T]) ForceDestroy(g auth.Grant, ids ...any) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	models, err := m.NewQuery().WithTrashed().WhereKey(ids).Get(g)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, model := range models {
		deleted, err := model.ForceDelete(g)
		if err != nil {
			return count, err
		}
		if deleted {
			count++
		}
	}
	return count, nil
}

// Restore answers SoftDeletes::restore: the row comes back.
func (m *Model[T]) Restore(g auth.Grant) (bool, error) {
	if !m.SoftDeletes {
		return false, fmt.Errorf("eloquent: %s does not soft delete, so there is nothing to restore", m.GetTable())
	}
	if err := m.fireModelEvent(Restoring); err != nil {
		return false, err
	}
	if err := m.SetAttribute(m.GetDeletedAtColumn(), nil); err != nil {
		return false, err
	}

	m.Exists = true
	restored, err := m.Save(g)
	if err != nil {
		return false, err
	}
	if err := m.fireModelEvent(Restored); err != nil {
		return false, err
	}
	return restored, nil
}

// RestoreQuietly answers SoftDeletes::restoreQuietly.
func (m *Model[T]) RestoreQuietly(g auth.Grant) (restored bool, err error) {
	return restored, m.WithoutEvents(func() error {
		restored, err = m.Restore(g)
		return err
	})
}

// WithTrashed answers the withTrashed macro SoftDeletingScope adds: the deleted
// rows come back into the query.
//
// It is a method rather than a macro for the reason Extend gives. On a model
// that does not soft delete it is an error rather than a query that quietly
// means something else.
func (b *Builder[T]) WithTrashed(withTrashed ...bool) *Builder[T] {
	if len(withTrashed) > 0 && !withTrashed[0] {
		return b.WithoutTrashed()
	}
	if err := b.requireSoftDeletes("withTrashed"); err != nil {
		return b.fail(err)
	}
	return b.WithoutGlobalScope(SoftDeletingScopeName)
}

// WithoutTrashed answers the withoutTrashed macro.
func (b *Builder[T]) WithoutTrashed() *Builder[T] {
	if err := b.requireSoftDeletes("withoutTrashed"); err != nil {
		return b.fail(err)
	}
	b.WithoutGlobalScope(SoftDeletingScopeName)
	b.query.WhereNull(b.model.GetQualifiedDeletedAtColumn())
	return b
}

// OnlyTrashed answers the onlyTrashed macro.
func (b *Builder[T]) OnlyTrashed() *Builder[T] {
	if err := b.requireSoftDeletes("onlyTrashed"); err != nil {
		return b.fail(err)
	}
	b.WithoutGlobalScope(SoftDeletingScopeName)
	b.query.WhereNotNull(b.model.GetQualifiedDeletedAtColumn())
	return b
}

// Restore answers the restore macro: it un-deletes every row the query matches,
// in one statement.
func (b *Builder[T]) Restore(g auth.Grant) (int64, error) {
	if err := b.requireSoftDeletes("restore"); err != nil {
		return 0, err
	}
	return b.WithTrashed().Update(g, map[string]any{b.model.GetDeletedAtColumn(): nil})
}

// RestoreOrCreate answers the restoreOrCreate macro.
func (b *Builder[T]) RestoreOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	if err := b.requireSoftDeletes("restoreOrCreate"); err != nil {
		return nil, err
	}
	model, err := b.WithTrashed().FirstOrCreate(g, attributes, values)
	if err != nil {
		return nil, err
	}
	if _, err := model.Restore(g); err != nil {
		return nil, err
	}
	return model, nil
}

// CreateOrRestore answers the createOrRestore macro.
func (b *Builder[T]) CreateOrRestore(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	if err := b.requireSoftDeletes("createOrRestore"); err != nil {
		return nil, err
	}
	model, err := b.WithTrashed().CreateOrFirst(g, attributes, values)
	if err != nil {
		return nil, err
	}
	if _, err := model.Restore(g); err != nil {
		return nil, err
	}
	return model, nil
}

func (b *Builder[T]) requireSoftDeletes(method string) error {
	if b.model.SoftDeletes {
		return nil
	}
	return fmt.Errorf("eloquent: %s on %s, which does not soft delete: set SoftDeletes on the model, or the filter means nothing", method, b.model.GetTable())
}
