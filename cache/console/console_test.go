package console_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	cacheconsole "github.com/arandu-io/hesape/cache/console"
	"github.com/arandu-io/hesape/console"
)

const action auth.Action = "cache:clear"

func newManager() *cache.CacheManager {
	return cache.NewCacheManager(cache.Config{
		Default: "memory",
		Stores:  map[string]cache.StoreConfig{"memory": {Driver: "array"}},
	})
}

// run builds the terminal a command is given and returns what it wrote.
func run(t *testing.T, cmd console.Command, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	io := console.NewIO(cmd.Name, args, &out, &out, nil)
	err := cmd.Run(context.Background(), io)
	return out.String(), err
}

func TestClearEmptiesOneTenant(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	acme, globex := auth.SystemGrant(action, "acme"), auth.SystemGrant(action, "globex")
	for _, g := range []auth.Grant{acme, globex} {
		if err := store.Put(ctx, g, "k", "v", time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	out, err := run(t, cacheconsole.NewClearCommand(m).Command(), "-tenant", "acme")
	if err != nil {
		t.Fatalf("cache:clear: %v", err)
	}
	if !strings.Contains(out, "cleared successfully") {
		t.Fatalf("cache:clear said %q", out)
	}

	if _, err := cache.Get[string](ctx, store, acme, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the tenant's entry = %v, want cache.ErrNotFound", err)
	}
	if _, err := cache.Get[string](ctx, store, globex, "k"); err != nil {
		t.Fatalf("another tenant's entry went with it: %v", err)
	}
}

// TestClearRefusesWithoutATenant is RULE 14 at the command line: a cache:clear
// that emptied the store would clear every other customer on the way past.
func TestClearRefusesWithoutATenant(t *testing.T) {
	m := newManager()

	_, err := run(t, cacheconsole.NewClearCommand(m).Command())
	if err == nil {
		t.Fatal("cache:clear with no tenant = nil, want a refusal")
	}
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cache:clear with no tenant = %v, want auth.ErrForbidden", err)
	}
}

func TestClearLocksNeedsNoTenantAndKeepsTheEntries(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	g := auth.SystemGrant(action, "acme")
	if err := store.Put(ctx, g, "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := run(t, cacheconsole.NewClearCommand(m).Command(), "-locks"); err != nil {
		t.Fatalf("cache:clear --locks: %v", err)
	}
	if _, err := cache.Get[string](ctx, store, g, "k"); err != nil {
		t.Fatalf("clearing the locks took an entry with it: %v", err)
	}
}

func TestClearRefusesTagsWithLocks(t *testing.T) {
	m := newManager()

	_, err := run(t, cacheconsole.NewClearCommand(m).Command(), "-locks", "-tags", "invoices")
	if err == nil {
		t.Fatal("cache:clear --locks --tags = nil, want a refusal: a lock has no tags")
	}
}

func TestClearWithTagsOrphansOnlyThoseEntries(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	g := auth.SystemGrant(action, "acme")

	tagged, err := store.Tags("invoices")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if err := tagged.Put(ctx, g, "k", "tagged", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, g, "k", "untagged", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := run(t, cacheconsole.NewClearCommand(m).Command(), "-tenant", "acme", "-tags", "invoices"); err != nil {
		t.Fatalf("cache:clear --tags: %v", err)
	}

	if _, err := cache.Get[string](ctx, tagged.Repository, g, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the tagged entry = %v, want cache.ErrNotFound", err)
	}
	got, err := cache.Get[string](ctx, store, g, "k")
	if err != nil {
		t.Fatalf("the untagged entry went with it: %v", err)
	}
	if got != "untagged" {
		t.Fatalf("the untagged entry = %q, want %q", got, "untagged")
	}
}

func TestForgetRemovesOneKey(t *testing.T) {
	ctx := context.Background()
	m := newManager()

	store, err := m.Store("memory")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	g := auth.SystemGrant(action, "acme")
	if err := store.Put(ctx, g, "gone", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, g, "kept", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out, err := run(t, cacheconsole.NewForgetCommand(m).Command(), "-tenant", "acme", "gone")
	if err != nil {
		t.Fatalf("cache:forget: %v", err)
	}
	if !strings.Contains(out, "gone") {
		t.Fatalf("cache:forget did not name the key: %q", out)
	}

	if _, err := cache.Get[string](ctx, store, g, "gone"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the key = %v, want cache.ErrNotFound", err)
	}
	if _, err := cache.Get[string](ctx, store, g, "kept"); err != nil {
		t.Fatalf("another key went with it: %v", err)
	}
}

func TestForgetNeedsAKey(t *testing.T) {
	m := newManager()

	if _, err := run(t, cacheconsole.NewForgetCommand(m).Command(), "-tenant", "acme"); err == nil {
		t.Fatal("cache:forget with no key = nil, want a refusal")
	}
}

func TestPruneStaleTagsSaysThereIsNothingToPrune(t *testing.T) {
	m := newManager()

	out, err := run(t, cacheconsole.NewPruneStaleTagsCommand(m).Command())
	if err != nil {
		t.Fatalf("cache:prune-stale-tags: %v", err)
	}
	if !strings.Contains(out, "nothing to prune") {
		t.Fatalf("cache:prune-stale-tags said %q, and a store with no stale tags should say so", out)
	}
}

func TestCacheTableWritesTheMigration(t *testing.T) {
	dir := t.TempDir()

	out, err := run(t, cacheconsole.NewCacheTableCommand(dir).Command())
	if err != nil {
		t.Fatalf("make:cache-table: %v", err)
	}
	if !strings.Contains(out, "Migration created") {
		t.Fatalf("make:cache-table said %q", out)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files written, want 1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "_create_cache_table.sql") {
		t.Fatalf("the migration is called %q", entries[0].Name())
	}

	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, want := range []string{"CREATE TABLE cache ", "CREATE TABLE cache_locks", "expiration"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the migration does not contain %q", want)
		}
	}
}
