// Package processors mirrors Illuminate\Database\Query\Processors: the hook a
// driver takes to adjust results on the way out of the connection.
//
// The source is the clone at laravel_illuminate/database/Query/Processors, at
// v12.52.0-54-gd59e7abde. The files it answers to:
//
//	Processor.php          -> Processor, in processor.go
//	MySqlProcessor.php     -> MySQLProcessor, in mysql.go
//	MariaDbProcessor.php   -> MariaDBProcessor, in mariadb.go
//	PostgresProcessor.php  -> PostgresProcessor, in postgres.go
//	SQLiteProcessor.php    -> SQLiteProcessor, in sqlite.go
//
// A processor exists so that three engines answering the same question three
// ways arrive at the caller as one answer: Postgres returns an inserted
// identifier as a row, MySQL reports it out of band, and both come back as an
// int64. Everything else it does is reshaping what a schema introspection query
// reported.
//
// # Where authorization is, and where it is not
//
// Not here. ProcessInsertGetID does run a statement, through the connection the
// builder is holding, and the Grant that allowed it was checked one layer up: a
// *query.Builder is reachable only from a repository that holds an auth.Grant
// and has already filtered by auth.Tenant(g), on reads exactly as on writes. A
// processor that took a Grant would be a second place to enforce authorization,
// and a second place for it to be forgotten.
//
// # Names
//
// ADR 0044: the name is Illuminate's, with the initial raised and initialisms
// in upper case. processInsertGetId is ProcessInsertGetID, MySqlProcessor is
// MySQLProcessor, MariaDbProcessor is MariaDBProcessor.
//
// The mechanical changes:
//
//   - ProcessInsertGetID returns (int64, error) where the PHP returns int|string
//     and lets the connection throw. query.Processor declares the signature.
//   - The connection is asked for the identifier through LastInsertIDConnection,
//     because query.Connection is narrowed to running statements and has no PDO
//     handle to reach through.
//   - ProcessColumns takes the CREATE TABLE statement as a variadic argument,
//     because Go has no default argument and SQLite's is the one that reads it.
//   - A result row is a query.Record -- a map -- where the PHP casts an array to
//     stdClass to read it with arrows. The values are coerced on the way out,
//     since a driver hands back a count as []byte or int64 depending on which
//     one it is.
//
// # Skipped, and why
//
//   - SqlServerProcessor.php: a driver this ecosystem does not carry.
package processors
