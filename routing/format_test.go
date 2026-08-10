package routing_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/routing"
)

// TestFormatRoutesGroupsByModuleAndSortsByPattern is what `aru routes` prints.
// The order is the whole value of the command: a list nobody can scan is a list
// nobody reads.
func TestFormatRoutesGroupsByModuleAndSortsByPattern(t *testing.T) {
	r := routing.NewRouter()
	billing := r.ForModule("billing")
	billing.Get("/invoices/{id}", ok).Name("invoices.show")
	billing.Get("/invoices", ok).Name("invoices.index")
	r.ForModule("auth").Post("/login", ok).Name("login")

	out := routing.FormatRoutes(r.Routes())

	if !strings.HasPrefix(out, "auth\n") {
		t.Errorf("the table does not start with the first module in order:\n%s", out)
	}
	for _, want := range []string{"auth", "billing", "POST", "/login", "login", "/invoices", "invoices.show"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table does not mention %q:\n%s", want, out)
		}
	}
	if index, show := strings.Index(out, "/invoices "), strings.Index(out, "/invoices/{id}"); index > show {
		t.Errorf("the routes of a module are not sorted by pattern:\n%s", out)
	}
}

// TestFormatRoutesSaysSoWhenThereAreNone. An empty string looks like a command
// that failed.
func TestFormatRoutesSaysSoWhenThereAreNone(t *testing.T) {
	if got := routing.FormatRoutes(nil); got != "no routes registered\n" {
		t.Fatalf("FormatRoutes(nil) = %q", got)
	}
}

// TestFormatRoutesPrintsAnUnnamedRouteShort, rather than printing an empty
// column that looks like a value.
func TestFormatRoutesPrintsAnUnnamedRouteShort(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/health", ok)

	out := routing.FormatRoutes(r.Routes())
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "/health") && strings.TrimRight(line, " ") != line {
			t.Fatalf("an unnamed route was padded to the name column: %q", line)
		}
	}
}
