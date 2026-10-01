// Package pgx links the PostgreSQL driver into the binary.
//
// Blank-import it and github.com/arandu-io/hesape/database can open a
// postgres:// DATABASE_URL:
//
//	import _ "github.com/arandu-io/hesape/database/connectors/pgx"
//
// The driver is jackc/pgx through its database/sql compatibility layer. pgx has
// a native interface that is faster; going through database/sql is what keeps
// one Repository, one set of queries and one migration path across three
// engines. The portability is worth more than the last percent.
//
// It is its own module, and that is not a preference. In Go there is no
// optional dependency: a database package carrying pgx, MySQL and SQLite would
// put all three in the go.sum of every project -- in the build, in the binary,
// and in the vulnerability surface. That is not hypothetical. The skeleton
// carried pgx and modernc/sqlite together, and govulncheck found a pgx advisory
// in a project that could have been SQLite-only.
package pgx

import (
	"errors"

	"github.com/arandu-io/hesape/database"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresConnector names the driver the blank import above linked in.
//
// It has no connect method: opening the connection, tuning the pool and pinging
// it at boot is database.Open, in one place, for all three engines.
type PostgresConnector struct{}

// Dialect reports the connection this connector answers for.
func (PostgresConnector) Dialect() database.Dialect { return database.DialectPostgres }

// DriverName is the name jackc/pgx registers with database/sql.
func (PostgresConnector) DriverName() string { return "pgx" }

// uniqueViolation is the SQLSTATE PostgreSQL raises for a duplicate in a
// unique index and in a primary key alike.
const uniqueViolation = "23505"

// CausedByUniqueViolation reports whether err carries SQLSTATE 23505.
//
// It reads the code through the SQLState method rather than through pgx's
// error type by name, so any error in the chain that reports an SQLSTATE is
// read the same way. The message is never consulted: it is translated by the
// server's lc_messages and reworded between versions, and the code is neither.
func (PostgresConnector) CausedByUniqueViolation(err error) bool {
	var coded interface{ SQLState() string }
	return errors.As(err, &coded) && coded.SQLState() == uniqueViolation
}

func init() { database.Register(PostgresConnector{}) }
