// Package database mirrors Illuminate\Database.
//
// The files it answers to, in the clone at
// laravel_illuminate/database:
//
//	ClassMorphViolationException.php
//	ConcurrencyErrorDetector.php
//	ConfigurationUrlParser.php
//	Connection.php
//	ConnectionInterface.php
//	ConnectionResolver.php
//	ConnectionResolverInterface.php
//	DatabaseManager.php
//	DatabaseServiceProvider.php
//	DatabaseTransactionRecord.php
//	DatabaseTransactionsManager.php
//	DeadlockException.php
//	DetectsConcurrencyErrors.php
//	DetectsLostConnections.php
//	Grammar.php
//	LazyLoadingViolationException.php
//	LostConnectionDetector.php
//	LostConnectionException.php
//	MariaDbConnection.php
//	MigrationServiceProvider.php
//	MultipleColumnsSelectedException.php
//	MultipleRecordsFoundException.php
//	MySqlConnection.php
//	PostgresConnection.php
//	QueryException.php
//	RecordNotFoundException.php
//	RecordsNotFoundException.php
//	SQLiteConnection.php
//	SQLiteDatabaseDoesNotExistException.php
//	Seeder.php
//	SqlServerConnection.php
//	UniqueConstraintViolationException.php
//
// Nothing is implemented here yet. docs/31-reorganizacao-hesape.md says what
// moves in, from where, and in which phase.
package database
