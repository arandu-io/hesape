package routing_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/routing"
)

// TestRouteIsGeneratedFromTheName is what replaces "/invoices/"+id at the call
// site. A hardcoded path keeps compiling after the route moves; this does not.
func TestRouteIsGeneratedFromTheName(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/", ok).Name("home")
	r.Get("/invoices", ok).Name("invoices.index")
	r.Get("/invoices/{id}", ok).Name("invoices.show")
	r.Get("/invoices/{id}/edit", ok).Name("invoices.edit")

	cases := []struct {
		name   string
		params []string
		want   string
	}{
		{"home", nil, "/"},
		{"invoices.index", nil, "/invoices"},
		{"invoices.show", []string{"42"}, "/invoices/42"},
		{"invoices.edit", []string{"42"}, "/invoices/42/edit"},
	}
	for _, c := range cases {
		got, err := r.Table().Route(c.name, c.params...)
		if err != nil {
			t.Errorf("Route(%q): %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("Route(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTheRootAnchorIsNotAParameter: "GET /{$}" is how the root route is
// written, and reading {$} as a parameter made Route("home") fail for the one
// route every application has.
func TestTheRootAnchorIsNotAParameter(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/{$}", ok).Name("home")

	got, err := r.Table().Route("home")
	if err != nil {
		t.Fatalf("Route(home): %v", err)
	}
	if got != "/" {
		t.Fatalf("Route(home) = %q, want /", got)
	}
}

// TestABrokenRouteSaysWhat: the error is what a person reads at 3am, so it
// names the route and what to do.
func TestABrokenRouteSaysWhat(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices", ok).Name("invoices.index")
	r.Get("/invoices/{id}", ok).Name("invoices.show")

	if _, err := r.Table().Route("invoices.list"); err == nil {
		t.Error("an unknown name was accepted")
	} else if !strings.Contains(err.Error(), "aru routes") {
		t.Errorf("the error does not say how to find the real name: %v", err)
	}

	if _, err := r.Table().Route("invoices.show"); err == nil {
		t.Error("a route needing an id was built without one")
	} else if !strings.Contains(err.Error(), "{id}") {
		t.Errorf("the error does not name the missing parameter: %v", err)
	}

	if _, err := r.Table().Route("invoices.index", "42"); err == nil {
		t.Error("a route taking no parameter accepted one")
	}
}

// TestMustPutsTheReasonInTheHref: a template helper cannot handle an error, and
// a broken link that says what is wrong beats one that points at "/".
func TestMustPutsTheReasonInTheHref(t *testing.T) {
	table := routing.NewRoutes()

	if got := table.Must("nowhere"); !strings.HasPrefix(got, "#") || !strings.Contains(got, "nowhere") {
		t.Errorf("Must(nowhere) = %q, want a # href naming the route", got)
	}
}

// TestTwoRoutesCannotShareAName: with two, every URL built from it goes to one
// of them, chosen by registration order. Finding that out at boot beats finding
// it out from a link that quietly points at the wrong page.
func TestTwoRoutesCannotShareAName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("two routes shared a name and nothing complained")
		}
	}()

	r := routing.NewRouter()
	r.Get("/a", ok).Name("dup")
	r.Get("/b", ok).Name("dup")
}

// TestAGroupNamesWhatIsUnderIt, and joins with a dot so that a forgotten
// separator cannot produce "adminusers" -- a name that works everywhere until
// somebody reads it.
func TestAGroupNamesWhatIsUnderIt(t *testing.T) {
	r := routing.NewRouter()
	admin := r.Group(routing.Group{Prefix: "/admin", Name: "admin"})
	admin.Get("/users", ok).Name("users")
	admin.Group(routing.Group{Prefix: "/reports", Name: "reports"}).Get("/monthly", ok).Name("monthly")

	for _, c := range []struct{ name, want string }{
		{"admin.users", "/admin/users"},
		{"admin.reports.monthly", "/admin/reports/monthly"},
	} {
		got, err := r.Table().Route(c.name)
		if err != nil {
			t.Errorf("Route(%q): %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("Route(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAGroupNamePrefixEndingInADotIsNotDoubled, because Laravel's own
// convention writes it that way and a caller arriving from there will.
func TestAGroupNamePrefixEndingInADotIsNotDoubled(t *testing.T) {
	r := routing.NewRouter()
	r.Group(routing.Group{Prefix: "/admin", Name: "admin."}).Get("/users", ok).Name("users")

	if _, err := r.Table().Route("admin.users"); err != nil {
		t.Fatalf("Route(admin.users): %v", err)
	}
}

// TestAnUnnamedRouteHasNoName is what FormatRoutes prints short, and it is the
// promise the old exported Name field did not keep: it was never written to.
func TestAnUnnamedRouteHasNoName(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/unnamed", ok)

	if got := r.Routes()[0].RouteName(); got != "" {
		t.Fatalf("RouteName() = %q, want empty", got)
	}
}

// TestTheTableIsSafeForConcurrentReads: modules register at boot on one
// goroutine and every request reads afterwards, and `go test -race` is the only
// thing that ever proves it.
func TestTheTableIsSafeForConcurrentReads(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices/{id}", ok).Name("invoices.show")

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				if _, err := r.Table().Route("invoices.show", "42"); err != nil {
					t.Error(err)
					return
				}
				_ = r.Routes()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
