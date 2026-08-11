package foundation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/routing"
)

// billing is the smallest thing that is a module: a name and its routes. It
// carries a field so that the test also proves the point the interface exists
// to make -- what a module needs is passed to its constructor and held, rather
// than resolved out of a container at the moment it is used.
type billing struct{ greeting string }

func (b *billing) Name() string { return "billing" }

func (b *billing) Routes(r *routing.Router) {
	r.Get("/invoices", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(b.greeting))
	}))
}

// relay implements Module and registers nothing, which is what a module with no
// HTTP surface does. It is here so that "an empty implementation is one line"
// stays true rather than being a sentence in a doc comment.
type relay struct{}

func (relay) Name() string             { return "relay" }
func (relay) Routes(_ *routing.Router) {}

// TestAModuleRegistersItsOwnRoutes: the contract is Name plus Routes, and
// Routes is handed the router rather than reaching for a global one. Nothing is
// registered anywhere until the composition root calls it.
func TestAModuleRegistersItsOwnRoutes(t *testing.T) {
	t.Parallel()

	mods := []foundation.Module{
		&billing{greeting: "one invoice"},
		relay{},
	}

	r := routing.NewRouter()
	for _, m := range mods {
		m.Routes(r.ForModule(m.Name()))
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "one invoice" {
		t.Fatalf("body = %q, want %q", got, "one invoice")
	}
}

// TestTheOptionalInterfacesAreOptional: a module implementing none of them is
// still a Module, and the kernel finds the ones it does implement by asking.
// This is the property every adapter depends on -- the queue module is
// Migratable and Diagnostic, the relay is neither, and both register the same
// way.
func TestTheOptionalInterfacesAreOptional(t *testing.T) {
	t.Parallel()

	var m foundation.Module = relay{}

	if _, ok := m.(foundation.Bootable); ok {
		t.Fatal("relay reports itself Bootable, and it has no Boot")
	}
	if _, ok := m.(foundation.Migratable); ok {
		t.Fatal("relay reports itself Migratable, and it declares no migrations")
	}
}
