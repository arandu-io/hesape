// Package events mirrors Illuminate\Database\Events.
//
// Every event is a plain value with the PHP's own field names, and a
// constructor that does what the PHP constructor does -- including reading the
// connection name off the connection rather than taking it, which is why
// ConnectionName is never a parameter.
//
// # Why the connection is an interface here
//
// In PHP these events hold a Connection. Here they hold a small interface
// declared in this package, because Connection lives in the database package
// and that package dispatches these events: naming the concrete type would
// close an import cycle. It is the same move query.Connection makes one
// package over, and it costs the events nothing -- they call one method on it.
//
// # The files it answers to
//
// In the clone at laravel_illuminate/database/Events:
//
//	ConnectionEstablished.php
//	ConnectionEvent.php
//	DatabaseBusy.php
//	DatabaseRefreshed.php
//	MigrationEnded.php
//	MigrationEvent.php
//	MigrationSkipped.php
//	MigrationStarted.php
//	MigrationsEnded.php
//	MigrationsEvent.php
//	MigrationsPruned.php
//	MigrationsStarted.php
//	ModelPruningFinished.php
//	ModelPruningStarting.php
//	ModelsPruned.php
//	NoPendingMigrations.php
//	QueryExecuted.php
//	SchemaDumped.php
//	SchemaLoaded.php
//	StatementPrepared.php
//	TransactionBeginning.php
//	TransactionCommitted.php
//	TransactionCommitting.php
//	TransactionRolledBack.php
//
// The three model pruning events are here even though Eloquent is not: a
// project prunes through its own repositories, and the event it fires should be
// the one a Laravel developer already has a listener for.
package events
