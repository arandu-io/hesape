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
	assertModelWritesRefused(t, g, auth.ErrReadOnly)
}

// readsOnly allows everything and says none of its actions write.
type readsOnly struct{ allowEverything }

func (readsOnly) Writes(auth.Action) bool { return false }

// A member holding the Grant a policy issued to view a row -- one that says
// viewing does not write -- saves, updates and deletes nothing through the
// model, and reads with it.
func TestModelWritesRefuseAGrantIssuedForReading(t *testing.T) {
	g, err := auth.Authorize(context.Background(), readsOnly{}, auth.Subject{ID: "bea", Tenant: "acme"}, "users.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertModelWritesRefused(t, g, auth.ErrReadGrant)
}

// assertModelWritesRefused runs every model write with g, wants each refused
// with want before a statement runs, and wants a read with g to run.
func assertModelWritesRefused(t *testing.T, g auth.Grant, want error) {
	t.Helper()
	ctx := context.Background()
	writes := map[string]func(m *user) error{
		"Save": func(m *user) error {
			m.Name = "Ada"
			_, err := m.Save(ctx, g)
			return err
		},
		"Save existing": func(m *user) error {
			if err := existing(m); err != nil {
				return err
			}
			m.Name = "Grace"
			_, err := m.Save(ctx, g)
			return err
		},
		"Delete existing": func(m *user) error {
			if err := existing(m); err != nil {
				return err
			}
			_, err := m.Delete(ctx, g)
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
		if err := write(m); !errors.Is(err, want) {
			t.Errorf("%s answered %v, want %v", name, err, want)
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

// A Grant of an operator acting in an account -- ImpersonateActing -- writes
// through the model like the account's owner would.
func TestModelWritesRunWithAGrantThatActs(t *testing.T) {
	ctx := context.Background()
	acting, err := auth.ImpersonateActing(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(ctx, allowEverything{}, acting, "users.update", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	m, connection := newUserModel()
	if _, err := newQuery(m.base()).Where("id", "=", 1).Update(ctx, g, map[string]any{"name": "Ada"}); err != nil {
		t.Fatalf("an update with an acting grant was refused: %v", err)
	}
	if len(connection.sqls()) != 1 {
		t.Fatalf("the update did not reach the connection: %v", connection.sqls())
	}
}

// existing makes m a row that was read from the database.
func existing(m *user) error {
	if err := m.SetRawAttributes(map[string]any{"id": int64(7), "name": "Ada", "email": "ada@example.com", "tenant_id": "acme"}, true); err != nil {
		return err
	}
	m.r.exists = true
	return nil
}
