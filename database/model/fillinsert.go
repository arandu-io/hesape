package model

import (
	"context"

	"github.com/arandu-io/hesape/auth"
)

// FillForInsert returns the rows, enriched with whatever the model would
// have put on them -- its defaults and its timestamps -- without making a
// model per row on the way to the database.
//
// It is the path a seeder and an importer take: one statement for a thousand
// rows, with the columns a save would have written -- the generated key of a
// model that uses unique ids among them.
func (b *Builder) FillForInsert(values []map[string]any) ([]map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, row := range values {
		instance, err := newModelInstance(b, row)
		if err != nil {
			return nil, err
		}
		if err := setUniqueID(instance); err != nil {
			return nil, err
		}
		if usesTimestamps(instance) {
			updateTimestamps(instance)
		}
		out = append(out, getAttributesForInsert(instance))
	}
	return out, nil
}

// FillAndInsert runs FillForInsert and inserts the result.
func (b *Builder) FillAndInsert(ctx context.Context, g auth.Grant, values []map[string]any) (bool, error) {
	rows, err := b.FillForInsert(values)
	if err != nil {
		return false, err
	}
	return b.Insert(ctx, g, rows...)
}

// FillAndInsertOrIgnore runs FillForInsert and inserts the result, dropping
// rows that violate a unique index.
func (b *Builder) FillAndInsertOrIgnore(ctx context.Context, g auth.Grant, values []map[string]any) (bool, error) {
	rows, err := b.FillForInsert(values)
	if err != nil {
		return false, err
	}
	return b.InsertOrIgnore(ctx, g, rows...)
}

// FillAndInsertGetID runs FillForInsert for one row, inserts it, and
// returns the value generated for the primary key.
func (b *Builder) FillAndInsertGetID(ctx context.Context, g auth.Grant, values map[string]any) (int64, error) {
	rows, err := b.FillForInsert([]map[string]any{values})
	if err != nil {
		return 0, err
	}
	return b.InsertGetID(ctx, g, rows[0], b.table.keyName)
}

// InsertOrIgnore writes values as new rows, dropping the ones that violate a
// unique index rather than failing the statement.
func (b *Builder) InsertOrIgnore(ctx context.Context, g auth.Grant, values ...map[string]any) (bool, error) {
	prepared, rows, err := prepareWrite(b, g, values)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return true, nil
	}

	if err := validateWriteQuery(prepared); err != nil {
		return false, err
	}
	sql := b.conn.grammar.CompileInsertOrIgnore(prepared.query, rows)

	bindings := make([]any, 0, len(rows)*len(rows[0]))
	for _, row := range rows {
		for _, column := range sortedKeys(row) {
			bindings = append(bindings, row[column])
		}
	}
	return b.conn.connection.Insert(ctx, sql, cleanBindings(bindings))
}

// IncrementOrCreate returns the row matching attributes with column set to
// def, or increments column by step on the row that was already there.
func (b *Builder) IncrementOrCreate(ctx context.Context, g auth.Grant, attributes map[string]any, column string, def, step any) (Entity, error) {
	if column == "" {
		column = "count"
	}
	if def == nil {
		def = 1
	}
	if step == nil {
		step = 1
	}

	instance, err := firstOrCreate(b, ctx, g, attributes, map[string]any{column: def})
	if err != nil {
		return nil, err
	}
	if instance.r.recent {
		return result(b, instance), nil
	}

	q := newModelQuery(instance)
	setKeysForSaveQuery(instance, q)
	if _, err := q.Increment(ctx, g, column, step, nil); err != nil {
		return nil, err
	}
	return result(b, instance), instance.Refresh(ctx, g)
}

// UseWritePDO points this builder's statement at the write connection, even
// though it reads.
//
// It is how a read that has to see what was just written avoids the
// replica lag that would otherwise make a fresh row look missing.
func (b *Builder) UseWritePDO() *Builder {
	b.query.UseWritePDO()
	return b
}

// OnClone registers a callback that runs on every copy this builder makes.
func (b *Builder) OnClone(callback func(*Builder)) *Builder {
	b.onCloneCallbacks = append(b.onCloneCallbacks, callback)
	return b
}
