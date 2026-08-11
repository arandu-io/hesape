package routing

import (
	"net/http"
	"strings"

	"github.com/arandu-io/hesape/pipeline"
)

// Options registers an OPTIONS route.
func (r *Router) Options(pattern string, h http.Handler, mws ...pipeline.Middleware[http.Handler]) *Route {
	return r.handle(http.MethodOptions, pattern, h, mws...)
}

// Redirect registers a route that redirects to destination with status.
// The handler it installs writes the Location and the status, which is the
// shape RedirectController has in Laravel and what a redirect route answers.
//
// It is registered under Any so that a link or form of any verb that lands
// here is redirected; the status is the caller's to set (302 for Redirect,
// 301 for PermanentRedirect).
func (r *Router) Redirect(uri, destination string, status ...int) *Route {
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination, code)
	})
	route := r.Any(uri, h)
	route.action["uses"] = "RedirectController"
	route.action["controller"] = "RedirectController"
	route.defaults["destination"] = destination
	route.defaults["status"] = code
	return route
}

// PermanentRedirect registers a 301 redirect route.
func (r *Router) PermanentRedirect(uri, destination string) *Route {
	return r.Redirect(uri, destination, http.StatusMovedPermanently)
}

// View registers a GET route that renders a view.
//
// The view renderer is the one SetViewRenderer wired; without it, the route
// answers 500, which is the honest answer to "the view layer was not given to
// the router". The handler it installs calls the renderer, and the data map is
// carried in the route's defaults -- the same place Redirect carries its
// destination, which keeps the two redirect-and-view routes shaped alike.
func (r *Router) View(pattern, view string, data ...map[string]any) *Route {
	d := map[string]any{}
	if len(data) > 0 {
		d = data[0]
	}
	status := http.StatusOK
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		vr := r.root.viewRenderer
		if vr == nil {
			http.Error(w, "routing: no view renderer wired", http.StatusInternalServerError)
			return
		}
		if err := vr.Render(w, view, d, status, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	route := r.Get(pattern, h)
	route.action["uses"] = "ViewController"
	route.action["controller"] = "ViewController"
	route.defaults["view"] = view
	route.defaults["data"] = d
	route.defaults["status"] = status
	return route
}

// GetRoutes returns the route collection, for URL generation and `aru routes`.
// It is the name Laravel's getRoutes gives the table.
func (r *Router) GetRoutes() *Routes { return r.table }

// Has reports whether a route with the given name is registered.
func (r *Router) Has(name string) bool { return r.table.HasNamedRoute(name) }

// HasMiddlewareGroup reports whether a middleware group with the given name
// exists.
func (r *Router) HasMiddlewareGroup(name string) bool {
	_, ok := r.root.middlewareGroups[name]
	return ok
}

// AliasMiddleware registers a short name for a middleware, for `aru routes`
// and for the kernel's resolution. It is Laravel's aliasMiddleware.
func (r *Router) AliasMiddleware(name string, mw pipeline.Middleware[http.Handler]) *Router {
	r.root.middlewareAliases[name] = mw
	return r
}

// GetMiddleware returns the middleware aliases.
func (r *Router) GetMiddleware() map[string]pipeline.Middleware[http.Handler] {
	out := make(map[string]pipeline.Middleware[http.Handler], len(r.root.middlewareAliases))
	for k, v := range r.root.middlewareAliases {
		out[k] = v
	}
	return out
}

// MiddlewareGroup registers a named bundle of middleware the kernel composes.
func (r *Router) MiddlewareGroup(name string, mws ...pipeline.Middleware[http.Handler]) *Router {
	r.root.middlewareGroups[name] = append(r.root.middlewareGroups[name], mws...)
	return r
}

// GetMiddlewareGroups returns the middleware groups.
func (r *Router) GetMiddlewareGroups() map[string][]pipeline.Middleware[http.Handler] {
	out := make(map[string][]pipeline.Middleware[http.Handler], len(r.root.middlewareGroups))
	for k, v := range r.root.middlewareGroups {
		out[k] = append([]pipeline.Middleware[http.Handler]{}, v...)
	}
	return out
}

// PrependMiddlewareToGroup adds a middleware to the start of a group, if it is
// not already there.
func (r *Router) PrependMiddlewareToGroup(name string, mw pipeline.Middleware[http.Handler]) *Router {
	group := r.root.middlewareGroups[name]
	for _, existing := range group {
		if middlewarePointer(existing) == middlewarePointer(mw) {
			return r
		}
	}
	r.root.middlewareGroups[name] = append([]pipeline.Middleware[http.Handler]{mw}, group...)
	return r
}

// PushMiddlewareToGroup adds a middleware to the end of a group, creating the
// group if it does not exist, if it is not already there.
func (r *Router) PushMiddlewareToGroup(name string, mw pipeline.Middleware[http.Handler]) *Router {
	group := r.root.middlewareGroups[name]
	for _, existing := range group {
		if middlewarePointer(existing) == middlewarePointer(mw) {
			return r
		}
	}
	r.root.middlewareGroups[name] = append(group, mw)
	return r
}

// RemoveMiddlewareFromGroup removes a middleware from a group by identity.
func (r *Router) RemoveMiddlewareFromGroup(name string, mw pipeline.Middleware[http.Handler]) *Router {
	group := r.root.middlewareGroups[name]
	target := middlewarePointer(mw)
	out := group[:0]
	for _, existing := range group {
		if middlewarePointer(existing) == target {
			continue
		}
		out = append(out, existing)
	}
	r.root.middlewareGroups[name] = out
	return r
}

// FlushMiddlewareGroups empties the middleware groups.
func (r *Router) FlushMiddlewareGroups() *Router {
	r.root.middlewareGroups = map[string][]pipeline.Middleware[http.Handler]{}
	return r
}

// Pattern sets a global where pattern, applied to every route at registration.
func (r *Router) Pattern(key, pattern string) {
	r.root.patterns[key] = pattern
}

// Patterns sets several global where patterns at once.
func (r *Router) Patterns(patterns map[string]string) {
	for k, v := range patterns {
		r.root.patterns[k] = v
	}
}

// GetPatterns returns the global where patterns.
func (r *Router) GetPatterns() map[string]string {
	out := make(map[string]string, len(r.root.patterns))
	for k, v := range r.root.patterns {
		out[k] = v
	}
	return out
}

// Matched registers a callback that fires after a route matches, before its
// middleware. It is the RouteMatched listener in Laravel.
func (r *Router) Matched(cb func(*Route, *http.Request)) {
	r.root.matchedCallbacks = append(r.root.matchedCallbacks, cb)
}

// Current returns the route the request matched, read from the request
// context. nil means the request did not go through a registered route.
func (r *Router) Current(req *http.Request) *Route {
	return RouteFromContext(req.Context())
}

// CurrentRouteName returns the name of the current route, or empty.
func (r *Router) CurrentRouteName(req *http.Request) string {
	if rt := r.Current(req); rt != nil {
		return rt.RouteName()
	}
	return ""
}

// CurrentRouteAction returns the action string of the current route, or empty.
func (r *Router) CurrentRouteAction(req *http.Request) string {
	if rt := r.Current(req); rt != nil {
		return rt.GetActionName()
	}
	return ""
}

// Is reports whether the current route's name matches any of the patterns,
// with * as a glob. It is the alias for CurrentRouteNamed.
func (r *Router) Is(req *http.Request, patterns ...string) bool {
	return r.CurrentRouteNamed(req, patterns...)
}

// CurrentRouteNamed reports whether the current route's name matches any of
// the patterns.
func (r *Router) CurrentRouteNamed(req *http.Request, patterns ...string) bool {
	if rt := r.Current(req); rt == nil {
		return false
	} else {
		return rt.Named(patterns...)
	}
}

// Uses reports whether the current route's action matches any of the patterns.
func (r *Router) Uses(req *http.Request, patterns ...string) bool {
	action := r.CurrentRouteAction(req)
	if action == "" {
		return false
	}
	for _, p := range patterns {
		if wildcardMatch(p, action) {
			return true
		}
	}
	return false
}

// CurrentRouteUses reports whether the current route's action equals action.
func (r *Router) CurrentRouteUses(req *http.Request, action string) bool {
	return r.CurrentRouteAction(req) == action
}

// CurrentRouteNamed2 is an alias kept for the surface that reads the verb
// form; CurrentRouteNamed is the canonical one.
func (r *Router) CurrentRouteNamed2(req *http.Request, patterns ...string) bool {
	return r.CurrentRouteNamed(req, patterns...)
}

// Input returns a parameter of the current route, or def. It is Laravel's
// input on the router, which reads the current route's parameter.
func (r *Router) Input(req *http.Request, key string, def ...any) any {
	if rt := r.Current(req); rt != nil {
		return rt.Parameter(req, key, def...)
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

// Bind registers a custom binder for a parameter key. The binder resolves the
// raw path value into the record the handler expects, and it MUST enforce
// authorization and tenant against the Grant in the context (REGRA 17).
func (r *Router) Bind(key string, fn BindingFunc) {
	r.root.binders.Bind(strings.ReplaceAll(key, "-", "_"), fn)
}

// Model registers a binder for a parameter key using fn. It is the semantic
// alias of Bind -- in Laravel, model takes a class and a callback; here the
// callback is the whole binder, because Go has no container to resolve a class
// from a string.
func (r *Router) Model(key string, fn BindingFunc) {
	r.Bind(key, fn)
}

// GetBindingCallback returns the binder for a key, or nil. It is what
// SubstituteBindings reads.
func (r *Router) GetBindingCallback(key string) BindingFunc {
	if r.root.binders == nil || r.root.binders.m == nil {
		return nil
	}
	return r.root.binders.m[strings.ReplaceAll(key, "-", "_")]
}

// SubstituteBindings resolves the explicit binders for the route's parameters,
// installing the resolved values as per-request overrides. A parameter with no
// binder is left as its raw string. The error is what a Missing handler or a
// 404 answers with.
//
// The binders receive the request context, which carries the auth.Grant, and a
// binder that loads a record MUST filter by tenant (REGRA 17): a binder that
// looks up by id without filtering delivers another customer's record through
// the URL, which is the most direct form of the cross-tenant leak.
func (r *Router) SubstituteBindings(rt *Route, req *http.Request) error {
	if rt == nil || req == nil || r.root.binders == nil {
		return nil
	}
	for _, name := range rt.ParameterNames() {
		fn := r.GetBindingCallback(name)
		if fn == nil {
			continue
		}
		val := req.PathValue(name)
		if val == "" {
			continue
		}
		resolved, err := fn(req.Context(), val, req)
		if err != nil {
			return err
		}
		if resolved != nil {
			rt.SetParameter(req, name, resolved)
		}
	}
	return nil
}

// SubstituteImplicitBindings resolves implicit bindings for the route using
// the registered binders. It delegates to SubstituteBindings, which is the
// explicit set; the implicit half -- reflecting a controller's type hints --
// is not in Go, and the contract here is the explicit binder.
func (r *Router) SubstituteImplicitBindings(rt *Route, req *http.Request) error {
	return r.SubstituteBindings(rt, req)
}

// RespondWithRoute dispatches the named route directly, bypassing the mux's
// match. It is what Laravel's respondWithRoute does: bind the request to the
// route and run it. Used by a caller that has a route name and a request that
// did not arrive through the mux.
func (r *Router) RespondWithRoute(req *http.Request, name string, w http.ResponseWriter) error {
	rt := r.table.GetByName(name)
	if rt == nil {
		return errRouteNotFound(name)
	}
	req = rt.Bind(req)
	rt.ToResponse(w, req)
	return nil
}

// Dispatch runs the request through the mux, which is what ServeHTTP does. It
// is here for the surface that reads it as a method; the mux already holds the
// routes, and dispatch is its job.
func (r *Router) Dispatch(w http.ResponseWriter, req *http.Request) {
	r.ServeHTTP(w, req)
}

// DispatchToRoute is an alias for Dispatch; the distinction in Laravel is that
// dispatchToRoute runs only the route match and not the global pipeline, and
// here the global pipeline is the mux, which is already the match.
func (r *Router) DispatchToRoute(w http.ResponseWriter, req *http.Request) {
	r.ServeHTTP(w, req)
}

// SetViewRenderer wires the view renderer the kernel built, so View routes can
// render. Without it, a View route answers 500.
func (r *Router) SetViewRenderer(vr ViewRenderer) {
	r.root.viewRenderer = vr
}

// GetViewRenderer returns the view renderer, or nil.
func (r *Router) GetViewRenderer() ViewRenderer { return r.root.viewRenderer }

// --- Resource delegation ---
//
// These return the PendingResourceRegistration the other fatia
// (routing:URL+bindings) built. They are the Router surface for resource,
// apiResource, singleton and their plural forms. The effective registration of
// the seven routes happens through ResourceRegistrar, which calls the same
// http.Handler path the function Resource[C] (in resource.go) uses -- so there
// is one form of handler, and two ways to reach it: the function, which takes
// a concrete controller and an Adapter; and the registrar, which takes a name
// and a controller string and produces the pending registration. See doc.go
// for the REGRA 9 analysis.

// Resource registers a resource controller and returns the pending
// registration, for fluent configuration before the routes are committed.
func (r *Router) Resource(name, controller string) *PendingResourceRegistration {
	return NewResourceRegistrar(r).Resource(name, controller)
}

// Resources registers several resource controllers at once.
func (r *Router) Resources(resources map[string]string) {
	reg := NewResourceRegistrar(r)
	for name, controller := range resources {
		reg.Resource(name, controller)
	}
}

// ApiResource registers an API resource (index, show, store, update, destroy)
// and returns the pending registration.
func (r *Router) ApiResource(name, controller string) *PendingResourceRegistration {
	pending := NewResourceRegistrar(r).Resource(name, controller)
	pending.Except("create", "edit")
	return pending
}

// ApiResources registers several API resources at once.
func (r *Router) ApiResources(resources map[string]string) {
	for name, controller := range resources {
		r.ApiResource(name, controller)
	}
}

// Singleton registers a singleton resource and returns the pending
// registration.
func (r *Router) Singleton(name, controller string) *PendingSingletonResourceRegistration {
	return NewResourceRegistrar(r).Singleton(name, controller)
}

// Singletons registers several singleton resources at once.
func (r *Router) Singletons(singletons map[string]string) {
	reg := NewResourceRegistrar(r)
	for name, controller := range singletons {
		reg.Singleton(name, controller)
	}
}

// ApiSingleton registers an API singleton (store, show, update, destroy) and
// returns the pending registration.
func (r *Router) ApiSingleton(name, controller string) *PendingSingletonResourceRegistration {
	pending := NewResourceRegistrar(r).Singleton(name, controller)
	pending.Except("edit")
	return pending
}

// ApiSingletons registers several API singletons at once.
func (r *Router) ApiSingletons(singletons map[string]string) {
	for name, controller := range singletons {
		r.ApiSingleton(name, controller)
	}
}

// errRouteNotFound is the error RespondWithRoute returns for an unknown name.
type routeNotFoundError string

func (e routeNotFoundError) Error() string {
	return "routing: no route named " + string(e)
}

func errRouteNotFound(name string) error { return routeNotFoundError(name) }
