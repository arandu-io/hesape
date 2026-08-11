package routing

import "strings"

// ResourceRegistrar registers the REST routes of a resource controller on the
// router: index, create, store, show, edit, update and destroy, each at its
// conventional path and with its conventional name.
//
// It mirrors Illuminate\Routing\ResourceRegistrar.
type ResourceRegistrar struct {
	router *Router
	params map[string]string
	verbs  map[string]string
}

// NewResourceRegistrar creates a registrar for r.
func NewResourceRegistrar(r *Router) *ResourceRegistrar {
	return &ResourceRegistrar{
		router: r,
		verbs: map[string]string{
			"create": "create",
			"edit":   "edit",
		},
	}
}

var resourceDefaults = []string{"index", "create", "store", "show", "edit", "update", "destroy"}
var singletonDefaults = []string{"show", "edit", "update"}

// Resource creates a PendingResourceRegistration that registers the routes of a
// conventional REST controller.
func (r *ResourceRegistrar) Resource(name, controller string) *PendingResourceRegistration {
	return &PendingResourceRegistration{
		registrar:  r,
		name:       name,
		controller: controller,
	}
}

// Singleton creates a PendingSingletonResourceRegistration for a singleton
// resource.
func (r *ResourceRegistrar) Singleton(name, controller string) *PendingSingletonResourceRegistration {
	return &PendingSingletonResourceRegistration{
		registrar:  r,
		name:       name,
		controller: controller,
	}
}

// GetResourceURI builds the base URI of a resource from its name.
func (r *ResourceRegistrar) GetResourceURI(name string) string {
	base := r.getResourceWildcard(name)
	return "/" + strings.ReplaceAll(name, ".", "/") + "/{" + base + "}"
}

func (r *ResourceRegistrar) getResourceWildcard(name string) string {
	if r.params != nil {
		if param, ok := r.params[name]; ok {
			return param
		}
	}
	last := name
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		last = name[i+1:]
	}
	return strings.ToLower(last)
}

func (r *ResourceRegistrar) parameterName(name, base string) string {
	if r.params != nil {
		if p, ok := r.params[base]; ok {
			return p
		}
	}
	return base
}

func applyOnlyExcept(defaults []string, opts map[string][]string) []string {
	if only, ok := opts["only"]; ok {
		set := make(map[string]bool, len(only))
		for _, m := range only {
			set[m] = true
		}
		var out []string
		for _, m := range defaults {
			if set[m] {
				out = append(out, m)
			}
		}
		return out
	}
	if except, ok := opts["except"]; ok {
		skip := make(map[string]bool, len(except))
		for _, m := range except {
			skip[m] = true
		}
		var out []string
		for _, m := range defaults {
			if !skip[m] {
				out = append(out, m)
			}
		}
		return out
	}
	return defaults
}

// PendingResourceRegistration holds the deferred registration of a
// resource controller until the caller has finished configuring it.
//
// It mirrors Illuminate\Routing\PendingResourceRegistration.
type PendingResourceRegistration struct {
	registrar  *ResourceRegistrar
	name       string
	controller string
	optMap     map[string][]string
	names      map[string]string
	params     map[string]string
	middleware []string
	shallow    bool
	scoped     bool
}

func (p *PendingResourceRegistration) options() map[string][]string {
	if p.optMap == nil {
		p.optMap = make(map[string][]string)
	}
	return p.optMap
}

// Only restricts the resource to the named methods.
func (p *PendingResourceRegistration) Only(methods ...string) *PendingResourceRegistration {
	setOpt(p.options(), "only", methods)
	return p
}

// Except excludes the named methods from the resource.
func (p *PendingResourceRegistration) Except(methods ...string) *PendingResourceRegistration {
	setOpt(p.options(), "except", methods)
	return p
}

// Names overrides the route names.
func (p *PendingResourceRegistration) Names(n map[string]string) *PendingResourceRegistration {
	p.names = n
	return p
}

// Name sets one route name.
func (p *PendingResourceRegistration) Name(method, name string) *PendingResourceRegistration {
	if p.names == nil {
		p.names = make(map[string]string)
	}
	p.names[method] = name
	return p
}

// Parameters overrides the route parameter names.
func (p *PendingResourceRegistration) Parameters(params map[string]string) *PendingResourceRegistration {
	p.params = params
	return p
}

// Parameter overrides one route parameter name.
func (p *PendingResourceRegistration) Parameter(previous, new string) *PendingResourceRegistration {
	if p.params == nil {
		p.params = make(map[string]string)
	}
	p.params[previous] = new
	return p
}

// Shallow nests the resource one level shallower: routes that act on a single
// record omit the parent prefix.
func (p *PendingResourceRegistration) Shallow(shallow bool) *PendingResourceRegistration {
	p.shallow = shallow
	return p
}

// Scoped scopes the child bindings.
func (p *PendingResourceRegistration) Scoped(scoped bool) *PendingResourceRegistration {
	p.scoped = scoped
	return p
}

// Missing sets a handler for when a model is not found.
func (p *PendingResourceRegistration) Missing() *PendingResourceRegistration { return p }

// WithTrashed includes soft-deleted records.
func (p *PendingResourceRegistration) WithTrashed() *PendingResourceRegistration { return p }

// Creatable adds create and store to the resource.
func (p *PendingResourceRegistration) Creatable() *PendingResourceRegistration { return p }

// Destroyable adds destroy to the resource.
func (p *PendingResourceRegistration) Destroyable() *PendingResourceRegistration { return p }

func setOpt(m map[string][]string, key string, values []string) {
	m[key] = values
}

// PendingSingletonResourceRegistration is the deferred registration of a
// singleton resource.
//
// It mirrors Illuminate\Routing\PendingSingletonResourceRegistration.
type PendingSingletonResourceRegistration struct {
	registrar   *ResourceRegistrar
	name        string
	controller  string
	optMap      map[string][]string
	names       map[string]string
	params      map[string]string
	creatable   bool
	destroyable bool
}

func (p *PendingSingletonResourceRegistration) optionsMap() map[string][]string {
	if p.optMap == nil {
		p.optMap = make(map[string][]string)
	}
	return p.optMap
}

// Only restricts the singleton to the named methods.
func (p *PendingSingletonResourceRegistration) Only(methods ...string) *PendingSingletonResourceRegistration {
	setOpt(p.optionsMap(), "only", methods)
	return p
}

// Except excludes the named methods.
func (p *PendingSingletonResourceRegistration) Except(methods ...string) *PendingSingletonResourceRegistration {
	setOpt(p.optionsMap(), "except", methods)
	return p
}

// Names overrides route names.
func (p *PendingSingletonResourceRegistration) Names(n map[string]string) *PendingSingletonResourceRegistration {
	p.names = n
	return p
}

// Name sets one route name.
func (p *PendingSingletonResourceRegistration) Name(method, name string) *PendingSingletonResourceRegistration {
	if p.names == nil {
		p.names = make(map[string]string)
	}
	p.names[method] = name
	return p
}

// Parameters overrides route parameter names.
func (p *PendingSingletonResourceRegistration) Parameters(params map[string]string) *PendingSingletonResourceRegistration {
	p.params = params
	return p
}

// Parameter overrides one route parameter name.
func (p *PendingSingletonResourceRegistration) Parameter(previous, new string) *PendingSingletonResourceRegistration {
	if p.params == nil {
		p.params = make(map[string]string)
	}
	p.params[previous] = new
	return p
}

// Creatable adds create, store and destroy.
func (p *PendingSingletonResourceRegistration) Creatable() *PendingSingletonResourceRegistration {
	p.creatable = true
	return p
}

// Destroyable adds destroy.
func (p *PendingSingletonResourceRegistration) Destroyable() *PendingSingletonResourceRegistration {
	p.destroyable = true
	return p
}

// Shallow sets shallow nesting.
func (p *PendingSingletonResourceRegistration) Shallow(shallow bool) *PendingSingletonResourceRegistration {
	return p
}

// Scoped scopes child bindings.
func (p *PendingSingletonResourceRegistration) Scoped(scoped bool) *PendingSingletonResourceRegistration {
	return p
}

// Missing sets the missing-model handler.
func (p *PendingSingletonResourceRegistration) Missing() *PendingSingletonResourceRegistration {
	return p
}

// WithTrashed includes soft-deleted records.
func (p *PendingSingletonResourceRegistration) WithTrashed() *PendingSingletonResourceRegistration {
	return p
}
