package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

func TestWithAttributesFiltersAndFills(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	q := model.NewQuery().WithAttributes(map[string]any{"name": "Ada"})
	if _, err := q.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `"users"."name" = ?`) {
		t.Errorf("sql = %q, want the attribute qualified and used as a condition", sql)
	}

	created, err := q.NewModelInstance(nil)
	if err != nil {
		t.Fatalf("NewModelInstance: %v", err)
	}
	if created.Entity.Name != "Ada" {
		t.Error("the pending attributes are what a model made off this query starts with")
	}
}

func TestWithAttributesCanSkipTheConditions(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	q := model.NewQuery().WithAttributes(map[string]any{"name": "Ada"}, false)
	if _, err := q.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sql := conn.last().SQL; strings.Contains(sql, `"name" = ?`) {
		t.Errorf("sql = %q, want no condition: $asConditions of false keeps the value for the instance only", sql)
	}
}

// savepointConnection is a connection that can say how deep it already is, which
// query.GetConnection() does not declare. See Savepointer.
type savepointConnection struct {
	*testConnection
	level    int
	wrapped  bool
	callback error
}

func (c *savepointConnection) Transaction(callback func() error) error {
	c.wrapped = true
	c.callback = callback()
	return c.callback
}

func (c *savepointConnection) TransactionLevel() int { return c.level }

func newSavepointModel(level int) (*Model[user], *savepointConnection) {
	inner := newTestConnection()
	conn := &savepointConnection{testConnection: inner, level: level}
	model := NewModel[user]("users", conn, newTestGrammar(), &testProcessor{conn: inner})
	return model, conn
}

func TestWithSavepointIfNeededOpensOneOnlyInsideATransaction(t *testing.T) {
	model, conn := newSavepointModel(0)

	ran := false
	if err := model.NewQuery().WithSavepointIfNeeded(func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithSavepointIfNeeded: %v", err)
	}
	if !ran || conn.wrapped {
		t.Error("a level of zero runs the callback plainly, which is the PHP's else branch")
	}

	model, conn = newSavepointModel(1)
	if err := model.NewQuery().WithSavepointIfNeeded(func() error { return nil }); err != nil {
		t.Fatalf("WithSavepointIfNeeded: %v", err)
	}
	if !conn.wrapped {
		t.Error("a level above zero wraps the callback, which is what makes it a savepoint")
	}
}

func TestWithSavepointIfNeededRunsPlainlyOnAConnectionThatCannotSay(t *testing.T) {
	model, _ := newUserModel()

	boom := errors.New("boom")
	if err := model.NewQuery().WithSavepointIfNeeded(func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the callback's own", err)
	}
}

func TestNewTypedBuilderIsWhatNewModelQueryGoesThrough(t *testing.T) {
	model, _ := newUserModel()

	base := model.NewBaseQueryBuilder()
	if got := tableOf(base.GetFrom()); got != "users" {
		t.Errorf("NewBaseQueryBuilder from = %q, want users", got)
	}

	b := model.NewTypedBuilder(base)
	if b.GetModel() != nil {
		t.Error("newTypedBuilder does not set the model: newModelQuery does that after, as there")
	}
	if b.GetQuery() != base {
		t.Error("the builder must be built over the query it was handed")
	}

	var _ *query.Builder = base
}
