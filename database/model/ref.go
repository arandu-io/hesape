package model

import (
	"context"
	"iter"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// The seam between the model and the relations that read it.
//
// # Why an adapter and not the model itself
//
// The relations tree declares the narrow contract it consumes -- concerns.Model
// and concerns.Builder -- and *Model cannot satisfy it directly. Every method on
// Model is promoted onto every entity, so the forty a relation calls would
// become forty names no entity could use for a field, and a dozen of the
// builder's chainables would have to return concerns.Builder instead of
// *Builder, ending every chain.
//
// So the adapter is confined to this file: two unexported types, reached by one
// method each, and one function back. Nothing outside the relation boundary
// sees it.
//
// # Why it is safe
//
// The relations compare models by key and table, never by pointer identity, and
// key their dictionaries by string. Every mutation they perform -- SetRelation,
// SetAttribute, UnsetAttribute -- goes through this adapter's pointer to the
// real *Model, so what a relation writes is on the row the caller holds.

// modelRef is *Model seen through the interface a relation asks for.
type modelRef struct {
	m *Model

	// err holds what a method with nowhere to report it could not say.
	//
	// concerns.Model.Fill returns nothing and Model.Fill returns an error,
	// because a value can fail to fit a field. Dropping it would be the worst of
	// the three options; holding it and answering it from the next method that
	// can is the shape Builder.err already uses.
	err error
}

// builderRef is *Builder seen the same way.
type builderRef struct{ b *Builder }

var (
	_ concerns.Model   = (*modelRef)(nil)
	_ concerns.Builder = (*builderRef)(nil)
)

// ref returns m as the model a relation takes.
//
// The value is cached on the row, so two calls answer the same one. Nothing
// keys a map by a model today, and a ref that was a new value every call would
// be a trap waiting for the first thing that did.
func refOf(m *Model) concerns.Model {
	if m.r.ref == nil {
		m.r.ref = &modelRef{m: m}
	}
	return m.r.ref
}

// Ref returns b as the builder a relation takes.
func (b *Builder) Ref() concerns.Builder { return &builderRef{b: b} }

// unref is the way back: the model behind a ref, and whether the value was one
// of this package's refs at all.
func unref(m concerns.Model) (*Model, bool) {
	ref, ok := m.(*modelRef)
	if !ok {
		return nil, false
	}
	return ref.m, true
}

// refsOf returns every row as the interface a relation takes.
func refsOf(rows Rows) []concerns.Model {
	out := make([]concerns.Model, 0, len(rows))
	for _, e := range rows {
		out = append(out, refOf(e.base()))
	}
	return out
}

// -- modelRef ---------------------------------------------------------------

func (r *modelRef) GetTable() string                    { return tableNameOf(r.m) }
func (r *modelRef) QualifyColumn(column string) string  { return qualifyColumn(r.m, column) }
func (r *modelRef) GetKeyName() string                  { return r.m.r.table.keyName }
func (r *modelRef) GetKeyType() string                  { return r.m.r.table.keyType }
func (r *modelRef) GetKey() any                         { return r.m.GetKey() }
func (r *modelRef) GetForeignKey() string               { return r.m.r.table.foreignKey }
func (r *modelRef) GetMorphClass() string               { return r.m.r.table.morphClass }
func (r *modelRef) GetAttribute(key string) any         { return r.m.GetAttribute(key) }
func (r *modelRef) GetAttributes() map[string]any       { return r.m.GetAttributes() }
func (r *modelRef) RelationLoaded(relation string) bool { return relationLoaded(r.m, relation) }
func (r *modelRef) GetCreatedAtColumn() string          { return r.m.r.table.createdAt }
func (r *modelRef) GetUpdatedAtColumn() string          { return r.m.r.table.updatedAt }
func (r *modelRef) UsesTimestamps() bool                { return usesTimestamps(r.m) }
func (r *modelRef) UnsetAttribute(key string)           { unsetAttribute(r.m, key) }
func (r *modelRef) IsRelation(key string) bool          { return isRelation(r.m, key) }
func (r *modelRef) Touches(relation string) bool        { return touches(r.m, relation) }
func (r *modelRef) FreshTimestamp() time.Time           { return freshTimestamp() }

// Exists and WasRecentlyCreated are methods on Model too, and are answered here
// from the row because the adapter holds it.
func (r *modelRef) Exists() bool             { return r.m.r.exists }
func (r *modelRef) WasRecentlyCreated() bool { return r.m.r.recent }

func (r *modelRef) GetRelation(relation string) (any, bool) { return getRelation(r.m, relation) }
func (r *modelRef) SetRelation(relation string, value any) {
	setRelation(&r.m.r.relations, relation, value)
}
func (r *modelRef) UnsetRelation(relation string) { unsetRelation(r.m, relation) }
func (r *modelRef) SetTable(table string)         { r.m.r.tableName = table }

func (r *modelRef) Fill(attributes map[string]any)      { r.hold(r.m.Fill(attributes)) }
func (r *modelRef) ForceFill(attributes map[string]any) { r.hold(r.m.ForceFill(attributes)) }

func (r *modelRef) SetAttribute(key string, value any) {
	r.hold(r.m.SetAttribute(key, value))
}

func (r *modelRef) SetRawAttributes(attributes map[string]any, sync bool) {
	r.hold(r.m.SetRawAttributes(attributes, sync))
}

func (r *modelRef) WithoutEvents(callback func() error) error {
	return r.m.WithoutEvents(callback)
}

// NewInstance answers a fresh row of the same table, as a ref.
//
// The error the constructor reports is held rather than dropped, and the next
// method that can report one does.
func (r *modelRef) NewInstance(attributes map[string]any) concerns.Model {
	instance, err := newInstance(r.m, attributes)
	if err != nil {
		failed := &modelRef{m: r.m.r.table.newModel(r.m.r.conn)}
		failed.hold(err)
		return failed
	}
	return refOf(instance)
}

func (r *modelRef) NewQuery() concerns.Builder { return newQuery(r.m).Ref() }

func (r *modelRef) Save(ctx context.Context, g auth.Grant) error {
	if err := r.taken(); err != nil {
		return err
	}
	_, err := r.m.Save(ctx, g)
	return err
}

// Delete answers rows affected where the model answers whether anything went.
func (r *modelRef) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	if err := r.taken(); err != nil {
		return 0, err
	}
	deleted, err := r.m.Delete(ctx, g)
	if err != nil || !deleted {
		return 0, err
	}
	return 1, nil
}

func (r *modelRef) Touch(ctx context.Context, g auth.Grant) error {
	if err := r.taken(); err != nil {
		return err
	}
	return r.m.Touch(ctx, g)
}

// hold keeps the first error a method with no return could not report.
func (r *modelRef) hold(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}

// taken answers the held error once and forgets it, so that a model which
// recovered is not refused forever.
func (r *modelRef) taken() error {
	err := r.err
	r.err = nil
	return err
}

// -- builderRef -------------------------------------------------------------

func (r *builderRef) GetModel() concerns.Model { return refOf(r.b.GetModel()) }
func (r *builderRef) GetQuery() *query.Builder { return r.b.query }

// ScopesOwnTableByTenant answers for every terminal on the builder at once,
// because they share the one door: prepare puts the table's tenant column on the
// statement, and nothing runs without going through it.
//
// The column it uses is the table's own, which is the reason this is answered
// here rather than left to the relation. A table that declares a different
// column, or none because it is shared, is filtered on what it declared --
// where a second filter added from outside would only know the default name,
// and would name a column a shared table does not have.
func (r *builderRef) ScopesOwnTableByTenant() bool { return true }

// The chainables. Each returns this ref rather than a new one: the builder
// mutates and returns itself, and a ref that allocated per call would make a
// chain of ten allocate ten.
//
// Every one but Where and WhereKey is a forward to the query the builder holds,
// which is all the builder's method does too. Where goes through the builder
// because it also takes a nested closure.
func (r *builderRef) Select(columns ...any) concerns.Builder {
	r.b.query.Select(columns...)
	return r
}

func (r *builderRef) AddSelect(columns ...any) concerns.Builder {
	r.b.query.AddSelect(columns...)
	return r
}

func (r *builderRef) Where(column any, args ...any) concerns.Builder {
	r.b.Where(column, args...)
	return r
}

func (r *builderRef) WhereIn(column any, values []any) concerns.Builder {
	r.b.query.WhereIn(column, values)
	return r
}

func (r *builderRef) WhereNotNull(columns ...any) concerns.Builder {
	r.b.query.WhereNotNull(columns...)
	return r
}

func (r *builderRef) WhereColumn(first any, args ...any) concerns.Builder {
	r.b.query.WhereColumn(first, args...)
	return r
}

func (r *builderRef) WhereKey(ids ...any) concerns.Builder {
	if len(ids) == 1 {
		r.b.WhereKey(ids[0])
		return r
	}
	r.b.WhereKey(ids)
	return r
}

func (r *builderRef) Join(table any, first any, args ...any) concerns.Builder {
	r.b.query.Join(table, first, args...)
	return r
}

func (r *builderRef) GroupBy(groups ...any) concerns.Builder {
	r.b.query.GroupBy(groups...)
	return r
}

func (r *builderRef) SelectRaw(expression string, bindings ...any) concerns.Builder {
	r.b.query.SelectRaw(expression, bindings...)
	return r
}

func (r *builderRef) OrderBy(column any, direction ...string) concerns.Builder {
	r.b.query.OrderBy(column, direction...)
	return r
}

func (r *builderRef) Limit(value int) concerns.Builder  { r.b.query.Limit(value); return r }
func (r *builderRef) Offset(value int) concerns.Builder { r.b.query.Offset(value); return r }
func (r *builderRef) Clone() concerns.Builder           { return &builderRef{b: clone(r.b)} }

func (r *builderRef) Get(ctx context.Context, g auth.Grant) ([]concerns.Model, error) {
	found, err := r.b.get(ctx, g)
	if err != nil {
		return nil, err
	}
	return refsOf(found), nil
}

// First answers (nil, nil) for a miss, as the interface says: ErrModelNotFound
// belongs to FirstOrFail, which is a different question.
func (r *builderRef) First(ctx context.Context, g auth.Grant) (concerns.Model, error) {
	found, err := first(r.b, ctx, g)
	if err != nil || found == nil {
		return nil, err
	}
	return refOf(found), nil
}

func (r *builderRef) Find(ctx context.Context, g auth.Grant, id any) (concerns.Model, error) {
	found, err := find(r.b, ctx, g, id)
	if err != nil || found == nil {
		return nil, err
	}
	return refOf(found), nil
}

func (r *builderRef) Cursor(ctx context.Context, g auth.Grant) iter.Seq2[concerns.Model, error] {
	return func(yield func(concerns.Model, error) bool) {
		found, err := r.b.get(ctx, g)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, e := range found {
			if !yield(refOf(e.base()), nil) {
				return
			}
		}
	}
}

func (r *builderRef) Paginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.LengthAwarePage, error) {
	items, meta, err := paginate(r.b, ctx, g, perPage, page, opts, columns...)
	if err != nil {
		return nil, nil, err
	}
	return refsOf(items), meta, nil
}

func (r *builderRef) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.Page, error) {
	items, meta, err := simplePaginate(r.b, ctx, g, perPage, page, opts, columns...)
	if err != nil {
		return nil, nil, err
	}
	return refsOf(items), meta, nil
}

func (r *builderRef) CursorPaginate(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.CursorPage, error) {
	items, meta, err := cursorPaginate(r.b, ctx, g, perPage, cursor, opts, columns...)
	if err != nil {
		return nil, nil, err
	}
	return refsOf(items), meta, nil
}

func (r *builderRef) Insert(ctx context.Context, g auth.Grant, values []map[string]any) error {
	_, err := r.b.Insert(ctx, g, values...)
	return err
}

func (r *builderRef) Update(ctx context.Context, g auth.Grant, values map[string]any) (int64, error) {
	return r.b.Update(ctx, g, values)
}

func (r *builderRef) Upsert(ctx context.Context, g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error) {
	return r.b.Upsert(ctx, g, values, uniqueBy, update)
}

func (r *builderRef) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	return r.b.Delete(ctx, g)
}
