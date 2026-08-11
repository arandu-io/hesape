package pgx_test

import (
	"os"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/conformance"
	_ "github.com/arandu-io/hesape/database/connectors/pgx"
)

// TestImportingRegistersTheDialect is what a blank import has to buy: after it,
// database.Open resolves the dialect without the project knowing a driver name.
//
// There is no connection test here. Reaching a server would need one running,
// and the claim this connector makes is about linking, not about the network
// -- the SQLite connector covers the end-to-end path without installing
// anything.
func TestImportingRegistersTheDialect(t *testing.T) {
	want := database.DialectPostgres

	for _, d := range database.Registered() {
		if d == want {
			return
		}
	}
	t.Fatalf("importing the connector did not register %s: registered = %v", want, database.Registered())
}

// TestConformance runs the shared suite against a real server.
//
// It is the test that would have caught the defect that shipped: every other
// test in this project ran against SQLite, which accepts `id TEXT PRIMARY KEY`.
// See the conformance package for the rest of that story.
//
// To run it: brew services start postgresql@17, then
// create the database once:
//
//	createdb arandu_test
//
// and run:
//
//	ARANDU_TEST_POSTGRES_DSN="postgres://localhost:5432/arandu_test?sslmode=disable" go test ./...
func TestConformance(t *testing.T) {
	conformance.Run(t, database.DialectPostgres, driverName(t), dsn())
}

// driverName is what the connector registered, read back rather than
// hardcoded: if the connector ever registers a different driver, this suite
// follows it instead of silently testing nothing.
func driverName(t *testing.T) string {
	t.Helper()
	name, err := database.DriverName(database.DialectPostgres)
	if err != nil {
		t.Fatalf("the connector did not register a driver: %v", err)
	}
	return name
}

// dsn comes from the environment, and an empty one skips the suite: `go test
// ./...` has to pass on a machine with no server installed.
func dsn() string { return os.Getenv("ARANDU_TEST_POSTGRES_DSN") }
