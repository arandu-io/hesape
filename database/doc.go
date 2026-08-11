// Package database is the data access contract: one connection, one repository
// shape, and no ORM.
//
// Queries are plain parameterized SQL, written by hand in the templates
// `aru make:module` emits, which keeps the query plan predictable and the value
// always in a placeholder. What this package adds on top is:
//
//  1. an auth.Grant required by every operation (the mandatory path);
//  2. tenant scoping taken from the Grant, never from a parameter;
//  3. automatic instrumentation into the Collector;
//  4. one Open, one pool policy, and each driver in its own module.
//
// # The tenant does not live here
//
// auth.Tenant(g) is the single source of a tenant for SQL. It used to be
// data.Tenant, one field read off a Grant sitting in the package that owns the
// SQL -- so the cache, the filesystem and the scheduler all had to import the
// database to know which customer a key belonged to. RULE 14 still holds: the
// tenant comes from the Grant, never from a path, a body, a query or a header.
//
// # Why the drivers are separate modules
//
// In Go there is no optional dependency. A single module carrying pgx, MySQL and
// SQLite would put all three in the go.sum of every project -- in the build, in
// the binary, and in the vulnerability surface. That is not hypothetical: the
// skeleton carried pgx and modernc/sqlite together, and govulncheck found a pgx
// advisory in a project that could have been SQLite-only.
//
// So each connector is its own module with its own go.mod, under Connectors
// where Illuminate keeps them:
//
//	go get github.com/arandu-io/hesape/database/connectors/sqlite   // needs nothing installed
//	go get github.com/arandu-io/hesape/database/connectors/pgx      // Postgres
//	go get github.com/arandu-io/hesape/database/connectors/mysql    // MySQL
//
// and the project blank-imports the ones it uses:
//
//	import (
//	    "github.com/arandu-io/hesape/database"
//	    _ "github.com/arandu-io/hesape/database/connectors/pgx"
//	    _ "github.com/arandu-io/hesape/database/connectors/sqlite"
//	)
//
//	db, closeDB, err := database.Open(cfg)
//
// Switching engines stays what ADR 0009 promised: a line in .env. The import
// list is what decides which engines a build can speak at all.
//
// ConnectionFactory is the piece that reads that registry. It lives here rather
// than under Connectors, and that is what makes the inversion work: a factory
// that imported the three connector modules to choose between them would put
// all three back in every go.sum. It never names a driver package -- it looks
// the dialect up in the registry the connector's init filled in. A project that
// speaks an engine this framework does not ship registers its own the same way;
// the ConnectionFactory doc walks through the four lines.
//
// The conformance subpackage is what makes "one Repository, three engines" a
// measurement rather than a claim: one suite, run by all three connectors
// against a real server.
//
// # Where the Grant is
//
// Repository is the door: every method on it takes an auth.Grant and filters by
// auth.Tenant(g), on a read exactly as on a write (RULE 17). Connection, DB and
// the migrator are the plumbing under that door and take none -- not as an
// exemption, but because a Grant they could not use to filter anything would be
// a parameter that looks like enforcement and is not, which is the failure mode
// Query.Filter was deleted for. `aru migrate` also runs down here, in a process
// with no request and no subject, where a Grant cannot be constructed at all.
//
// So a module reaches rows through a Repository. A module that reaches them
// through a Connection is a module that gets sent back in review.
//
// # The Illuminate files this package answers to
//
// In the clone at laravel_illuminate/database:
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
// Grammar.php is answered one package over, by query.BaseGrammar: it is the
// abstract base Query\Grammars\Grammar extends, the grammars live in
// query/grammars, and splitting the base from its subclasses across two
// packages would have meant an import cycle for no gain. Its whole public
// surface -- Wrap, WrapTable, WrapArray, Columnize, Parameterize, Parameter,
// QuoteString, Escape, IsExpression, GetValue, GetDateFormat, GetTablePrefix,
// SetTablePrefix -- is there.
//
// SqlServerConnection.php has no counterpart and will not get one: RULE 11
// names Postgres, MySQL and SQLite for the conventional profile.
// LazyLoadingViolationException and ClassMorphViolationException are Eloquent's
// -- one is thrown by lazy loading and the other by the morph map, and there is
// neither.
//
// Eloquent has no counterpart and never will: Post::find(1)->update() is
// persistence that proves no authorization decision, and RULE 17 requires one on
// the way in and on the way out. docs/31-reorganizacao-hesape.md says what else
// moves in and from where.
package database
