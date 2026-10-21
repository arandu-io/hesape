package controllers

import (
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/session"
)

// Router registers routes and the controller actions that answer them.
//
// It is a shell over hesape/routing.Router, which since Go 1.22 rests on
// http.ServeMux for methods and path parameters. What it adds is what a
// controller action needs and that router deliberately does not hold: the view
// renderer and the flash, and the adapter built from them. See the package
// documentation.
//
// It stores no routes of its own. Every registration goes straight through to
// the hesape/routing router, whose table is shared by every sub-router, so a
// route registered through a group is in the table the root reads.
type Router struct {
	inner  *routing.Router
	table  *Routes
	render hhttp.Renderer
	flash  *session.Flash
}

// NewRouter returns an empty router, with no renderer and no flash.
func NewRouter() *Router {
	inner := routing.NewRouter()
	return &Router{inner: inner, table: &Routes{inner: inner.Table()}}
}

// WithRenderer returns a router whose actions can render views.
//
// The kernel calls it at boot with the view module's renderer. Without it,
// Context.View returns an error naming the missing renderer rather than
// panicking.
func (r *Router) WithRenderer(rd hhttp.Renderer) *Router {
	g := *r
	g.render = rd
	return &g
}

// WithFlash returns a router whose actions can answer a rejected request.
//
// The kernel calls it at boot, the way it calls WithRenderer, so no
// application wires it and none can forget to. Without it an action that
// returns validation.Errors to a page reaches the panic path, which is the
// honest answer: a rejection that cannot be flashed is a rejection nobody will
// see. A request that wants JSON does not need it, because its messages travel
// in the body of the answer rather than to the next page.
func (r *Router) WithFlash(f *session.Flash) *Router {
	g := *r
	g.flash = f
	return &g
}

// Flash returns the flash this router was wired with, or nil when it was
// wired with none.
//
// A module that writes flash messages of its own -- a notice after a form
// succeeded -- takes this one rather than building another with
// session.NewFlash. The router's flash is the one the application reads back
// on the next page, and its Secure attribute is the one the session cookie was
// given; a module that builds its own repeats that decision and can make it
// differently. nil means the router came from NewRouter with no WithFlash, as
// in a test.
//
// Group, ForModule, WithRenderer and every other method that returns a
// sub-router carry the flash over, so the answer is the same at any depth.
func (r *Router) Flash() *session.Flash { return r.flash }

// Group returns a sub-router with the prefix appended and the middleware
// inherited. The route table is shared with the parent.
//
// It takes the prefix and the middleware, and names nothing: a route is named
// with Route.Name, and a second way to name it here would be a second rule
// for what a route is called.
func (r *Router) Group(prefix string, mws ...hhttp.Middleware) *Router {
	g := *r
	g.inner = r.inner.Group(routing.Group{Prefix: prefix, Middleware: mws})
	return &g
}

// ForModule returns a sub-router that tags its routes with the module name,
// so the route listing can group them. The kernel calls it for each module.
func (r *Router) ForModule(name string) *Router {
	g := *r
	g.inner = r.inner.ForModule(name)
	return &g
}

// Routing returns the hesape/routing router underneath, scoped exactly as this
// one is: the same prefix, middleware and module.
//
// It is for a module whose routes are declared against that router -- a
// handler-only module from the component collection -- so the kernel can hand
// it the same table without a second router. A route registered on it is in
// this router's table; a controller action registered on it has to be adapted
// by hand, which is why an application registers on this router instead.
func (r *Router) Routing() *routing.Router { return r.inner }

// Get registers a GET route.
//
//	r.Get("/health", health).Name("health")
func (r *Router) Get(pattern string, h http.HandlerFunc, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Get(pattern, h, mws...)
}

// Post registers a POST route.
func (r *Router) Post(pattern string, h http.HandlerFunc, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Post(pattern, h, mws...)
}

// Put registers a PUT route.
func (r *Router) Put(pattern string, h http.HandlerFunc, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Put(pattern, h, mws...)
}

// Patch registers a PATCH route.
func (r *Router) Patch(pattern string, h http.HandlerFunc, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Patch(pattern, h, mws...)
}

// Delete registers a DELETE route.
func (r *Router) Delete(pattern string, h http.HandlerFunc, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Delete(pattern, h, mws...)
}

// Action registers one controller action, for a route outside a resource.
//
//	r.Action(http.MethodGet, "/dashboard", dashboard.Index).Name("dashboard")
//
// It is not a second way to route: the registration is the one every verb
// method goes through, and what this adds is the handler, built from the
// action by this router's adapter -- see hesape/http.NewAdapter for how each
// error an action returns is answered.
func (r *Router) Action(method, pattern string, h func(*hhttp.Context) error, mws ...hhttp.Middleware) *routing.Route {
	return r.inner.Match([]string{method}, pattern, r.adapt(h), mws...)
}

// Table returns the route table, for URL generation and for the route
// listing.
func (r *Router) Table() *Routes { return r.table }

// Routes returns the registered routes, in registration order.
func (r *Router) Routes() []*routing.Route { return r.inner.Routes() }

// ServeHTTP dispatches to the underlying router.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.inner.ServeHTTP(w, req) }

// adapt turns a controller action into the handler the router dispatches to.
//
// It is a method because the renderer, the route table and the flash it
// closes over reach it from the router; the method value r.adapt is the
// one-argument function routing.Adapter asks for.
func (r *Router) adapt(h func(*hhttp.Context) error) http.Handler {
	return hhttp.NewAdapter(r.render, r.table.inner, r.flash)(h)
}

// Routes is the table of registered routes, and the index by name.
//
// It forwards to hesape/routing.Routes and holds nothing. The method that
// builds a path from a name is URL here, as a controller and a view have
// always called it.
type Routes struct{ inner *routing.Routes }

// URL builds the path of a named route, filling the parameters in order.
//
//	URL("home")                  -> "/"
//	URL("invoices.show", "42")   -> "/invoices/42"
//
// A hardcoded "/invoices/"+id compiles and keeps compiling after the route
// moves. This does not: an unknown name or a wrong number of parameters is an
// error the caller sees, not a 404 the user sees.
func (t *Routes) URL(name string, params ...string) (string, error) {
	return t.inner.Route(name, params...)
}

// Must is URL for the places that cannot handle an error -- a template
// helper, mostly. It returns the message as the href, so a broken link says
// what is wrong instead of pointing at "/".
func (t *Routes) Must(name string, params ...string) string {
	return t.inner.Must(name, params...)
}

// All returns the routes in registration order.
func (t *Routes) All() []*routing.Route { return t.inner.All() }
