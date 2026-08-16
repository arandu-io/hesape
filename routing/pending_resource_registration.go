package routing

import (
	"net/http"

	"github.com/arandu-io/hesape/pipeline"
)

// PendingResourceRegistration holds a resource's registration open while the
// caller configures it. Registration is explicit rather than implicit, so
// Register is the call that commits it:
//
//	r.Resource("invoices", "InvoiceController").Only("index", "show").Register()
type PendingResourceRegistration struct {
	registrar  *ResourceRegistrar
	name       string
	controller string
	options    ResourceOptions
	registered bool
}

// NewPendingResourceRegistration builds a pending registration for name and
// controller.
func NewPendingResourceRegistration(registrar *ResourceRegistrar, name, controller string, options ResourceOptions) *PendingResourceRegistration {
	return &PendingResourceRegistration{
		registrar:  registrar,
		name:       name,
		controller: controller,
		options:    options,
	}
}

// Only limits registration to the named actions.
func (p *PendingResourceRegistration) Only(methods ...string) *PendingResourceRegistration {
	p.options.Only = methods
	return p
}

// Except excludes the named actions from registration.
func (p *PendingResourceRegistration) Except(methods ...string) *PendingResourceRegistration {
	p.options.Except = methods
	return p
}

// Names renames the routes by action. The empty key renames every action at
// once.
func (p *PendingResourceRegistration) Names(names map[string]string) *PendingResourceRegistration {
	p.options.Names = names
	return p
}

// Name renames a single action's route.
func (p *PendingResourceRegistration) Name(method, name string) *PendingResourceRegistration {
	if p.options.Names == nil {
		p.options.Names = map[string]string{}
	}
	p.options.Names[method] = name
	return p
}

// Parameters overrides the route's wildcard names, keyed by resource segment.
func (p *PendingResourceRegistration) Parameters(parameters map[string]string) *PendingResourceRegistration {
	p.options.Parameters = parameters
	return p
}

// Parameter overrides a single wildcard name, previous replaced by
// replacement.
func (p *PendingResourceRegistration) Parameter(previous, replacement string) *PendingResourceRegistration {
	if p.options.Parameters == nil {
		p.options.Parameters = map[string]string{}
	}
	p.options.Parameters[previous] = replacement
	return p
}

// Middleware wraps every route the resource registers.
func (p *PendingResourceRegistration) Middleware(middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	p.options.Middleware = middleware
	for method, existing := range p.options.MiddlewareFor {
		p.options.MiddlewareFor[method] = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, existing...), middleware...))
	}
	return p
}

// MiddlewareFor wraps only the named actions -- the write half of a resource
// behind a confirmation, the read half open.
func (p *PendingResourceRegistration) MiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	if len(p.options.Middleware) > 0 {
		middleware = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, p.options.Middleware...), middleware...))
	}
	if p.options.MiddlewareFor == nil {
		p.options.MiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.MiddlewareFor[method] = middleware
	}
	return p
}

// WithoutMiddleware removes middleware from every route the resource
// registers.
func (p *PendingResourceRegistration) WithoutMiddleware(middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	p.options.ExcludedMiddleware = append(p.options.ExcludedMiddleware, middleware...)
	return p
}

// WithoutMiddlewareFor removes middleware from only the named actions.
func (p *PendingResourceRegistration) WithoutMiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	if p.options.ExcludedMiddlewareFor == nil {
		p.options.ExcludedMiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.ExcludedMiddlewareFor[method] = middleware
	}
	return p
}

// Where sets a regex constraint on the resource's route parameters.
func (p *PendingResourceRegistration) Where(wheres map[string]string) *PendingResourceRegistration {
	p.options.Wheres = wheres
	return p
}

// Shallow nests the resource one level shallower: the routes that act on one
// record drop the parent prefix.
func (p *PendingResourceRegistration) Shallow(shallow ...bool) *PendingResourceRegistration {
	p.options.Shallow = len(shallow) == 0 || shallow[0]
	return p
}

// Missing sets what answers when a binding resolves nothing -- a redirect to
// the index, rather than a 404.
func (p *PendingResourceRegistration) Missing(callback http.Handler) *PendingResourceRegistration {
	p.options.Missing = callback
	return p
}

// Scoped binds the child of a nested resource through its parent, so an id
// belonging to somebody else's parent is a 404 rather than a page.
func (p *PendingResourceRegistration) Scoped(fields ...map[string]string) *PendingResourceRegistration {
	p.options.Scoped = true
	if len(fields) > 0 {
		p.options.BindingFields = fields[0]
		return p
	}
	if p.options.BindingFields == nil {
		p.options.BindingFields = map[string]string{}
	}
	return p
}

// WithTrashed allows soft-deleted records through implicit model binding.
// With no arguments it covers show, edit and update.
func (p *PendingResourceRegistration) WithTrashed(methods ...string) *PendingResourceRegistration {
	p.options.WithTrashed = true
	p.options.Trashed = methods
	return p
}

// Register commits the resource and returns its routes.
func (p *PendingResourceRegistration) Register() *Routes {
	p.registered = true
	return p.registrar.Register(p.name, p.controller, p.options)
}

// PendingSingletonResourceRegistration holds a singleton resource's
// registration open while the caller configures it.
type PendingSingletonResourceRegistration struct {
	registrar  *ResourceRegistrar
	name       string
	controller string
	options    ResourceOptions
	registered bool
}

// NewPendingSingletonResourceRegistration builds a pending registration for
// name and controller.
func NewPendingSingletonResourceRegistration(registrar *ResourceRegistrar, name, controller string, options ResourceOptions) *PendingSingletonResourceRegistration {
	return &PendingSingletonResourceRegistration{
		registrar:  registrar,
		name:       name,
		controller: controller,
		options:    options,
	}
}

// Only limits registration to the named actions.
func (p *PendingSingletonResourceRegistration) Only(methods ...string) *PendingSingletonResourceRegistration {
	p.options.Only = methods
	return p
}

// Except excludes the named actions from registration.
func (p *PendingSingletonResourceRegistration) Except(methods ...string) *PendingSingletonResourceRegistration {
	p.options.Except = methods
	return p
}

// Names renames the routes by action. The empty key renames every action at
// once.
func (p *PendingSingletonResourceRegistration) Names(names map[string]string) *PendingSingletonResourceRegistration {
	p.options.Names = names
	return p
}

// Name renames a single action's route.
func (p *PendingSingletonResourceRegistration) Name(method, name string) *PendingSingletonResourceRegistration {
	if p.options.Names == nil {
		p.options.Names = map[string]string{}
	}
	p.options.Names[method] = name
	return p
}

// Parameters overrides the route's wildcard names, keyed by resource segment.
func (p *PendingSingletonResourceRegistration) Parameters(parameters map[string]string) *PendingSingletonResourceRegistration {
	p.options.Parameters = parameters
	return p
}

// Parameter overrides a single wildcard name, previous replaced by
// replacement.
func (p *PendingSingletonResourceRegistration) Parameter(previous, replacement string) *PendingSingletonResourceRegistration {
	if p.options.Parameters == nil {
		p.options.Parameters = map[string]string{}
	}
	p.options.Parameters[previous] = replacement
	return p
}

// Middleware wraps every route the resource registers.
func (p *PendingSingletonResourceRegistration) Middleware(middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	p.options.Middleware = middleware
	for method, existing := range p.options.MiddlewareFor {
		p.options.MiddlewareFor[method] = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, existing...), middleware...))
	}
	return p
}

// MiddlewareFor wraps only the named actions.
func (p *PendingSingletonResourceRegistration) MiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	if len(p.options.Middleware) > 0 {
		middleware = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, p.options.Middleware...), middleware...))
	}
	if p.options.MiddlewareFor == nil {
		p.options.MiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.MiddlewareFor[method] = middleware
	}
	return p
}

// WithoutMiddleware removes middleware from every route the resource
// registers.
func (p *PendingSingletonResourceRegistration) WithoutMiddleware(middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	p.options.ExcludedMiddleware = append(p.options.ExcludedMiddleware, middleware...)
	return p
}

// WithoutMiddlewareFor removes middleware from only the named actions.
func (p *PendingSingletonResourceRegistration) WithoutMiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	if p.options.ExcludedMiddlewareFor == nil {
		p.options.ExcludedMiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.ExcludedMiddlewareFor[method] = middleware
	}
	return p
}

// Where sets a regex constraint on the resource's route parameters.
func (p *PendingSingletonResourceRegistration) Where(wheres map[string]string) *PendingSingletonResourceRegistration {
	p.options.Wheres = wheres
	return p
}

// Creatable adds create, store and destroy: a singleton that can be brought
// into being.
func (p *PendingSingletonResourceRegistration) Creatable() *PendingSingletonResourceRegistration {
	p.options.Creatable = true
	return p
}

// Destroyable adds the destroy route.
func (p *PendingSingletonResourceRegistration) Destroyable() *PendingSingletonResourceRegistration {
	p.options.Destroyable = true
	return p
}

// Missing sets what answers when a binding resolves nothing.
func (p *PendingSingletonResourceRegistration) Missing(callback http.Handler) *PendingSingletonResourceRegistration {
	p.options.Missing = callback
	return p
}

// WithTrashed allows soft-deleted records through implicit model binding.
func (p *PendingSingletonResourceRegistration) WithTrashed(methods ...string) *PendingSingletonResourceRegistration {
	p.options.WithTrashed = true
	p.options.Trashed = methods
	return p
}

// Register commits the singleton resource and returns its routes.
func (p *PendingSingletonResourceRegistration) Register() *Routes {
	p.registered = true
	return p.registrar.Singleton(p.name, p.controller, p.options)
}
