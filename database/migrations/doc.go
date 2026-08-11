// Package migrations mirrors Illuminate\Database\Migrations.
//
// # A migration does not run at boot
//
// RULE 16, and it is not a style preference. `aru migrate` is a step of the
// deployment pipeline, run once, by one process. Called from the start-up path
// of an application with N replicas it becomes N migrators racing over one
// table, and the ones that lose report a duplicate key on a table they were in
// the middle of creating. Nothing in this package makes that convenient: there
// is no MigrateOnBoot, and there will not be one.
//
// The other half of the rule belongs to the migration, and no code can check
// it: every migration is compatible with the binary that is still serving
// traffic while the rollout finishes. A new column is nullable or carries a
// default, because the old binary's INSERT does not mention it. Removing a
// column takes two releases -- the first stops writing it, the second drops it
// -- because the old binary's SELECT still names it. A rename is an add, a
// backfill, and a drop, which is three releases.
//
// # Discovery: a registry, not a directory
//
// This is the thing that had to change, and Register carries the argument in
// full. In short: Illuminate globs database/migrations/*_*.php and require_once
// s each file, because in PHP a file becomes a class by being read. Go has no
// such step -- a package nothing imports is not in the binary -- so a scan at
// run time would find files the compiler never saw, and in a deployed
// container would find nothing at all, because the source is not there.
//
// So a migration registers itself:
//
//	func init() { migrations.Register(CreateUsersTable{}) }
//
// and main.go blank-imports the package they live in. That import is
// require_once, moved from run time to link time, with the compiler checking
// that every registered migration implements Migration before anything runs.
//
// The alternative considered and rejected was embedding the directory and
// treating a migration as SQL text. It reads well until the first migration
// that has to read rows before it writes -- a backfill, a conditional drop --
// and then there are two kinds of migration, which RULE 9 does not allow.
//
// Order comes from the name and from nothing else: GetName answers
// "2026_08_11_000000_create_users_table", the registry sorts by that string,
// and two machines therefore apply the same migrations in the same order.
//
// # There is no Grant here
//
// Every path to application rows carries an auth.Grant and filters by
// auth.Tenant(g), on reads as much as on writes (RULE 17). A migration is not
// such a path: it is DDL, run by a pipeline step, in a process with no request
// and no subject -- there is nothing a Grant could be built from, and inventing
// a subject to satisfy a signature would be worse than not having one. The
// repository table is framework metadata, not tenant data, for the same reason.
//
// # The files it answers to
//
// In the clone at laravel_illuminate/database/Migrations:
//
//	DatabaseMigrationRepository.php
//	Migration.php
//	MigrationCreator.php
//	MigrationRepositoryInterface.php
//	MigrationResult.php
//	Migrator.php
//
// Migrator::requireFiles and Migrator::getFilesystem have no counterpart:
// both exist to load PHP source at run time, which is the language facility Go
// replaced with the compiler. The stubs are string constants rather than files
// in a stubs/ directory, so `aru make:migration` works from any working
// directory.
package migrations
