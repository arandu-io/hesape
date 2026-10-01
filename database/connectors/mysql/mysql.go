// Package mysql links the MySQL driver into the binary.
//
// Blank-import it and github.com/arandu-io/hesape/database can open a mysql://
// or mariadb:// DATABASE_URL:
//
//	import _ "github.com/arandu-io/hesape/database/connectors/mysql"
//
// MySQL is supported and is not the recommendation. Every query in this
// collection is written with "?", which MySQL takes directly, so nothing about
// the SQL changes -- what changes is that Postgres is where the migration
// story, the transactional DDL and the outbox relay are least surprising.
//
// It is its own module, for the reason every connector is: in Go there is no
// optional dependency, so a package carrying all three drivers would put all
// three in the go.sum of every project.
package mysql

import (
	"errors"

	"github.com/arandu-io/hesape/database"

	gomysql "github.com/go-sql-driver/mysql"
)

// MySqlConnector names the driver the blank import above linked in.
//
// There is no MariaDB connector: MariaDB speaks the MySQL wire protocol through
// this same driver, so mariadb:// is a DATABASE_URL scheme rather than a second
// connector to link.
type MySqlConnector struct{}

// Dialect reports the connection this connector answers for.
func (MySqlConnector) Dialect() database.Dialect { return database.DialectMySQL }

// DriverName is the name go-sql-driver/mysql registers with database/sql.
func (MySqlConnector) DriverName() string { return "mysql" }

// The error numbers MySQL and MariaDB report for a duplicate key: ER_DUP_ENTRY,
// and ER_DUP_ENTRY_WITH_KEY_NAME, which the server sends instead when it can
// name the key. Both cover a unique index and the primary key alike. The
// SQLSTATE is no help here -- it is 23000 for every integrity constraint,
// a NOT NULL among them.
const (
	erDupEntry            = 1062
	erDupEntryWithKeyName = 1586
)

// CausedByUniqueViolation reports whether err is a MySQL error numbered 1062
// or 1586.
//
// The number is read from the driver's error type and never from the
// message, which the server translates with lc_messages.
func (MySqlConnector) CausedByUniqueViolation(err error) bool {
	var driverErr *gomysql.MySQLError
	if !errors.As(err, &driverErr) {
		return false
	}
	return driverErr.Number == erDupEntry || driverErr.Number == erDupEntryWithKeyName
}

func init() { database.Register(MySqlConnector{}) }
