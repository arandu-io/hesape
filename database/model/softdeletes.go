package model

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// SoftDeletingScopeName is the identifier the soft-delete filter is registered
// under on a table that soft deletes.
//
// It is a constant rather than a computed name, so that WithoutGlobalScope can
// name the filter the table registered without holding a reference to it.
const SoftDeletingScopeName = "SoftDeletingScope"

// softDeletingScope filters the deleted rows out of a query.
func softDeletingScope(b *Builder) {
	b.query.WhereNull(b.qualify(b.table.deletedAt))
}

// softDelete is the delete a soft-deleting table's builder runs instead of a
// DELETE: a stamp on the deleted-at column of every row the query matches.
func softDelete(ctx context.Context, b *Builder, g auth.Grant) (int64, error) {
	return b.Update(ctx, g, map[string]any{deletedAtColumn(b): freshTimestamp()})
}

// deletedAtColumn returns the deleted_at column name to filter or set:
// qualified when the query joins, bare otherwise, because a bare name is
// ambiguous across a join and a qualified one is refused on the left of a SET
// by some engines.
func deletedAtColumn(b *Builder) string {
	if len(b.query.Joins) > 0 {
		return b.qualify(b.table.deletedAt)
	}
	return b.table.deletedAt
}

// Trashed reports whether the row has been soft deleted.
func (m *Model) Trashed() bool {
	if !live(m) || m.r.table.deletedAt == "" {
		return false
	}
	value := m.GetAttribute(m.r.table.deletedAt)
	return value != nil && !isZero(value)
}

// performDeleteOnModel deletes the row, or marks it deleted: a table that soft
// deletes, when the row is not being force deleted, marks the row instead of
// removing it.
func performDeleteOnModel(m *Model, ctx context.Context, g auth.Grant) error {
	if m.r.table.softDeletes && !m.r.forceDeleting {
		return runSoftDelete(m, ctx, g)
	}

	q := newModelQuery(m)
	setKeysForSaveQuery(m, q)
	if _, err := q.ForceDelete(ctx, g); err != nil {
		return err
	}
	m.r.exists = false
	return nil
}

// runSoftDelete sets the deleted_at column to the current time instead of
// removing the row, and fires the Trashed event.
func runSoftDelete(m *Model, ctx context.Context, g auth.Grant) error {
	now := freshTimestamp()
	t := m.r.table
	column := t.deletedAt

	columns := map[string]any{column: now}
	if err := m.SetAttribute(column, now); err != nil {
		return err
	}

	if usesTimestamps(m) && t.updatedAt != "" {
		columns[t.updatedAt] = now
		if err := m.SetAttribute(t.updatedAt, now); err != nil {
			return err
		}
	}

	q := newModelQuery(m)
	setKeysForSaveQuery(m, q)
	if _, err := q.Update(ctx, g, columns); err != nil {
		return err
	}

	syncOriginalAttributes(m, sortedKeys(columns)...)
	return fireModelEvent(m, Trashed)
}

// ForceDelete removes the row even if the table soft deletes. On a table that
// does not soft delete it is a plain delete.
func (m *Model) ForceDelete(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	if !m.r.table.softDeletes {
		return m.Delete(ctx, g)
	}
	if err := fireModelEvent(m, ForceDeleting); err != nil {
		return false, err
	}

	m.r.forceDeleting = true
	deleted, err := m.Delete(ctx, g)
	m.r.forceDeleting = false
	if err != nil {
		return false, err
	}
	if deleted {
		if err := fireModelEvent(m, ForceDeleted); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// Restore clears the deleted_at column and saves the row: the row comes back.
func (m *Model) Restore(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	if !m.r.table.softDeletes {
		return false, fmt.Errorf("model: %s does not soft delete, so there is nothing to restore", m.r.table.name)
	}
	if err := fireModelEvent(m, Restoring); err != nil {
		return false, err
	}
	if err := m.SetAttribute(m.r.table.deletedAt, nil); err != nil {
		return false, err
	}

	m.r.exists = true
	restored, err := m.Save(ctx, g)
	if err != nil {
		return false, err
	}
	if err := fireModelEvent(m, Restored); err != nil {
		return false, err
	}
	return restored, nil
}

// WithTrashed includes the soft-deleted rows in the query.
//
// On a table that does not soft delete it is an error rather than a query that
// quietly means something else.
func (b *Builder) WithTrashed(withTrashed ...bool) *Builder {
	if len(withTrashed) > 0 && !withTrashed[0] {
		return b.WithoutTrashed()
	}
	if err := requireSoftDeletes(b, "withTrashed"); err != nil {
		return fail(b, err)
	}
	return b.WithoutGlobalScope(SoftDeletingScopeName)
}

// WithoutTrashed removes the global soft-delete scope and re-adds an explicit
// not-deleted filter, so trashed rows stay excluded even after the scope is
// gone.
func (b *Builder) WithoutTrashed() *Builder {
	if err := requireSoftDeletes(b, "withoutTrashed"); err != nil {
		return fail(b, err)
	}
	b.WithoutGlobalScope(SoftDeletingScopeName)
	b.query.WhereNull(b.qualify(b.table.deletedAt))
	return b
}

// OnlyTrashed restricts the query to soft-deleted rows only.
func (b *Builder) OnlyTrashed() *Builder {
	if err := requireSoftDeletes(b, "onlyTrashed"); err != nil {
		return fail(b, err)
	}
	b.WithoutGlobalScope(SoftDeletingScopeName)
	b.query.WhereNotNull(b.qualify(b.table.deletedAt))
	return b
}

// Restore un-deletes every row the query matches, in one statement.
func (b *Builder) Restore(ctx context.Context, g auth.Grant) (int64, error) {
	if err := requireSoftDeletes(b, "restore"); err != nil {
		return 0, err
	}
	return b.WithTrashed().Update(ctx, g, map[string]any{b.table.deletedAt: nil})
}

// RestoreOrCreate finds the first trashed-or-not row matching attributes and
// restores it, or creates one from attributes and values if none matches.
func (b *Builder) RestoreOrCreate(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	if err := requireSoftDeletes(b, "restoreOrCreate"); err != nil {
		return nil, err
	}
	model, err := firstOrCreate(b.WithTrashed(), ctx, g, attributes, values)
	if err != nil {
		return nil, err
	}
	if _, err := model.Restore(ctx, g); err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// CreateOrRestore finds the first row matching attributes, including trashed,
// and restores it if trashed; otherwise it creates one from attributes and
// values.
func (b *Builder) CreateOrRestore(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	if err := requireSoftDeletes(b, "createOrRestore"); err != nil {
		return nil, err
	}
	model, err := createOrFirst(b.WithTrashed(), ctx, g, attributes, values)
	if err != nil {
		return nil, err
	}
	if _, err := model.Restore(ctx, g); err != nil {
		return nil, err
	}
	return result(b, model), nil
}

func requireSoftDeletes(b *Builder, method string) error {
	if b.table.softDeletes {
		return nil
	}
	return fmt.Errorf("model: %s on %s, which does not soft delete: set SoftDeletes on the table, or the filter means nothing", method, b.table.name)
}
