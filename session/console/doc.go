// Package console mirrors Illuminate\Session\Console.
//
// The files it answers to, in the clone at
// laravel_illuminate/Session/Console:
//
//	SessionTableCommand.php
//
// [SessionTableCommand] writes the migration that creates the table
// [session.DatabaseSessionHandler] reads. It writes a file and runs nothing: a
// migration that ran itself would be N replicas racing each other at boot
// (RULE 16), and emitting a file is what lets somebody read it before it
// reaches production.
package console
