package eloquent

import "reflect"

// Scope answers Illuminate\Database\Eloquent\Scope: a filter every query on a
// model carries until somebody removes it by name.
type Scope[T any] interface {
	// Apply answers Scope::apply.
	Apply(builder *Builder[T], model *Model[T])
}

// ScopeExtender is the optional half of a Scope: Illuminate calls extend() when
// the scope has it, which is how SoftDeletingScope hangs withTrashed and
// onlyTrashed off the builder. PHP asks method_exists; Go asks the type.
type ScopeExtender[T any] interface {
	// Extend answers Scope::extend.
	Extend(builder *Builder[T])
}

// AddGlobalScope answers HasGlobalScopes::addGlobalScope.
//
// The identifier is the class name in PHP, where a scope is always a class. Here
// it is given, because a closure has no name and because removing a scope by
// name is the whole point of WithoutGlobalScope.
func (m *Model[T]) AddGlobalScope(identifier string, scope Scope[T]) *Model[T] {
	if m.globalScopes == nil {
		m.globalScopes = map[string]Scope[T]{}
	}
	m.globalScopes[identifier] = scope
	return m
}

// GetGlobalScopes answers HasGlobalScopes::getGlobalScopes.
func (m *Model[T]) GetGlobalScopes() map[string]Scope[T] { return cloneScopes(m.globalScopes) }

// HasGlobalScope answers HasGlobalScopes::hasGlobalScope.
func (m *Model[T]) HasGlobalScope(identifier string) bool {
	_, ok := m.globalScopes[identifier]
	return ok
}

// funcScope is a Scope written as a function, which is what passing a Closure to
// addGlobalScope does in PHP.
type funcScope[T any] func(*Builder[T])

// Apply answers Scope::apply.
func (f funcScope[T]) Apply(builder *Builder[T], _ *Model[T]) { f(builder) }

// ScopeFunc wraps a function as a Scope, for the closure form of
// addGlobalScope.
func ScopeFunc[T any](apply func(*Builder[T])) Scope[T] { return funcScope[T](apply) }

func cloneScopes[T any](in map[string]Scope[T]) map[string]Scope[T] {
	if in == nil {
		return nil
	}
	out := make(map[string]Scope[T], len(in))
	for identifier, scope := range in {
		out[identifier] = scope
	}
	return out
}

// scopeIdentifier answers what PHP's get_class($scope) gives withoutGlobalScope
// when it is handed the scope rather than its name.
func scopeIdentifier[T any](scope Scope[T]) string {
	t := reflect.TypeOf(scope)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
