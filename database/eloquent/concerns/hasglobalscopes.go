package concerns

import (
	"sort"
	"sync"

	"github.com/arandu-io/hesape/database/eloquent/relations"
)

// Scope answers Illuminate\Database\Eloquent\Scope: a constraint applied to
// every query for a model.
//
// The PHP contract is an object with apply(Builder, Model); here it is the
// function, because an interface with one method and no state is a function
// with extra steps.
type Scope func(query relations.Builder)

// HasGlobalScopes answers
// Illuminate\Database\Eloquent\Concerns\HasGlobalScopes.
//
// A global scope is a where clause every query for a model gets whether the
// caller asked or not -- soft deletes are one. It is the sharpest tool in the
// model, because a scope nobody remembers is a filter nobody can see in the
// call site, and removing it is a one-word method call.
//
// It is NOT where tenant scoping goes. A tenant filter that can be lifted with
// withoutGlobalScope is a tenant filter, and RULE 14 says the tenant comes from
// the Grant on the way to the repository, where nothing can remove it.
type HasGlobalScopes struct {
	scopes map[string]Scope
	mutex  sync.RWMutex
}

// AddGlobalScope answers HasGlobalScopes::addGlobalScope.
func (h *HasGlobalScopes) AddGlobalScope(name string, scope Scope) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.scopes == nil {
		h.scopes = map[string]Scope{}
	}
	h.scopes[name] = scope
}

// AddGlobalScopes answers HasGlobalScopes::addGlobalScopes.
func (h *HasGlobalScopes) AddGlobalScopes(scopes map[string]Scope) {
	for name, scope := range scopes {
		h.AddGlobalScope(name, scope)
	}
}

// HasGlobalScope answers HasGlobalScopes::hasGlobalScope.
func (h *HasGlobalScopes) HasGlobalScope(name string) bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	_, ok := h.scopes[name]
	return ok
}

// GetGlobalScope answers HasGlobalScopes::getGlobalScope.
func (h *HasGlobalScopes) GetGlobalScope(name string) Scope {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.scopes[name]
}

// GetGlobalScopes answers HasGlobalScopes::getGlobalScopes.
func (h *HasGlobalScopes) GetGlobalScopes() map[string]Scope {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	out := make(map[string]Scope, len(h.scopes))
	for name, scope := range h.scopes {
		out[name] = scope
	}
	return out
}

// GlobalScopeNames returns the registered names in a stable order.
//
// The PHP applies scopes in the order they were added, which its array
// remembers. A Go map does not, so applying them in map order would produce a
// different statement per run; sorted is the closest thing to deterministic
// that a map allows.
func (h *HasGlobalScopes) GlobalScopeNames() []string {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	names := make([]string, 0, len(h.scopes))
	for name := range h.scopes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApplyGlobalScopes applies every registered scope to a query, minus the ones
// named in without -- which is Builder::withoutGlobalScopes.
func (h *HasGlobalScopes) ApplyGlobalScopes(query relations.Builder, without ...string) relations.Builder {
	for _, name := range h.GlobalScopeNames() {
		if contains(without, name) {
			continue
		}
		h.GetGlobalScope(name)(query)
	}
	return query
}
