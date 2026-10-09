package model

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

type allowEverything struct{}

func (allowEverything) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// A model write with a Grant issued to a subject an operator is viewing as is
// refused before any statement runs -- a save, an update, a delete, a forced
// delete, an upsert, an increment -- and a read with it runs.
func TestModelWritesRefuseAGrantThatOnlyReads(t *testing.T) {
	ctx := context.Background()
	viewed, err := auth.Impersonate(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(ctx, allowEverything{}, viewed, "users.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	writes := map[string]func(m *user) error{
		"Save": func(m *user) error {
			m.Name = "Ada"
			_, err := m.Save(ctx, g)
			return err
		},
		"Create": func(m *user) error {
			_, err := newQuery(m.base()).Create(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"Update": func(m *user) error {
			_, err := newQuery(m.base()).Where("id", "=", 1).Update(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"Upsert": func(m *user) error {
			_, err := newQuery(m.base()).Upsert(ctx, g, []map[string]any{{"id": 1, "name": "Ada"}}, []string{"id"}, nil)
			return err
		},
		"Increment": func(m *user) error {
			_, err := newQuery(m.base()).Where("id", "=", 1).Increment(ctx, g, "id", 1, nil)
			return err
		},
		"InsertOrIgnore": func(m *user) error {
			_, err := newQuery(m.base()).InsertOrIgnore(ctx, g, map[string]any{"name": "Ada"})
			return err
		},
		"Delete": func(m *user) error {
			_, err := newQuery(m.base()).Where("id", "=", 1).Delete(ctx, g)
			return err
		},
		"ForceDelete": func(m *user) error {
			_, err := newQuery(m.base()).Where("id", "=", 1).ForceDelete(ctx, g)
			return err
		},
	}
	for name, write := range writes {
		m, connection := newUserModel(softDeletes)
		if err := write(m); !errors.Is(err, auth.ErrReadOnly) {
			t.Errorf("%s answered %v, want ErrReadOnly", name, err)
		}
		if statements := connection.sqls(); len(statements) != 0 {
			t.Errorf("%s reached the connection: %v", name, statements)
		}
	}

	m, connection := newUserModel()
	if _, err := newQuery(m.base()).Where("id", "=", 1).Get(ctx, g); err != nil {
		t.Fatalf("a read with the same grant was refused: %v", err)
	}
	if len(connection.sqls()) != 1 {
		t.Fatalf("the read did not run: %v", connection.sqls())
	}
}
