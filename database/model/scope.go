package model

import "slices"

// Scope is a filter every query on a table carries until somebody removes it by
// name: the global scopes of TableSpec.Scopes, and whatever WithGlobalScope adds
// to one query.
//
// It is a function over the builder, and that is the whole of it. A local scope
// -- a filter a caller asks for by name -- is a method on the generated query
// type, written in its custom block, where the compiler checks the name.
type Scope func(*Builder)

// sortedScopeNames keeps the order scopes are applied in stable. A Go map has no
// order, and two scopes that disagree about the order they run in are a bug that
// only shows up sometimes.
func sortedScopeNames(scopes map[string]Scope) []string {
	out := make([]string, 0, len(scopes))
	for identifier := range scopes {
		out = append(out, identifier)
	}
	slices.Sort(out)
	return out
}

func cloneScopes(in map[string]Scope) map[string]Scope {
	if in == nil {
		return nil
	}
	out := make(map[string]Scope, len(in))
	for identifier, scope := range in {
		out[identifier] = scope
	}
	return out
}
