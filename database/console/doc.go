// Package console holds the database commands: db, db:monitor, db:show,
// db:table, db:wipe and model:prune.
//
// A command here is a console.Command value, not a class discovered by scanning
// a namespace: the listing and the compiler read the same slice, so a command
// missing from it does not exist and one in it with a broken Run does not build.
// Commands(deps) answers the slice.
//
// The collaborators each command needs arrive in a Deps value rather than
// through a registry, which is also what makes every one of them runnable in a
// test with a fake resolver and no database.
//
// # Two commands do less than expected, on purpose
//
// `db` prints the client invocation instead of running it. Executing a binary
// this framework did not ship, with credentials, from a directory it does not
// control, is a supply chain problem wearing a convenience's clothes -- and
// printing it means the person's own client, history and .psqlrc are the ones
// that get used. The password is never printed; CommandEnvironment names the
// variable it should travel in.
//
// `model:prune` prunes what the application registered rather than what a
// directory scan found. Go has no such scan -- a type nothing references is not
// in the binary -- and the register is also the list somebody can read to find
// out what the command is going to delete.
//
// There is no dump command, for the reason `db` gives: it would shell out to
// pg_dump and mysqldump.
//
// Reaching the schema catalogue is the Tables function on Deps, because the
// catalogue is the schema package's business and this one does not depend on it.
package console
