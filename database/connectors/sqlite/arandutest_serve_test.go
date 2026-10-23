package sqlite_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest"
	"github.com/arandu-io/hesape/routing"
)

// notesMigration is the schema of the application below: one table.
func notesMigration(ctx context.Context, env arandutest.Env) error {
	_, err := env.DB.ExecContext(ctx, "CREATE TABLE notes (owner TEXT NOT NULL)")
	return err
}

// notesApp writes a note for the owner a POST names and lists every note it
// can see on a GET: what a test needs to tell its own rows from another's.
func notesApp(_ context.Context, env arandutest.Env) (arandutest.Application, error) {
	r := routing.NewRouter()
	r.Post("/notes", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, err := env.DB.ExecContext(req.Context(), "INSERT INTO notes (owner) VALUES (?)", req.PostFormValue("owner")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	r.Get("/notes", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rows, err := env.DB.QueryContext(req.Context(), "SELECT owner FROM notes ORDER BY owner")
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

// Two tests running at once each write a row and only ever see their own: each
// has a database of its own, on SQLite a file of its own. The barrier holds
// both between the write and the read, so the two runs overlap whatever the
// scheduler does.
func TestServeGivesParallelTestsADatabaseEach(t *testing.T) {
	var written sync.WaitGroup
	written.Add(2)
	both := make(chan struct{})
	go func() { written.Wait(); close(both) }()

	for _, owner := range []string{"alice", "bob"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			server := arandutest.Serve(t, notesApp, arandutest.SQLite(), arandutest.Migrate(notesMigration))
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

// The file goes with the test: what one run wrote is not there for the next.
func TestServeRemovesTheSQLiteFileWhenTheTestEnds(t *testing.T) {
	var path string
	t.Run("serving", func(t *testing.T) {
		arandutest.Serve(t, func(ctx context.Context, env arandutest.Env) (arandutest.Application, error) {
			path = strings.TrimPrefix(env.DatabaseURL, "sqlite://")
			return notesApp(ctx, env)
		}, arandutest.SQLite(), arandutest.Migrate(notesMigration)).Client().
			Post("/notes", map[string]string{"owner": "alice"}).AssertOk()
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the test database is not at %s: %v", path, err)
		}
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s survived the test (%v)", path, err)
	}
}
