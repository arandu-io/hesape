package query_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

type allowEverything struct{}

func (allowEverything) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// readOnlyGrant is a Grant issued to a subject an operator is viewing as.
func readOnlyGrant(t *testing.T) auth.Grant {
	t.Helper()
	viewed, err := auth.Impersonate(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowEverything{}, viewed, "invoice.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// Every statement that writes refuses a Grant that only reads before the
// connection is reached; a read with the same Grant runs.
func TestEveryWriteRefusesAGrantThatOnlyReads(t *testing.T) {
	ctx := context.Background()
	g := readOnlyGrant(t)
	writes := map[string]func(c *fakeConnection) error{
		"Insert": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).Insert(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"InsertOrIgnore": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).InsertOrIgnore(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"InsertGetID": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).InsertGetID(ctx, g, map[string]any{"name": "Ada"}, "")
			return err
		},
		"InsertUsing": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).InsertUsing(ctx, g, []any{"name"}, newTestBuilder(c).Select("name"))
			return err
		},
		"InsertOrIgnoreReturning": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).InsertOrIgnoreReturning(ctx, g, []map[string]any{{"name": "Ada"}}, []string{"name"}, []string{"id"})
			return err
		},
		"Update": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).Where("id", "=", 1).Update(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"UpdateOrInsert": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).UpdateOrInsert(ctx, g, map[string]any{"id": 1}, map[string]any{"name": "Ada"})
			return err
		},
		"Upsert": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).Upsert(ctx, g, []map[string]any{{"id": 1, "name": "Ada"}}, []string{"id"}, nil)
			return err
		},
		"Increment": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).Where("id", "=", 1).Increment(ctx, g, "votes", 1, nil)
			return err
		},
		"Delete": func(c *fakeConnection) error {
			_, err := newTestBuilder(c).Delete(ctx, g, 1)
			return err
		},
		"Truncate": func(c *fakeConnection) error {
			return newTestBuilder(c).Truncate(ctx, g)
		},
	}
	for name, write := range writes {
		connection := &fakeConnection{inserted: true, affected: 1}
		err := write(connection)
		if !errors.Is(err, auth.ErrReadOnly) {
			t.Errorf("%s answered %v, want ErrReadOnly", name, err)
		}
		for _, call := range connection.calls {
			if call.kind != "select" {
				t.Errorf("%s reached the connection with a %s: %s", name, call.kind, call.sql)
			}
		}
	}

	connection := &fakeConnection{}
	if _, err := newTestBuilder(connection).Where("id", "=", 1).Get(ctx, g); err != nil {
		t.Fatalf("a read with the same grant was refused: %v", err)
	}
	if len(connection.calls) != 1 {
		t.Fatalf("the read did not run: %v", connection.calls)
	}
}
