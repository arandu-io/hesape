package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// registry maps a dialect to the database/sql driver name a compartment
// registered for it.
var (
	registryMu sync.RWMutex
	registry   = map[Dialect]string{}
)

// sqlOpen is a variable so a test can substitute it. Opening a real connection
// to prove that the registry resolves the right driver name would need three
// servers running, and the thing under test is the resolution, not the network.
var sqlOpen = sql.Open

// Register records that a driver for this dialect is linked into the binary.
//
// Driver compartments call it from init(). It is not meant for application code:
// a project that registers its own driver name is a project that will get a
// different pool policy than every other, for no gain.
//
// Registering the same dialect twice panics rather than picking one. Two drivers
// for one dialect is an import nobody meant to add, and finding out at boot is
// better than finding out from a query that behaves differently.
func Register(d Dialect, driverName string) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if existing, taken := registry[d]; taken && existing != driverName {
		panic(fmt.Sprintf("database: %s is already registered to the %q driver, and %q wants it too -- remove one of the imports",
			d, existing, driverName))
	}
	registry[d] = driverName
}

// Registered reports the dialects this binary can speak, sorted.
//
// `aru doctor` reads it, and so does the error below.
func Registered() []Dialect {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return sortedLocked()
}

// sortedLocked returns the registered dialects. The caller holds the lock,
// which is what keeps this callable from inside another locked section.
func sortedLocked() []Dialect {
	out := make([]Dialect, 0, len(registry))
	for d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Open connects, tunes the pool, and returns the instrumented handle plus the
// function that closes it.
//
// The pool policy lives here rather than in each project's main, because it is
// not a preference: the defaults of database/sql are an unbounded pool, which
// turns one traffic spike into "too many connections" on the server instead of a
// queue in the process.
func Open(cfg Config) (*DB, func(), error) {
	driverName, err := driverFor(cfg.Connection)
	if err != nil {
		return nil, nil, err
	}

	// SQLite creates the database file but never the directory above it, and the
	// error it gives for a missing directory names neither.
	if path := cfg.SQLitePath(); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("creating the database directory: %w", err)
		}
	}

	sqldb, err := sqlOpen(driverName, cfg.DSN())
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", cfg.Redacted(), err)
	}

	if cfg.Connection == DialectSQLite {
		// One writer. SQLite serializes writes anyway, and letting the pool open
		// more connections only converts the wait into "database is locked".
		sqldb.SetMaxOpenConns(1)
	} else {
		sqldb.SetMaxOpenConns(25)
		sqldb.SetMaxIdleConns(5)
		sqldb.SetConnMaxLifetime(time.Hour)
	}

	// And it connects, here, before anything else runs.
	//
	// sql.Open resolves the driver and builds a pool; it does not speak to the
	// server. So a wrong password, a database that was never created, a role
	// that cannot log in, a port nothing listens on, a host that does not
	// resolve -- every one of them used to be deferred to the first query, and
	// the first query is inside a request. The application booted, said it was
	// listening, and then answered the first visitor with a driver sentence on
	// an error page.
	//
	// A process that cannot reach its database is not up, and saying so at boot
	// is what turns "the site is broken" into a line in the log of the deploy
	// that broke it.
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, nil, fmt.Errorf("connecting to %s: %w", cfg.Redacted(), err)
	}

	return Wrap(sqldb, cfg.Connection), func() { _ = sqldb.Close() }, nil
}

// connectTimeout bounds the check above.
//
// Long enough for a container that is still starting, short enough that a
// deploy pointed at an unreachable host fails while somebody is still watching
// it. Without a bound, a host that drops packets instead of refusing them hangs
// the boot until the operating system gives up, which is minutes.
const connectTimeout = 5 * time.Second

// driverFor resolves the dialect, or explains exactly what is missing.
//
// This error is the shape the whole product aims at: it names the probable
// cause and the command that fixes it. "unknown driver pgx" -- what
// database/sql says on its own -- sends people looking for a typo in .env.
func driverFor(d Dialect) (string, error) {
	// The read lock is taken once, and everything under it stays lock-free.
	//
	// It used to call Registered() from in here, which takes the read lock
	// again. sync.RWMutex is not reentrant: with a writer waiting between the
	// two acquisitions, Go blocks the second reader to keep the writer from
	// starving, and the process deadlocks permanently. Found by audit.
	registryMu.RLock()
	name, found := registry[d]
	dialects := sortedLocked()
	registryMu.RUnlock()

	if found {
		return name, nil
	}

	linked := "none"
	if len(dialects) > 0 {
		linked = ""
		for i, a := range dialects {
			if i > 0 {
				linked += ", "
			}
			linked += string(a)
		}
	}
	return "", fmt.Errorf(
		"DB_CONNECTION is %s and no driver for it is linked into this binary (linked: %s).\n"+
			"Add it:\n\n    go get github.com/arandu-io/database/%s\n\n"+
			"and blank-import it in main.go, next to the other drivers:\n\n    _ \"github.com/arandu-io/database/%s\"",
		d, linked, compartment(d), compartment(d))
}

// compartment is the module name that carries the driver for a dialect.
func compartment(d Dialect) string {
	switch d {
	case DialectPostgres:
		return "pgx"
	case DialectMySQL:
		return "mysql"
	default:
		return "sqlite"
	}
}

// DriverName returns the database/sql driver a compartment registered for a
// dialect, or the error that names the missing import.
//
// It exists for the conformance suite, which has to open a connection with the
// same driver Open would use rather than a name it hardcoded -- a suite testing
// a driver nobody links is a suite testing nothing.
func DriverName(d Dialect) (string, error) { return driverFor(d) }
