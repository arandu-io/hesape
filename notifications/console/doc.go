// Package console is the one command the notifications component ships.
//
// It mirrors Illuminate\Notifications\Console. The files it answers to, in the
// clone at laravel_illuminate/notifications/Console (Laravel 13,
// illuminate/notifications ^13.0):
//
//	NotificationTableCommand.php  -> NotificationTableCommand
//
// [NotificationTableCommand] writes the migration that creates the table
// [notifications.TableStore] reads. It writes a file and runs nothing: a
// migration that ran itself would be N replicas racing each other at boot
// (RULE 16), and emitting a file is what lets somebody read it before it
// reaches production.
//
// The SQL it writes is [notifications.Migrations], not a stub file of its own.
// A project that keeps its migrations as Go values passes that to
// database.Migrate and never runs this command; a project that keeps .sql files
// runs it. Either way there is one definition of the table (RULE 9).
//
// # The method with no answer here
//
// migrationStubFile() is reason 2 of the porting rule -- a method that exists
// only to serve the framework's own plumbing. It returns the path of a .stub
// file inside the installed PHP package, which
// MigrationGeneratorCommand::handle copies and substitutes into.
// [NotificationTableCommand.MigrationStub] returns the SQL directly, so there is
// no file to locate and no install layout to depend on.
package console
