package routing_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/routing"
)

// row is one line of a route table: the method, the pattern and the name.
type row struct{ method, pattern, name string }

// assertTable fails for every row the router did not register exactly, and
// for every route it registered that the table does not list.
func assertTable(t *testing.T, r *routing.Router, want []row) {
	t.Helper()
	got := map[string]string{} // "METHOD pattern" -> name
	for _, route := range r.Routes() {
		got[route.Method+" "+route.Pattern] = route.RouteName()
	}
	listed := map[string]bool{}
	for _, w := range want {
		key := w.method + " " + w.pattern
		listed[key] = true
		name, registered := got[key]
		if !registered {
			t.Errorf("%s was not registered", key)
			continue
		}
		if name != w.name {
			t.Errorf("%s is named %q, want %q", key, name, w.name)
		}
	}
	for key, name := range got {
		if !listed[key] {
			t.Errorf("registered %s (%s), which the table does not list", key, name)
		}
	}
}

// mustPanicWith runs register and requires it to panic with a message that
// carries every one of the given fragments.
func mustPanicWith(t *testing.T, register func(), fragments ...string) {
	t.Helper()
	defer func() {
		t.Helper()
		recovered := recover()
		if recovered == nil {
			t.Fatal("registration did not panic")
		}
		message := fmt.Sprint(recovered)
		for _, fragment := range fragments {
			if !strings.Contains(message, fragment) {
				t.Errorf("panic %q does not mention %q", message, fragment)
			}
		}
	}()
	register()
}

// tasks records which action answered and the path parameters it read, which
// is what proves a nested route hands the action its parent and its record.
type tasks struct{ seen *[]string }

func (c tasks) note(req *request, action string, params ...string) error {
	entry := action
	for _, param := range params {
		entry += " " + param + "=" + req.r.PathValue(param)
	}
	*c.seen = append(*c.seen, entry)
	return nil
}

func (c tasks) Index(req *request) error   { return c.note(req, "index", "project") }
func (c tasks) Create(req *request) error  { return c.note(req, "create", "project") }
func (c tasks) Store(req *request) error   { return c.note(req, "store", "project") }
func (c tasks) Show(req *request) error    { return c.note(req, "show", "task") }
func (c tasks) Edit(req *request) error    { return c.note(req, "edit", "task") }
func (c tasks) Update(req *request) error  { return c.note(req, "update", "task") }
func (c tasks) Destroy(req *request) error { return c.note(req, "destroy", "task") }

// TestANestedResourceListsUnderItsParentAndShowsAtTheRecordsOwnPath is the
// shape of a nested resource: the three collection routes under the parent,
// the four member routes shallow, and every name under the whole dotted name.
func TestANestedResourceListsUnderItsParentAndShowsAtTheRecordsOwnPath(t *testing.T) {
	r := routing.NewRouter()
	routes := routing.Resource(r, "projects.tasks", tasks{seen: new([]string)}, adapt)

	assertTable(t, r, []row{
		{"GET", "/projects/{project}/tasks", "projects.tasks.index"},
		{"GET", "/projects/{project}/tasks/create", "projects.tasks.create"},
		{"POST", "/projects/{project}/tasks", "projects.tasks.store"},
		{"GET", "/tasks/{task}", "projects.tasks.show"},
		{"GET", "/tasks/{task}/edit", "projects.tasks.edit"},
		{"PUT", "/tasks/{task}", "projects.tasks.update"},
		{"PATCH", "/tasks/{task}", "projects.tasks.update"},
		{"DELETE", "/tasks/{task}", "projects.tasks.destroy"},
	})
	if len(routes) != 7 {
		t.Errorf("returned %d routes, want 7: one per action, PATCH riding on update", len(routes))
	}
}

// TestTheNestedRoutesHandTheActionItsParameters: the collection routes carry
// the parent's id and the member routes the record's, each under the name the
// table promises. A route that matched but read the wrong parameter would pass
// a table test and load nothing.
func TestTheNestedRoutesHandTheActionItsParameters(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Resource(r, "projects.tasks", tasks{seen: &seen}, adapt)

	for _, call := range []struct{ method, path, want string }{
		{"GET", "/projects/7/tasks", "index project=7"},
		{"GET", "/projects/7/tasks/create", "create project=7"},
		{"POST", "/projects/7/tasks", "store project=7"},
		{"GET", "/tasks/9", "show task=9"},
		{"GET", "/tasks/9/edit", "edit task=9"},
		{"PUT", "/tasks/9", "update task=9"},
		{"PATCH", "/tasks/9", "update task=9"},
		{"DELETE", "/tasks/9", "destroy task=9"},
	} {
		seen = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(call.method, call.path, nil))
		if len(seen) != 1 || seen[0] != call.want {
			t.Errorf("%s %s reached %q, want [%s]", call.method, call.path, seen, call.want)
		}
	}
}

func TestANestedResourceBuildsItsURLsByName(t *testing.T) {
	r := routing.NewRouter()
	routing.Resource(r, "projects.tasks", tasks{seen: new([]string)}, adapt)

	for _, c := range []struct {
		name, param, want string
	}{
		{"projects.tasks.index", "7", "/projects/7/tasks"},
		{"projects.tasks.create", "7", "/projects/7/tasks/create"},
		{"projects.tasks.show", "9", "/tasks/9"},
		{"projects.tasks.update", "9", "/tasks/9"},
	} {
		if got, err := r.Table().Route(c.name, c.param); err != nil || got != c.want {
			t.Errorf("Route(%s, %s) = %q, %v; want %s", c.name, c.param, got, err, c.want)
		}
	}
}

// TestADeeperNestingGrowsOnlyTheCollectionPath: every parent appears in the
// path that lists, and none in the path to one record.
func TestADeeperNestingGrowsOnlyTheCollectionPath(t *testing.T) {
	r := routing.NewRouter()
	routing.Resource(r, "organizations.projects.tasks", listOnly{}, adapt)

	assertTable(t, r, []row{
		{"GET", "/organizations/{organization}/projects/{project}/tasks", "organizations.projects.tasks.index"},
		{"GET", "/tasks/{task}", "organizations.projects.tasks.show"},
	})
}

// TestANestedParameterIsTheSingularWrittenWithUnderscores: the mux refuses a
// dash in a wildcard name, and Router.Model registers a dashed key under
// underscores, so the parameter is spelled the way both of them read it.
func TestANestedParameterIsTheSingularWrittenWithUnderscores(t *testing.T) {
	r := routing.NewRouter()
	routing.Resource(r, "categories.purchase-orders", listOnly{}, adapt)

	assertTable(t, r, []row{
		{"GET", "/categories/{category}/purchase-orders", "categories.purchase-orders.index"},
		{"GET", "/purchase-orders/{purchase_order}", "categories.purchase-orders.show"},
	})
}

// TestAFlatResourceAndOneNestedUnderItShareARouter: the parent's own routes
// read {id} and its children's collection reads {project} at the same depth.
// The mux has to accept both, or the commonest nesting cannot be registered.
func TestAFlatResourceAndOneNestedUnderItShareARouter(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Resource(r, "projects", invoices{seen: &seen}, adapt)
	routing.Resource(r, "projects.tasks", tasks{seen: &seen}, adapt)

	for _, call := range []struct{ method, path, want string }{
		{"GET", "/projects/7", "show"},
		{"GET", "/projects/7/edit", "edit"},
		{"GET", "/projects/7/tasks", "index project=7"},
		{"POST", "/projects/7/tasks", "store project=7"},
	} {
		seen = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(call.method, call.path, nil))
		if len(seen) != 1 || seen[0] != call.want {
			t.Errorf("%s %s reached %q, want [%s]", call.method, call.path, seen, call.want)
		}
	}
}

// TestANestedResourceInheritsItsGroup: a nested resource inside a guarded
// group must come out guarded on both halves -- the collection under the
// parent and the shallow member path.
func TestANestedResourceInheritsItsGroup(t *testing.T) {
	r := routing.NewRouter()
	guarded := 0
	app := r.Group(routing.Group{
		Prefix: "/app",
		Name:   "app",
		Middleware: []pipeline.Middleware[http.Handler]{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					guarded++
					next.ServeHTTP(w, req)
				})
			},
		},
	})
	routing.Resource(app, "projects.tasks", tasks{seen: new([]string)}, adapt)

	index, err := r.Table().Route("app.projects.tasks.index", "7")
	if err != nil || index != "/app/projects/7/tasks" {
		t.Fatalf("Route(app.projects.tasks.index, 7) = %q, %v; want /app/projects/7/tasks", index, err)
	}
	show, err := r.Table().Route("app.projects.tasks.show", "9")
	if err != nil || show != "/app/tasks/9" {
		t.Fatalf("Route(app.projects.tasks.show, 9) = %q, %v; want /app/tasks/9", show, err)
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, index, nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, show, nil))
	if guarded != 2 {
		t.Fatalf("the group's middleware ran %d times for two nested routes, want 2", guarded)
	}
}

func TestAnEmptySegmentInAResourceNamePanics(t *testing.T) {
	mustPanicWith(t, func() {
		routing.Resource(routing.NewRouter(), "projects..tasks", listOnly{}, adapt)
	}, `"projects..tasks"`, "empty segment")
}

// settings implements every action a resource can have. A singleton registers
// three of them, and that the other four stay out is part of what is tested.
type settings struct{ invoices }

func TestASingletonRegistersShowEditAndUpdateWithoutAnID(t *testing.T) {
	r := routing.NewRouter()
	routes := routing.Singleton(r, "settings", settings{invoices{seen: new([]string)}}, adapt)

	assertTable(t, r, []row{
		{"GET", "/settings", "settings.show"},
		{"GET", "/settings/edit", "settings.edit"},
		{"PUT", "/settings", "settings.update"},
		{"PATCH", "/settings", "settings.update"},
	})
	if len(routes) != 3 {
		t.Errorf("returned %d routes, want 3", len(routes))
	}
	if path, err := r.Table().Route("settings.update"); err != nil || path != "/settings" {
		t.Errorf("Route(settings.update) = %q, %v; want /settings", path, err)
	}
}

func TestTheSingletonRoutesActuallyAnswer(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Singleton(r, "settings", settings{invoices{seen: &seen}}, adapt)

	for _, call := range []struct{ method, path, action string }{
		{"GET", "/settings", "show"},
		{"GET", "/settings/edit", "edit"},
		{"PUT", "/settings", "update"},
		{"PATCH", "/settings", "update"},
	} {
		seen = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(call.method, call.path, nil))
		if len(seen) != 1 || seen[0] != call.action {
			t.Errorf("%s %s reached %v, want [%s]", call.method, call.path, seen, call.action)
		}
	}
}

func TestASingletonOnAControllerThatImplementsNoneOfItsThreeRegistersNothing(t *testing.T) {
	r := routing.NewRouter()
	if routes := routing.Singleton(r, "settings", nothing{}, adapt); len(routes) != 0 {
		t.Fatalf("registered %d routes, want 0", len(routes))
	}
}

// billing reads the parent a nested singleton answers under.
type billing struct{ seen *[]string }

func (c billing) Show(req *request) error {
	*c.seen = append(*c.seen, "show project="+req.r.PathValue("project"))
	return nil
}

func TestANestedSingletonAnswersUnderItsParent(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	routing.Singleton(r, "projects.billing", billing{seen: &seen}, adapt)

	assertTable(t, r, []row{
		{"GET", "/projects/{project}/billing", "projects.billing.show"},
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/projects/7/billing", nil))
	if len(seen) != 1 || seen[0] != "show project=7" {
		t.Fatalf("GET /projects/7/billing reached %q, want [show project=7]", seen)
	}
}

// notes is a resource controller with one action beyond the seven. Show and
// Publish record the same parameter, which is what an action under the member
// path has to give it.
type notes struct{ seen *[]string }

func (c notes) Show(req *request) error {
	*c.seen = append(*c.seen, "show id="+req.r.PathValue("id"))
	return nil
}

func (c notes) Publish(req *request) error {
	*c.seen = append(*c.seen, "publish id="+req.r.PathValue("id"))
	return nil
}

// TestAResourceActionSitsUnderTheRecordAndReadsWhatShowReads: the action is
// at the member path of the same name, with the same parameter, so a
// controller's Show and its Publish find the note the same way.
func TestAResourceActionSitsUnderTheRecordAndReadsWhatShowReads(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	c := notes{seen: &seen}
	routing.Resource(r, "notes", c, adapt)
	route := routing.ResourceAction(r, "notes", "publish", http.MethodPost, c.Publish, adapt)

	assertTable(t, r, []row{
		{"GET", "/notes/{id}", "notes.show"},
		{"POST", "/notes/{id}/publish", "notes.publish"},
	})
	if route.RouteName() != "notes.publish" {
		t.Errorf("returned the route named %q, want notes.publish", route.RouteName())
	}

	for _, call := range []struct{ method, path, want string }{
		{"GET", "/notes/42", "show id=42"},
		{"POST", "/notes/42/publish", "publish id=42"},
	} {
		seen = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(call.method, call.path, nil))
		if len(seen) != 1 || seen[0] != call.want {
			t.Errorf("%s %s reached %q, want [%s]", call.method, call.path, seen, call.want)
		}
	}
	if path, err := r.Table().Route("notes.publish", "42"); err != nil || path != "/notes/42/publish" {
		t.Errorf("Route(notes.publish, 42) = %q, %v; want /notes/42/publish", path, err)
	}
}

func TestANestedResourceActionSitsAtTheRecordsOwnPath(t *testing.T) {
	r := routing.NewRouter()
	var seen []string
	closeTask := func(req *request) error {
		seen = append(seen, "close task="+req.r.PathValue("task"))
		return nil
	}
	routing.ResourceAction(r, "projects.tasks", "close", "patch", closeTask, adapt)

	assertTable(t, r, []row{
		{"PATCH", "/tasks/{task}/close", "projects.tasks.close"},
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, "/tasks/9/close", nil))
	if len(seen) != 1 || seen[0] != "close task=9" {
		t.Fatalf("PATCH /tasks/9/close reached %q, want [close task=9]", seen)
	}
}

// TestAResourceActionRefusesGET: an action changes state, and a GET that
// changes state is fired by a prefetching browser or a crawler without anybody
// choosing to. The refusal is at registration, where the line that wrote it is
// still on the stack, and it names the route.
func TestAResourceActionRefusesGET(t *testing.T) {
	publish := func(*request) error { return nil }
	mustPanicWith(t, func() {
		routing.ResourceAction(routing.NewRouter(), "notes", "publish", http.MethodGet, publish, adapt)
	}, `"notes.publish"`, "GET", "changes state", "POST, PUT, PATCH or DELETE")
}

// TestAResourceActionRefusesEveryMethodThatDoesNotChangeState is the same
// refusal for the spellings a GET hides behind and for the methods that are
// not a change either.
func TestAResourceActionRefusesEveryMethodThatDoesNotChangeState(t *testing.T) {
	publish := func(*request) error { return nil }
	for _, method := range []string{"get", "HEAD", "OPTIONS", ""} {
		t.Run("method "+method, func(t *testing.T) {
			mustPanicWith(t, func() {
				routing.ResourceAction(routing.NewRouter(), "notes", "publish", method, publish, adapt)
			}, `"notes.publish"`, "changes state")
		})
	}
}

func TestAResourceActionAcceptsTheFourMethodsThatChangeState(t *testing.T) {
	r := routing.NewRouter()
	act := func(*request) error { return nil }
	for _, method := range []string{"POST", "put", "Patch", "DELETE"} {
		routing.ResourceAction(r, "notes", "do-"+strings.ToLower(method), method, act, adapt)
	}
	assertTable(t, r, []row{
		{"POST", "/notes/{id}/do-post", "notes.do-post"},
		{"PUT", "/notes/{id}/do-put", "notes.do-put"},
		{"PATCH", "/notes/{id}/do-patch", "notes.do-patch"},
		{"DELETE", "/notes/{id}/do-delete", "notes.do-delete"},
	})
}

func TestAResourceActionWithoutANamePanics(t *testing.T) {
	act := func(*request) error { return nil }
	mustPanicWith(t, func() {
		routing.ResourceAction(routing.NewRouter(), "notes", "", http.MethodPost, act, adapt)
	}, `"notes"`, "no action name")
}

// TestAResourceActionCarriesTheGuardOfItsGroup: an action registered beside a
// guarded resource that came out unguarded is the one route in the resource a
// person could reach without signing in.
func TestAResourceActionCarriesTheGuardOfItsGroup(t *testing.T) {
	r := routing.NewRouter()
	guarded := false
	admin := r.Group(routing.Group{
		Prefix: "/admin",
		Name:   "admin",
		Middleware: []pipeline.Middleware[http.Handler]{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					guarded = true
					next.ServeHTTP(w, req)
				})
			},
		},
	})
	act := func(*request) error { return nil }
	routing.ResourceAction(admin, "notes", "publish", http.MethodPost, act, adapt)

	path, err := r.Table().Route("admin.notes.publish", "42")
	if err != nil || path != "/admin/notes/42/publish" {
		t.Fatalf("Route(admin.notes.publish, 42) = %q, %v; want /admin/notes/42/publish", path, err)
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	if !guarded {
		t.Fatal("the group's middleware did not run for a route registered by ResourceAction")
	}
}
