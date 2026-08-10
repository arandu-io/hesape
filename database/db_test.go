package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/log"
)

func TestQueryIsRecordedWithItsOrigin(t *testing.T) {
	sqldb, _ := newFakeDB()
	defer sqldb.Close()
	db := database.Wrap(sqldb, database.DialectPostgres)

	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE tenant_id = $1`, "t1")
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	rows.Close()

	if col.QueryCount() != 1 {
		t.Fatalf("recorded %d queries, want 1", col.QueryCount())
	}
	q := col.Queries()[0]
	if !strings.Contains(q.SQL, "FROM users") {
		t.Fatalf("recorded SQL = %q", q.SQL)
	}
	if len(q.Args) != 1 || q.Args[0] != "t1" {
		t.Fatalf("recorded args = %v, want [t1]", q.Args)
	}
	// The caller is the point of the whole feature: a query list without file
	// and line does not tell you where the N+1 lives.
	if !strings.HasSuffix(q.Caller.File, "db_test.go") {
		t.Fatalf("caller file = %q, want this test file", q.Caller.File)
	}
	if q.Caller.Line == 0 {
		t.Fatal("caller line was not captured")
	}
}

func TestExecRecordsAffectedRows(t *testing.T) {
	sqldb, _ := newFakeDB()
	defer sqldb.Close()
	db := database.Wrap(sqldb, database.DialectPostgres)

	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, "u1"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}

	if col.QueryCount() != 1 || col.Queries()[0].Rows != 1 {
		t.Fatalf("recorded = %+v, want one record with Rows=1", col.Queries())
	}
}

func TestQueryRecordsTheError(t *testing.T) {
	sqldb, state := newFakeDB()
	defer sqldb.Close()
	state.failOn = "FROM users"
	state.failErr = errors.New("relation \"users\" does not exist")
	db := database.Wrap(sqldb, database.DialectPostgres)

	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	if _, err := db.QueryContext(ctx, `SELECT id FROM users`); err == nil {
		t.Fatal("QueryContext succeeded, want the driver error")
	}

	if col.QueryCount() != 1 || col.Queries()[0].Err == nil {
		t.Fatalf("the failed query must be recorded with its error, got %+v", col.Queries())
	}
}

// TestRecordingIsFreeInProduction is the zero cost claim: with no Collector in
// the context, every Record call must be a no-op on a nil receiver.
func TestRecordingIsFreeInProduction(t *testing.T) {
	sqldb, _ := newFakeDB()
	defer sqldb.Close()
	db := database.Wrap(sqldb, database.DialectPostgres)

	ctx := context.Background()
	if log.FromContext(ctx) != nil {
		t.Fatal("a bare context must carry no Collector")
	}

	rows, err := db.QueryContext(ctx, `SELECT id FROM users`)
	if err != nil {
		t.Fatalf("QueryContext without a Collector: %v", err)
	}
	rows.Close()
}

// TestNewIDIsAUUIDv4: the id comes from the application, never from the
// database, because gen_random_uuid, UUID() and randomblob are three spellings
// of the same idea and depending on any of them ties the schema to one engine.
func TestNewIDIsAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := database.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != 36 {
			t.Fatalf("id = %q, want 36 characters", id)
		}
		if id[14] != '4' {
			t.Fatalf("id = %q, want version 4 in the third group", id)
		}
		if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("id = %q, want variant 10 in the fourth group", id)
		}
		if seen[id] {
			t.Fatalf("NewID repeated %q", id)
		}
		seen[id] = true
	}
}
