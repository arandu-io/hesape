package conformance

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/schema"
)

// The two namespaces the wipe test creates: the one the session searches, and
// the one beside it that the wipe must leave alone. On PostgreSQL they are
// schemas of the suite's database, on MySQL databases of the suite's server.
const (
	wipeSearched = "arandu_conformance_wipe_searched"
	wipeBeside   = "arandu_conformance_wipe_beside"
)

// testAWipeStaysInTheSearchPath is the defect that shipped: DropAllTables
// listed its tables with no schema, which the PostgreSQL grammar reads as every
// non-system schema of the database and the MySQL one as every database the
// account can read, and dropped them all. migrate:fresh on a connection whose
// search_path was one schema emptied every schema beside it -- in an
// application's suite that gives each test a schema of its own, the tables of
// every test running at the same time, which then failed with "relation
// arandu_migrations does not exist".
//
// A session that searches one namespace wipes it, tables and views, and the
// namespace beside it keeps both.
//
// The wipe runs on a pool of one connection opened for it, because a search
// path set with SET belongs to one session and a pool hands each statement to
// whichever connection is free. It never runs on the suite's own connection:
// that one searches the database's default schema, which is somebody's real
// one on a server the suite was pointed at by accident.
func testAWipeStaysInTheSearchPath(t *testing.T, dialect database.Dialect, driverName, dsn string, db *database.DB) {
	ctx := context.Background()

	var scoped *sql.DB
	switch dialect {
	case database.DialectPostgres:
		for _, name := range []string{wipeSearched, wipeBeside} {
			dropNamespace(t, db, dialect, name)
			if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+name); err != nil {
				t.Fatalf("creating schema %s: %v", name, err)
			}
			t.Cleanup(func() { dropNamespace(t, db, dialect, name) })
		}
		scoped = session(t, driverName, dsn, `SET search_path TO `+wipeSearched)
	case database.DialectMySQL:
		for _, name := range []string{wipeSearched, wipeBeside} {
			dropNamespace(t, db, dialect, name)
			if _, err := db.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
				// The CI account is root; an account that may not create a
				// database cannot set this test up, and says so.
				t.Skipf("the account cannot create database %s, which this test needs: %v", name, err)
			}
			t.Cleanup(func() { dropNamespace(t, db, dialect, name) })
		}
		scoped = session(t, driverName, dsn, `USE `+wipeSearched)
	default:
		// SQLite's namespaces are files: main is a file of its own, so the
		// suite's database is never touched, and the one beside it is
		// attached under the name the other engines give their second schema.
		dir := t.TempDir()
		scoped = session(t, driverName, filepath.Join(dir, "searched.sqlite"),
			fmt.Sprintf(`ATTACH DATABASE '%s' AS %s`, filepath.Join(dir, "beside.sqlite"), wipeBeside))
	}

	searched := wipeSearched
	if dialect == database.DialectSQLite {
		searched = "main"
	}
	for _, statement := range []string{
		`CREATE TABLE ` + searched + `.wiped (id INTEGER PRIMARY KEY)`,
		`CREATE VIEW ` + searched + `.wiped_view AS SELECT id FROM ` + searched + `.wiped`,
		`CREATE TABLE ` + wipeBeside + `.kept (id INTEGER PRIMARY KEY)`,
		`CREATE VIEW ` + wipeBeside + `.kept_view AS SELECT id FROM ` + wipeBeside + `.kept`,
	} {
		if _, err := scoped.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	builder := schemaBuilder(dialect, database.Wrap(scoped, dialect))
	path, err := builder.SearchPath(ctx)
	if err != nil {
		t.Fatalf("SearchPath: %v", err)
	}
	if !slices.Equal(path, []string{searched}) {
		t.Fatalf("SearchPath = %q, want [%s]: the session searches one namespace", path, searched)
	}

	if err := builder.DropAllViews(ctx); err != nil {
		t.Fatalf("DropAllViews: %v", err)
	}
	if err := builder.DropAllTables(ctx); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}

	if names := relations(t, builder, searched); len(names) != 0 {
		t.Errorf("the wipe left %q in %s, the namespace the session searches", names, searched)
	}
	if names := relations(t, builder, wipeBeside); !slices.Equal(names, []string{"kept", "kept_view"}) {
		t.Errorf("%s holds %q after the wipe, want [kept kept_view]: a wipe of one namespace reached the one beside it", wipeBeside, names)
	}
}

// testTheDefaultSearchPathStillReachesPublic: scoping the wipe must not stop
// it wiping what it always wiped on a connection nobody configured. On
// PostgreSQL that connection searches "$user", public; "$user" resolves to a
// schema named after the role, which is left out when there is none, and
// public is what remains -- so a table created unqualified is one the wipe
// lists. It is proven on the listing rather than by dropping public, which on
// a shared test database holds tables that are not this suite's.
//
// The resolution of "$user" is asked of a session of its own, with the
// suite's schema behind it, so the answer does not depend on how the server's
// default was configured.
func testTheDefaultSearchPathStillReachesPublic(t *testing.T, dialect database.Dialect, driverName, dsn string, db *database.DB) {
	if dialect != database.DialectPostgres {
		t.Skip("PostgreSQL is the engine with a search path; MySQL's selected database and SQLite's main are asserted by the wipe test")
	}
	ctx := context.Background()
	name := table("default_path")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+name+` (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	var current string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	builder := schemaBuilder(dialect, db)
	path, err := builder.SearchPath(ctx)
	if err != nil {
		t.Fatalf("SearchPath: %v", err)
	}
	if len(path) == 0 || path[0] != current {
		t.Fatalf("SearchPath = %q, want it to begin with %s, the schema an unqualified table is created in", path, current)
	}
	tables, err := builder.GetTables(ctx, path...)
	if err != nil {
		t.Fatalf("GetTables: %v", err)
	}
	if !slices.ContainsFunc(tables, func(table schema.TableInfo) bool { return table.Name == name && table.Schema == current }) {
		t.Fatalf("the wipe's listing over %q does not include %s.%s, a table created unqualified", path, current, name)
	}

	dropNamespace(t, db, dialect, wipeSearched)
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+wipeSearched); err != nil {
		t.Fatalf("creating schema %s: %v", wipeSearched, err)
	}
	t.Cleanup(func() { dropNamespace(t, db, dialect, wipeSearched) })
	scoped := session(t, driverName, dsn, `SET search_path TO "$user", `+wipeSearched)
	var role string
	if err := scoped.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatalf("current_user: %v", err)
	}
	path, err = schemaBuilder(dialect, database.Wrap(scoped, dialect)).SearchPath(ctx)
	if err != nil {
		t.Fatalf("SearchPath: %v", err)
	}
	if slices.Contains(path, "$user") || len(path) == 0 || path[len(path)-1] != wipeSearched {
		t.Fatalf("SearchPath = %q for \"$user\", %s, want $user resolved to %s or left out, then %s", path, wipeSearched, role, wipeSearched)
	}
}

// session opens a pool of exactly one connection and runs setup on it, so the
// session state setup leaves -- a search path, a selected database, an
// attachment -- is the state every later statement runs in.
func session(t *testing.T, driverName, dsn, setup string) *sql.DB {
	t.Helper()
	handle, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatalf("opening a session: %v", err)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0)
	handle.SetConnMaxIdleTime(0)
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.ExecContext(context.Background(), setup); err != nil {
		t.Fatalf("%s: %v", setup, err)
	}
	return handle
}

// relations names the tables and views in one namespace, sorted.
func relations(t *testing.T, builder *schema.Builder, namespace string) []string {
	t.Helper()
	ctx := context.Background()
	tables, err := builder.GetTables(ctx, namespace)
	if err != nil {
		t.Fatalf("GetTables(%s): %v", namespace, err)
	}
	views, err := builder.GetViews(ctx, namespace)
	if err != nil {
		t.Fatalf("GetViews(%s): %v", namespace, err)
	}
	names := make([]string, 0, len(tables)+len(views))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	for _, view := range views {
		names = append(names, view.Name)
	}
	slices.Sort(names)
	return names
}

// dropNamespace removes a schema or database the wipe test created, with
// whatever is still in it. A missing one is not an error: this also runs
// before the create.
func dropNamespace(t *testing.T, db *database.DB, dialect database.Dialect, name string) {
	t.Helper()
	statement := `DROP SCHEMA IF EXISTS ` + name + ` CASCADE`
	if dialect == database.DialectMySQL {
		statement = `DROP DATABASE IF EXISTS ` + name
	}
	_, _ = db.ExecContext(context.Background(), statement)
}
