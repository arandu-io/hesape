package console_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
	dbconsole "github.com/arandu-io/hesape/database/console"
	"github.com/arandu-io/hesape/database/console/migrations"
)

// TestProhibitDestructiveCommandsStopsTheFive is the guard an application turns
// on in production. Without it, migrate:fresh is one typed line away from an
// empty database.
func TestProhibitDestructiveCommandsStopsTheFive(t *testing.T) {
	dbconsole.ProhibitDestructiveCommands(true)
	t.Cleanup(func() { dbconsole.ProhibitDestructiveCommands(false) })

	for _, name := range dbconsole.DestructiveCommands {
		var out strings.Builder
		io := console.NewIO(name, nil, &out, &out, nil)
		if !console.IsProhibited(name, io) {
			t.Errorf("%s ran with destructive commands prohibited", name)
		}
	}
}

// TestProhibitDestructiveCommandsLetsThemBackOn covers the false argument, which
// is how a test that has to exercise one of them turns the guard off.
func TestProhibitDestructiveCommandsLetsThemBackOn(t *testing.T) {
	dbconsole.ProhibitDestructiveCommands(true)
	dbconsole.ProhibitDestructiveCommands(false)

	for _, name := range dbconsole.DestructiveCommands {
		var out strings.Builder
		io := console.NewIO(name, nil, &out, &out, nil)
		if console.IsProhibited(name, io) {
			t.Errorf("%s stayed prohibited after the guard was turned off", name)
		}
	}
}

// TestTheProhibitedListNamesCommandsThatExist keeps the list honest: a name that
// no command answers to is a guard that protects nothing, and it would look
// exactly like a guard that works.
func TestTheProhibitedListNamesCommandsThatExist(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range dbconsole.Commands(dbconsole.Deps{}) {
		registered[c.Name] = true
	}
	for _, c := range migrations.Commands(migrations.Deps{}) {
		registered[c.Name] = true
	}

	for _, name := range dbconsole.DestructiveCommands {
		if !registered[name] {
			t.Errorf("%s is prohibited and no command answers to it", name)
		}
	}
}
