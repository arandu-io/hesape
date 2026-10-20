package foundation_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/routing"
)

// invoices is a module that adds one command to the binary, the way a module
// adds migrations and tasks: declared on the module, collected by the kernel.
type invoices struct{}

func (invoices) Name() string             { return "invoices" }
func (invoices) Routes(_ *routing.Router) {}

func (invoices) Commands() []console.Command {
	return []console.Command{{
		Name:        "invoices:close",
		Description: "close the open invoices",
		Run:         func(context.Context, *console.IO) error { return nil },
	}}
}

// TestAModuleDeclaresItsCommandsThroughCommandable: the kernel finds a module's
// commands by asking whether it is Commandable, so the interface has to be the
// one method a module writes -- Commands() []console.Command -- and a module
// that declares none must not satisfy it by accident.
func TestAModuleDeclaresItsCommandsThroughCommandable(t *testing.T) {
	t.Parallel()

	var declared foundation.Module = invoices{}
	commandable, ok := declared.(foundation.Commandable)
	if !ok {
		t.Fatal("a module with a Commands method is not foundation.Commandable, so the kernel would never see its commands")
	}
	commands := commandable.Commands()
	if len(commands) != 1 || commands[0].Name != "invoices:close" {
		t.Fatalf("Commands() = %+v, want the one invoices:close command the module declared", commands)
	}

	var silent foundation.Module = relay{}
	if _, ok := silent.(foundation.Commandable); ok {
		t.Fatal("a module that declares no commands satisfies foundation.Commandable")
	}
}
