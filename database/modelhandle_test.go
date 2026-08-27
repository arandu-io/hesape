package database_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/log"
)

// The claim this file exists for, made where the compiler can check it: the
// handle a module constructor is handed is a model connection. Nothing wraps
// it, nothing unwraps it, and there is no second type to keep instrumented.
var _ model.DB = (*database.DB)(nil)

// Widget is a module's row.
type Widget struct {
	ID     string `db:"id"`
	Status string `db:"status"`
}

// widgetRepository is the module-shaped consumer: it holds exactly what a
// module constructor is given and nothing else.
type widgetRepository struct{ db *database.DB }

func newWidgetRepository(db *database.DB) *widgetRepository {
	return &widgetRepository{db: db}
}

// Open reads through the Model, off the handle the constructor received.
func (r *widgetRepository) Open(ctx context.Context, g auth.Grant) (model.Collection[Widget], error) {
	return model.Query[Widget](r.db).Where("status", "=", "open").Get(ctx, g)
}

// TestAModuleReadsThroughTheModelOnTheHandleItWasGiven is the whole of Task 2
// measured at once: the query compiles against the handle a constructor takes,
// the tenant on the Grant reaches the SQL, the row comes back hydrated, and
// the statement is on the Collector with its values.
func TestAModuleReadsThroughTheModelOnTheHandleItWasGiven(t *testing.T) {
	handle, state := newFakeDB()
	db := database.Wrap(handle, database.DialectPostgres)
	state.answer([]string{"id", "status"}, []driver.Value{"w-1", "open"})

	collector := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), collector)
	g := auth.SystemGrant("widget.list", "acme")

	widgets, err := newWidgetRepository(db).Open(ctx, g)
	if err != nil {
		t.Fatalf("reading through the model: %v", err)
	}
	if len(widgets) != 1 || widgets[0].ID != "w-1" || widgets[0].Status != "open" {
		t.Fatalf("the row came back as %+v", widgets)
	}
	t.Logf("ROW            : %+v", *widgets[0])

	queries := collector.Queries()
	if len(queries) != 1 {
		t.Fatalf("the Collector holds %d statements, want 1", len(queries))
	}
	t.Logf("COLLECTOR SQL  : %s", queries[0].SQL)
	t.Logf("COLLECTOR ARGS : %v", queries[0].Args)
	t.Logf("COLLECTOR CALLER: %s:%d", queries[0].Caller.File, queries[0].Caller.Line)

	if !strings.Contains(queries[0].SQL, `"tenant_id"`) {
		t.Fatalf("the statement carries no tenant filter: %s", queries[0].SQL)
	}
	if !strings.Contains(queries[0].SQL, "$1") || strings.Contains(queries[0].SQL, "?") {
		t.Fatalf("the statement was not renumbered for Postgres: %s", queries[0].SQL)
	}

	tenant := false
	for _, arg := range queries[0].Args {
		if arg == auth.Tenant(g) {
			tenant = true
		}
	}
	if !tenant {
		t.Fatalf("the tenant is not among the values: %v", queries[0].Args)
	}
}

// TestAModelStatementJoinsAnOpenTransaction: the read has to run on the
// transaction the module opened, not beside it. The two are indistinguishable
// from the SQL, so this counts them at the driver, where the pool has already
// decided which connection each one got.
func TestAModelStatementJoinsAnOpenTransaction(t *testing.T) {
	handle, state := newFakeDB()
	db := database.Wrap(handle, database.DialectSQLite)
	state.answer([]string{"id", "status"}, []driver.Value{"w-1", "open"})

	collector := log.NewCollector("req-2")
	ctx := log.WithCollector(context.Background(), collector)
	repository := newWidgetRepository(db)

	err := database.Transaction(ctx, db, func(ctx context.Context) error {
		_, err := repository.Open(ctx, auth.SystemGrant("widget.list", "acme"))
		return err
	})
	if err != nil {
		t.Fatalf("the transaction: %v", err)
	}

	joined, beside := state.statementsOnTransaction()
	t.Logf("DRIVER         : joined=%d beside=%d, Collector holds %d", joined, beside, len(collector.Queries()))
	if joined != 1 || beside != 0 {
		t.Fatalf("the model statement did not join the transaction: joined=%d beside=%d", joined, beside)
	}
	if len(collector.Queries()) != 1 {
		t.Fatalf("the Collector holds %d statements inside the transaction, want 1", len(collector.Queries()))
	}
}

// TestAModelWriteRunsThroughTheHandle covers the other four verbs, which is
// where the recording is easiest to lose: an insert, an update and a delete
// each reach the driver through a different method.
func TestAModelWriteRunsThroughTheHandle(t *testing.T) {
	handle, state := newFakeDB()
	db := database.Wrap(handle, database.DialectPostgres)
	state.answer([]string{"id"}, []driver.Value{"w-1"})

	collector := log.NewCollector("req-3")
	ctx := log.WithCollector(context.Background(), collector)
	g := auth.SystemGrant("widget.write", "acme")

	q := model.Query[Widget](db)
	if _, err := q.Insert(ctx, g, map[string]any{"id": "w-1", "status": "open"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := model.Query[Widget](db).Where("id", "=", "w-1").
		Update(ctx, g, map[string]any{"status": "closed"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := model.Query[Widget](db).Where("id", "=", "w-1").Delete(ctx, g); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	queries := collector.Queries()
	if len(queries) != 3 {
		t.Fatalf("the Collector holds %d statements, want 3", len(queries))
	}
	for _, record := range queries {
		t.Logf("COLLECTOR SQL  : %s  %v", record.SQL, record.Args)
		if strings.Contains(record.SQL, "?") {
			t.Fatalf("the statement reached the driver unbound: %s", record.SQL)
		}
		if !strings.Contains(record.SQL, `"tenant_id"`) {
			t.Fatalf("the statement carries no tenant: %s", record.SQL)
		}
	}
}

// TestTheHandleRefusesToReportAnInsertedIdentifier: a DB is the pool, so it
// carries no "identifier of the last insert" -- on a pool that is whoever
// inserted most recently, which is one request handed another request's row.
// A caller that asks anyway is told what is missing rather than given a zero.
func TestTheHandleRefusesToReportAnInsertedIdentifier(t *testing.T) {
	handle, _ := newFakeDB()
	db := database.Wrap(handle, database.DialectSQLite)

	ctx := context.Background()
	g := auth.SystemGrant("widget.write", "acme")

	_, err := model.Query[Widget](db).
		InsertGetID(ctx, g, map[string]any{"status": "open"}, "id")
	if err == nil {
		t.Fatal("InsertGetID reported an identifier off a pool")
	}
	t.Logf("REFUSAL        : %v", err)
	if !strings.Contains(err.Error(), "GetLastInsertID") {
		t.Fatalf("the refusal does not name what is missing: %v", err)
	}
}
