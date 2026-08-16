// Package console is the one command the notifications component ships.
//
// [NotificationTableCommand] writes the migration that creates the table
// [notifications.TableStore] reads. It writes a file and runs nothing: a
// migration that ran itself would be N replicas racing each other at boot, and
// emitting a file is what lets somebody read it before it reaches production.
//
// The SQL it writes is [notifications.Migrations], not a stub file of its own,
// so there is no file to locate and no install layout to depend on. A project
// that keeps its migrations as Go values passes that to database.Migrate and
// never runs this command; a project that keeps .sql files runs it. Either way
// there is one definition of the table.
package console
