package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
)

// viewingGrant is a Grant issued to a customer an operator is only viewing as.
func viewingGrant(t *testing.T, tenant string) auth.Grant {
	t.Helper()
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: tenant, Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: tenant})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowAll{}, viewed, action, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestAViewingGrantWritesNoCacheEntry: the read-only mode of an impersonation
// held only in the database, so an operator viewing a customer's account could
// overwrite, delete and flush the customer's cached state.
func TestAViewingGrantWritesNoCacheEntry(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	if err := r.Put(ctx, grantFor("acme"), "k", "original", time.Hour); err != nil {
		t.Fatal(err)
	}
	g := viewingGrant(t, "acme")
	ran := false
	writes := map[string]func() error{
		"Put":            func() error { return r.Put(ctx, g, "k", "changed", time.Hour) },
		"Put expired":    func() error { return r.Put(ctx, g, "k", "changed", 0) },
		"Set":            func() error { return r.Set(ctx, g, "k", "changed", time.Hour) },
		"PutMany":        func() error { return r.PutMany(ctx, g, map[string]any{"k": "changed"}, time.Hour) },
		"Forever":        func() error { return r.Forever(ctx, g, "k", "changed") },
		"Forget":         func() error { return r.Forget(ctx, g, "k") },
		"Delete":         func() error { return r.Delete(ctx, g, "k") },
		"DeleteMultiple": func() error { return r.DeleteMultiple(ctx, g, "k") },
		"Flush":          func() error { return r.Flush(ctx, g) },
		"Clear":          func() error { return r.Clear(ctx, g) },
		"Add": func() error {
			_, err := r.Add(ctx, g, "new", "v", time.Hour)
			return err
		},
		"Increment": func() error {
			_, err := r.Increment(ctx, g, "n", 1, time.Hour)
			return err
		},
		"Decrement": func() error {
			_, err := r.Decrement(ctx, g, "n", 1, time.Hour)
			return err
		},
		"Touch": func() error {
			_, err := r.Touch(ctx, g, "k", time.Minute)
			return err
		},
		"Pull": func() error {
			_, err := cache.Pull[string](ctx, r, g, "k")
			return err
		},
		"WithoutOverlapping": func() error {
			return r.WithoutOverlapping(ctx, g, "job", func(context.Context) error { ran = true; return nil }, time.Minute, 0)
		},
	}
	for name, write := range writes {
		if err := write(); !errors.Is(err, auth.ErrReadOnly) {
			t.Errorf("%s with a viewing Grant answered %v, want auth.ErrReadOnly", name, err)
		}
	}
	if ran {
		t.Error("WithoutOverlapping ran its callback under a lock a viewing Grant took")
	}
	got, err := cache.Get[string](ctx, r, g, "k")
	if err != nil || got != "original" {
		t.Fatalf("the entry reads %q, %v after the refused writes, want the original", got, err)
	}
	for _, key := range []string{"new", "n"} {
		if has, _ := r.Has(ctx, grantFor("acme"), key); has {
			t.Errorf("a refused write left %q in the store", key)
		}
	}
}

// TestAViewingGrantComputesWithoutStoring: viewing a page that remembers a
// value must keep working, so Remember answers with what it computed, and the
// store is left as it was.
func TestAViewingGrantComputesWithoutStoring(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := viewingGrant(t, "acme")
	got, err := cache.Remember(ctx, r, g, "total", time.Hour, func(context.Context) (int, error) { return 42, nil })
	if err != nil || got != 42 {
		t.Fatalf("Remember answered %d, %v for a viewing Grant, want the computed value", got, err)
	}
	if has, _ := r.Has(ctx, grantFor("acme"), "total"); has {
		t.Fatal("Remember stored what a viewing Grant computed")
	}
}

// TestAViewingGrantMintsNoTagGeneration: the first read through a tag writes
// its generation, so a viewing Grant reading a tagged cache was a write, and
// flushing a tag was one in so many words.
func TestAViewingGrantMintsNoTagGeneration(t *testing.T) {
	ctx := context.Background()
	store := cache.NewArrayStore()
	tagged, err := cache.New(store).Tags("invoices")
	if err != nil {
		t.Fatal(err)
	}
	g := viewingGrant(t, "acme")
	if _, err := cache.Get[string](ctx, tagged.Repository, g, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("a tagged read with a viewing Grant answered %v, want ErrNotFound", err)
	}
	key, err := tagged.GetTags().TagKey(g, "invoices")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("a viewing Grant's read stored a tag generation: %v", err)
	}
	if err := tagged.Flush(ctx, g); !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("Flush of a tag with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
	if err := tagged.GetTags().FlushTag(ctx, g, "invoices"); !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("FlushTag with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
}

// TestAViewingGrantRefreshesNoStaleEntry: a stale flexible entry is refreshed
// behind the reader under a lock, and both the lock and the refresh are
// writes a viewing Grant must not make.
func TestAViewingGrantRefreshesNoStaleEntry(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	owner := grantFor("acme")
	if err := r.Put(ctx, owner, "total", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(ctx, owner, "flexible:created:total", time.Now().Add(-time.Hour).Unix(), time.Hour); err != nil {
		t.Fatal(err)
	}
	computed := make(chan struct{}, 1)
	got, err := cache.Flexible(ctx, r, viewingGrant(t, "acme"), "total", time.Minute, time.Hour, func(context.Context) (int, error) {
		computed <- struct{}{}
		return 2, nil
	})
	if err != nil || got != 1 {
		t.Fatalf("Flexible answered %d, %v for a viewing Grant, want the stale value", got, err)
	}
	select {
	case <-computed:
		t.Fatal("a viewing Grant set off the background refresh of a stale entry")
	case <-time.After(300 * time.Millisecond):
	}
}
