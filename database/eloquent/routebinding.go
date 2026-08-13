package eloquent

import (
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/str"
)

// GetRouteKey is Model::getRouteKey: the value that stands for this row in a URL.
//
// It is an any because the PHP returns mixed and the key is whatever the key
// column holds. routing.UrlRoutable asks for a string, because a path segment is
// one; a model type that is bound in a route formats this value there.
func (m *Model[T]) GetRouteKey() any { return m.GetAttribute(m.GetRouteKeyName()) }

// GetRouteKeyName is Model::getRouteKeyName: the column the route binds on.
func (m *Model[T]) GetRouteKeyName() string { return m.GetKeyName() }

// ResolveRouteBindingQuery is Model::resolveRouteBindingQuery: the where clause
// a bound value becomes.
//
// It only builds, so it takes no Grant. Nothing it returns can run without one
// -- First and Get both refuse a Grant with no tenant -- which is the whole
// reason this is the query and ResolveRouteBinding is the lookup.
func (m *Model[T]) ResolveRouteBindingQuery(b *Builder[T], value any, field string) *Builder[T] {
	if field == "" {
		field = m.GetRouteKeyName()
	}
	return b.Where(field, "=", value)
}

// ResolveRouteBinding is Model::resolveRouteBinding: the row a URL segment
// stands for.
//
// # This is the line a multi-tenant application leaks on
//
// /invoices/9 arrives, 9 is read off the path, a row is loaded by that id alone,
// and the reader is handed another customer's invoice. No policy was violated --
// none was consulted. So the Grant is a parameter and not something read out of
// a context or a global: a resolver written without it does not compile, and the
// query it builds refuses a Grant carrying no tenant before any SQL exists
// (RULE 14, RULE 17).
//
// The PHP returns null when nothing matched; here that is a nil model with a nil
// error, which is a 404 and not a failure.
func (m *Model[T]) ResolveRouteBinding(g auth.Grant, value any, field string) (*Model[T], error) {
	return m.ResolveRouteBindingQuery(m.NewQuery(), value, field).First(g)
}

// ResolveSoftDeletableRouteBinding is
// Model::resolveSoftDeletableRouteBinding: the same lookup, trashed rows
// included.
func (m *Model[T]) ResolveSoftDeletableRouteBinding(g auth.Grant, value any, field string) (*Model[T], error) {
	return m.ResolveRouteBindingQuery(m.NewQuery().WithTrashed(), value, field).First(g)
}

// ChildRouteBindingRelationshipName is
// Model::childRouteBindingRelationshipName: the relation a scoped nested
// resource resolves through, which is the child type camel cased and pluralised.
//
// The PHP keeps it protected, because there the name is fed straight into
// $this->{$name}(). Here it is exported, because ResolveChildRouteBinding is a
// package function and cannot reach a protected method of its own.
func ChildRouteBindingRelationshipName(childType string) string {
	return str.Plural(str.Camel(childType))
}

// ResolveChildRouteBinding is Model::resolveChildRouteBinding: the row a URL
// segment stands for, restricted to the children of this one.
//
// It is a function and not a method because the child is a model of another
// type, and a method on Model[T] cannot name Model[R]. childType names the
// relation on the parent, exactly as it does there; children is that relation as
// the typed query it stands for, because the Relation a resolver hands back is
// narrowed to what the eager loader asks of it and cannot produce a Builder[R].
// That is the same reason RelationResolvers exists: Go makes no type from a
// string.
//
// The Grant is filtered on by First, on the child's own table. A nested resource
// scoped only by its parent is still a row somebody else owns when the parent
// was reached without a tenant filter, so both ends carry it.
func ResolveChildRouteBinding[T, R any](parent *Model[T], children *Builder[R], g auth.Grant, childType string, value any, field string) (*Model[R], error) {
	q, err := childRouteBindingQuery(parent, children, childType, value, field)
	if err != nil {
		return nil, err
	}
	return q.First(g)
}

// ResolveSoftDeletableChildRouteBinding is
// Model::resolveSoftDeletableChildRouteBinding: the same lookup, trashed rows
// included.
func ResolveSoftDeletableChildRouteBinding[T, R any](parent *Model[T], children *Builder[R], g auth.Grant, childType string, value any, field string) (*Model[R], error) {
	q, err := childRouteBindingQuery(parent, children, childType, value, field)
	if err != nil {
		return nil, err
	}
	return q.WithTrashed().First(g)
}

// childRouteBindingQuery answers Model::resolveChildRouteBindingQuery.
//
// The PHP qualifies the field when the relation goes through a pivot or a third
// table; here it qualifies always, because a qualified column is what the
// unqualified one compiles to on a single table anyway, and the caller cannot
// tell this function which shape the relation has.
func childRouteBindingQuery[T, R any](parent *Model[T], children *Builder[R], childType string, value any, field string) (*Builder[R], error) {
	name := ChildRouteBindingRelationshipName(childType)
	if _, ok := parent.RelationResolvers[name]; !ok {
		return nil, fmt.Errorf("%w: %s on %s", ErrRelationNotFound, name, parent.GetTable())
	}
	related := children.GetModel()
	if field == "" {
		field = related.GetRouteKeyName()
	}
	return related.ResolveRouteBindingQuery(children, value, related.QualifyColumn(field)), nil
}
