package routing

import (
	"net/http"

	"github.com/arandu-io/hesape/pipeline"
)

// PendingResourceRegistration holds a resource's registration open while the
// caller configures it.
//
// It mirrors Illuminate\Routing\PendingResourceRegistration. PHP registers on
// destruction, so a resource nobody configured still lands in the table; Go
// has no destructor, so Register is the call that commits it:
//
//	r.Resource("invoices", "InvoiceController").Only("index", "show").Register()
//
// See doc.go, which records __destruct as the one thing here that has no
// counterpart and why.
type PendingResourceRegistration struct {
	registrar  *ResourceRegistrar
	name       string
	controller string
	options    ResourceOptions
	registered bool
}

// NewPendingResourceRegistration is PendingResourceRegistration::__construct.
func NewPendingResourceRegistration(registrar *ResourceRegistrar, name, controller string, options ResourceOptions) *PendingResourceRegistration {
	return &PendingResourceRegistration{
		registrar:  registrar,
		name:       name,
		controller: controller,
		options:    options,
	}
}

// Only is PendingResourceRegistration::only.
func (p *PendingResourceRegistration) Only(methods ...string) *PendingResourceRegistration {
	p.options.Only = methods
	return p
}

// Except is PendingResourceRegistration::except.
func (p *PendingResourceRegistration) Except(methods ...string) *PendingResourceRegistration {
	p.options.Except = methods
	return p
}

// Names is PendingResourceRegistration::names. The empty key renames every
// action at once, which is PHP's string form of the same argument.
func (p *PendingResourceRegistration) Names(names map[string]string) *PendingResourceRegistration {
	p.options.Names = names
	return p
}

// Name is PendingResourceRegistration::name.
func (p *PendingResourceRegistration) Name(method, name string) *PendingResourceRegistration {
	if p.options.Names == nil {
		p.options.Names = map[string]string{}
	}
	p.options.Names[method] = name
	return p
}

// Parameters is PendingResourceRegistration::parameters.
func (p *PendingResourceRegistration) Parameters(parameters map[string]string) *PendingResourceRegistration {
	p.options.Parameters = parameters
	return p
}

// Parameter is PendingResourceRegistration::parameter.
func (p *PendingResourceRegistration) Parameter(previous, replacement string) *PendingResourceRegistration {
	if p.options.Parameters == nil {
		p.options.Parameters = map[string]string{}
	}
	p.options.Parameters[previous] = replacement
	return p
}

// Middleware is PendingResourceRegistration::middleware.
func (p *PendingResourceRegistration) Middleware(middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	p.options.Middleware = middleware
	for method, existing := range p.options.MiddlewareFor {
		p.options.MiddlewareFor[method] = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, existing...), middleware...))
	}
	return p
}

// MiddlewareFor is PendingResourceRegistration::middlewareFor. It wraps only
// the named actions -- the write half of a resource behind a confirmation, the
// read half open.
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

// WithoutMiddleware is PendingResourceRegistration::withoutMiddleware.
func (p *PendingResourceRegistration) WithoutMiddleware(middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	p.options.ExcludedMiddleware = append(p.options.ExcludedMiddleware, middleware...)
	return p
}

// WithoutMiddlewareFor is PendingResourceRegistration::withoutMiddlewareFor.
func (p *PendingResourceRegistration) WithoutMiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingResourceRegistration {
	if p.options.ExcludedMiddlewareFor == nil {
		p.options.ExcludedMiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.ExcludedMiddlewareFor[method] = middleware
	}
	return p
}

// Where is PendingResourceRegistration::where.
func (p *PendingResourceRegistration) Where(wheres map[string]string) *PendingResourceRegistration {
	p.options.Wheres = wheres
	return p
}

// Shallow is PendingResourceRegistration::shallow.
func (p *PendingResourceRegistration) Shallow(shallow ...bool) *PendingResourceRegistration {
	p.options.Shallow = len(shallow) == 0 || shallow[0]
	return p
}

// Missing is PendingResourceRegistration::missing. It sets what answers when a
// binding resolves nothing -- a redirect to the index, rather than a 404.
func (p *PendingResourceRegistration) Missing(callback http.Handler) *PendingResourceRegistration {
	p.options.Missing = callback
	return p
}

// Scoped is PendingResourceRegistration::scoped. It binds the child of a
// nested resource through its parent, so an id belonging to somebody else's
// parent is a 404 rather than a page.
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

// WithTrashed is PendingResourceRegistration::withTrashed. With no arguments
// it covers show, edit and update.
func (p *PendingResourceRegistration) WithTrashed(methods ...string) *PendingResourceRegistration {
	p.options.WithTrashed = true
	p.options.Trashed = methods
	return p
}

// Register is PendingResourceRegistration::register. It commits the resource
// and returns its routes.
func (p *PendingResourceRegistration) Register() *Routes {
	p.registered = true
	return p.registrar.Register(p.name, p.controller, p.options)
}

// PendingSingletonResourceRegistration holds a singleton resource's
// registration open while the caller configures it.
//
// It mirrors Illuminate\Routing\PendingSingletonResourceRegistration.
type PendingSingletonResourceRegistration struct {
	registrar  *ResourceRegistrar
	name       string
	controller string
	options    ResourceOptions
	registered bool
}

// NewPendingSingletonResourceRegistration is
// PendingSingletonResourceRegistration::__construct.
func NewPendingSingletonResourceRegistration(registrar *ResourceRegistrar, name, controller string, options ResourceOptions) *PendingSingletonResourceRegistration {
	return &PendingSingletonResourceRegistration{
		registrar:  registrar,
		name:       name,
		controller: controller,
		options:    options,
	}
}

// Only is PendingSingletonResourceRegistration::only.
func (p *PendingSingletonResourceRegistration) Only(methods ...string) *PendingSingletonResourceRegistration {
	p.options.Only = methods
	return p
}

// Except is PendingSingletonResourceRegistration::except.
func (p *PendingSingletonResourceRegistration) Except(methods ...string) *PendingSingletonResourceRegistration {
	p.options.Except = methods
	return p
}

// Names is PendingSingletonResourceRegistration::names.
func (p *PendingSingletonResourceRegistration) Names(names map[string]string) *PendingSingletonResourceRegistration {
	p.options.Names = names
	return p
}

// Name is PendingSingletonResourceRegistration::name.
func (p *PendingSingletonResourceRegistration) Name(method, name string) *PendingSingletonResourceRegistration {
	if p.options.Names == nil {
		p.options.Names = map[string]string{}
	}
	p.options.Names[method] = name
	return p
}

// Parameters is PendingSingletonResourceRegistration::parameters.
func (p *PendingSingletonResourceRegistration) Parameters(parameters map[string]string) *PendingSingletonResourceRegistration {
	p.options.Parameters = parameters
	return p
}

// Parameter is PendingSingletonResourceRegistration::parameter.
func (p *PendingSingletonResourceRegistration) Parameter(previous, replacement string) *PendingSingletonResourceRegistration {
	if p.options.Parameters == nil {
		p.options.Parameters = map[string]string{}
	}
	p.options.Parameters[previous] = replacement
	return p
}

// Middleware is PendingSingletonResourceRegistration::middleware.
func (p *PendingSingletonResourceRegistration) Middleware(middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	p.options.Middleware = middleware
	for method, existing := range p.options.MiddlewareFor {
		p.options.MiddlewareFor[method] = UniqueMiddleware(append(append([]pipeline.Middleware[http.Handler]{}, existing...), middleware...))
	}
	return p
}

// MiddlewareFor is PendingSingletonResourceRegistration::middlewareFor.
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

// WithoutMiddleware is PendingSingletonResourceRegistration::withoutMiddleware.
func (p *PendingSingletonResourceRegistration) WithoutMiddleware(middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	p.options.ExcludedMiddleware = append(p.options.ExcludedMiddleware, middleware...)
	return p
}

// WithoutMiddlewareFor is
// PendingSingletonResourceRegistration::withoutMiddlewareFor.
func (p *PendingSingletonResourceRegistration) WithoutMiddlewareFor(methods []string, middleware ...pipeline.Middleware[http.Handler]) *PendingSingletonResourceRegistration {
	if p.options.ExcludedMiddlewareFor == nil {
		p.options.ExcludedMiddlewareFor = map[string][]pipeline.Middleware[http.Handler]{}
	}
	for _, method := range methods {
		p.options.ExcludedMiddlewareFor[method] = middleware
	}
	return p
}

// Where is PendingSingletonResourceRegistration::where.
func (p *PendingSingletonResourceRegistration) Where(wheres map[string]string) *PendingSingletonResourceRegistration {
	p.options.Wheres = wheres
	return p
}

// Creatable is PendingSingletonResourceRegistration::creatable. It adds
// create, store and destroy: a singleton that can be brought into being.
func (p *PendingSingletonResourceRegistration) Creatable() *PendingSingletonResourceRegistration {
	p.options.Creatable = true
	return p
}

// Destroyable is PendingSingletonResourceRegistration::destroyable.
func (p *PendingSingletonResourceRegistration) Destroyable() *PendingSingletonResourceRegistration {
	p.options.Destroyable = true
	return p
}

// Missing is PendingSingletonResourceRegistration::missing.
func (p *PendingSingletonResourceRegistration) Missing(callback http.Handler) *PendingSingletonResourceRegistration {
	p.options.Missing = callback
	return p
}

// WithTrashed is PendingSingletonResourceRegistration::withTrashed.
func (p *PendingSingletonResourceRegistration) WithTrashed(methods ...string) *PendingSingletonResourceRegistration {
	p.options.WithTrashed = true
	p.options.Trashed = methods
	return p
}

// Register is PendingSingletonResourceRegistration::register.
func (p *PendingSingletonResourceRegistration) Register() *Routes {
	p.registered = true
	return p.registrar.Singleton(p.name, p.controller, p.options)
}
