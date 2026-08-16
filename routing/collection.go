package routing

import (
	"net/http"
)

// Add registers a route in the table and returns it. It is the exported form
// of the add the router uses, for a caller that holds a table it built
// separately -- a test, or a compiled route collection.
func (t *Routes) Add(route *Route) *Route {
	if route == nil {
		return nil
	}
	t.add(route)
	return route
}

// Match returns the route that answers the request, or nil. It is the table's
// side of what the mux does: the mux has already picked a route and dispatched
// it, but Current, a middleware that runs before the handler, and a custom
// dispatcher all need to find the route from the request, and they do it here.
//
// Non-fallback routes are tried first, in registration order; a fallback
// route is returned only if nothing else matched.
func (t *Routes) Match(req *http.Request) *Route {
	if req == nil || t == nil {
		return nil
	}
	var fallback *Route
	for _, route := range t.All() {
		if !route.Matches(req, true) {
			continue
		}
		if route.IsFallback() {
			if fallback == nil {
				fallback = route
			}
			continue
		}
		return route
	}
	return fallback
}

// GetByName returns the route registered with name, or nil. It is the
// exported form the URL generator reads.
func (t *Routes) GetByName(name string) *Route {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.byName[name]
}

// GetByAction returns the route whose action string is action, or nil. The
// action string is what Uses set -- "PostController@show" or "Closure". The
// lookup is a scan, because the action is set after registration through a
// fluent call, and an index maintained at registration would miss it.
func (t *Routes) GetByAction(action string) *Route {
	if t == nil {
		return nil
	}
	for _, route := range t.All() {
		if route.GetActionName() == action {
			return route
		}
	}
	return nil
}

// GetRoutes returns the routes in registration order. It is an alias for All.
func (t *Routes) GetRoutes() []*Route {
	if t == nil {
		return nil
	}
	return t.All()
}

// GetRoutesByMethod returns the routes that answer the given method, in
// registration order. A route registered with ANY answers every method.
func (t *Routes) GetRoutesByMethod(method string) []*Route {
	if t == nil {
		return nil
	}
	var out []*Route
	for _, route := range t.All() {
		if route.methodMatches(method) {
			out = append(out, route)
		}
	}
	return out
}

// GetRoutesByName returns a copy of the name index.
func (t *Routes) GetRoutesByName() map[string]*Route {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]*Route, len(t.byName))
	for k, v := range t.byName {
		out[k] = v
	}
	return out
}

// Count returns the number of routes registered.
func (t *Routes) Count() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.all)
}

// HasNamedRoute reports whether a route with the given name is registered.
func (t *Routes) HasNamedRoute(name string) bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.byName[name]
	return ok
}

// Refresh rebuilds the name index from the route list. A name set fluently
// after the route was added to the table -- through Name, which writes the
// field and then calls register -- is already indexed; Refresh is for the
// caller that rewrites names in bulk and needs the index to follow.
func (t *Routes) Refresh() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byName = map[string]*Route{}
	for _, route := range t.all {
		if route.name != "" {
			t.byName[route.name] = route
		}
	}
}

// RefreshNameLookups is an alias for Refresh.
func (t *Routes) RefreshNameLookups() { t.Refresh() }

// RefreshActionLookups is a no-op: the action index is a scan, not a
// maintained index, so there is nothing to rebuild.
func (t *Routes) RefreshActionLookups() {}
