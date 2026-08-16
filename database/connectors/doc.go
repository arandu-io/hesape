// Package connectors is the parent of the three connector modules. It holds no
// code of its own, and it never will: each connector below is its own Go module,
// because in Go there is no optional dependency and a package here that imported
// all three would put pgx, go-sql-driver/mysql and modernc.org/sqlite in the
// go.sum of every project that uses the collection.
//
//	connectors/pgx      PostgresConnector    jackc/pgx
//	connectors/mysql    MySqlConnector       go-sql-driver/mysql
//	connectors/sqlite   SQLiteConnector      modernc.org/sqlite
//
// The Connector interface they satisfy is database.Connector, one level up: it
// names a database.Dialect, so declaring it here would need this package to
// import database while database imports it back.
//
// MariaDB is spoken by MySqlConnector -- mariadb:// is a DATABASE_URL scheme,
// not a second driver. Choosing between connectors is the blank import plus
// database.Open, so there is no factory here either.
package connectors
