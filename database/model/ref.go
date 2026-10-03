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

// The seam between the typed model and the relations that read it.
//
// # Why an adapter and not one type
//
// The relations tree declares the narrow contract it consumes -- concerns.Model
// and concerns.Builder -- and *Model[T] cannot satisfy it directly, for two
// reasons that are both about Go rather than about design.
//
// Fourteen of the twenty-seven methods on concerns.Builder are chainables that
// return Builder. Builder[T].Where returns *Builder[T]. Go has no covariant
// return, so satisfying the interface with the typed builder would mean renaming
// fourteen methods of the public fluent API.
//
// And the tree cannot be made generic to meet the model instead. Model[T] holds
// RelationResolvers as one map, and one map holds a has-many to Post beside a
// belongs-to to Team; one map value type means one non-generic Relation. MorphTo
// is heterogeneous by construction -- its dictionary is keyed by a morph alias
// resolved at run time -- and there is no type parameter to write there at all.
//
// So the erasure is confined to this file. Two unexported types, reached by one
// method each, and one function back. Nothing outside the relation boundary
// sees an untyped model.
//
// # Why it is safe
//
// The relations compare models by key and table, never by pointer identity, and
// key their dictionaries by string. Every mutation they perform -- SetRelation,
// SetAttribute, UnsetAttribute -- goes through this adapter's pointer to the
// real *Model[T], so what a relation writes is on the model the caller holds.

// modelRef is *Model[T] seen through the interface a relation asks for.
//
// It is one type for every T rather than one per T. A generic adapter had its
// whole method set compiled again for each model type, in every package that
// named the model, and the adapter does nothing that depends on T: every method
// is a call through row, which *Model[T] satisfies with methods it already has,
// plus the three hooks below that only the typed model can answer.
type modelRef struct {
	m row

	// err holds what a method with nowhere to report it could not say.
	//
	// concerns.Model.Fill returns nothing and Model[T].Fill returns an error,
	// because a value can fail to fit a field. Dropping it would be the worst of
	// the three options; holding it and answering it from the next method that
	// can is the shape Builder[T].err already uses.
	err error
}

// builderRef is *Builder[T] seen the same way, and for the same reason one type
// for every T.
type builderRef struct{ b rowsBuilder }

// rowsBuilder is the typed builder as builderRef and the paginators reach it:
// methods *Builder[T] already has with these exact signatures, plus the hooks
// only the typed builder can answer.
type rowsBuilder interface {
	GetQuery() *query.Builder
	Insert(ctx context.Context, g auth.Grant, values ...map[string]any) (bool, error)
	Update(ctx context.Context, g auth.Grant, values map[string]any) (int64, error)
	Upsert(ctx context.Context, g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error)
	Delete(ctx context.Context, g auth.Grant) (int64, error)
	GetCountForPagination(ctx context.Context, g auth.Grant) (int64, error)

	// get runs the query and hands back the models. See Builder.get.
	get(ctx context.Context, g auth.Grant, columns ...any) (models, error)

	// modelRow is the model the builder queries.
	modelRow() row

	// cloneRows is Clone, as this interface.
	cloneRows() rowsBuilder

	// whereRow is Where, for a caller that cannot name the typed builder: it
	// still recognises the typed nested closure Where takes.
	whereRow(column any, args ...any)
}

// refState is what modelRef reads and writes on the typed model that has no
// method with a signature the adapter can call: two fields, the table, and the
// loaded relations. The typed model hands out pointers to its own, so what the
// adapter writes is on the model the caller holds.
type refState struct {
	exists             *bool
	wasRecentlyCreated *bool
	table              *string
	relations          *map[string]any
}

var (
	_ concerns.Model   = (*modelRef)(nil)
	_ concerns.Builder = (*builderRef)(nil)
)

// Ref returns m as the model a relation takes.
//
// The value is cached on the model, so two calls answer the same one. Nothing
// keys a map by a model today, and a ref that was a new value every call would
// be a trap waiting for the first thing that did.
func (m *Model[T]) Ref() concerns.Model {
	if m.ref == nil {
		m.ref = &modelRef{m: m}
	}
	return m.ref
}

// refState hands modelRef the fields it writes. See refState.
func (m *Model[T]) refState() refState {
	return refState{
		exists:             &m.Exists,
		wasRecentlyCreated: &m.WasRecentlyCreated,
		table:              &m.Table,
		relations:          &m.relations,
	}
}

// newRow is NewInstance for a caller that cannot name *Model[T].
func (m *Model[T]) newRow(attributes map[string]any) (row, error) {
	instance, err := m.NewInstance(attributes, false)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// newQueryRef is NewQuery, as the builder a relation takes.
func (m *Model[T]) newQueryRef() concerns.Builder { return m.NewQuery().Ref() }

// Ref returns b as the builder a relation takes.
func (b *Builder[T]) Ref() concerns.Builder { return &builderRef{b: b} }

// modelRow is the hook behind rowsBuilder.modelRow.
func (b *Builder[T]) modelRow() row { return b.model }

// cloneRows is the hook behind rowsBuilder.cloneRows.
func (b *Builder[T]) cloneRows() rowsBuilder { return clone(b) }

// whereRow is the hook behind rowsBuilder.whereRow.
func (b *Builder[T]) whereRow(column any, args ...any) { b.Where(column, args...) }

// Unref is the way back: the typed model behind a ref, and whether the ref was
// over this entity at all.
//
// A ref over another entity answers false rather than panicking, because the
// question "is this relation's model a Post" is one a caller is entitled to ask
// and get no for.
func Unref[T any](m concerns.Model) (*Model[T], bool) {
	ref, ok := m.(*modelRef)
	if !ok {
		return nil, false
	}
	typed, ok := ref.m.(*Model[T])
	return typed, ok
}

// refsOf returns every model as the interface a relation takes.
func refsOf(found models) []concerns.Model {
	out := make([]concerns.Model, 0, len(found))
	for _, m := range found {
		out = append(out, m.Ref())
	}
	return out
}

// -- modelRef ---------------------------------------------------------------

func (r *modelRef) GetTable() string                    { return r.m.GetTable() }
func (r *modelRef) QualifyColumn(column string) string  { return r.m.QualifyColumn(column) }
func (r *modelRef) GetKeyName() string                  { return r.m.GetKeyName() }
func (r *modelRef) GetKeyType() string                  { return r.m.GetKeyType() }
func (r *modelRef) GetKey() any                         { return r.m.GetKey() }
func (r *modelRef) GetForeignKey() string               { return r.m.GetForeignKey() }
func (r *modelRef) GetMorphClass() string               { return r.m.GetMorphClass() }
func (r *modelRef) GetAttribute(key string) any         { return r.m.GetAttribute(key) }
func (r *modelRef) GetAttributes() map[string]any       { return r.m.GetAttributes() }
func (r *modelRef) RelationLoaded(relation string) bool { return r.m.RelationLoaded(relation) }
func (r *modelRef) GetCreatedAtColumn() string          { return r.m.GetCreatedAtColumn() }
func (r *modelRef) GetUpdatedAtColumn() string          { return r.m.GetUpdatedAtColumn() }
func (r *modelRef) UsesTimestamps() bool                { return r.m.UsesTimestamps() }
func (r *modelRef) UnsetAttribute(key string)           { r.m.UnsetAttribute(key) }
func (r *modelRef) IsRelation(key string) bool          { return r.m.IsRelation(key) }
func (r *modelRef) Touches(relation string) bool        { return r.m.Touches(relation) }

func (r *modelRef) FreshTimestamp() time.Time { return r.m.FreshTimestamp() }

// Exists and WasRecentlyCreated are fields on the model and methods here, and
// that is the collision the adapter exists to absorb: a Go type cannot have
// both under one name, and this is a different type.
func (r *modelRef) Exists() bool             { return *r.m.refState().exists }
func (r *modelRef) WasRecentlyCreated() bool { return *r.m.refState().wasRecentlyCreated }

func (r *modelRef) GetRelation(relation string) (any, bool) { return r.m.GetRelation(relation) }
func (r *modelRef) SetRelation(relation string, value any) {
	setRelation(r.m.refState().relations, relation, value)
}
func (r *modelRef) UnsetRelation(relation string) { delete(*r.m.refState().relations, relation) }
func (r *modelRef) SetTable(table string)         { *r.m.refState().table = table }

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

// NewInstance answers a fresh model of the same kind, as a ref.
//
// The error the typed constructor reports is held rather than dropped, and the
// next method that can report one does.
func (r *modelRef) NewInstance(attributes map[string]any) concerns.Model {
	instance, err := r.m.newRow(attributes)
	if err != nil {
		failed := &modelRef{m: r.m}
		failed.hold(err)
		return failed
	}
	return instance.Ref()
}

func (r *modelRef) NewQuery() concerns.Builder { return r.m.newQueryRef() }

func (r *modelRef) Save(ctx context.Context, g auth.Grant) error {
	if err := r.taken(); err != nil {
		return err
	}
	_, err := r.m.Save(ctx, g)
	return err
}

// Delete answers rows affected where the typed one answers whether anything
// went.
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

func (r *builderRef) GetModel() concerns.Model { return r.b.modelRow().Ref() }
func (r *builderRef) GetQuery() *query.Builder { return r.b.GetQuery() }

// ScopesOwnTableByTenant answers for every terminal on the typed builder at
// once, because they share the one door: prepare puts the model's tenant column
// on the statement, and nothing runs without going through it.
//
// The column it uses is the model's own, which is the reason this is answered
// here rather than left to the relation. A model that declares a different
// column, or declares none because its table is shared, is filtered on what it
// declared -- where a second filter added from outside would only know the
// default name, and would name a column a shared table does not have.
func (r *builderRef) ScopesOwnTableByTenant() bool { return true }

// The chainables. Each returns this ref rather than a new one: the typed
// builder mutates and returns itself, and a ref that allocated per call would
// make a chain of ten allocate ten.
//
// Every one but Where and WhereKey is a forward to the query the typed builder
// holds, which is all the typed method does too. Where goes through the typed
// builder because it also takes the typed nested closure.
func (r *builderRef) Select(columns ...any) concerns.Builder {
	r.b.GetQuery().Select(columns...)
	return r
}

func (r *builderRef) AddSelect(columns ...any) concerns.Builder {
	r.b.GetQuery().AddSelect(columns...)
	return r
}

func (r *builderRef) Where(column any, args ...any) concerns.Builder {
	r.b.whereRow(column, args...)
	return r
}

func (r *builderRef) WhereIn(column any, values []any) concerns.Builder {
	r.b.GetQuery().WhereIn(column, values)
	return r
}

func (r *builderRef) WhereNotNull(columns ...any) concerns.Builder {
	r.b.GetQuery().WhereNotNull(columns...)
	return r
}

func (r *builderRef) WhereColumn(first any, args ...any) concerns.Builder {
	r.b.GetQuery().WhereColumn(first, args...)
	return r
}

func (r *builderRef) WhereKey(ids ...any) concerns.Builder {
	if len(ids) == 1 {
		whereKey(r.b, ids[0])
		return r
	}
	whereKey(r.b, ids)
	return r
}

func (r *builderRef) Join(table any, first any, args ...any) concerns.Builder {
	r.b.GetQuery().Join(table, first, args...)
	return r
}

func (r *builderRef) GroupBy(groups ...any) concerns.Builder {
	r.b.GetQuery().GroupBy(groups...)
	return r
}

func (r *builderRef) SelectRaw(expression string, bindings ...any) concerns.Builder {
	r.b.GetQuery().SelectRaw(expression, bindings...)
	return r
}

func (r *builderRef) OrderBy(column any, direction ...string) concerns.Builder {
	r.b.GetQuery().OrderBy(column, direction...)
	return r
}

func (r *builderRef) Limit(value int) concerns.Builder  { r.b.GetQuery().Limit(value); return r }
func (r *builderRef) Offset(value int) concerns.Builder { r.b.GetQuery().Offset(value); return r }
func (r *builderRef) Clone() concerns.Builder           { return &builderRef{b: r.b.cloneRows()} }

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
	found, err := firstRow(r.b, ctx, g)
	if err != nil || found == nil {
		return nil, err
	}
	return found.Ref(), nil
}

func (r *builderRef) Find(ctx context.Context, g auth.Grant, id any) (concerns.Model, error) {
	found, err := findRow(r.b, ctx, g, id)
	if err != nil || found == nil {
		return nil, err
	}
	return found.Ref(), nil
}

func (r *builderRef) Cursor(ctx context.Context, g auth.Grant) iter.Seq2[concerns.Model, error] {
	return func(yield func(concerns.Model, error) bool) {
		// The typed cursor reports its failure through a pointer rather than
		// beside each value, so the error is read after the walk and yielded
		// then. A stream that fails halfway has already handed out rows, which
		// is why the interface carries the error beside the value at all.
		found, err := r.b.get(ctx, g)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, m := range found {
			if !yield(m.Ref(), nil) {
				return
			}
		}
	}
}

func (r *builderRef) Paginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.LengthAwarePage, error) {
	items, total, perPage, page, err := paginateRows(r.b, ctx, g, perPage, page, columns...)
	if err != nil {
		return nil, nil, err
	}
	return refsOf(items), pagination.NewLengthAwarePage(len(items), int(total), perPage, page, opts), nil
}

func (r *builderRef) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.Page, error) {
	items, meta, err := simplePaginateRows(r.b, ctx, g, perPage, page, opts, columns...)
	if err != nil {
		return nil, nil, err
	}
	return refsOf(items), meta, nil
}

func (r *builderRef) CursorPaginate(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, opts pagination.Options, columns ...any) ([]concerns.Model, *pagination.CursorPage, error) {
	items, meta, err := cursorPaginateRows(r.b, ctx, g, perPage, cursor, opts, columns...)
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
