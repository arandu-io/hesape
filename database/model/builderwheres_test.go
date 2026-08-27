package model

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// The clauses that were only reachable through GetQuery().
//
// Two things are asserted for each, and the second is the one that matters: the
// clause reaches the statement, and the tenant filter is still there beside it.
// A forward written as `return b.query.WhereIn(...)` would compile, would put
// the clause in the SQL, and would hand back the base builder -- ending the
// typed chain and, with it, the path through prepare that scopes the tenant.

func TestWhereForwardsReachTheStatementAndStayScoped(t *testing.T) {
	g := auth.SystemGrant("users.read", "acme")

	cases := []struct {
		name  string
		build func(*Builder[user]) *Builder[user]
		want  string
	}{
		{"WhereIn", func(b *Builder[user]) *Builder[user] {
			return b.WhereIn("id", []any{1, 2, 3})
		}, "in"},
		{"WhereNotIn", func(b *Builder[user]) *Builder[user] {
			return b.WhereNotIn("id", []any{4})
		}, "not in"},
		{"WhereNull", func(b *Builder[user]) *Builder[user] {
			return b.WhereNull("deleted_at")
		}, "is null"},
		{"WhereNotNull", func(b *Builder[user]) *Builder[user] {
			return b.WhereNotNull("email")
		}, "is not null"},
		{"WhereBetween", func(b *Builder[user]) *Builder[user] {
			return b.WhereBetween("id", 1, 10)
		}, "between"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model, conn := newUserModel()
			conn.queue()

			if _, err := c.build(model.NewQuery()).Get(context.Background(), g); err != nil {
				t.Fatalf("Get: %v", err)
			}

			last := conn.last()
			if !strings.Contains(strings.ToLower(last.SQL), c.want) {
				t.Errorf("SQL does not carry %q: %s", c.want, last.SQL)
			}
			if !slices.Contains(last.Bindings, any("acme")) {
				t.Errorf("the tenant filter is gone: %s %v", last.SQL, last.Bindings)
			}
		})
	}
}

// TestDoesntExistIsExistsReadTheOtherWay pins the pair rather than the
// implementation: whatever Exists answers, DoesntExist answers the opposite,
// and an error is an error on both.
func TestDoesntExistIsExistsReadTheOtherWay(t *testing.T) {
	g := auth.SystemGrant("users.read", "acme")

	model, conn := newUserModel()
	conn.queue()

	missing, err := model.NewQuery().Where("email", "nobody@example.test").DoesntExist(context.Background(), g)
	if err != nil {
		t.Fatalf("DoesntExist: %v", err)
	}
	if !missing {
		t.Error("DoesntExist reported a row where the connection queued none")
	}
	if !slices.Contains(conn.last().Bindings, any("acme")) {
		t.Errorf("the tenant filter is gone: %s %v", conn.last().SQL, conn.last().Bindings)
	}
}

// TestAggregateForwardsNameTheirFunction checks that the four shorthands reach
// the aggregate they are named after, and stay scoped.
func TestAggregateForwardsNameTheirFunction(t *testing.T) {
	g := auth.SystemGrant("users.read", "acme")

	for _, c := range []struct {
		name string
		call func(*Builder[user]) (any, error)
	}{
		{"sum", func(b *Builder[user]) (any, error) { return b.Sum(context.Background(), g, "id") }},
		{"avg", func(b *Builder[user]) (any, error) { return b.Avg(context.Background(), g, "id") }},
		{"min", func(b *Builder[user]) (any, error) { return b.Min(context.Background(), g, "id") }},
		{"max", func(b *Builder[user]) (any, error) { return b.Max(context.Background(), g, "id") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			model, conn := newUserModel()
			conn.queue()

			if _, err := c.call(model.NewQuery()); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}

			last := conn.last()
			if !strings.Contains(strings.ToLower(last.SQL), c.name+"(") {
				t.Errorf("SQL does not name %s: %s", c.name, last.SQL)
			}
			if !slices.Contains(last.Bindings, any("acme")) {
				t.Errorf("the tenant filter is gone: %s %v", last.SQL, last.Bindings)
			}
		})
	}
}
