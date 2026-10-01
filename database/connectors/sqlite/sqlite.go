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
// This is the default in .env because it needs nothing installed.
//
// It is its own module even so, and that is the case that made the rule: the
// skeleton used to carry pgx into every SQLite-only project, vulnerability
// surface included.
package sqlite

import (
	"errors"

	"github.com/arandu-io/hesape/database"

	modernc "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// SQLiteConnector names the driver the blank import above linked in.
//
// It does not check for a missing database file: a missing file is created, and
// the directory above it too, in database.Open -- SQLite creates the file and
// never the directory.
type SQLiteConnector struct{}

// Dialect reports the connection this connector answers for.
func (SQLiteConnector) Dialect() database.Dialect { return database.DialectSQLite }

// DriverName is the name modernc.org/sqlite registers with database/sql.
func (SQLiteConnector) DriverName() string { return "sqlite" }

// CausedByUniqueViolation reports whether err is an SQLite error with the
// extended result code SQLITE_CONSTRAINT_UNIQUE or
// SQLITE_CONSTRAINT_PRIMARYKEY.
//
// The extended code is what tells a duplicate apart from the other constraint
// failures: the primary code is SQLITE_CONSTRAINT for all of them, a NOT NULL
// and a CHECK included. The driver turns extended codes on for every
// connection it opens, so the code read here is always the extended one.
func (SQLiteConnector) CausedByUniqueViolation(err error) bool {
	var driverErr *modernc.Error
	if !errors.As(err, &driverErr) {
		return false
	}
	switch driverErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return true
	}
	return false
}

func init() { database.Register(SQLiteConnector{}) }
