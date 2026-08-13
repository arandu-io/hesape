package eloquent

import "fmt"

// NamedScope is one entry of Model.NamedScopes: what PHP writes as a
// scopeSomething method on the model.
//
// The builder is the scope's first parameter there too -- Builder::callScope
// prepends it -- and the rest is whatever the caller passed. It is a func and
// not a method because Go cannot look a method up by name, which is the same
// reason RelationResolvers is a map.
type NamedScope[T any] func(builder *Builder[T], parameters ...any) *Builder[T]

// HasNamedScope is Model::hasNamedScope.
//
// The PHP asks method_exists for 'scope'.ucfirst($scope), and then for a method
// carrying the #[Scope] attribute. Neither question exists in Go, so the scope is
// registered under its bare name -- Active, not scopeActive -- and this is a
// lookup in that map.
func (m *Model[T]) HasNamedScope(scope string) bool {
	_, ok := m.NamedScopes[scope]
	return ok
}

// CallNamedScope is Model::callNamedScope.
//
// The PHP takes the parameters as one array with the builder already at the
// front of it, because callScope put it there. Here the builder is its own
// argument: an []any that must have a *Builder[T] in slot zero is a signature
// that documents nothing and fails at run time.
func (m *Model[T]) CallNamedScope(scope string, b *Builder[T], parameters ...any) *Builder[T] {
	apply, ok := m.NamedScopes[scope]
	if !ok {
		return b.fail(fmt.Errorf("%w: %s on %s", ErrNamedScopeNotFound, scope, m.GetTable()))
	}
	return apply(b, parameters...)
}

// HasNamedScope is Builder::hasNamedScope.
func (b *Builder[T]) HasNamedScope(scope string) bool {
	return b.model != nil && b.model.HasNamedScope(scope)
}

// Scopes is Builder::scopes: the named scopes applied in order, each one's
// wheres grouped so that an or inside a scope cannot escape it.
//
// The PHP takes either a list of names or a map of name to parameters, because a
// PHP array is both. Go has two types there, so this takes the names and
// CallNamedScope takes the parameters -- a scope that needs arguments is called
// by name rather than listed.
func (b *Builder[T]) Scopes(scopes ...string) *Builder[T] {
	out := b
	for _, scope := range scopes {
		before := len(out.query.Wheres)
		out = out.model.CallNamedScope(scope, out)
		groupNewWheres(out.query, before)
	}
	return out
}
