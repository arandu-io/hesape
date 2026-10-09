package routing

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/arandu-io/hesape/str"
)

// Adapter turns one controller action into an http.Handler.
//
// C is the request context a controller action receives, and it is a type
// parameter rather than a named type because that type lives above this
// package: hesape/http owns the request context, the renderer and the answer
// to a rejected form, and a router that imported it would be a router that had
// to know what an answer looks like in order to match a path.
//
// The layer that owns C supplies the adapter, and the call reads:
//
//	routing.Resource(r, "invoices", InvoiceController{}, adapt)
//
// where adapt is that layer's own function. It is the same one a single route
// already goes through -- r.Get("/x", adapt(c.Show)) -- passed by name instead
// of applied, so Resource can apply it to the seven actions it finds.
type Adapter[C any] func(func(*C) error) http.Handler

// The seven actions of a resource controller, one interface each.
//
// The usual shape of this takes one object and registers seven routes whether
// or not the methods exist -- a request to a missing one is a runtime error.
// Here each action is its own tiny interface, and Resource registers exactly
// the ones the controller implements. A route that exists is a route that
// answers.
//
// Seven interfaces rather than one with seven methods, for the same reason the
// kernel splits Bootable, Background and Closable: a controller that only lists
// and shows should not be forced to write five empty methods, and five empty
// methods is how a 500 gets committed.
type (
	// Indexer answers GET /thing -- the list.
	Indexer[C any] interface {
		Index(*C) error
	}
	// Creator answers GET /thing/create -- the empty form.
	Creator[C any] interface {
		Create(*C) error
	}
	// Storer answers POST /thing -- the form submission.
	Storer[C any] interface {
		Store(*C) error
	}
	// Shower answers GET /thing/{id} -- one record.
	Shower[C any] interface {
		Show(*C) error
	}
	// Editor answers GET /thing/{id}/edit -- the filled form.
	Editor[C any] interface {
		Edit(*C) error
	}
	// Updater answers PUT and PATCH /thing/{id}.
	Updater[C any] interface {
		Update(*C) error
	}
	// Destroyer answers DELETE /thing/{id}.
	Destroyer[C any] interface {
		Destroy(*C) error
	}
)

// Resource registers the REST routes a controller implements.
//
//	routing.Resource(r, "invoices", InvoiceController{}, adapt)
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
// The order matters: /invoices/create is registered before /invoices/{id} so a
// GET of "create" reaches the form rather than being read as an id. Go's
// ServeMux prefers the more specific pattern, and registering in this order
// keeps the intent readable even where the mux would sort it out anyway.
//
// A controller implementing none of the seven registers nothing and returns
// zero routes, which is a wiring mistake worth seeing in the route table.
//
// It is a function and not a method on Router because a method cannot take a
// type parameter in Go, and C has to come from somewhere. The router is the
// first argument, so the line still reads left to right as "register, on this
// router, this resource".
//
// # Nested by a dot
//
// A dotted name nests the resource under its parents, and the nesting is
// shallow: the three routes that act on the collection sit under the parent,
// and the four that act on one record sit at that record's own path.
//
//	routing.Resource(r, "projects.tasks", TaskController{}, adapt)
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
// A deep path to one record carries two ids that can disagree --
// /projects/1/tasks/9 where task 9 belongs to project 2 -- and every action
// would have to check that they agree. The shallow path carries only the id
// that decides. Every route keeps the whole dotted name, so the seven read as
// one resource in the route table. More dots nest further, and only the
// collection path grows: "organizations.projects.tasks" lists at
// /organizations/{organization}/projects/{project}/tasks and shows at
// /tasks/{task}.
//
// Each parameter of a nested resource is the singular of its segment, with
// dashes written as underscores -- {project}, {task}, {purchase_order} -- which
// is also the key Router.Model binds, so a record type registered for "task"
// resolves the member routes. A single segment keeps {id}, which is what it
// has always registered and what the controllers written against it read.
//
// The parent's parameter says where the person navigated, not whose data it
// is, and nothing here reads a tenant from the path. The action loads the
// parent under its Grant and filters the children by it, so a project id that
// belongs to another tenant finds nothing.
//
// An empty segment, as in "projects..tasks", panics at registration.
func Resource[C any](r *Router, name string, controller any, adapt Adapter[C]) []*Route {
	at := resourcePathsOf(name)

	var out []*Route
	add := func(route *Route) { out = append(out, route) }

	if c, ok := controller.(Indexer[C]); ok {
		add(registerAction(r, http.MethodGet, at.collection, at.name+".index", c.Index, adapt))
	}
	if c, ok := controller.(Creator[C]); ok {
		add(registerAction(r, http.MethodGet, at.collection+"/create", at.name+".create", c.Create, adapt))
	}
	if c, ok := controller.(Storer[C]); ok {
		add(registerAction(r, http.MethodPost, at.collection, at.name+".store", c.Store, adapt))
	}
	if c, ok := controller.(Shower[C]); ok {
		add(registerAction(r, http.MethodGet, at.member, at.name+".show", c.Show, adapt))
	}
	if c, ok := controller.(Editor[C]); ok {
		add(registerAction(r, http.MethodGet, at.member+"/edit", at.name+".edit", c.Edit, adapt))
	}
	if c, ok := controller.(Updater[C]); ok {
		add(registerUpdate(r, at.member, at.name+".update", c.Update, adapt))
	}
	if c, ok := controller.(Destroyer[C]); ok {
		add(registerAction(r, http.MethodDelete, at.member, at.name+".destroy", c.Destroy, adapt))
	}
	return out
}

// Singleton registers the routes of a resource there is exactly one of where
// it is reached -- the account's settings, a project's billing -- so no route
// carries an id.
//
//	routing.Singleton(r, "settings", SettingsController{}, adapt)
//
//	GET    /settings        show     settings.show
//	GET    /settings/edit   edit     settings.edit
//	PUT    /settings        update   settings.update
//	PATCH  /settings        update   settings.update
//
// It registers whichever of Shower, Editor and Updater the controller
// implements and nothing else. There is no list of one thing, so an Index,
// Create, Store or Destroy on the same controller is not registered; a
// singleton that is created or removed takes a Post or a Delete route of its
// own. A controller implementing none of the three returns zero routes.
//
// A dotted name nests it under its parents with the same parameters Resource
// uses: "projects.billing" answers at /projects/{project}/billing, named
// projects.billing.show and so on. As there, the parent's parameter is where
// the person navigated, and the action loads the parent under its Grant.
func Singleton[C any](r *Router, name string, controller any, adapt Adapter[C]) []*Route {
	at := resourcePathsOf(name)

	var out []*Route
	add := func(route *Route) { out = append(out, route) }

	if c, ok := controller.(Shower[C]); ok {
		add(registerAction(r, http.MethodGet, at.collection, at.name+".show", c.Show, adapt))
	}
	if c, ok := controller.(Editor[C]); ok {
		add(registerAction(r, http.MethodGet, at.collection+"/edit", at.name+".edit", c.Edit, adapt))
	}
	if c, ok := controller.(Updater[C]); ok {
		add(registerUpdate(r, at.collection, at.name+".update", c.Update, adapt))
	}
	return out
}

// ResourceAction registers one named action on a record of a resource: a verb
// beyond the seven, such as publish, cancel or approve.
//
//	routing.ResourceAction(r, "notes", "publish", http.MethodPost, c.Publish, adapt)
//
//	POST   /notes/{id}/publish     notes.publish
//
// The action sits under the path Resource gives show for the same name, and
// reads the same parameter, so every action of one controller finds its
// record the same way. On a nested resource that is the record's own path:
//
//	routing.ResourceAction(r, "projects.tasks", "close", http.MethodPost, c.Close, adapt)
//
//	POST   /tasks/{task}/close     projects.tasks.close
//
// It is registered on the router it is given, so it carries that group's
// prefix, name and middleware -- the same guard as the resource registered
// beside it -- and it goes through the same adapter.
//
// The method is POST, PUT, PATCH or DELETE, in any case. An action changes
// state, and a GET that changes state is one that a prefetching browser, a
// crawler or a link in an email fires without anybody choosing to; any other
// method, and an empty action, panic at registration with the route's name. A
// read that is not one of the seven is a GET route of its own, and an
// operation chosen by a form field is one action per operation.
func ResourceAction[C any](r *Router, resource, action, method string, h func(*C) error, adapt Adapter[C]) *Route {
	at := resourcePathsOf(resource)
	action = strings.Trim(action, "/")
	if action == "" {
		panic(fmt.Sprintf("routing: ResourceAction on %q was given no action name. Name the verb, as in \"publish\"", at.name))
	}
	name := at.name + "." + action

	method = strings.ToUpper(method)
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		shown := method
		if shown == "" {
			shown = "no method"
		}
		panic(fmt.Sprintf("routing: resource action %q is registered with %s, and an action changes state. Register it with POST, PUT, PATCH or DELETE; a read is a GET route of its own", name, shown))
	}
	return registerAction(r, method, at.member+"/"+action, name, h, adapt)
}

// resourcePaths is where the routes of one resource name go.
type resourcePaths struct {
	// name is what every route name starts with: "invoices", "projects.tasks".
	name string
	// collection is the path of index, create and store, under every parent.
	// It is also where a singleton answers.
	collection string
	// member is the path of show, edit, update and destroy, and the one a
	// resource action extends.
	member string
}

// resourcePathsOf reads a resource name. A name without a dot is one segment
// and keeps {id}; a dotted one nests, shallow, with every parameter named by
// resourceParameter.
func resourcePathsOf(name string) resourcePaths {
	name = strings.Trim(name, "/")
	if !strings.Contains(name, ".") {
		base := "/" + name
		return resourcePaths{name: name, collection: base, member: base + "/{id}"}
	}

	segments := strings.Split(name, ".")
	var collection strings.Builder
	for i, segment := range segments {
		segment = strings.Trim(segment, "/")
		if segment == "" {
			panic(fmt.Sprintf("routing: resource name %q has an empty segment. Nest with one dot between two names, as in \"projects.tasks\"", name))
		}
		segments[i] = segment
		collection.WriteString("/" + segment)
		if i < len(segments)-1 {
			collection.WriteString("/{" + resourceParameter(segment) + "}")
		}
	}
	last := segments[len(segments)-1]
	return resourcePaths{
		name:       name,
		collection: collection.String(),
		member:     "/" + last + "/{" + resourceParameter(last) + "}",
	}
}

// resourceParameter is the path parameter that stands for one record of a
// segment: the singular of its last path element, with dashes as underscores,
// which makes it a wildcard name the mux accepts and the key Router.Model
// binds.
func resourceParameter(segment string) string {
	if i := strings.LastIndexByte(segment, '/'); i >= 0 {
		segment = segment[i+1:]
	}
	return normalizeBindingKey(str.Singular(segment))
}

// registerAction adapts one controller action and registers it under a
// method, a pattern and a name.
func registerAction[C any](r *Router, method, pattern, name string, h func(*C) error, adapt Adapter[C]) *Route {
	return r.handle(method, pattern, adapt(h)).Name(name)
}

// registerUpdate registers an update under PUT and PATCH. PATCH answers the
// same handler and shows the same name. Only the PUT row is indexed by it, so
// URL generation has one answer rather than two.
func registerUpdate[C any](r *Router, pattern, name string, h func(*C) error, adapt Adapter[C]) *Route {
	route := r.handle(http.MethodPut, pattern, adapt(h))
	route.siblings = append(route.siblings, r.handle(http.MethodPatch, pattern, adapt(h)))
	return route.Name(name)
}
