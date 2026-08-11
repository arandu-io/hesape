// Package connectors mirrors Illuminate\Database\Connectors.
//
// It holds no code of its own, and it never will: each connector below is its
// own Go module, because in Go there is no optional dependency and a package
// here that imported all three would put pgx, go-sql-driver/mysql and
// modernc.org/sqlite in the go.sum of every project that uses the collection.
//
//	connectors/pgx      PostgresConnector    jackc/pgx
//	connectors/mysql    MySqlConnector       go-sql-driver/mysql
//	connectors/sqlite   SQLiteConnector      modernc.org/sqlite
//
// The Connector interface they satisfy is database.Connector, one level up:
// it names a database.Dialect, so declaring it here would need this package to
// import database while database imports it back. ConnectorInterface.php has no
// such constraint, and the interface is what moves.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Connectors:
//
//	ConnectionFactory.php
//	Connector.php
//	ConnectorInterface.php
//	MariaDbConnector.php
//	MySqlConnector.php
//	PostgresConnector.php
//	SQLiteConnector.php
//	SqlServerConnector.php
//
// MariaDB is spoken by MySqlConnector -- mariadb:// is a DATABASE_URL scheme,
// not a second driver. SQL Server has no counterpart: RULE 11 names Postgres,
// MySQL and SQLite for the conventional profile and nothing else.
//
// ConnectionFactory has none either, and does not need one: it exists in
// Illuminate to pick a connector from a config array at runtime, which here is
// the blank import plus database.Open.
package connectors
