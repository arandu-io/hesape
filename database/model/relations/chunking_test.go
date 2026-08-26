package relations_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/pagination"
)

// seedManyRoles gives user 1 six roles in acme and two in another tenant, so a
// walk can be counted and a leak can be seen.
func seedManyRoles() (*db, *model) {
	database := newDB()
	user := newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"})

	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		database.seed("roles", map[string]any{"id": id, "tenant_id": "acme"})
		database.seed("role_user", map[string]any{"user_id": "1", "role_id": id, "tenant_id": "acme"})
	}

	// Another customer's rows, on the same two tables, reachable by the same
	// join. Every assertion below that counts to six is also asserting these
	// eight rows were never seen.
	for _, id := range []string{"y", "z"} {
		database.seed("roles", map[string]any{"id": id, "tenant_id": "other"})
		database.seed("role_user", map[string]any{"user_id": "1", "role_id": id, "tenant_id": "other"})
	}

	return database, user
}

func rolesSeenBy(models []relations.Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.GetAttribute("id").(string))
	}
	return out
}

// TestBelongsToManyChunkWalksEveryRowOnce is the property a chunk has to have
// and the one an unordered or unlimited page silently breaks: every row exactly
// once, and the walk ends.
func TestBelongsToManyChunkWalksEveryRowOnce(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var seen []string
	pages := 0

	finished, err := rolesOf(database, user).Chunk(ctx, g, 2, func(results []relations.Model, page int) bool {
		pages++
		seen = append(seen, rolesSeenBy(results)...)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !finished {
		t.Error("Chunk reported it was stopped, but no callback answered false")
	}

	if got := len(seen); got != 6 {
		t.Fatalf("the walk saw %d rows (%v), want the tenant's 6", got, seen)
	}
	if pages != 3 {
		t.Errorf("the walk took %d pages of 2 over 6 rows, want 3", pages)
	}

	// Six distinct ids, so no row arrived twice -- which is what an unordered
	// offset walk does under a real engine.
	distinct := map[string]bool{}
	for _, id := range seen {
		if distinct[id] {
			t.Fatalf("row %q arrived twice: %v", id, seen)
		}
		distinct[id] = true
	}
}

// TestBelongsToManyChunkNeverCrossesTheTenant covers the paged read.
//
// The parent is the same user in both tenants and the join is the same join, so
// nothing about the shape of the query says which rows should come back. Only
// the tenant filter does, and it has to be on every page rather than the first.
func TestBelongsToManyChunkNeverCrossesTheTenant(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var seen []string
	if _, err := rolesOf(database, user).Chunk(ctx, g, 2, func(results []relations.Model, _ int) bool {
		seen = append(seen, rolesSeenBy(results)...)
		return true
	}); err != nil {
		t.Fatal(err)
	}

	for _, id := range seen {
		if id == "y" || id == "z" {
			t.Fatalf("a chunked read returned another tenant's row %q: %v", id, seen)
		}
	}
}

// TestBelongsToManyChunkStops: the callback answering false ends the walk, and
// Chunk says so.
func TestBelongsToManyChunkStops(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	pages := 0
	finished, err := rolesOf(database, user).Chunk(ctx, g, 2, func([]relations.Model, int) bool {
		pages++
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished {
		t.Error("Chunk reported it finished after the callback stopped it")
	}
	if pages != 1 {
		t.Errorf("the walk ran %d pages after being stopped on the first", pages)
	}
}

// TestBelongsToManyChunkHydratesThePivotOnEveryPage is the line that separates
// this relation's chunk from the through relation's. Reading model.pivot inside
// the callback is the ordinary reason to chunk a many-to-many.
func TestBelongsToManyChunkHydratesThePivotOnEveryPage(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	if _, err := rolesOf(database, user).Chunk(ctx, g, 2, func(results []relations.Model, page int) bool {
		for _, model := range results {
			pivot, ok := model.GetRelation("pivot")
			if !ok {
				t.Errorf("page %d handed over a model with no pivot", page)
				return false
			}
			if got := pivot.(relations.Model).GetAttribute("user_id"); !sameValue(got, "1") {
				t.Errorf("page %d carried pivot user_id %v, want the parent's", page, got)
				return false
			}
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
}

// TestBelongsToManyChunkByIdPagesByTheLastKey: the id-paged walk sees each row
// once without an offset, which is what makes it safe to write while walking.
func TestBelongsToManyChunkByIdPagesByTheLastKey(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var seen []string
	if _, err := rolesOf(database, user).ChunkById(ctx, g, 2, func(results []relations.Model, _ int) bool {
		seen = append(seen, rolesSeenBy(results)...)
		return true
	}, "", ""); err != nil {
		t.Fatal(err)
	}

	want := []string{"a", "b", "c", "d", "e", "f"}
	if len(seen) != len(want) {
		t.Fatalf("the walk saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("the walk saw %v, want %v in ascending key order", seen, want)
		}
	}
}

// TestBelongsToManyChunkByIdDescReversesTheOrder.
func TestBelongsToManyChunkByIdDescReversesTheOrder(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var seen []string
	if _, err := rolesOf(database, user).ChunkByIdDesc(ctx, g, 2, func(results []relations.Model, _ int) bool {
		seen = append(seen, rolesSeenBy(results)...)
		return true
	}, "", ""); err != nil {
		t.Fatal(err)
	}

	want := []string{"f", "e", "d", "c", "b", "a"}
	for i := range want {
		if i >= len(seen) || seen[i] != want[i] {
			t.Fatalf("the descending walk saw %v, want %v", seen, want)
		}
	}
}

// TestBelongsToManyEachCountsAcrossPages: the index the callback gets is the
// row's place in the whole walk, not in its page.
func TestBelongsToManyEachCountsAcrossPages(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var keys []int
	if _, err := rolesOf(database, user).Each(ctx, g, func(_ relations.Model, key int) bool {
		keys = append(keys, key)
		return true
	}, 2); err != nil {
		t.Fatal(err)
	}

	for i, key := range keys {
		if key != i {
			t.Fatalf("Each handed over keys %v, want 0..%d across the pages", keys, len(keys)-1)
		}
	}
}

// TestBelongsToManyLazyStopsEarly: abandoning the range loop stops the walk
// rather than draining it, and no error is reported for a caller who left.
func TestBelongsToManyLazyStopsEarly(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	taken := 0
	for model, err := range rolesOf(database, user).Lazy(ctx, g, 2) {
		if err != nil {
			t.Fatal(err)
		}
		if model == nil {
			t.Fatal("Lazy yielded a nil model with no error")
		}
		taken++
		if taken == 3 {
			break
		}
	}

	if taken != 3 {
		t.Fatalf("the loop took %d rows, want the 3 it asked for", taken)
	}
}

// TestBelongsToManyLazyByIdWalksEverything.
func TestBelongsToManyLazyByIdWalksEverything(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	var seen []string
	for model, err := range rolesOf(database, user).LazyById(ctx, g, 2, "", "") {
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, model.GetAttribute("id").(string))
	}

	if len(seen) != 6 {
		t.Fatalf("LazyById saw %v, want the tenant's 6 rows", seen)
	}
}

// TestBelongsToManyCursorHydratesThePivot: the streamed rows carry their pivot
// too, which is the whole difference between this and a bare select.
func TestBelongsToManyCursorHydratesThePivot(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	rows := 0
	for model, err := range rolesOf(database, user).Cursor(ctx, g) {
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := model.GetRelation("pivot"); !ok {
			t.Fatal("Cursor streamed a model with no pivot")
		}
		rows++
	}

	if rows != 6 {
		t.Fatalf("Cursor streamed %d rows, want the tenant's 6", rows)
	}
}

// TestBelongsToManyPaginateHydratesThePivot.
func TestBelongsToManyPaginateHydratesThePivot(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	paginator, err := rolesOf(database, user).Paginate(ctx, g, 2, 1, pagination.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if got := paginator.Total(); got != 6 {
		t.Errorf("the paginator reported %d rows in total, want the tenant's 6", got)
	}
	items := paginator.Items()
	if len(items) != 2 {
		t.Fatalf("page 1 held %d rows, want 2", len(items))
	}
	for _, model := range items {
		if _, ok := model.GetRelation("pivot"); !ok {
			t.Error("a paginated row came back without its pivot")
		}
	}
}

// TestBelongsToManyChunkRefusesAGrantWithNoTenant: a zero Grant is refused
// rather than compiled into `tenant_id = ”`, on the paged path as on the
// plain one.
func TestBelongsToManyChunkRefusesAGrantWithNoTenant(t *testing.T) {
	database, user := seedManyRoles()

	called := false
	_, err := rolesOf(database, user).Chunk(context.Background(), auth.Grant{}, 2,
		func([]relations.Model, int) bool { called = true; return true })

	if err == nil {
		t.Fatal("Chunk accepted a Grant carrying no tenant")
	}
	if called {
		t.Error("the callback ran before the Grant was refused")
	}
}

// TestChunkRefusesAChunkOfNothing: zero rows a page is a walk that never
// advances, and PHP dies on the division rather than saying so.
func TestChunkRefusesAChunkOfNothing(t *testing.T) {
	database, user := seedManyRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	if _, err := rolesOf(database, user).Chunk(ctx, g, 0, func([]relations.Model, int) bool {
		return true
	}); err == nil {
		t.Fatal("Chunk accepted a page size of 0")
	}
}
