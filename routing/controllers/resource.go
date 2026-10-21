package controllers

import (
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
)

// The seven actions of a resource controller, one interface each, and the
// single action of an invokable one.
//
// Each is an alias to a generic interface of hesape/routing instantiated with
// hesape/http.Context. Instantiating is what keeps them aliases: a controller
// declaring Index(*hhttp.Context) error satisfies routing.Indexer of that
// context literally, so the type assertion Resource makes finds it, and a
// controller written against either name is one type to the compiler.
type (
	// Indexer answers GET /thing -- the list.
	Indexer = routing.Indexer[hhttp.Context]
	// Creator answers GET /thing/create -- the empty form.
	Creator = routing.Creator[hhttp.Context]
	// Storer answers POST /thing -- the form submission.
	Storer = routing.Storer[hhttp.Context]
	// Shower answers GET /thing/{id} -- one record.
	Shower = routing.Shower[hhttp.Context]
	// Editor answers GET /thing/{id}/edit -- the filled form.
	Editor = routing.Editor[hhttp.Context]
	// Updater answers PUT and PATCH /thing/{id}.
	Updater = routing.Updater[hhttp.Context]
	// Destroyer answers DELETE /thing/{id}.
	Destroyer = routing.Destroyer[hhttp.Context]
	// Invoker is a controller with one action. It answers one route, so the
	// action needs no name beyond Invoke.
	Invoker = routing.Invoker[hhttp.Context]
)

// Resource registers the REST routes a controller implements.
//
//	r.Resource("invoices", InvoiceController{})
//
// The seven, in the conventional order and with the conventional names:
//
//	GET    /invoices             index    invoices.index
//	GET    /invoices/create      create   invoices.create
//	POST   /invoices             store    invoices.store
//	GET    /invoices/{id}        show     invoices.show
//	GET    /invoices/{id}/edit   edit     invoices.edit
//	PUT    /invoices/{id}        update   invoices.update
//	PATCH  /invoices/{id}        update   invoices.update
//	DELETE /invoices/{id}        destroy  invoices.destroy
//
// A dotted name nests the resource under its parents, and the nesting is
// shallow: the list, the form and the store sit under the parent, and the four
// that act on one record at that record's own path. Every route keeps the
// whole dotted name.
//
//	r.Resource("projects.tasks", TaskController{})
//
//	GET    /projects/{project}/tasks          index    projects.tasks.index
//	GET    /projects/{project}/tasks/create   create   projects.tasks.create
//	POST   /projects/{project}/tasks          store    projects.tasks.store
//	GET    /tasks/{task}                      show     projects.tasks.show
//	GET    /tasks/{task}/edit                 edit     projects.tasks.edit
//	PUT    /tasks/{task}                      update   projects.tasks.update
//	PATCH  /tasks/{task}                      update   projects.tasks.update
//	DELETE /tasks/{task}                      destroy  projects.tasks.destroy
//
// The parent's parameter is where the person navigated, never whose data it
// is: the action loads the parent under its Grant and filters the children by
// it. A name without a dot keeps {id}. routing.Resource holds the whole rule.
//
// A controller implementing none of the seven registers nothing and returns
// zero routes, which is a wiring mistake worth seeing in the route listing.
// Each action is answered the way Action answers one.
func (r *Router) Resource(name string, controller any) []*routing.Route {
	return routing.Resource(r.inner, name, controller, r.adapt)
}

// Singleton registers the routes of a resource there is exactly one of where
// it is reached -- the account's settings, a project's billing -- so no route
// carries an id.
//
//	r.Singleton("settings", SettingsController{})
//
//	GET    /settings        show     settings.show
//	GET    /settings/edit   edit     settings.edit
//	PUT    /settings        update   settings.update
//	PATCH  /settings        update   settings.update
//
// It registers whichever of Shower, Editor and Updater the controller
// implements and nothing else, and a controller implementing none of them
// returns zero routes. A dotted name nests it as Resource nests one:
// "projects.billing" answers at /projects/{project}/billing. Each action is
// answered the way Action answers one.
func (r *Router) Singleton(name string, controller any) []*routing.Route {
	return routing.Singleton(r.inner, name, controller, r.adapt)
}

// ResourceAction registers one named action on a record of a resource: a verb
// beyond the seven, such as publish, cancel or approve.
//
//	r.ResourceAction(http.MethodPost, "notes", "publish", notes.Publish)
//
//	POST   /notes/{id}/publish     notes.publish
//
// It sits under the path Resource gives show for the same name and reads the
// same parameter, so every action of one controller finds its record the same
// way. On a nested resource that is the record's own path:
// ResourceAction(http.MethodPost, "projects.tasks", "close", ...) answers
// POST /tasks/{task}/close as projects.tasks.close.
//
// The method comes first, as in Action, and it is POST, PUT, PATCH or DELETE:
// an action changes state, and a GET that changes state is one a prefetching
// browser or a crawler fires without anybody choosing to. Any other method,
// and an empty action, panic at registration. The middleware is the route's
// own and runs after the group's.
func (r *Router) ResourceAction(method, resource, action string, h func(*hhttp.Context) error, mws ...hhttp.Middleware) *routing.Route {
	return routing.ResourceAction(r.inner, resource, action, method, h, r.adapt).Middleware(mws...)
}

// Invokable registers a single-action controller under one method and one
// pattern.
//
//	r.Invokable(http.MethodPost, "/exports", ExportController{}).Name("exports.store")
//
// The method comes first, as in Action. The controller is an Invoker rather
// than an any, so a controller without Invoke fails to compile at this line
// instead of registering nothing. The route is not named; name it with Name,
// as any single route. It is registered on this router, so it carries the
// group's prefix and middleware, and an empty method panics at registration.
func (r *Router) Invokable(method, pattern string, controller Invoker) *routing.Route {
	return routing.Invokable(r.inner, method, pattern, controller, r.adapt)
}
