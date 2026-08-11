package database

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

// The tests are in the package rather than beside it, because the registry is
// package state and resetting it between cases is the only way these stay
// independent of each other's order.

// testConnector is a Connector reduced to the two things Register reads. The
// real ones are modules of their own under connectors/, and importing one here
// would put its driver in this module's go.sum -- which is the arrangement
// these tests exist to keep working.
type testConnector struct {
	dialect Dialect
	driver  string
}

func (c testConnector) Dialect() Dialect   { return c.dialect }
func (c testConnector) DriverName() string { return c.driver }

func reset(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	registry = map[Dialect]string{}
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = map[Dialect]string{}
		registryMu.Unlock()
	})
}

func TestOpenUsesTheRegisteredDriver(t *testing.T) {
	reset(t)
	Register(testConnector{DialectPostgres, "pgx"})

	var opened string
	swap(t, func(driverName, dsn string) (*sql.DB, error) {
		opened = driverName
		return sql.Open("arandu-open-test", dsn)
	})

	db, closeDB, err := Open(Config{
		Connection: DialectPostgres,
		Host:       "localhost", Port: "5432",
		Username: "u", Password: "p", Database: "app",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	if opened != "pgx" {
		t.Errorf("opened with %q, want the registered driver", opened)
	}
	if db.Dialect() != DialectPostgres {
		t.Errorf("dialect = %q", db.Dialect())
	}
}

// TestAMissingDriverSaysWhichOneAndHow is the error this package exists to
// produce. What database/sql says on its own is `unknown driver "pgx"`, which
// sends people looking for a typo in .env -- the problem is a missing import,
// and nothing in that message says so.
func TestAMissingDriverSaysWhichOneAndHow(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "sqlite"})

	_, _, err := Open(Config{
		Connection: DialectPostgres,
		Host:       "localhost", Port: "5432", Username: "u", Database: "app",
	})
	if err == nil {
		t.Fatal("a dialect with no driver opened")
	}

	message := err.Error()
	for _, want := range []string{
		"pgsql",  // what is configured
		"sqlite", // what is linked, so the gap is visible
		"go get github.com/arandu-io/hesape/database/connectors/pgx", // the command that fixes it
		// And where the import goes. It said cmd/app/main.go, a file the
		// skeleton does not have -- ADR 0019 moved the entrypoint to main.go
		// in the root. The message sent whoever hit it to look for a
		// directory that is not there, and this test pinned the wrong string
		// rather than catching it.
		"blank-import it in main.go",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q:\n%s", want, message)
		}
	}
}

// TestTheErrorIsUsefulWithNothingLinked: the first run of a project that forgot
// every import must not produce an error about an empty list.
func TestTheErrorIsUsefulWithNothingLinked(t *testing.T) {
	reset(t)

	_, _, err := Open(Config{Connection: DialectSQLite, Database: t.TempDir() + "/app.sqlite"})
	if err == nil {
		t.Fatal("opened with no driver linked at all")
	}
	if !strings.Contains(err.Error(), "linked: none") {
		t.Errorf("the error does not say that nothing is linked:\n%s", err)
	}
	if !strings.Contains(err.Error(), "database/connectors/sqlite") {
		t.Errorf("the error does not name the connector to install:\n%s", err)
	}
}

// TestTwoDriversForOneDialectPanics: it is an import nobody meant to add, and
// finding out at boot beats finding out from a query that behaves differently.
func TestTwoDriversForOneDialectPanics(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "sqlite"})

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a second driver for the same dialect was accepted")
		}
		if !strings.Contains(recovered.(string), "remove one of the imports") {
			t.Errorf("the panic does not say what to do: %v", recovered)
		}
	}()
	Register(testConnector{DialectSQLite, "sqlite3"})
}

// TestRegisteringTheSameDriverTwiceIsFine: a module imported through two paths
// initialises once per path in some build layouts, and panicking there would
// break a build for no reason.
func TestRegisteringTheSameDriverTwiceIsFine(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "sqlite"})
	Register(testConnector{DialectSQLite, "sqlite"})

	if got := Registered(); len(got) != 1 {
		t.Fatalf("registered = %v, want one entry", got)
	}
}

func TestRegisteredIsSorted(t *testing.T) {
	reset(t)
	Register(testConnector{DialectPostgres, "pgx"})
	Register(testConnector{DialectMySQL, "mysql"})
	Register(testConnector{DialectSQLite, "sqlite"})

	got := Registered()
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("registered = %v, want sorted so two runs report the same thing", got)
		}
	}
}

// TestSQLiteGetsOneWriter: SQLite serializes writes anyway, and a larger pool
// only converts the wait into "database is locked" -- which reads like
// corruption and is really a pool setting.
func TestSQLiteGetsOneWriter(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "arandu-open-test"})
	swap(t, sql.Open)

	db, closeDB, err := Open(Config{
		Connection: DialectSQLite,
		Database:   t.TempDir() + "/nested/app.sqlite",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	if got := db.Unwrap().Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
}

// TestTheServerPoolIsBounded: the default in database/sql is unlimited, which
// turns one traffic spike into "too many connections" on the server rather than
// a queue in this process.
func TestTheServerPoolIsBounded(t *testing.T) {
	reset(t)
	Register(testConnector{DialectPostgres, "arandu-open-test"})
	swap(t, sql.Open)

	db, closeDB, err := Open(Config{
		Connection: DialectPostgres,
		Host:       "localhost", Port: "5432", Username: "u", Database: "app",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeDB()

	if got := db.Unwrap().Stats().MaxOpenConnections; got <= 1 || got > 100 {
		t.Errorf("MaxOpenConnections = %d, want a bounded pool above one", got)
	}
}

// TestTheSQLiteDirectoryIsCreated: SQLite creates the file and never the
// directory above it, and the error it gives for a missing one names neither.
func TestTheSQLiteDirectoryIsCreated(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "arandu-open-test"})
	swap(t, sql.Open)

	_, closeDB, err := Open(Config{
		Connection: DialectSQLite,
		Database:   t.TempDir() + "/a/b/c/app.sqlite",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	closeDB()
}

func swap(t *testing.T, fn func(string, string) (*sql.DB, error)) {
	t.Helper()
	previous := sqlOpen
	sqlOpen = fn
	t.Cleanup(func() { sqlOpen = previous })
}

// TestOpenDoesNotDeadlockAgainstRegister is a permanent deadlock an audit found.
//
// driverFor took the read lock and then called Registered, which takes it
// again. sync.RWMutex is not reentrant: with a writer waiting between the two
// acquisitions, Go blocks the second reader so the writer does not starve, and
// the process hangs forever with no error and no log line.
//
// The window is small -- Register runs from init() and Open runs after -- which
// is exactly why it would have been found in production and not in a test.
func TestOpenDoesNotDeadlockAgainstRegister(t *testing.T) {
	reset(t)
	Register(testConnector{DialectSQLite, "arandu-open-test"})
	swap(t, sql.Open)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Writers and readers interleaving, which is what a plugin registering
		// late while requests are already flowing looks like.
		for i := range 200 {
			if i%2 == 0 {
				registryMu.Lock()
				registry[DialectMySQL] = "mysql"
				delete(registry, DialectMySQL)
				registryMu.Unlock()
				continue
			}
			// The dialect that is NOT registered, so it walks the error path --
			// which is where the second acquisition was.
			_, _, _ = Open(Config{
				Connection: DialectPostgres,
				Host:       "h", Port: "1", Username: "u", Database: "d",
			})
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Open deadlocked against Register")
	}
}

// A process that cannot reach its database is not up.
//
// sql.Open resolves the driver and builds a pool without speaking to the server,
// so every connection failure -- a wrong password, a database nobody created, a
// port nothing listens on -- used to be deferred to the first query. The
// application booted, logged that it was listening, and answered the first
// visitor with a driver sentence on an error page. The deploy that broke it had
// already been declared successful.
//
// Driven through the sqlOpen seam rather than a socket: what is under test is
// that Open refuses, and standing up three servers to prove it would mean this
// never runs in CI.
func TestOpenRefusesADatabaseItCannotReach(t *testing.T) {
	reset(t)
	Register(testConnector{DialectPostgres, "unreachable-probe"})
	sql.Register("unreachable-probe", unreachableDriver{})

	original := sqlOpen
	t.Cleanup(func() { sqlOpen = original })

	cfg := Config{
		Connection: DialectPostgres,
		Host:       "db.internal", Port: "5432",
		Database: "shop", Username: "shop", Password: "hunter2",
	}

	db, closeFn, err := Open(cfg)
	if err == nil {
		if closeFn != nil {
			closeFn()
		}
		t.Fatalf("Open answered a usable handle for a database it cannot reach: %v", db)
	}
	if !strings.Contains(err.Error(), "connecting to") {
		t.Errorf("the error does not say what failed: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error carries the password, and this string reaches a log: %v", err)
	}
}

// unreachableDriver connects to nothing. database/sql calls Connect only when
// somebody asks for a connection, which before this change was the first query
// inside the first request.
type unreachableDriver struct{}

func (unreachableDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New(`dial tcp 10.0.0.9:5432: connect: connection refused`)
}
