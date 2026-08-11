// Package sqlite links the SQLite driver into the binary.
//
// Blank-import it and github.com/arandu-io/hesape/database can open a sqlite://
// DATABASE_URL:
//
//	import _ "github.com/arandu-io/hesape/database/connectors/sqlite"
//
// The driver is modernc.org/sqlite, which is SQLite translated to Go rather than
// wrapped: no cgo, no C toolchain, and cross-compiling still produces one static
// binary. mattn/go-sqlite3 is faster and needs cgo, which costs the deploy story
// this framework is built on.
//
// This is the default in .env because it needs nothing installed (ADR 0009).
//
// It is its own module even so, and that is the case that made the rule: the
// skeleton used to carry pgx into every SQLite-only project, vulnerability
// surface included.
package sqlite

import (
	"github.com/arandu-io/hesape/database"

	_ "modernc.org/sqlite"
)

// SQLiteConnector names the driver the blank import above linked in.
//
// It is Illuminate\Database\Connectors\SQLiteConnector. The one thing
// Illuminate's has that this does not is the check for a missing database file:
// here a missing file is created, and the directory above it too, in
// database.Open -- SQLite creates the file and never the directory.
type SQLiteConnector struct{}

// Dialect reports the connection this connector answers for.
func (SQLiteConnector) Dialect() database.Dialect { return database.DialectSQLite }

// DriverName is the name modernc.org/sqlite registers with database/sql.
func (SQLiteConnector) DriverName() string { return "sqlite" }

func init() { database.Register(SQLiteConnector{}) }
