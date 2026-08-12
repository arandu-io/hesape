package routing

import (
	"net/http"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/str"
)

// ResourceRegistrar registers the REST routes of a resource controller:
// index, create, store, show, edit, update and destroy, each at its
// conventional path and under its conventional name.
//
// It mirrors Illuminate\Routing\ResourceRegistrar.
type ResourceRegistrar struct {
	router *Router

	// parameters is the per-resource parameter map, taken from the options the
	// first time a resource is registered through this registrar.
	parameters map[string]string
	// singular marks the registrar as having been asked for singular
	// parameters, which is PHP's $parameters === 'singular'.
	singular bool
}

// resourceDefaults is ResourceRegistrar::$resourceDefaults.
var resourceDefaults = []string{"index", "create", "store", "show", "edit", "update", "destroy"}

// singletonResourceDefaults is ResourceRegistrar::$singletonResourceDefaults.
var singletonResourceDefaults = []string{"show", "edit", "update"}

// The three pieces of state PHP keeps as class statics. A Go process serves
// many requests at once where PHP serves one, so they are behind a lock: these
// are set at boot and read on every registration, and a test that sets them
// runs beside a test that reads them.
var (
	resourceMu         sync.RWMutex
	parameterMap       = map[string]string{}
	singularParameters = true
	resourceVerbs      = map[string]string{"create": "create", "edit": "edit"}
)

// NewResourceRegistrar is ResourceRegistrar::__construct.
func NewResourceRegistrar(router *Router) *ResourceRegistrar {
	return &ResourceRegistrar{router: router}
}

// ResourceOptions is the options array PHP passes through register.
//
// It is a struct and not a map because every key of it is read by name, and a
// misspelled key in a map is a silently ignored option.
type ResourceOptions struct {
	// Only restricts the resource to these actions.
	Only []string
	// Except removes these actions from the resource.
	Except []string
	// Names overrides route names, keyed by action. The empty key overrides
	// the base name for every action.
	Names map[string]string
	// As prefixes every route name.
	As string
	// Parameters overrides the wildcard names, keyed by resource segment.
	Parameters map[string]string
	// Middleware wraps every route of the resource.
	Middleware []pipeline.Middleware[http.Handler]
	// MiddlewareFor wraps only the named actions.
	MiddlewareFor map[string][]pipeline.Middleware[http.Handler]
	// ExcludedMiddleware is removed from every route of the resource.
	ExcludedMiddleware []pipeline.Middleware[http.Handler]
	// ExcludedMiddlewareFor is removed only from the named actions.
	ExcludedMiddlewareFor map[string][]pipeline.Middleware[http.Handler]
	// Wheres constrains the resource's parameters.
	Wheres map[string]string
	// Missing answers when a binding resolves nothing.
	Missing http.Handler
	// Shallow nests the resource one level shallower: the routes that act on
	// one record drop the parent prefix.
	Shallow bool
	// BindingFields binds the resource's parameters by a column other than the
	// route key, which is what Scoped sets.
	BindingFields map[string]string
	// Scoped marks the resource as scoping its child bindings even when no
	// binding field was named.
	Scoped bool
	// Trashed lists the actions that may resolve soft-deleted records. An
	// empty slice with Trashed set means show, edit and update.
	Trashed []string
	// WithTrashed says Trashed was asked for, so an empty list means the
	// default three rather than none.
	WithTrashed bool
	// Creatable adds create, store and destroy to a singleton.
	Creatable bool
	// Destroyable adds destroy to a singleton.
	Destroyable bool
}

// Register is ResourceRegistrar::register. It registers the resource's routes
// and returns them.
func (r *ResourceRegistrar) Register(name, controller string, options ResourceOptions) *Routes {
	if len(options.Parameters) > 0 && r.parameters == nil {
		r.parameters = options.Parameters
	}

	// A name with a slash in it is a resource the developer wants prefixed, so
	// the prefix is set up here rather than left to them.
	if strings.Contains(name, "/") {
		return r.prefixedResource(name, controller, options)
	}

	segments := strings.Split(name, ".")
	base := r.GetResourceWildcard(segments[len(segments)-1])

	collection := NewRoutes()

	for _, m := range r.getResourceMethods(resourceDefaults, options) {
		route := r.addResourceMethod(m, name, base, controller, r.optionsForMethod(m, options))
		if route == nil {
			continue
		}
		if options.BindingFields != nil {
			r.setResourceBindingFields(route, options.BindingFields)
		}
		if options.WithTrashed && trashedApplies(m, options) {
			route.WithTrashed()
		}
		collection.Add(route)
	}

	return collection
}

// Singleton is ResourceRegistrar::singleton. A singleton resource is one
// record with no id in its path -- a profile, a subscription.
func (r *ResourceRegistrar) Singleton(name, controller string, options ResourceOptions) *Routes {
	if len(options.Parameters) > 0 && r.parameters == nil {
		r.parameters = options.Parameters
	}

	if strings.Contains(name, "/") {
		return r.prefixedSingleton(name, controller, options)
	}

	defaults := singletonResourceDefaults
	switch {
	case options.Creatable:
		defaults = append(append([]string{}, defaults...), "create", "store", "destroy")
	case options.Destroyable:
		defaults = append(append([]string{}, defaults...), "destroy")
	}

	collection := NewRoutes()

	for _, m := range r.getResourceMethods(defaults, options) {
		route := r.addSingletonMethod(m, name, controller, r.optionsForMethod(m, options))
		if route == nil {
			continue
		}
		if options.BindingFields != nil {
			r.setResourceBindingFields(route, options.BindingFields)
		}
		collection.Add(route)
	}

	return collection
}

// optionsForMethod applies the per-action middleware overrides, which is what
// PHP does at the top of its register loop.
func (r *ResourceRegistrar) optionsForMethod(method string, options ResourceOptions) ResourceOptions {
	out := options
	if mws, ok := options.MiddlewareFor[method]; ok {
		out.Middleware = mws
	}
	if mws, ok := options.ExcludedMiddlewareFor[method]; ok {
		out.ExcludedMiddleware = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, options.ExcludedMiddleware...), mws...))
	}
	return out
}

// trashedApplies reports whether an action may resolve soft-deleted records.
// An empty list means the three that act on one record, which is PHP's
// array_intersect with ['show', 'edit', 'update'].
func trashedApplies(method string, options ResourceOptions) bool {
	if len(options.Trashed) == 0 {
		return method == "show" || method == "edit" || method == "update"
	}
	for _, m := range options.Trashed {
		if m == method {
			return true
		}
	}
	return false
}

// prefixedResource is ResourceRegistrar::prefixedResource.
func (r *ResourceRegistrar) prefixedResource(name, controller string, options ResourceOptions) *Routes {
	name, prefix := r.getResourcePrefix(name)
	sub := NewResourceRegistrar(r.router.Group(Group{Prefix: prefix}))
	sub.parameters = r.parameters
	return sub.Register(name, controller, options)
}

// prefixedSingleton is ResourceRegistrar::prefixedSingleton.
func (r *ResourceRegistrar) prefixedSingleton(name, controller string, options ResourceOptions) *Routes {
	name, prefix := r.getResourcePrefix(name)
	sub := NewResourceRegistrar(r.router.Group(Group{Prefix: prefix}))
	sub.parameters = r.parameters
	return sub.Singleton(name, controller, options)
}

// getResourcePrefix is ResourceRegistrar::getResourcePrefix.
func (r *ResourceRegistrar) getResourcePrefix(name string) (string, string) {
	segments := strings.Split(name, "/")
	prefix := strings.Join(segments[:len(segments)-1], "/")
	return segments[len(segments)-1], prefix
}

// getResourceMethods is ResourceRegistrar::getResourceMethods.
func (r *ResourceRegistrar) getResourceMethods(defaults []string, options ResourceOptions) []string {
	methods := defaults

	if options.Only != nil {
		keep := make(map[string]bool, len(options.Only))
		for _, m := range options.Only {
			keep[m] = true
		}
		filtered := make([]string, 0, len(methods))
		for _, m := range methods {
			if keep[m] {
				filtered = append(filtered, m)
			}
		}
		methods = filtered
	}

	if options.Except != nil {
		drop := make(map[string]bool, len(options.Except))
		for _, m := range options.Except {
			drop[m] = true
		}
		filtered := make([]string, 0, len(methods))
		for _, m := range methods {
			if !drop[m] {
				filtered = append(filtered, m)
			}
		}
		methods = filtered
	}

	return methods
}

// addResourceMethod dispatches to the seven adders. PHP builds the method name
// with string concatenation and calls it; Go names them.
func (r *ResourceRegistrar) addResourceMethod(method, name, base, controller string, options ResourceOptions) *Route {
	switch method {
	case "index":
		return r.addResourceIndex(name, base, controller, options)
	case "create":
		return r.addResourceCreate(name, base, controller, options)
	case "store":
		return r.addResourceStore(name, base, controller, options)
	case "show":
		return r.addResourceShow(name, base, controller, options)
	case "edit":
		return r.addResourceEdit(name, base, controller, options)
	case "update":
		return r.addResourceUpdate(name, base, controller, options)
	case "destroy":
		return r.addResourceDestroy(name, base, controller, options)
	default:
		return nil
	}
}

// addResourceIndex is ResourceRegistrar::addResourceIndex.
func (r *ResourceRegistrar) addResourceIndex(name, base, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name)
	options.Missing = nil
	return r.register(http.MethodGet, uri, name, controller, "index", options)
}

// addResourceCreate is ResourceRegistrar::addResourceCreate.
func (r *ResourceRegistrar) addResourceCreate(name, base, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name) + "/" + Verbs()["create"]
	options.Missing = nil
	return r.register(http.MethodGet, uri, name, controller, "create", options)
}

// addResourceStore is ResourceRegistrar::addResourceStore.
func (r *ResourceRegistrar) addResourceStore(name, base, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name)
	options.Missing = nil
	return r.register(http.MethodPost, uri, name, controller, "store", options)
}

// addResourceShow is ResourceRegistrar::addResourceShow.
func (r *ResourceRegistrar) addResourceShow(name, base, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name) + "/{" + base + "}"
	return r.register(http.MethodGet, uri, name, controller, "show", options)
}

// addResourceEdit is ResourceRegistrar::addResourceEdit.
func (r *ResourceRegistrar) addResourceEdit(name, base, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name) + "/{" + base + "}/" + Verbs()["edit"]
	return r.register(http.MethodGet, uri, name, controller, "edit", options)
}

// addResourceUpdate is ResourceRegistrar::addResourceUpdate. It answers PUT
// and PATCH, as PHP does, from one registration.
func (r *ResourceRegistrar) addResourceUpdate(name, base, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name) + "/{" + base + "}"
	return r.registerMatch([]string{http.MethodPut, http.MethodPatch}, uri, name, controller, "update", options)
}

// addResourceDestroy is ResourceRegistrar::addResourceDestroy.
func (r *ResourceRegistrar) addResourceDestroy(name, base, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name) + "/{" + base + "}"
	return r.register(http.MethodDelete, uri, name, controller, "destroy", options)
}

// addSingletonMethod dispatches to the singleton adders.
func (r *ResourceRegistrar) addSingletonMethod(method, name, controller string, options ResourceOptions) *Route {
	switch method {
	case "create":
		return r.addSingletonCreate(name, controller, options)
	case "store":
		return r.addSingletonStore(name, controller, options)
	case "show":
		return r.addSingletonShow(name, controller, options)
	case "edit":
		return r.addSingletonEdit(name, controller, options)
	case "update":
		return r.addSingletonUpdate(name, controller, options)
	case "destroy":
		return r.addSingletonDestroy(name, controller, options)
	default:
		return nil
	}
}

// addSingletonCreate is ResourceRegistrar::addSingletonCreate.
func (r *ResourceRegistrar) addSingletonCreate(name, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name) + "/" + Verbs()["create"]
	options.Missing = nil
	return r.register(http.MethodGet, uri, name, controller, "create", options)
}

// addSingletonStore is ResourceRegistrar::addSingletonStore.
func (r *ResourceRegistrar) addSingletonStore(name, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name)
	options.Missing = nil
	return r.register(http.MethodPost, uri, name, controller, "store", options)
}

// addSingletonShow is ResourceRegistrar::addSingletonShow.
func (r *ResourceRegistrar) addSingletonShow(name, controller string, options ResourceOptions) *Route {
	uri := r.GetResourceUri(name)
	options.Missing = nil
	return r.register(http.MethodGet, uri, name, controller, "show", options)
}

// addSingletonEdit is ResourceRegistrar::addSingletonEdit.
func (r *ResourceRegistrar) addSingletonEdit(name, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name) + "/" + Verbs()["edit"]
	return r.register(http.MethodGet, uri, name, controller, "edit", options)
}

// addSingletonUpdate is ResourceRegistrar::addSingletonUpdate.
func (r *ResourceRegistrar) addSingletonUpdate(name, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name)
	return r.registerMatch([]string{http.MethodPut, http.MethodPatch}, uri, name, controller, "update", options)
}

// addSingletonDestroy is ResourceRegistrar::addSingletonDestroy.
func (r *ResourceRegistrar) addSingletonDestroy(name, controller string, options ResourceOptions) *Route {
	name = r.getShallowName(name, options)
	uri := r.GetResourceUri(name)
	return r.register(http.MethodDelete, uri, name, controller, "destroy", options)
}

// getShallowName is ResourceRegistrar::getShallowName.
func (r *ResourceRegistrar) getShallowName(name string, options ResourceOptions) string {
	if !options.Shallow {
		return name
	}
	segments := strings.Split(name, ".")
	return segments[len(segments)-1]
}

// setResourceBindingFields is ResourceRegistrar::setResourceBindingFields.
func (r *ResourceRegistrar) setResourceBindingFields(route *Route, bindingFields map[string]string) {
	fields := map[string]string{}
	for _, name := range route.ParameterNames() {
		fields[name] = bindingFields[name]
	}
	route.SetBindingFields(fields)
}

// GetResourceUri is ResourceRegistrar::getResourceUri. A nested name becomes
// the parent's path with the parent's wildcard in it, and the resource's own
// wildcard removed -- the adders put it back where they need it.
func (r *ResourceRegistrar) GetResourceUri(resource string) string {
	if !strings.Contains(resource, ".") {
		return resource
	}

	segments := strings.Split(resource, ".")
	uri := r.getNestedResourceUri(segments)

	return strings.ReplaceAll(uri, "/{"+r.GetResourceWildcard(segments[len(segments)-1])+"}", "")
}

// getNestedResourceUri is ResourceRegistrar::getNestedResourceUri.
func (r *ResourceRegistrar) getNestedResourceUri(segments []string) string {
	out := make([]string, 0, len(segments))
	for _, s := range segments {
		out = append(out, s+"/{"+r.GetResourceWildcard(s)+"}")
	}
	return strings.Join(out, "/")
}

// GetResourceWildcard is ResourceRegistrar::getResourceWildcard. It turns a
// resource segment into the parameter name that stands for one of them:
// "invoices" becomes "invoice", and a dash becomes an underscore because that
// is what a path parameter may carry.
func (r *ResourceRegistrar) GetResourceWildcard(value string) string {
	resourceMu.RLock()
	mapped, global := parameterMap[value]
	singularGlobal := singularParameters
	resourceMu.RUnlock()

	switch {
	case r.parameters != nil && r.parameters[value] != "":
		value = r.parameters[value]
	case global:
		value = mapped
	case r.singular || singularGlobal:
		value = str.Singular(value)
	}

	return strings.ReplaceAll(value, "-", "_")
}

// getResourceRouteName is ResourceRegistrar::getResourceRouteName.
func (r *ResourceRegistrar) getResourceRouteName(resource, method string, options ResourceOptions) string {
	name := resource

	if options.Names != nil {
		if base, ok := options.Names[""]; ok && base != "" {
			name = base
		}
		if specific, ok := options.Names[method]; ok && specific != "" {
			return specific
		}
	}

	prefix := ""
	if options.As != "" {
		prefix = options.As + "."
	}

	return strings.Trim(prefix+name+"."+method, ".")
}

// register creates one route of the resource and applies the options that
// getResourceAction carries in PHP: the name, the action string, the
// middleware, the where constraints and the missing-model handler.
func (r *ResourceRegistrar) register(method, uri, name, controller, action string, options ResourceOptions) *Route {
	return r.finish(r.router.handle(method, uri, r.handlerFor(controller, action), options.Middleware...), name, controller, action, options)
}

// registerMatch is register for the update route, which answers two verbs.
func (r *ResourceRegistrar) registerMatch(methods []string, uri, name, controller, action string, options ResourceOptions) *Route {
	return r.finish(r.router.Match(methods, uri, r.handlerFor(controller, action), options.Middleware...), name, controller, action, options)
}

// finish applies the action's options to a route just registered.
func (r *ResourceRegistrar) finish(route *Route, name, controller, action string, options ResourceOptions) *Route {
	route.Uses(controller + "@" + action)
	route.SetControllerMethod(controller, action)
	route.Name(r.getResourceRouteName(name, action, options))

	if len(options.ExcludedMiddleware) > 0 {
		route.WithoutMiddleware(options.ExcludedMiddleware...)
	}
	if len(options.Wheres) > 0 {
		route.WhereMap(options.Wheres)
	}
	if options.Missing != nil {
		route.Missing(options.Missing)
	}
	if options.Scoped {
		route.ScopeBindings()
	}
	return route
}

// handlerFor turns a controller and an action into the handler the mux calls,
// through the dispatcher the kernel wired.
//
// Without a dispatcher the route still exists and answers 500 saying so, which
// is the same shape a View route has without a renderer: a route that is in
// the table and does not answer is easier to find than a route that was never
// registered.
func (r *ResourceRegistrar) handlerFor(controller, action string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		dispatcher := r.router.GetControllerDispatcher()
		if dispatcher == nil {
			http.Error(w, "routing: no controller dispatcher wired for "+controller+"@"+action, http.StatusInternalServerError)
			return
		}
		handler := dispatcher.Dispatch(RouteFromContext(req.Context()), controller, action)
		if handler == nil {
			http.Error(w, "routing: the controller dispatcher has no answer for "+controller+"@"+action, http.StatusInternalServerError)
			return
		}
		handler.ServeHTTP(w, req)
	})
}

// SingularParameters is ResourceRegistrar::singularParameters. It sets whether
// an unmapped resource segment is singularised into its wildcard.
func SingularParameters(singular ...bool) {
	value := true
	if len(singular) > 0 {
		value = singular[0]
	}
	resourceMu.Lock()
	singularParameters = value
	resourceMu.Unlock()
}

// GetParameters is ResourceRegistrar::getParameters. It returns the global
// wildcard map.
func GetParameters() map[string]string {
	resourceMu.RLock()
	defer resourceMu.RUnlock()
	out := make(map[string]string, len(parameterMap))
	for k, v := range parameterMap {
		out[k] = v
	}
	return out
}

// SetParameters is ResourceRegistrar::setParameters. It replaces the global
// wildcard map.
func SetParameters(parameters map[string]string) {
	resourceMu.Lock()
	defer resourceMu.Unlock()
	parameterMap = map[string]string{}
	for k, v := range parameters {
		parameterMap[k] = v
	}
}

// Verbs is ResourceRegistrar::verbs. Called with nothing it returns the verbs
// used in resource URIs; called with a map it merges them, which is how
// "create" becomes "criar" for an application in another language.
func Verbs(verbs ...map[string]string) map[string]string {
	if len(verbs) == 0 || len(verbs[0]) == 0 {
		resourceMu.RLock()
		defer resourceMu.RUnlock()
		out := make(map[string]string, len(resourceVerbs))
		for k, v := range resourceVerbs {
			out[k] = v
		}
		return out
	}

	resourceMu.Lock()
	defer resourceMu.Unlock()
	for k, v := range verbs[0] {
		resourceVerbs[k] = v
	}
	out := make(map[string]string, len(resourceVerbs))
	for k, v := range resourceVerbs {
		out[k] = v
	}
	return out
}
