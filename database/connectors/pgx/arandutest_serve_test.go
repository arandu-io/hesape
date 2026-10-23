package pgx_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/routing"
)

// serveDSN is the server the tests below make their databases on: the one the
// conformance suite runs against, or the test database the kit reads. Empty
// skips them, as it skips the suite, so `go test ./...` passes on a machine
// with no server; CI sets the DSN, and the job checks the suite ran.
func serveDSN(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"ARANDU_TEST_POSTGRES_DSN", "ARANDU_TEST_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(name); strings.HasPrefix(v, "postgres") {
			return v
		}
	}
	t.Skip("no Postgres server: set ARANDU_TEST_POSTGRES_DSN")
	return ""
}

// notesMigrations counts how often the migration below ran in this process.
var notesMigrations atomic.Int32

func notesMigration(ctx context.Context, env arandutest.Env) error {
	notesMigrations.Add(1)
	_, err := env.DB.ExecContext(ctx, "CREATE TABLE arandutest_notes (owner TEXT NOT NULL)")
	return err
}

// notesApp writes a note for the owner a POST names and lists every note it
// can see on a GET.
func notesApp(_ context.Context, env arandutest.Env) (arandutest.Application, error) {
	r := routing.NewRouter()
	r.Post("/notes", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, err := env.DB.ExecContext(req.Context(), "INSERT INTO arandutest_notes (owner) VALUES (?)", req.PostFormValue("owner")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	r.Get("/notes", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rows, err := env.DB.QueryContext(req.Context(), "SELECT owner FROM arandutest_notes ORDER BY owner")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()
		var owners []string
		for rows.Next() {
			var owner string
			if err := rows.Scan(&owner); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			owners = append(owners, owner)
		}
		_, _ = w.Write([]byte("notes: " + strings.Join(owners, ",")))
	}))
	return arandutest.Application{Handler: r, Routes: r.Routes()}, nil
}

// databaseName is the database a connection URL names.
func databaseName(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(u.Path, "/")
}

// openAdmin connects to the database the DSN names, to look at the server
// from outside the kit.
func openAdmin(t *testing.T, dsn string) *database.DB {
	t.Helper()
	cfg, err := database.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db, closeDB, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDB)
	return db
}

func exists(t *testing.T, db *database.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM pg_database WHERE datname = ?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// Two tests running at once each write a row and only ever see their own: on
// Postgres each is a copy of the template, made for it and dropped after it.
func TestServeGivesParallelTestsADatabaseEach(t *testing.T) {
	dsn := serveDSN(t)
	var written sync.WaitGroup
	written.Add(2)
	both := make(chan struct{})
	go func() { written.Wait(); close(both) }()

	for _, owner := range []string{"alice", "bob"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			server := arandutest.Serve(t, notesApp, arandutest.Database(dsn), arandutest.Migrate(notesMigration))
			client := server.Client()

			client.Post("/notes", map[string]string{"owner": owner}).AssertOk()
			written.Done()
			select {
			case <-both:
			case <-time.After(30 * time.Second):
				t.Fatal("the other test never wrote its row")
			}
			client.Get("/notes").AssertSee("notes: " + owner).AssertDontSee(",")
		})
	}
}

// The template is migrated once and copied for every test; each copy is named
// arandutest_..., carries the schema, is dropped when its test ends, and the
// database the URL names is never written to.
func TestServeCopiesOneTemplateAndDropsEveryCopy(t *testing.T) {
	dsn := serveDSN(t)
	admin := openAdmin(t, dsn)
	before := notesMigrations.Load()

	var names []string
	for range 3 {
		t.Run("copy", func(t *testing.T) {
			var name string
			server := arandutest.Serve(t, func(ctx context.Context, env arandutest.Env) (arandutest.Application, error) {
				name = databaseName(t, env.DatabaseURL)
				return notesApp(ctx, env)
			}, arandutest.Database(dsn), arandutest.Migrate(notesMigration))

			if !strings.HasPrefix(name, "arandutest_") {
				t.Fatalf("the test's database is %q, outside the arandutest_ prefix", name)
			}
			if !exists(t, admin, name) {
				t.Fatalf("%s is not on the server while its test runs", name)
			}
			server.Client().Post("/notes", map[string]string{"owner": "run"}).AssertOk()
			server.Client().Get("/notes").AssertSee("notes: run").AssertDontSee(",")
			names = append(names, name)
		})
	}

	if ran := notesMigrations.Load() - before; ran > 1 {
		t.Errorf("the migration ran %d times for three tests, want at most once", ran)
	}
	for _, name := range names {
		if exists(t, admin, name) {
			t.Errorf("%s survived its test", name)
		}
	}
	var table *string
	if err := admin.QueryRowContext(context.Background(), "SELECT to_regclass('public.arandutest_notes')::text").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != nil {
		t.Errorf("the database the URL names, %s, holds the test's table", databaseName(t, dsn))
	}
}
