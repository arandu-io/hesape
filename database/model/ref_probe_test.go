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

// The probe for the merge of this package with database/model/relations.
//
// The two packages declare different contracts for the same idea, and nothing
// implements both: *Model[T] does not satisfy concerns.Model, and *Builder[T]
// does not satisfy concerns.Builder. The plan is to bridge them with an
// adapter rather than to change either signature, and this file exists to
// answer one question before that plan is written against: can an adapter over
// *Model[T] satisfy concerns.Model at all?
//
// It is a test file so that it ships with nothing. What it proves is checked by
// the compiler, at the two var declarations below, and by nothing else -- there
// is no Test function here, because a stub whose body panics has nothing to
// assert. When the merge lands this file is deleted and its contents move into
// ref.go, with the panics replaced by the real delegation.
//
// # What it already answers
//
// The field-versus-method collision is not a collision at the adapter. Model[T]
// declares Exists and WasRecentlyCreated as fields (model.go), and
// concerns.Model declares them as methods; a Go type cannot have both under one
// name, but the adapter is a different type -- it reads the field and answers
// the method. So the eight field-to-method conversions the merge looked like it
// needed are not needed: the struct keeps its fields.
//
// The same holds for every signature that differs only in what it returns or in
// how it reports failure. Save is (bool, error) here and error there; Delete is
// (bool, error) here and (int64, error) there; SetTable returns *Model[T] here
// and nothing there. The adapter absorbs all of it.
//
// What it does NOT absorb is a method that does not exist. Four were missing and
// have since been written; the model side of this probe is delegation
// throughout. What is left in stubs is the builder, whose fourteen chainables
// are the covariant-return problem the adapter exists to answer.

// modelRef is *Model[T] seen through the interface a relation asks for.
type modelRef[T any] struct{ m *Model[T] }

// builderRef is *Builder[T] seen the same way.
type builderRef[T any] struct{ b *Builder[T] }

// The probe. If these two lines compile, the adapter is possible.
var (
	_ concerns.Model   = (*modelRef[struct{}])(nil)
	_ concerns.Builder = (*builderRef[struct{}])(nil)
)

// -- modelRef: what delegates today, unchanged ------------------------------

func (r *modelRef[T]) GetTable() string                    { return r.m.GetTable() }
func (r *modelRef[T]) QualifyColumn(column string) string  { return r.m.QualifyColumn(column) }
func (r *modelRef[T]) GetKeyName() string                  { return r.m.GetKeyName() }
func (r *modelRef[T]) GetKeyType() string                  { return r.m.GetKeyType() }
func (r *modelRef[T]) GetKey() any                         { return r.m.GetKey() }
func (r *modelRef[T]) GetForeignKey() string               { return r.m.GetForeignKey() }
func (r *modelRef[T]) GetMorphClass() string               { return r.m.GetMorphClass() }
func (r *modelRef[T]) GetAttribute(key string) any         { return r.m.GetAttribute(key) }
func (r *modelRef[T]) GetAttributes() map[string]any       { return r.m.GetAttributes() }
func (r *modelRef[T]) RelationLoaded(relation string) bool { return r.m.RelationLoaded(relation) }
func (r *modelRef[T]) GetCreatedAtColumn() string          { return r.m.GetCreatedAtColumn() }
func (r *modelRef[T]) GetUpdatedAtColumn() string          { return r.m.GetUpdatedAtColumn() }
func (r *modelRef[T]) UsesTimestamps() bool                { return r.m.UsesTimestamps() }
func (r *modelRef[T]) FreshTimestamp() time.Time           { return r.m.FreshTimestamp() }

func (r *modelRef[T]) WithoutEvents(callback func() error) error {
	return r.m.WithoutEvents(callback)
}

func (r *modelRef[T]) GetRelation(relation string) (any, bool) {
	return r.m.GetRelation(relation)
}

// -- modelRef: the field-versus-method collision, answered ------------------
//
// These two are the reason the merge looked impossible. They are fields on
// Model[T] and methods here, and the adapter simply reads the field.

func (r *modelRef[T]) Exists() bool             { return r.m.Exists }
func (r *modelRef[T]) WasRecentlyCreated() bool { return r.m.WasRecentlyCreated }

// -- modelRef: signatures that differ only in the return --------------------

func (r *modelRef[T]) SetTable(table string)               { r.m.SetTable(table) }
func (r *modelRef[T]) Fill(attributes map[string]any)      { _ = r.m.Fill(attributes) }
func (r *modelRef[T]) ForceFill(attributes map[string]any) { _ = r.m.ForceFill(attributes) }

func (r *modelRef[T]) SetAttribute(key string, value any) { _ = r.m.SetAttribute(key, value) }

func (r *modelRef[T]) SetRawAttributes(attributes map[string]any, sync bool) {
	_ = r.m.SetRawAttributes(attributes, sync)
}

func (r *modelRef[T]) SetRelation(relation string, value any) { r.m.SetRelation(relation, value) }
func (r *modelRef[T]) UnsetRelation(relation string)          { r.m.UnsetRelation(relation) }

// NewInstance drops the error the typed constructor reports.
//
// The interface has nowhere to put it. The real ref holds it and returns it
// from the next method that runs, which is the shape Builder[T].err already
// uses; the probe only has to prove the signature fits.
func (r *modelRef[T]) NewInstance(attributes map[string]any) concerns.Model {
	instance, err := r.m.NewInstance(attributes, false)
	if err != nil {
		return nil
	}
	return &modelRef[T]{m: instance}
}

func (r *modelRef[T]) NewQuery() concerns.Builder {
	return &builderRef[T]{b: r.m.NewQuery()}
}

// Save discards the "was anything written" bool. It becomes state on the model
// in the real ref, not a lost return.
func (r *modelRef[T]) Save(_ context.Context, g auth.Grant) error {
	_, err := r.m.Save(context.Background(), g)
	return err
}

// Delete answers rows affected where the typed one answers a bool.
func (r *modelRef[T]) Delete(_ context.Context, g auth.Grant) (int64, error) {
	deleted, err := r.m.Delete(context.Background(), g)
	if err != nil || !deleted {
		return 0, err
	}
	return 1, nil
}

// -- modelRef: the four that used to be missing -----------------------------
//
// They were the whole gap on this side, and they are on the model now:
// relationsurface.go. Every method of concerns.Model is delegation.

func (r *modelRef[T]) UnsetAttribute(key string)  { r.m.UnsetAttribute(key) }
func (r *modelRef[T]) IsRelation(key string) bool { return r.m.IsRelation(key) }
func (r *modelRef[T]) Touches(rel string) bool    { return r.m.Touches(rel) }

func (r *modelRef[T]) Touch(ctx context.Context, g auth.Grant) error {
	return r.m.Touch(ctx, g)
}

// -- builderRef -------------------------------------------------------------
//
// The covariant-return problem lives here: fourteen of these return Builder,
// and *Builder[T] returns *Builder[T]. The adapter answers it by construction,
// which is what these stubs prove; the bodies are W0.4's work.

func (r *builderRef[T]) GetModel() concerns.Model { return &modelRef[T]{m: r.b.GetModel()} }
func (r *builderRef[T]) GetQuery() *query.Builder { return r.b.GetQuery() }

func (r *builderRef[T]) Select(...any) concerns.Builder    { panic("W0.4") }
func (r *builderRef[T]) AddSelect(...any) concerns.Builder { panic("W0.4") }
func (r *builderRef[T]) Where(any, ...any) concerns.Builder {
	panic("W0.4")
}
func (r *builderRef[T]) WhereIn(any, []any) concerns.Builder      { panic("W0.4") }
func (r *builderRef[T]) WhereNotNull(...any) concerns.Builder     { panic("W0.4") }
func (r *builderRef[T]) WhereColumn(any, ...any) concerns.Builder { panic("W0.4") }
func (r *builderRef[T]) WhereKey(...any) concerns.Builder         { panic("W0.4") }
func (r *builderRef[T]) Join(any, any, ...any) concerns.Builder   { panic("W0.4") }
func (r *builderRef[T]) GroupBy(...any) concerns.Builder          { panic("W0.4") }
func (r *builderRef[T]) SelectRaw(string, ...any) concerns.Builder {
	panic("W0.4")
}
func (r *builderRef[T]) OrderBy(any, ...string) concerns.Builder { panic("W0.4") }
func (r *builderRef[T]) Limit(int) concerns.Builder              { panic("W0.4") }
func (r *builderRef[T]) Offset(int) concerns.Builder             { panic("W0.4") }
func (r *builderRef[T]) Clone() concerns.Builder                 { panic("W0.4") }

func (r *builderRef[T]) Cursor(context.Context, auth.Grant) iter.Seq2[concerns.Model, error] {
	panic("W0.4")
}

func (r *builderRef[T]) Paginate(context.Context, auth.Grant, int, int, pagination.Options, ...any) (*pagination.LengthAwarePaginator[concerns.Model], error) {
	panic("W0.4")
}

func (r *builderRef[T]) SimplePaginate(context.Context, auth.Grant, int, int, pagination.Options, ...any) (*pagination.Paginator[concerns.Model], error) {
	panic("W0.4")
}

func (r *builderRef[T]) CursorPaginate(context.Context, auth.Grant, int, *pagination.Cursor, pagination.Options, ...any) (*pagination.CursorPaginator[concerns.Model], error) {
	panic("W0.4")
}

func (r *builderRef[T]) Get(context.Context, auth.Grant) ([]concerns.Model, error) {
	panic("W0.4")
}

func (r *builderRef[T]) First(context.Context, auth.Grant) (concerns.Model, error) {
	panic("W0.4")
}

func (r *builderRef[T]) Find(context.Context, auth.Grant, any) (concerns.Model, error) {
	panic("W0.4")
}

func (r *builderRef[T]) Insert(context.Context, auth.Grant, []map[string]any) error {
	panic("W0.4")
}

func (r *builderRef[T]) Update(context.Context, auth.Grant, map[string]any) (int64, error) {
	panic("W0.4")
}

func (r *builderRef[T]) Upsert(context.Context, auth.Grant, []map[string]any, []string, []string) (int64, error) {
	panic("W0.4")
}

func (r *builderRef[T]) Delete(context.Context, auth.Grant) (int64, error) { panic("W0.4") }
