// Package console mirrors Illuminate\Database\Console.
//
// A command here is a console.Command value, not a class discovered by scanning
// a namespace: the listing and the compiler read the same slice, so a command
// missing from it does not exist and one in it with a broken Run does not
// build. Commands(deps) answers the slice.
//
// The collaborators each command needs arrive in a Deps value rather than
// through a container (ADR 0001), which is also what makes every one of them
// runnable in a test with a fake resolver and no database.
//
// # Two commands do less than their PHP counterparts, on purpose
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
// # The files it answers to
//
// In the clone at laravel_illuminate/database/Console:
//
//	DatabaseInspectionCommand.php
//	DbCommand.php
//	DumpCommand.php
//	MonitorCommand.php
//	PruneCommand.php
//	ShowCommand.php
//	ShowModelCommand.php
//	TableCommand.php
//	WipeCommand.php
//
// DatabaseInspectionCommand is the abstract base the inspection commands
// extend, and what it holds is three helpers for reaching the schema builder;
// they are the Tables function on Deps here, because the schema catalogue is
// the schema package's business and this one does not depend on it.
//
// ShowModelCommand has no counterpart: it inspects an Eloquent model by
// reflection, and there is no Eloquent. DumpCommand has none either -- it
// shells out to pg_dump and mysqldump, which is the same argument the `db`
// command's doc makes above.
package console
