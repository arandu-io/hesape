package relations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
)

// TestBelongsToManyEagerLoadsInOneQueryWithThePivot: the join means the
// intermediate table costs no extra round trip, and the pivot columns come back
// aliased so that Match knows which parent each row belongs to.
func TestBelongsToManyEagerLoadsInOneQueryWithThePivot(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	relation := eagerRolesOf(database, users[0])

	before := database.count()
	models := asModels(users)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "roles", relation, nil); err != nil {
		t.Fatal(err)
	}

	if ran := database.count() - before; ran != 1 {
		t.Fatalf("a many-to-many eager load ran %d queries, want 1: the pivot is joined, not queried\n%s",
			ran, strings.Join(database.log[len(database.log)-ran:], "\n"))
	}

	loaded, _ := models[0].GetRelation("roles")
	roles := loaded.([]relations.Model)
	if len(roles) != 2 {
		t.Fatalf("user 1 came back with %d roles, want 2", len(roles))
	}

	pivot, ok := roles[0].GetRelation("pivot")
	if !ok {
		t.Fatal("the related model came back without its pivot")
	}
	if got := pivot.(relations.Model).GetAttribute("user_id"); !sameValue(got, "1") {
		t.Fatalf("the pivot carried user_id %v, want the parent's key", got)
	}
	if roles[0].GetAttribute("pivot_user_id") != nil {
		t.Error("the pivot_ column was left on the related model: its attributes would lie about which table they came from")
	}
}

// TestAttachStampsTheTenantOnThePivotRow is RULE 14 on the write side of a
// shared table.
//
// A pivot row written without the tenant is a row every scoped read will miss
// -- an attach that reports success and a relation that comes back empty.
func TestAttachStampsTheTenantOnThePivotRow(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	relation := rolesOf(database, users[2])
	if err := relation.Attach(ctx, g, "admin", nil, false); err != nil {
		t.Fatal(err)
	}

	rows := database.tables["role_user"]
	last := rows[len(rows)-1]

	if last["tenant_id"] != "acme" {
		t.Fatalf("the attached pivot row carried tenant %v, want the Grant's", last["tenant_id"])
	}
	if !sameValue(last["user_id"], "3") || !sameValue(last["role_id"], "admin") {
		t.Fatalf("the attached row was %v, want user 3 to role admin", last)
	}
}

// TestDetachOnlyReachesTheGrantsTenant.
func TestDetachOnlyReachesTheGrantsTenant(t *testing.T) {
	database, users := seedRoles()
	database.seed("role_user", map[string]any{"user_id": "1", "role_id": "admin", "tenant_id": "other"})

	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	if _, err := rolesOf(database, users[0]).Detach(ctx, g, nil, false); err != nil {
		t.Fatal(err)
	}

	for _, row := range database.tables["role_user"] {
		if sameValue(row["user_id"], "1") && row["tenant_id"] == "acme" {
			t.Fatalf("a row of this tenant survived the detach: %v", row)
		}
	}

	survived := false
	for _, row := range database.tables["role_user"] {
		if row["tenant_id"] == "other" {
			survived = true
		}
	}
	if !survived {
		t.Fatal("the detach removed another tenant's pivot row")
	}
}

// TestSyncAttachesWhatIsMissingAndDetachesWhatIsNotListed.
func TestSyncAttachesWhatIsMissingAndDetachesWhatIsNotListed(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	changes, err := rolesOf(database, users[0]).Sync(ctx, g, []any{"admin", "auditor"})
	if err != nil {
		t.Fatal(err)
	}

	if len(changes.Attached) != 1 || !sameValue(changes.Attached[0], "auditor") {
		t.Errorf("attached %v, want auditor", changes.Attached)
	}
	if len(changes.Detached) != 1 || !sameValue(changes.Detached[0], "editor") {
		t.Errorf("detached %v, want editor", changes.Detached)
	}

	after := rolesFor(database, "1")
	if strings.Join(after, ",") != "admin,auditor" {
		t.Errorf("user 1 ended up with %v, want admin and auditor", after)
	}
}

// TestSyncWithoutDetachingLeavesWhatIsThere.
func TestSyncWithoutDetachingLeavesWhatIsThere(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	if _, err := rolesOf(database, users[0]).SyncWithoutDetaching(ctx, g, []any{"auditor"}); err != nil {
		t.Fatal(err)
	}

	after := rolesFor(database, "1")
	if strings.Join(after, ",") != "admin,editor,auditor" {
		t.Errorf("user 1 ended up with %v, want nothing removed and auditor added", after)
	}
}

// TestToggleFlipsEachIdAndReportsBothSides.
func TestToggleFlipsEachIdAndReportsBothSides(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	changes, err := rolesOf(database, users[0]).Toggle(ctx, g, []any{"admin", "auditor"}, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes.Detached) != 1 || !sameValue(changes.Detached[0], "admin") {
		t.Errorf("detached %v, want admin -- it was attached", changes.Detached)
	}
	if len(changes.Attached) != 1 || !sameValue(changes.Attached[0], "auditor") {
		t.Errorf("attached %v, want auditor -- it was not", changes.Attached)
	}
}

// TestSyncWithPivotValuesWritesTheValuesOnEveryRow.
func TestSyncWithPivotValuesWritesTheValuesOnEveryRow(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.assign", "acme")

	relation := rolesOf(database, users[2]).WithPivot("granted_by")
	if _, err := relation.SyncWithPivotValues(ctx, g, []any{"admin"}, map[string]any{"granted_by": "root"}); err != nil {
		t.Fatal(err)
	}

	rows := database.tables["role_user"]
	last := rows[len(rows)-1]
	if last["granted_by"] != "root" {
		t.Fatalf("the pivot row was written as %v, want granted_by on it", last)
	}
}

// TestWithPivotValueRefusesNull answers the PHP's InvalidArgumentException: a
// null pivot value is a filter that matches nothing and a default that writes
// nothing.
func TestWithPivotValueRefusesNull(t *testing.T) {
	database, users := seedRoles()

	if _, err := rolesOf(database, users[0]).WithPivotValue("granted_by", nil); err == nil {
		t.Fatal("withPivotValue accepted null")
	}
}

// TestThePivotAccessorCanBeRenamed answers BelongsToMany::as.
func TestThePivotAccessorCanBeRenamed(t *testing.T) {
	database, users := seedRoles()
	ctx, g := context.Background(), auth.SystemGrant("role.view", "acme")

	relation := rolesOf(database, users[0]).As("membership")
	roles, err := relation.Get(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		t.Fatal("no roles came back")
	}
	if !roles[0].RelationLoaded("membership") {
		t.Fatal("the pivot was not set under the accessor as() named")
	}
}

func seedRoles() (*db, []*model) {
	database := newDB()

	users := []*model{
		newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"}),
		newModel(database, "users", "user", map[string]any{"id": "2", "tenant_id": "acme"}),
		newModel(database, "users", "user", map[string]any{"id": "3", "tenant_id": "acme"}),
	}

	database.seed("roles",
		map[string]any{"id": "admin", "label": "Admin", "tenant_id": "acme"},
		map[string]any{"id": "editor", "label": "Editor", "tenant_id": "acme"},
		map[string]any{"id": "auditor", "label": "Auditor", "tenant_id": "acme"},
	)
	database.seed("role_user",
		map[string]any{"user_id": "1", "role_id": "admin", "tenant_id": "acme"},
		map[string]any{"user_id": "1", "role_id": "editor", "tenant_id": "acme"},
		map[string]any{"user_id": "2", "role_id": "editor", "tenant_id": "acme"},
	)

	return database, users
}

func rolesOf(database *db, parent relations.Model) *relations.BelongsToMany {
	return relations.NewBelongsToMany(
		newModel(database, "roles", "role", nil).NewQuery(),
		parent,
		"role_user",
		"user_id",
		"role_id",
		"id",
		"id",
		"roles",
	)
}

func eagerRolesOf(database *db, parent relations.Model) *relations.BelongsToMany {
	var relation *relations.BelongsToMany
	relations.NoConstraints(func() { relation = rolesOf(database, parent) })
	return relation
}

func rolesFor(database *db, user string) []string {
	out := []string{}
	for _, row := range database.tables["role_user"] {
		if sameValue(row["user_id"], user) && row["tenant_id"] == "acme" {
			out = append(out, row["role_id"].(string))
		}
	}
	return out
}
