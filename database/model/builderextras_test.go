package model

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWithAttributesFiltersAndFills(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	q := newQuery(model.base()).WithAttributes(map[string]any{"name": "Ada"})
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
	if created.(*user).Name != "Ada" {
		t.Error("the pending attributes are what a model made off this query starts with")
	}
}

func TestWithAttributesCanSkipTheConditions(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	q := newQuery(model.base()).WithAttributes(map[string]any{"name": "Ada"}, false)
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

func newSavepointModel(level int) (*user, *savepointConnection) {
	inner := newTestConnection()
	conn := &savepointConnection{testConnection: inner, level: level}
	return newUserTable().New(conn).(*user), conn
}

func TestWithSavepointIfNeededOpensOneOnlyInsideATransaction(t *testing.T) {
	model, conn := newSavepointModel(0)

	ran := false
	if err := newQuery(model.base()).WithSavepointIfNeeded(func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithSavepointIfNeeded: %v", err)
	}
	if !ran || conn.wrapped {
		t.Error("a level of zero runs the callback plainly, which is the PHP's else branch")
	}

	model, conn = newSavepointModel(1)
	if err := newQuery(model.base()).WithSavepointIfNeeded(func() error { return nil }); err != nil {
		t.Fatalf("WithSavepointIfNeeded: %v", err)
	}
	if !conn.wrapped {
		t.Error("a level above zero wraps the callback, which is what makes it a savepoint")
	}
}

func TestWithSavepointIfNeededRunsPlainlyOnAConnectionThatCannotSay(t *testing.T) {
	model, _ := newUserModel()

	boom := errors.New("boom")
	if err := newQuery(model.base()).WithSavepointIfNeeded(func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the callback's own", err)
	}
}

// TestGetModelIsThePrototypeTheQueryRunsThrough: a relation reads the table off
// the model a builder runs through, and renames it when it joins the table to
// itself -- so it is one model per builder, the same on every call, and it
// stands for no row.
func TestGetModelIsThePrototypeTheQueryRunsThrough(t *testing.T) {
	model, _ := newUserModel()

	b := newQuery(model.base())
	prototype := b.GetModel()
	if prototype != b.GetModel() {
		t.Error("two calls answered two models, and a rename on the first would not reach the second")
	}
	if !prototype.r.prototype || prototype.Exists() {
		t.Error("the model a query runs through stands for a row")
	}
	if got := tableOf(b.GetQuery().GetFrom()); got != "users" {
		t.Errorf("the query reads %q, want users", got)
	}

	refOf(prototype.base()).SetTable("laravel_reserved_0")
	if got := b.Qualify("id"); got != "laravel_reserved_0.id" {
		t.Errorf("Qualify = %q, want the alias the relation renamed the table to", got)
	}
}
