package model

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/pagination"
)

// The reads and writes taken straight off a model -- Find, Create and their
// neighbours -- forwarded to a fresh builder.
//
// Every one of them starts from NewQuery, so the global scopes are on, which is
// what makes Find skip a soft deleted row.

// Find calls Find on a fresh query for the model.
func (m *Model[T]) Find(ctx context.Context, g auth.Grant, id any, columns ...any) (*T, error) {
	return m.NewQuery().Find(ctx, g, id, columns...)
}

// FindMany calls FindMany on a fresh query for the model.
func (m *Model[T]) FindMany(ctx context.Context, g auth.Grant, ids []any, columns ...any) (Collection[T], error) {
	return m.NewQuery().FindMany(ctx, g, ids, columns...)
}

// FindOrFail calls FindOrFail on a fresh query for the model.
func (m *Model[T]) FindOrFail(ctx context.Context, g auth.Grant, id any, columns ...any) (*T, error) {
	return m.NewQuery().FindOrFail(ctx, g, id, columns...)
}

// FindOrNew calls FindOrNew on a fresh query for the model.
func (m *Model[T]) FindOrNew(ctx context.Context, g auth.Grant, id any, columns ...any) (*T, error) {
	return m.NewQuery().FindOrNew(ctx, g, id, columns...)
}

// First calls First on a fresh query for the model.
func (m *Model[T]) First(ctx context.Context, g auth.Grant, columns ...any) (*T, error) {
	return m.NewQuery().First(ctx, g, columns...)
}

// FirstOrNew calls FirstOrNew on a fresh query for the model.
func (m *Model[T]) FirstOrNew(ctx context.Context, g auth.Grant, attributes, values map[string]any) (*T, error) {
	return m.NewQuery().FirstOrNew(ctx, g, attributes, values)
}

// FirstOrCreate calls FirstOrCreate on a fresh query for the model.
func (m *Model[T]) FirstOrCreate(ctx context.Context, g auth.Grant, attributes, values map[string]any) (*T, error) {
	return m.NewQuery().FirstOrCreate(ctx, g, attributes, values)
}

// UpdateOrCreate calls UpdateOrCreate on a fresh query for the model.
func (m *Model[T]) UpdateOrCreate(ctx context.Context, g auth.Grant, attributes, values map[string]any) (*T, error) {
	return m.NewQuery().UpdateOrCreate(ctx, g, attributes, values)
}

// Create calls Create on a fresh query for the model.
func (m *Model[T]) Create(ctx context.Context, g auth.Grant, attributes map[string]any) (*T, error) {
	return m.NewQuery().Create(ctx, g, attributes)
}

// ForceCreate calls ForceCreate on a fresh query for the model.
func (m *Model[T]) ForceCreate(ctx context.Context, g auth.Grant, attributes map[string]any) (*T, error) {
	return m.NewQuery().ForceCreate(ctx, g, attributes)
}

// With calls With on a fresh query for the model.
func (m *Model[T]) With(relations ...string) *Builder[T] {
	return m.NewQuery().With(relations...)
}

// Where calls Where on a fresh query for the model.
func (m *Model[T]) Where(column any, args ...any) *Builder[T] {
	return m.NewQuery().Where(column, args...)
}

// WhereKey calls WhereKey on a fresh query for the model.
func (m *Model[T]) WhereKey(id any) *Builder[T] { return m.NewQuery().WhereKey(id) }

// WithTrashed calls WithTrashed on a fresh query for the model.
func (m *Model[T]) WithTrashed() *Builder[T] { return m.NewQuery().WithTrashed() }

// OnlyTrashed calls OnlyTrashed on a fresh query for the model.
func (m *Model[T]) OnlyTrashed() *Builder[T] { return m.NewQuery().OnlyTrashed() }

// Latest calls Latest on a fresh query for the model: newest first, by column
// or by the created-at column when none is given.
func (m *Model[T]) Latest(column ...string) *Builder[T] { return m.NewQuery().Latest(column...) }

// Oldest calls Oldest on a fresh query for the model: oldest first, by column
// or by the created-at column when none is given.
func (m *Model[T]) Oldest(column ...string) *Builder[T] { return m.NewQuery().Oldest(column...) }

// SimplePaginate calls SimplePaginate on a fresh query for the model: one
// page, in the order the table returns it, and whether there is another.
func (m *Model[T]) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (*pagination.Paginator[*T], error) {
	return m.NewQuery().SimplePaginate(ctx, g, perPage, page, opts, columns...)
}

// DoesntExist calls DoesntExist on a fresh query for the model: whether the
// tenant's table holds no row the global scopes let through, which is the
// question a seeder asks before it fills one.
//
// It has no Exists counterpart on the model, because Exists is the field that
// says whether this instance is stored, and a type cannot carry a field and a
// method of the same name. NewQuery().Exists asks the positive question.
func (m *Model[T]) DoesntExist(ctx context.Context, g auth.Grant) (bool, error) {
	return m.NewQuery().DoesntExist(ctx, g)
}
