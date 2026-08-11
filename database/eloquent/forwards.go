package eloquent

import "github.com/arandu-io/hesape/auth"

// The reads and writes a PHP model answers as a static -- User::find(1),
// User::create([...]) -- are not methods on Illuminate's Model at all: they go
// through __callStatic, which forwards to a fresh builder. Go has no
// __callStatic and a generic type has no statics, so the forward is written out,
// once, here.
//
// Every one of them starts from NewQuery, so the global scopes are on -- which
// is what makes User::find() skip a soft deleted row there and here.

// Find answers Model::find, forwarded to the builder.
func (m *Model[T]) Find(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().Find(g, id, columns...)
}

// FindMany answers Model::findMany, forwarded to the builder.
func (m *Model[T]) FindMany(g auth.Grant, ids []any, columns ...any) (Collection[T], error) {
	return m.NewQuery().FindMany(g, ids, columns...)
}

// FindOrFail answers Model::findOrFail, forwarded to the builder.
func (m *Model[T]) FindOrFail(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().FindOrFail(g, id, columns...)
}

// FindOrNew answers Model::findOrNew, forwarded to the builder.
func (m *Model[T]) FindOrNew(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return m.NewQuery().FindOrNew(g, id, columns...)
}

// First answers Model::first, forwarded to the builder.
func (m *Model[T]) First(g auth.Grant, columns ...any) (*Model[T], error) {
	return m.NewQuery().First(g, columns...)
}

// FirstOrNew answers Model::firstOrNew, forwarded to the builder.
func (m *Model[T]) FirstOrNew(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().FirstOrNew(g, attributes, values)
}

// FirstOrCreate answers Model::firstOrCreate, forwarded to the builder.
func (m *Model[T]) FirstOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().FirstOrCreate(g, attributes, values)
}

// UpdateOrCreate answers Model::updateOrCreate, forwarded to the builder.
func (m *Model[T]) UpdateOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	return m.NewQuery().UpdateOrCreate(g, attributes, values)
}

// Create answers Model::create, forwarded to the builder.
func (m *Model[T]) Create(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	return m.NewQuery().Create(g, attributes)
}

// ForceCreate answers Model::forceCreate, forwarded to the builder.
func (m *Model[T]) ForceCreate(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	return m.NewQuery().ForceCreate(g, attributes)
}

// With answers Model::with, forwarded to the builder.
func (m *Model[T]) With(relations ...string) *Builder[T] {
	return m.NewQuery().With(relations...)
}

// Where answers Model::where, forwarded to the builder.
func (m *Model[T]) Where(column any, args ...any) *Builder[T] {
	return m.NewQuery().Where(column, args...)
}

// WhereKey answers Model::whereKey, forwarded to the builder.
func (m *Model[T]) WhereKey(id any) *Builder[T] { return m.NewQuery().WhereKey(id) }

// WithTrashed answers the withTrashed macro, forwarded to the builder.
func (m *Model[T]) WithTrashed() *Builder[T] { return m.NewQuery().WithTrashed() }

// OnlyTrashed answers the onlyTrashed macro, forwarded to the builder.
func (m *Model[T]) OnlyTrashed() *Builder[T] { return m.NewQuery().OnlyTrashed() }
