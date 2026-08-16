package console

import "github.com/arandu-io/hesape/console"

// DestructiveCommands are the five that throw data away.
//
// They are named here rather than found by a marker on each command, because
// the list is the point: somebody reading it can see exactly what
// ProhibitDestructiveCommands turns off, and adding a sixth destructive command
// without adding it here is a mistake a reviewer can catch.
//
// db:wipe drops every table. migrate:fresh drops them and migrates again.
// migrate:refresh rolls everything back and replays it. migrate:reset rolls
// everything back. migrate:rollback undoes the last batch.
var DestructiveCommands = []string{
	"db:wipe",
	"migrate:fresh",
	"migrate:refresh",
	"migrate:reset",
	"migrate:rollback",
}

// ProhibitDestructiveCommands turns DestructiveCommands on or off.
//
// It is the one line an application puts in its bootstrap so that the commands
// which throw data away cannot run in production:
//
//	if config.Env == "production" {
//	    dbconsole.ProhibitDestructiveCommands(true)
//	}
//
// It calls [github.com/arandu-io/hesape/console.Prohibit] on the five
// names, which is where that state already lives.
//
// Go has no default arguments, so the argument is always given. Passing
// true is what a caller almost always means; passing false is how a test
// that needs to exercise one of the five turns the guard back off.
//
// # Why a list and not a flag on the command
//
// A destructive command that has to remember to declare itself is a command
// somebody will add without declaring. The list is read by a person, in one
// place, and the guard it feeds is checked by console.IsProhibited on the way
// in -- so the command still exists, still appears in the help, and refuses
// with a sentence instead of vanishing.
func ProhibitDestructiveCommands(prohibit bool) {
	for _, name := range DestructiveCommands {
		console.Prohibit(name, prohibit)
	}
}
