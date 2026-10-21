// The router documentation examples, compiled, so a line in a doc comment
// that stops compiling is a test that stops compiling.

package controllers_test

import (
	"fmt"
	"net/http"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing/controllers"
)

// invoices implements three of the seven resource actions, which is what
// Resource registers routes for.
type invoices struct{}

func (invoices) Index(*hhttp.Context) error { return nil }
func (invoices) Show(*hhttp.Context) error  { return nil }
func (invoices) Store(*hhttp.Context) error { return nil }

func ExampleRouter_Get() {
	r := controllers.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}).Name("health")

	path, err := r.Table().URL("health")
	fmt.Println(path, err)
	// Output: /health <nil>
}

func ExampleRouter_Action() {
	r := controllers.NewRouter()
	r.Action("GET", "/dashboard", func(*hhttp.Context) error { return nil }).Name("dashboard")

	path, err := r.Table().URL("dashboard")
	fmt.Println(path, err)
	// Output: /dashboard <nil>
}

func ExampleRouter_Resource() {
	r := controllers.NewRouter()
	routes := r.Resource("invoices", invoices{})

	for _, route := range routes {
		fmt.Println(route.RouteName())
	}
	// Output:
	// invoices.index
	// invoices.store
	// invoices.show
}

func ExampleRouter_Group() {
	r := controllers.NewRouter()
	admin := r.Group("/admin")
	admin.Action("GET", "/comments", func(*hhttp.Context) error { return nil }).Name("admin.comments")

	path, err := r.Table().URL("admin.comments")
	fmt.Println(path, err)
	// Output: /admin/comments <nil>
}

// tasks lists the tasks of a project and shows one.
type tasks struct{}

func (tasks) Index(*hhttp.Context) error { return nil }
func (tasks) Show(*hhttp.Context) error  { return nil }

func ExampleRouter_Resource_nested() {
	r := controllers.NewRouter()
	r.Resource("projects.tasks", tasks{})

	list, _ := r.Table().URL("projects.tasks.index", "3")
	one, _ := r.Table().URL("projects.tasks.show", "9")
	fmt.Println(list)
	fmt.Println(one)
	// Output:
	// /projects/3/tasks
	// /tasks/9
}

// settings shows and updates the one record there is.
type settings struct{}

func (settings) Show(*hhttp.Context) error   { return nil }
func (settings) Update(*hhttp.Context) error { return nil }

func ExampleRouter_Singleton() {
	r := controllers.NewRouter()
	for _, route := range r.Singleton("settings", settings{}) {
		fmt.Println(route.RouteName())
	}

	path, err := r.Table().URL("settings.show")
	fmt.Println(path, err)
	// Output:
	// settings.show
	// settings.update
	// /settings <nil>
}

func ExampleRouter_ResourceAction() {
	r := controllers.NewRouter()
	publish := func(*hhttp.Context) error { return nil }
	r.ResourceAction(http.MethodPost, "notes", "publish", publish)

	path, err := r.Table().URL("notes.publish", "7")
	fmt.Println(path, err)
	// Output: /notes/7/publish <nil>
}

// export is a controller with one action.
type export struct{}

func (export) Invoke(*hhttp.Context) error { return nil }

func ExampleRouter_Invokable() {
	r := controllers.NewRouter()
	r.Invokable(http.MethodPost, "/exports", export{}).Name("exports.store")

	path, err := r.Table().URL("exports.store")
	fmt.Println(path, err)
	// Output: /exports <nil>
}
