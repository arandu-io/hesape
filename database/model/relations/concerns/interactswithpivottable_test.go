package concerns

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// fakePivotHost is the BelongsToMany half InteractsWithPivotTable is mixed into.
//
// It answers a real base query builder for the pivot table, scoped the way the
// concrete relation scopes it, so what the tests read back is the statement the
// package would have sent.
type fakePivotHost struct {
	conn   *fakeConnection
	parent Model
	relate Model
	table  string

	touched int
	pivots  []*fakeModel
}

func newPivotHost(conn *fakeConnection) *fakePivotHost {
	parent := newFakeModel("users")
	parent.attributes["id"] = 7

	related := newFakeModel("roles")
	related.keyType = "string"

	return &fakePivotHost{conn: conn, parent: parent, relate: related, table: "role_user"}
}

func (h *fakePivotHost) GetTable() string  { return h.table }
func (h *fakePivotHost) GetParent() Model  { return h.parent }
func (h *fakePivotHost) GetRelated() Model { return h.relate }
func (h *fakePivotHost) ParentKeyValue() any {
	return h.parent.GetAttribute(h.parent.GetKeyName())
}

func (h *fakePivotHost) GetForeignPivotKeyName() string { return "user_id" }
func (h *fakePivotHost) GetRelatedPivotKeyName() string { return "role_id" }
func (h *fakePivotHost) GetQualifiedForeignPivotKeyName() string {
	return h.table + ".user_id"
}
func (h *fakePivotHost) GetQualifiedRelatedPivotKeyName() string {
	return h.table + ".role_id"
}
func (h *fakePivotHost) GetRelatedKeyName() string { return h.relate.GetKeyName() }
func (h *fakePivotHost) CreatedAt() string         { return "created_at" }
func (h *fakePivotHost) UpdatedAt() string         { return "updated_at" }

// NewPivotStatement is the bare statement over the pivot table, already scoped
// to the grant's tenant -- which is what the concrete relation does, and what
// makes the four execute helpers safe to run without a second scoping pass.
func (h *fakePivotHost) NewPivotStatement(g auth.Grant) (*query.Builder, error) {
	return ScopeTenantQuery(newQuery(h.conn, h.table), h.table, g)
}

// NewPivotQuery is NewPivotStatement plus this relation's own constraint.
func (h *fakePivotHost) NewPivotQuery(g auth.Grant) (*query.Builder, error) {
	statement, err := h.NewPivotStatement(g)
	if err != nil {
		return nil, err
	}
	return statement.Where(h.GetQualifiedForeignPivotKeyName(), h.ParentKeyValue()), nil
}

func (h *fakePivotHost) NewPivot(attributes map[string]any, exists bool) Model {
	pivot := newFakeModel(h.table)
	pivot.exists = exists
	for key, value := range attributes {
		pivot.attributes[key] = value
	}
	h.pivots = append(h.pivots, pivot)
	return pivot
}

func (h *fakePivotHost) TouchIfTouching(context.Context, auth.Grant) error {
	h.touched++
	return nil
}

// newPivot returns the trait under test wired to a host, and the connection the
// statements land on.
func newPivot(columns ...string) (*InteractsWithPivotTable, *fakePivotHost, *fakeConnection) {
	conn := &fakeConnection{}
	host := newPivotHost(conn)
	pivot := &InteractsWithPivotTable{Host: host}
	pivot.WithPivot(columns...)
	return pivot, host, conn
}

// TestStampTenantPutsTheTenantOnEveryRow.
//
// The where clause on a select is only half of it. A pivot row written without
// the column is a row no scoped read returns -- an attach that appears to work
// and a relation that comes back empty -- and on a table whose tenant column is
// nullable it is a row every tenant can see.
func TestStampTenantPutsTheTenantOnEveryRow(t *testing.T) {
	pivot, _, _ := newPivot()

	rows := []map[string]any{
		{"user_id": 7, "role_id": "admin"},
		{"user_id": 7, "role_id": "editor"},
	}

	for _, row := range pivot.stampTenant(rows, grant()) {
		if row[TenantColumn] != "acme" {
			t.Fatalf("row %#v went in without the tenant, so no scoped read will ever return it", row)
		}
	}
}

// TestStampTenantDoesNotOverwriteAColumnTheCallerSet: an explicit value is the
// caller's, and silently replacing it would make the column unwritable.
func TestStampTenantDoesNotOverwriteAColumnTheCallerSet(t *testing.T) {
	pivot, _, _ := newPivot()

	rows := pivot.stampTenant([]map[string]any{{"role_id": "admin", TenantColumn: "other"}}, grant())
	if rows[0][TenantColumn] != "other" {
		t.Fatalf("the tenant the caller set was replaced with %v", rows[0][TenantColumn])
	}
}

// TestStampTenantWritesNothingForAModelThatOptedOut: a shared table has no
// tenant column, and naming one would be a statement the engine refuses.
func TestStampTenantWritesNothingForAModelThatOptedOut(t *testing.T) {
	pivot, host, _ := newPivot()
	host.parent = sharedModel{newFakeModel("users")}

	rows := pivot.stampTenant([]map[string]any{{"role_id": "admin"}}, grant())
	if _, stamped := rows[0][TenantColumn]; stamped {
		t.Fatalf("a shared parent had its rows stamped: %#v", rows[0])
	}
}

// TestStampTenantWritesNothingForAGrantWithNoTenant. Nothing reaches here with
// one -- NewPivotStatement refuses first -- and stamping the empty string would
// write a row that is worse than not writing at all.
func TestStampTenantWritesNothingForAGrantWithNoTenant(t *testing.T) {
	pivot, _, _ := newPivot()

	rows := pivot.stampTenant([]map[string]any{{"role_id": "admin"}}, auth.Grant{})
	if _, stamped := rows[0][TenantColumn]; stamped {
		t.Fatalf("the empty tenant was stamped onto %#v", rows[0])
	}
}

// TestAttachInsertsThePivotRowCarryingBothKeysAndTheTenant.
func TestAttachInsertsThePivotRowCarryingBothKeysAndTheTenant(t *testing.T) {
	pivot, host, conn := newPivot()

	if err := pivot.Attach(context.Background(), grant(), []any{"admin", "editor"}, nil); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	ran := conn.only(t)
	if ran.kind != "insert" {
		t.Fatalf("Attach ran a %s", ran.kind)
	}

	columns := columnsOf(t, ran.sql)
	want := []string{"role_id", TenantColumn, "user_id"}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("Attach wrote columns %v, want %v", columns, want)
	}
	if !reflect.DeepEqual(ran.bindings, []any{"admin", "acme", 7, "editor", "acme", 7}) {
		t.Fatalf("Attach sent %#v, want both rows keyed to the parent and the tenant", ran.bindings)
	}
	if host.touched != 1 {
		t.Fatalf("Attach touched %d times, want once", host.touched)
	}
}

// TestAttachRefusesAGrantWithNoTenant before it writes anything: the pivot row
// is what says this customer's user has that customer's role.
func TestAttachRefusesAGrantWithNoTenant(t *testing.T) {
	pivot, _, conn := newPivot()

	if err := pivot.Attach(context.Background(), auth.Grant{}, []any{"admin"}, nil); err == nil {
		t.Fatal("Attach wrote a pivot row under a grant carrying no tenant")
	}
	if len(conn.statements) != 0 {
		t.Fatalf("Attach ran %#v before refusing", conn.statements)
	}
}

// TestAttachDoesNotTouchWhenToldNotTo: sync attaches one record at a time and
// touches once at the end, so the inner calls must not each touch.
func TestAttachDoesNotTouchWhenToldNotTo(t *testing.T) {
	pivot, host, _ := newPivot()

	if err := pivot.Attach(context.Background(), grant(), []any{"admin"}, nil, false); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if host.touched != 0 {
		t.Fatalf("Attach touched %d times when told not to", host.touched)
	}
}

// TestAttachWithACustomPivotSavesOneModelPerRow rather than one insert for all
// of them, which is the whole reason using() exists: the pivot model's own
// casts and events have to run.
func TestAttachWithACustomPivotSavesOneModelPerRow(t *testing.T) {
	pivot, host, conn := newPivot()
	pivot.Using = true

	if err := pivot.Attach(context.Background(), grant(), []any{"admin", "editor"}, nil); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if len(conn.statements) != 0 {
		t.Fatalf("a custom pivot went in by raw insert: %#v", conn.statements)
	}
	if len(host.pivots) != 2 {
		t.Fatalf("Attach built %d pivot models, want one per row", len(host.pivots))
	}
	for _, model := range host.pivots {
		if model.saved != 1 {
			t.Fatalf("a pivot model was saved %d times", model.saved)
		}
		if model.exists {
			t.Fatal("a pivot model being attached was built as already existing")
		}
	}
}

// TestAttachCarriesTheExtraAttributesAndTheTimestamps.
func TestAttachCarriesTheExtraAttributesAndTheTimestamps(t *testing.T) {
	pivot, _, conn := newPivot("created_at", "updated_at", "expires_at")

	err := pivot.Attach(context.Background(), grant(), []any{"admin"}, map[string]any{"expires_at": "2026-09-01"})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	ran := conn.only(t)
	want := []string{"created_at", "expires_at", "role_id", TenantColumn, "updated_at", "user_id"}
	if got := columnsOf(t, ran.sql); !reflect.DeepEqual(got, want) {
		t.Fatalf("Attach wrote columns %v, want %v", got, want)
	}
}

// TestAttachWritesNoTimestampsThePivotDoesNotDeclare: withPivot is what says
// the intermediate table has those columns, and writing one it does not have is
// a statement the engine refuses.
func TestAttachWritesNoTimestampsThePivotDoesNotDeclare(t *testing.T) {
	pivot, _, conn := newPivot()

	if err := pivot.Attach(context.Background(), grant(), []any{"admin"}, nil); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if got := columnsOf(t, conn.only(t).sql); len(got) != 3 {
		t.Fatalf("Attach wrote %v, want only the two keys and the tenant", got)
	}
}

// TestDetachNarrowsToTheIdsItWasGiven, on top of the relation's own constraint
// and the tenant filter that came with the statement.
func TestDetachNarrowsToTheIdsItWasGiven(t *testing.T) {
	pivot, host, conn := newPivot()
	conn.affected = 1

	affected, err := pivot.Detach(context.Background(), grant(), []any{"admin"})
	if err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if affected != 1 {
		t.Fatalf("Detach reported %d rows, and the connection said 1", affected)
	}

	ran := conn.only(t)
	if ran.kind != "delete" {
		t.Fatalf("Detach ran a %s", ran.kind)
	}
	if !reflect.DeepEqual(ran.bindings, []any{"acme", 7, "admin"}) {
		t.Fatalf("Detach sent %#v, want the tenant, the parent and the id", ran.bindings)
	}
	if host.touched != 1 {
		t.Fatalf("Detach touched %d times, want once", host.touched)
	}
}

// TestDetachWithNoIdsDetachesEverythingTheRelationReaches -- and reaches is
// doing the work: the statement is the relation's own, so it is already narrowed
// to this parent and this tenant. A detach that dropped either would delete
// another customer's rows.
func TestDetachWithNoIdsDetachesEverythingTheRelationReaches(t *testing.T) {
	pivot, _, conn := newPivot()

	if _, err := pivot.Detach(context.Background(), grant(), nil); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	ran := conn.only(t)
	if !reflect.DeepEqual(ran.bindings, []any{"acme", 7}) {
		t.Fatalf("Detach sent %#v, want the tenant and the parent and nothing narrower", ran.bindings)
	}
	if strings.Contains(ran.sql, "role_id") {
		t.Fatalf("Detach narrowed by id when it was given none: %s", ran.sql)
	}
}

// TestDetachOfAnEmptyListRunsNothing: an empty list is not "everything", and
// treating it as such is the difference between a no-op and deleting the
// parent's rows.
func TestDetachOfAnEmptyListRunsNothing(t *testing.T) {
	pivot, host, conn := newPivot()

	affected, err := pivot.Detach(context.Background(), grant(), []any{})
	if err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if affected != 0 {
		t.Fatalf("Detach reported %d rows for an empty list", affected)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("Detach of nothing ran %#v", conn.statements)
	}
	if host.touched != 0 {
		t.Fatalf("Detach of nothing touched %d times", host.touched)
	}
}

// TestSyncAttachesWhatIsMissingAndDetachesWhatIsNotWanted.
func TestSyncAttachesWhatIsMissingAndDetachesWhatIsNotWanted(t *testing.T) {
	pivot, _, conn := newPivot()
	conn.rows = []query.Record{{"role_id": "admin"}, {"role_id": "editor"}}

	changes, err := pivot.Sync(context.Background(), grant(), []any{"admin", "viewer"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if !reflect.DeepEqual(changes.Attached, []any{"viewer"}) {
		t.Fatalf("Sync attached %#v, want the one that was missing", changes.Attached)
	}
	if !reflect.DeepEqual(changes.Detached, []any{"editor"}) {
		t.Fatalf("Sync detached %#v, want the one that was not asked for", changes.Detached)
	}
	if len(changes.Updated) != 0 {
		t.Fatalf("Sync updated %#v, and no record carried attributes", changes.Updated)
	}
}

// TestSyncWithoutDetachingLeavesWhatIsThere: the same call with detaching off,
// which is the difference between adding a role and replacing every role.
func TestSyncWithoutDetachingLeavesWhatIsThere(t *testing.T) {
	pivot, _, conn := newPivot()
	conn.rows = []query.Record{{"role_id": "admin"}, {"role_id": "editor"}}

	changes, err := pivot.SyncWithoutDetaching(context.Background(), grant(), []any{"viewer"})
	if err != nil {
		t.Fatalf("SyncWithoutDetaching: %v", err)
	}

	if len(changes.Detached) != 0 {
		t.Fatalf("SyncWithoutDetaching detached %#v", changes.Detached)
	}
	if !reflect.DeepEqual(changes.Attached, []any{"viewer"}) {
		t.Fatalf("SyncWithoutDetaching attached %#v", changes.Attached)
	}
}

// TestSyncUpdatesAnAttachedRecordThatCarriesAttributes rather than attaching a
// second pivot row for the same pair.
func TestSyncUpdatesAnAttachedRecordThatCarriesAttributes(t *testing.T) {
	pivot, _, conn := newPivot("expires_at")
	conn.rows = []query.Record{{"role_id": "admin"}}
	conn.affected = 1

	changes, err := pivot.Sync(context.Background(), grant(),
		[]AttachRecord{{ID: "admin", Attributes: map[string]any{"expires_at": "2026-09-01"}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if !reflect.DeepEqual(changes.Updated, []any{"admin"}) {
		t.Fatalf("Sync updated %#v, want the record it already had", changes.Updated)
	}
	if len(changes.Attached) != 0 {
		t.Fatalf("Sync attached %#v for a pair that was already there", changes.Attached)
	}

	var updates int
	for _, ran := range conn.statements {
		if ran.kind == "update" {
			updates++
		}
	}
	if updates != 1 {
		t.Fatalf("Sync ran %d updates: %#v", updates, conn.statements)
	}
}

// TestSyncOfNothingWithoutDetachingRunsNothing, including the read that would
// otherwise be made to answer a question nobody asked.
func TestSyncOfNothingWithoutDetachingRunsNothing(t *testing.T) {
	pivot, host, conn := newPivot()

	changes, err := pivot.Sync(context.Background(), grant(), []any{}, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("Sync of nothing ran %#v", conn.statements)
	}
	if host.touched != 0 {
		t.Fatalf("Sync of nothing touched %d times", host.touched)
	}
	if len(changes.Attached) != 0 || len(changes.Detached) != 0 || len(changes.Updated) != 0 {
		t.Fatalf("Sync of nothing reported %#v", changes)
	}
}

// TestSyncMatchesADatabaseStringKeyAgainstACallersIntKey.
//
// MySQL's text protocol returns every column as bytes, which the connection
// turns into a string, so an int primary key reaches a relation as "7" while the
// caller holds int64(7). Every decision Sync makes -- what is already attached,
// what was asked for, what is missing -- goes through GetDictionaryKey, which
// renders both to "7" for exactly this reason.
//
// Pinned because the alternative is the worst shape this could take: Sync
// treating the two as different keys would detach and reattach the same pair on
// every call, forever, with no error anywhere.
func TestSyncMatchesADatabaseStringKeyAgainstACallersIntKey(t *testing.T) {
	pivot, host, conn := newPivot()
	host.relate.(*fakeModel).keyType = "int"

	conn.rows = []query.Record{{"role_id": "7"}}

	changes, err := pivot.Sync(context.Background(), grant(), []any{int64(7)})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(changes.Attached) != 0 {
		t.Fatalf("Sync attached %#v, and that row was already attached under the same key", changes.Attached)
	}
	if len(changes.Detached) != 0 {
		t.Fatalf("Sync detached %#v, and that row was asked for under the same key", changes.Detached)
	}

	for _, ran := range conn.statements {
		if ran.kind != "select" {
			t.Fatalf("Sync ran a %s for a pair that was already in the state asked for: %s", ran.kind, ran.sql)
		}
	}
}

// TestSyncReportsBothHalvesOfTheChangeInTheRelatedKeysType.
//
// Attached is built from the ids the caller passed and Detached from the keys
// the database returned, and CastKey exists to make both come back as the
// related model's key type. It read Go types only, so the half that arrived as
// text from MySQL was handed back as text: one call answering int64 for what it
// attached and a string for what it detached, for the same model and the same
// column.
//
// A caller ranging over both and asserting int64 panics on the second half.
func TestSyncReportsBothHalvesOfTheChangeInTheRelatedKeysType(t *testing.T) {
	pivot, host, conn := newPivot()
	host.relate.(*fakeModel).keyType = "int"

	conn.rows = []query.Record{{"role_id": "7"}}

	changes, err := pivot.Sync(context.Background(), grant(), []any{int64(9)})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if !reflect.DeepEqual(changes.Attached, []any{int64(9)}) {
		t.Fatalf("Sync attached %#v, want int64", changes.Attached)
	}
	if !reflect.DeepEqual(changes.Detached, []any{int64(7)}) {
		t.Fatalf("Sync detached %#v, want int64: the key came back from the database as text, "+
			"and the caller reads the two halves of one change set the same way", changes.Detached)
	}
}

// TestToggleReportsADetachedKeyInTheRelatedKeysType, which is the other place a
// key read out of the database is handed back to the caller.
func TestToggleReportsADetachedKeyInTheRelatedKeysType(t *testing.T) {
	pivot, host, conn := newPivot()
	host.relate.(*fakeModel).keyType = "int"

	conn.rows = []query.Record{{"role_id": "7"}}

	changes, err := pivot.Toggle(context.Background(), grant(), []any{int64(7)})
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if !reflect.DeepEqual(changes.Detached, []any{int64(7)}) {
		t.Fatalf("Toggle detached %#v, want int64", changes.Detached)
	}
}

// TestToggleTakesOffWhatIsOnAndPutsOnWhatIsNot.
func TestToggleTakesOffWhatIsOnAndPutsOnWhatIsNot(t *testing.T) {
	pivot, _, conn := newPivot()
	conn.rows = []query.Record{{"role_id": "admin"}}

	changes, err := pivot.Toggle(context.Background(), grant(), []any{"admin", "viewer"})
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}

	if !reflect.DeepEqual(changes.Detached, []any{"admin"}) {
		t.Fatalf("Toggle detached %#v, want the one that was attached", changes.Detached)
	}
	if !reflect.DeepEqual(changes.Attached, []any{"viewer"}) {
		t.Fatalf("Toggle attached %#v, want the one that was not", changes.Attached)
	}
}

// TestUpdateExistingPivotStampsTheUpdatedAtColumnThePivotDeclares, and leaves it
// alone when the intermediate table has no such column.
func TestUpdateExistingPivotStampsTheUpdatedAtColumnThePivotDeclares(t *testing.T) {
	pivot, _, conn := newPivot("expires_at", "updated_at")
	conn.affected = 1

	updated, err := pivot.UpdateExistingPivot(context.Background(), grant(), "admin",
		map[string]any{"expires_at": "2026-09-01"})
	if err != nil {
		t.Fatalf("UpdateExistingPivot: %v", err)
	}
	if updated != 1 {
		t.Fatalf("UpdateExistingPivot reported %d rows", updated)
	}

	ran := conn.only(t)
	if !strings.Contains(ran.sql, "updated_at") {
		t.Fatalf("the update left updated_at alone on a pivot that declares it: %s", ran.sql)
	}

	bare, _, bareConn := newPivot("expires_at")
	if _, err := bare.UpdateExistingPivot(context.Background(), grant(), "admin",
		map[string]any{"expires_at": "2026-09-01"}); err != nil {
		t.Fatalf("UpdateExistingPivot: %v", err)
	}
	if strings.Contains(bareConn.only(t).sql, "updated_at") {
		t.Fatal("the update wrote updated_at on a pivot that does not declare the column")
	}
}

// TestAllRelatedIDsReadsTheKeysOffThePivotQuery, which carries the tenant filter
// and the parent constraint.
func TestAllRelatedIDsReadsTheKeysOffThePivotQuery(t *testing.T) {
	pivot, _, conn := newPivot()
	conn.rows = []query.Record{{"role_id": "admin"}, {"role_id": "editor"}}

	keys, err := pivot.AllRelatedIDs(context.Background(), grant())
	if err != nil {
		t.Fatalf("AllRelatedIDs: %v", err)
	}
	if !reflect.DeepEqual(keys, []any{"admin", "editor"}) {
		t.Fatalf("AllRelatedIDs answered %#v", keys)
	}

	ran := conn.only(t)
	if !reflect.DeepEqual(ran.bindings, []any{"acme", 7}) {
		t.Fatalf("the read sent %#v, want the tenant and the parent", ran.bindings)
	}
}

// TestGetCurrentlyAttachedPivotsHydratesEachRowAsAnExistingPivot.
func TestGetCurrentlyAttachedPivotsHydratesEachRowAsAnExistingPivot(t *testing.T) {
	pivot, host, conn := newPivot()
	conn.rows = []query.Record{{"role_id": "admin", "user_id": 7}}

	pivots, err := pivot.GetCurrentlyAttachedPivots(context.Background(), grant())
	if err != nil {
		t.Fatalf("GetCurrentlyAttachedPivots: %v", err)
	}
	if len(pivots) != 1 {
		t.Fatalf("%d pivots for one row", len(pivots))
	}
	if !pivots[0].Exists() {
		t.Fatal("a pivot read out of the table was built as not existing, so saving it would insert a duplicate")
	}
	if host.pivots[0].GetAttribute("role_id") != "admin" {
		t.Fatalf("the pivot was hydrated as %#v", host.pivots[0].attributes)
	}
}

// TestParseIDsAcceptsWhatTheCallerHas: a scalar, a model, a list of either, or a
// list of records.
func TestParseIDsAcceptsWhatTheCallerHas(t *testing.T) {
	pivot, _, _ := newPivot()

	admin := newFakeModel("roles")
	admin.attributes["id"] = "admin"
	editor := newFakeModel("roles")
	editor.attributes["id"] = "editor"

	for _, c := range []struct {
		name string
		in   any
		want []any
	}{
		{"nil", nil, nil},
		{"a scalar", "admin", []any{"admin"}},
		{"a model", Model(admin), []any{"admin"}},
		{"models", []Model{admin, editor}, []any{"admin", "editor"}},
		{"records", []AttachRecord{{ID: "admin"}, {ID: "editor"}}, []any{"admin", "editor"}},
		{"a mixed list", []any{admin, "editor"}, []any{"admin", "editor"}},
		{"strings", []string{"admin", "editor"}, []any{"admin", "editor"}},
		{"ints", []int{1, 2}, []any{1, 2}},
		{"int64s", []int64{1, 2}, []any{int64(1), int64(2)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := pivot.ParseIDs(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ParseIDs(%#v) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

// TestParseIDReadsTheRelatedKeyOffAModel and passes anything else through.
func TestParseIDReadsTheRelatedKeyOffAModel(t *testing.T) {
	pivot, _, _ := newPivot()

	admin := newFakeModel("roles")
	admin.attributes["id"] = "admin"

	if got := pivot.ParseID(Model(admin)); got != "admin" {
		t.Fatalf("ParseID of a model = %v", got)
	}
	if got := pivot.ParseID(42); got != 42 {
		t.Fatalf("ParseID of a scalar = %v", got)
	}
}

// TestFormatRecordsListEmitsAMapInKeyOrder.
//
// A Go map has no order, and an insert whose rows reshuffle between runs is a
// statement nobody can reproduce from a log. Key order is the one order
// available without asking the caller for a slice.
func TestFormatRecordsListEmitsAMapInKeyOrder(t *testing.T) {
	pivot, _, _ := newPivot()

	in := map[string]map[string]any{
		"viewer": {"expires_at": "3"},
		"admin":  {"expires_at": "1"},
		"editor": {"expires_at": "2"},
	}

	// Run it more than once: a single pass over a three-key map lands in sorted
	// order once in six by luck.
	for range 8 {
		got := pivot.FormatRecordsList(in)
		want := []AttachRecord{
			{ID: "admin", Attributes: map[string]any{"expires_at": "1"}},
			{ID: "editor", Attributes: map[string]any{"expires_at": "2"}},
			{ID: "viewer", Attributes: map[string]any{"expires_at": "3"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("FormatRecordsList = %#v, want %#v", got, want)
		}
	}
}

// TestFormatRecordsListPassesRecordsThroughWhole, which is what keeps an
// id-with-attributes list from being flattened to bare ids on the way in.
func TestFormatRecordsListPassesRecordsThroughWhole(t *testing.T) {
	pivot, _, _ := newPivot()

	records := []AttachRecord{{ID: "admin", Attributes: map[string]any{"expires_at": "1"}}}
	if got := pivot.FormatRecordsList(records); !reflect.DeepEqual(got, records) {
		t.Fatalf("FormatRecordsList = %#v, want the records unchanged", got)
	}

	one := AttachRecord{ID: "admin"}
	if got := pivot.FormatRecordsList(one); !reflect.DeepEqual(got, []AttachRecord{one}) {
		t.Fatalf("FormatRecordsList of one record = %#v", got)
	}
}

// TestGetTypeSwapValueMatchesTheRelatedKeysType.
//
// This is what makes a change set answer one type per column. The keys on one
// side come out of the database and the ones on the other from the caller, and
// the caller reads both halves the same way.
//
// Text is in the table because a driver may hand an integer column back as
// bytes, which the connection renders to a string. Text that is not the whole of
// a base-ten integer is refused and passes through unchanged: reading "7.9" as
// far as it parses would make it the same key as "7", which is the failure this
// prevents rather than a lesser version of it.
func TestGetTypeSwapValueMatchesTheRelatedKeysType(t *testing.T) {
	for _, c := range []struct {
		keyType string
		in      any
		want    any
	}{
		{"int", 7, int64(7)},
		{"int", int32(7), int64(7)},
		{"int", uint32(7), int64(7)},
		{"int", float64(7), int64(7)},
		{"int", float32(7), int64(7)},
		{"integer", int64(7), int64(7)},

		// What a driver hands back for an integer column over a text protocol.
		{"int", "7", int64(7)},
		{"int", []byte("7"), int64(7)},
		{"int", "-7", int64(-7)},

		// Refused, and left as it came.
		{"int", "seven", "seven"},
		{"int", "7.9", "7.9"},
		{"int", " 7", " 7"},
		{"int", "", ""},
		{"int", "9223372036854775808", "9223372036854775808"},

		{"string", 7, "7"},
		{"string", "admin", "admin"},
		{"uuid", 7, 7},
	} {
		got := GetTypeSwapValue(c.keyType, c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("GetTypeSwapValue(%q, %#v) = %#v, want %#v", c.keyType, c.in, got, c.want)
		}
	}
}

// TestGetTypeSwapValueCountsWhatTheDictionaryCounts.
//
// The two read the same keys: one decides whether two keys are the same, the
// other decides what type the caller is handed back. A value the dictionary
// renders as a number and this one refuses is a key that compares equal and is
// reported as a different type, which is the shape the text case had.
func TestGetTypeSwapValueCountsWhatTheDictionaryCounts(t *testing.T) {
	numbers := []any{
		int(7), int8(7), int16(7), int32(7), int64(7),
		uint(7), uint8(7), uint16(7), uint32(7), uint64(7),
		float32(7), float64(7),
		"7", []byte("7"),
	}

	for _, value := range numbers {
		key, err := GetDictionaryKey(value)
		if err != nil {
			t.Fatalf("GetDictionaryKey(%#v): %v", value, err)
		}
		if key != "7" {
			t.Fatalf("GetDictionaryKey(%#v) = %q, and this table is the values that key as 7", value, key)
		}
		if got := GetTypeSwapValue("int", value); got != int64(7) {
			t.Errorf("GetTypeSwapValue(\"int\", %#v) = %#v, and the dictionary counts it as 7", value, got)
		}
	}
}

// TestCastKeyUsesTheRelatedModelsKeyType, which is where the key type comes from
// rather than from the parent's.
func TestCastKeyUsesTheRelatedModelsKeyType(t *testing.T) {
	pivot, host, _ := newPivot()

	if got := pivot.CastKey(7); got != "7" {
		t.Fatalf("CastKey = %#v for a related model keyed by string", got)
	}

	related := newFakeModel("roles")
	related.keyType = "int"
	host.relate = related

	got := pivot.CastKeys([]any{8, "9"})
	if !reflect.DeepEqual(got, []any{int64(8), int64(9)}) {
		t.Fatalf("CastKeys = %#v; both are integer keys and one of them only arrived as text", got)
	}
}

// TestWithPivotAndHasPivotColumn: the column list is what says the intermediate
// table has a column, and everything that writes one asks first.
func TestWithPivotAndHasPivotColumn(t *testing.T) {
	pivot, _, _ := newPivot()

	if pivot.HasPivotColumn("expires_at") {
		t.Fatal("a column nobody declared was reported present")
	}

	pivot.WithPivot("expires_at").WithPivot("notes")
	if !pivot.HasPivotColumn("expires_at") || !pivot.HasPivotColumn("notes") {
		t.Fatalf("WithPivot did not accumulate: %v", pivot.PivotColumns)
	}
}

// TestBaseAttachRecordCarriesTheDeclaredPivotValues, which is how
// wherePivot-style constants land on every row a relation attaches.
func TestBaseAttachRecordCarriesTheDeclaredPivotValues(t *testing.T) {
	pivot, _, _ := newPivot()
	pivot.PivotValues = []PivotValue{{Column: "kind", Value: "grant"}}

	record := pivot.BaseAttachRecord("admin", false)

	if record["role_id"] != "admin" || record["user_id"] != 7 {
		t.Fatalf("BaseAttachRecord = %#v, want both keys", record)
	}
	if record["kind"] != "grant" {
		t.Fatalf("BaseAttachRecord dropped the declared pivot value: %#v", record)
	}
	if _, timed := record["created_at"]; timed {
		t.Fatalf("BaseAttachRecord stamped a timestamp it was not asked for: %#v", record)
	}
}

// TestAddTimestampsToAttachmentSkipsCreatedAtOnAnExistingRow, which is the
// difference between updating a pivot row and rewriting when it was made.
func TestAddTimestampsToAttachmentSkipsCreatedAtOnAnExistingRow(t *testing.T) {
	pivot, host, _ := newPivot("created_at", "updated_at")

	fresh := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	host.parent.(*fakeModel).timestamp = fresh

	fromNew := pivot.AddTimestampsToAttachment(map[string]any{}, false)
	if fromNew["created_at"] != fresh || fromNew["updated_at"] != fresh {
		t.Fatalf("a new row got %#v, want both timestamps", fromNew)
	}

	fromExisting := pivot.AddTimestampsToAttachment(map[string]any{}, true)
	if _, has := fromExisting["created_at"]; has {
		t.Fatalf("an existing row had created_at rewritten: %#v", fromExisting)
	}
	if fromExisting["updated_at"] != fresh {
		t.Fatalf("an existing row got %#v, want updated_at", fromExisting)
	}
}

// TestDifferenceAndIntersectionCompareByValueNotByBoxedType.
//
// The keys on one side come off a driver and the ones on the other from the
// caller, so int64(1) and 1 are the same row. Comparing the boxed values would
// make sync detach and reattach the same pair on every run.
func TestDifferenceAndIntersectionCompareByValueNotByBoxedType(t *testing.T) {
	left := []any{int64(1), "admin", 3}
	right := []any{1, "admin"}

	if got := difference(left, right); !reflect.DeepEqual(got, []any{3}) {
		t.Fatalf("difference = %#v, want only the key not on the right", got)
	}
	if got := intersection(left, right); !reflect.DeepEqual(got, []any{int64(1), "admin"}) {
		t.Fatalf("intersection = %#v, want the two keys both sides hold", got)
	}
}

// TestShouldTouchDefaultsToTouching: the variadic exists so the common call
// reads like the PHP, and the default has to be the PHP's.
func TestShouldTouchDefaultsToTouching(t *testing.T) {
	if !shouldTouch(nil) {
		t.Fatal("a call that said nothing about touching did not touch")
	}
	if !shouldTouch([]bool{true}) {
		t.Fatal("an explicit true did not touch")
	}
	if shouldTouch([]bool{false}) {
		t.Fatal("an explicit false touched")
	}
}
