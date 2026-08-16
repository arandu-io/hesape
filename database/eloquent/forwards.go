package eloquent

import "github.com/arandu-io/hesape/auth"

// The reads and writes taken straight off a model -- Find, Create and their
// neighbours -- forwarded to a fresh builder.
//
// Every one of them starts from NewQuery, so the global scopes are on, which is
// what makes Find skip a soft deleted row.

// Find calls Find on a fresh query for the model.
func (m *Model[T]) Find(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().Find(g, id, columns...)
}

// FindMany calls FindMany on a fresh query for the model.
func (m *Model[T]) FindMany(g auth.Grant, ids []any, columns ...any) (Collection[T], error) {
	return m.NewQuery().FindMany(g, ids, columns...)
}

// FindOrFail calls FindOrFail on a fresh query for the model.
func (m *Model[T]) FindOrFail(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().FindOrFail(g, id, columns...)
}

// FindOrNew calls FindOrNew on a fresh query for the model.
func (m *Model[T]) FindOrNew(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().FindOrNew(g, id, columns...)
}

// First calls First on a fresh query for the model.
func (m *Model[T]) First(g auth.Grant, columns ...any) (*Model[T], error) {
	return m.NewQuery().First(g, columns...)
}

// FirstOrNew calls FirstOrNew on a fresh query for the model.
func (m *Model[T]) FirstOrNew(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().FirstOrNew(g, attributes, values)
}

// FirstOrCreate calls FirstOrCreate on a fresh query for the model.
func (m *Model[T]) FirstOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().FirstOrCreate(g, attributes, values)
}

// UpdateOrCreate calls UpdateOrCreate on a fresh query for the model.
func (m *Model[T]) UpdateOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().UpdateOrCreate(g, attributes, values)
}

// Create calls Create on a fresh query for the model.
func (m *Model[T]) Create(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	return m.NewQuery().Create(g, attributes)
}

// ForceCreate calls ForceCreate on a fresh query for the model.
func (m *Model[T]) ForceCreate(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	return m.NewQuery().ForceCreate(g, attributes)
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
