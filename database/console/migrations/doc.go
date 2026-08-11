// Package migrations mirrors Illuminate\Database\Console\Migrations.
//
// Eight commands, as console.Command values, with the flags the PHP declares in
// its $signature -- --database, --path, --pretend, --step, --batch, --seed,
// --force. Commands(deps) answers them all.
//
// migrate, migrate:rollback, migrate:reset, migrate:refresh, migrate:fresh and
// db:wipe all name the same isolation lock. RULE 16 is why: `aru migrate` is a
// pipeline step, and two of them against one database is the race the rule
// exists to prevent. A registry with no lock issuer refuses to run them rather
// than running them unprotected.
//
// migrate:fresh and db:wipe drop every table, and both refuse to run unless the
// application handed over a Wipe deliberately. "Drop everything" is not a
// capability a framework should assume it has.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Console/Migrations:
//
//	BaseCommand.php
//	FreshCommand.php
//	InstallCommand.php
//	MigrateCommand.php
//	MigrateMakeCommand.php
//	RefreshCommand.php
//	ResetCommand.php
//	RollbackCommand.php
//	StatusCommand.php
//	TableGuesser.php
//
// BaseCommand is the abstract base the others extend, and all it carries is
// getMigrationPaths; that is the pathsOf helper here, because Go has embedding
// and no protected, and a base class with one helper on it is a function.
package migrations
