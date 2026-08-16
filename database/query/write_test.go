package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

// TestInsertWritesTheTenantIntoTheRow: an insert has no where clause to filter,
// so the tenant is the column it writes.
func TestInsertWritesTheTenantIntoTheRow(t *testing.T) {
	connection := &fakeConnection{inserted: true}
	values := map[string]any{"email": "ana@example.com"}

	ok, err := newTestBuilder(connection).Insert(context.Background(), grant(), values)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the insert reported failure")
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"tenant_id"`) {
		t.Fatalf("the row was written without a tenant:\n%s", sql)
	}
	// The columns are sorted, so email comes before tenant_id and the bindings
	// have to arrive in that order or every value lands one column over.
	bindings := connection.lastBindings()
	if len(bindings) != 2 || bindings[0] != "ana@example.com" || bindings[1] != "acme" {
		t.Fatalf("the bindings do not follow the sorted columns: %#v", bindings)
	}
	if _, found := values["tenant_id"]; found {
		t.Fatal("the caller's map was written into")
	}
}

// TestInsertRefusesToWriteIntoAnotherTenant: the Grant wins over the value.
func TestInsertRefusesToWriteIntoAnotherTenant(t *testing.T) {
	connection := &fakeConnection{inserted: true}

	if _, err := newTestBuilder(connection).Insert(context.Background(), grant(),
		map[string]any{"email": "ana@example.com", "tenant_id": "globex"}); err != nil {
		t.Fatal(err)
	}
	for _, binding := range connection.lastBindings() {
		if binding == "globex" {
			t.Fatalf("a row was written into a tenant nobody authorized: %#v", connection.lastBindings())
		}
	}
}

func TestInsertOfManyRowsKeepsTheColumnsLinedUp(t *testing.T) {
	connection := &fakeConnection{inserted: true}

	if _, err := newTestBuilder(connection).Insert(context.Background(), grant(),
		map[string]any{"email": "ana@example.com", "role": "admin"},
		map[string]any{"role": "owner", "email": "bruno@example.com"}); err != nil {
		t.Fatal(err)
	}

	bindings := connection.lastBindings()
	want := []any{"ana@example.com", "admin", "acme", "bruno@example.com", "owner", "acme"}
	if len(bindings) != len(want) {
		t.Fatalf("the flattened bindings are %#v", bindings)
	}
	for i := range want {
		if bindings[i] != want[i] {
			t.Fatalf("binding %d is %#v, want %#v -- the second row was written in map order", i, bindings[i], want[i])
		}
	}
}

func TestInsertOfNothingRunsNothing(t *testing.T) {
	connection := &fakeConnection{}
	ok, err := newTestBuilder(connection).Insert(context.Background(), grant())
	if err != nil || !ok {
		t.Fatalf("an empty insert answered %v, %v", ok, err)
	}
	if len(connection.calls) != 0 {
		t.Fatal("an empty insert ran a statement")
	}
}

func TestInsertGetIDReadsTheIdThroughTheProcessor(t *testing.T) {
	connection := &fakeConnection{}
	id, err := newTestBuilder(connection).InsertGetID(context.Background(), grant(),
		map[string]any{"email": "ana@example.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Fatalf("insertGetId answered %d", id)
	}
}

func TestInsertOrIgnoreAnswersHowManyRowsItKept(t *testing.T) {
	connection := &fakeConnection{affected: 1}
	kept, err := newTestBuilder(connection).InsertOrIgnore(context.Background(), grant(),
		map[string]any{"email": "ana@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("insertOrIgnore answered %d", kept)
	}
	if !strings.HasPrefix(connection.lastSQL(), "insert or ignore into") {
		t.Fatalf("insertOrIgnore compiled as a plain insert:\n%s", connection.lastSQL())
	}
}

// TestInsertUsingScopesTheSelectItReadsFrom: an insert reading from a select is
// a read, and a read carries the tenant.
func TestInsertUsingScopesTheSelectItReadsFrom(t *testing.T) {
	connection := &fakeConnection{affected: 3}
	source := query.NewBuilder(connection, &fakeGrammar{}, &fakeProcessor{}).From("staff").Select("email")

	rows, err := newTestBuilder(connection).InsertUsing(context.Background(), grant(), []any{"email"}, source)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("insertUsing answered %d", rows)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"staff"."tenant_id" = ?`) {
		t.Fatalf("the select inside the insert was not scoped:\n%s", sql)
	}
	if bindings := connection.lastBindings(); len(bindings) != 1 || bindings[0] != "acme" {
		t.Fatalf("the subquery bindings did not survive: %#v", bindings)
	}
}

func TestUpdateFiltersByTenantAndBindsTheValuesFirst(t *testing.T) {
	connection := &fakeConnection{affected: 2}

	affected, err := newTestBuilder(connection).Where("role", "admin").
		Update(context.Background(), grant(), map[string]any{"role": "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if affected != 2 {
		t.Fatalf("update answered %d", affected)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Fatalf("the update was not scoped:\n%s", sql)
	}
	bindings := connection.lastBindings()
	if len(bindings) != 3 || bindings[0] != "owner" || bindings[1] != "acme" || bindings[2] != "admin" {
		t.Fatalf("the update bindings are %#v, and the set values come before the where", bindings)
	}
}

func TestUpdateCannotMoveARowToAnotherTenant(t *testing.T) {
	connection := &fakeConnection{affected: 1}

	if _, err := newTestBuilder(connection).Update(context.Background(), grant(),
		map[string]any{"tenant_id": "globex"}); err != nil {
		t.Fatal(err)
	}
	for _, binding := range connection.lastBindings() {
		if binding == "globex" {
			t.Fatalf("a row was handed to another tenant: %#v", connection.lastBindings())
		}
	}
}

func TestUpdateOrInsertInsertsWhenNothingMatches(t *testing.T) {
	connection := &fakeConnection{inserted: true}

	ok, err := newTestBuilder(connection).UpdateOrInsert(context.Background(), grant(),
		map[string]any{"email": "ana@example.com"}, map[string]any{"role": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("updateOrInsert reported failure")
	}
	if len(connection.calls) != 2 || connection.calls[1].kind != "insert" {
		t.Fatalf("updateOrInsert ran %+v", connection.calls)
	}
	if !strings.Contains(connection.calls[0].sql, `"email" = ?`) {
		t.Fatalf("the attributes did not become a where clause:\n%s", connection.calls[0].sql)
	}
}

func TestUpdateOrInsertUpdatesOneRowWhenSomethingMatches(t *testing.T) {
	connection := &fakeConnection{
		results:  [][]query.Record{{{"exists": int64(1)}}},
		affected: 1,
	}

	ok, err := newTestBuilder(connection).UpdateOrInsert(context.Background(), grant(),
		map[string]any{"email": "ana@example.com"}, map[string]any{"role": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("updateOrInsert reported failure")
	}
	if connection.calls[1].kind != "update" {
		t.Fatalf("updateOrInsert ran a %s", connection.calls[1].kind)
	}
	if !strings.Contains(connection.calls[1].sql, "limit") && !strings.Contains(connection.calls[1].sql, `"role" = ?`) {
		t.Fatalf("the update did not set what it was given:\n%s", connection.calls[1].sql)
	}
}

func TestUpsertNeedsUniqueColumns(t *testing.T) {
	connection := &fakeConnection{}
	if _, err := newTestBuilder(connection).Upsert(context.Background(), grant(),
		[]map[string]any{{"id": 1}}, nil, nil); err == nil {
		t.Fatal("an upsert with no conflict target was accepted")
	}
	if len(connection.calls) != 0 {
		t.Fatal("the upsert reached the connection")
	}
}

func TestUpsertWithAnEmptyUpdateListIsAnInsert(t *testing.T) {
	connection := &fakeConnection{inserted: true}

	rows, err := newTestBuilder(connection).Upsert(context.Background(), grant(),
		[]map[string]any{{"id": 1}}, []string{"id"}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("the upsert answered %d", rows)
	}
	if connection.calls[0].kind != "insert" {
		t.Fatalf("an empty update list did not become a plain insert: %s", connection.calls[0].kind)
	}
}

func TestUpsertUpdatesEveryColumnWhenTheListIsNil(t *testing.T) {
	connection := &fakeConnection{affected: 1}

	if _, err := newTestBuilder(connection).Upsert(context.Background(), grant(),
		[]map[string]any{{"id": 1, "email": "ana@example.com"}}, []string{"id", "tenant_id"}, nil); err != nil {
		t.Fatal(err)
	}
	sql := connection.lastSQL()
	if !strings.Contains(sql, "do update set email, id, tenant_id") {
		t.Fatalf("a nil update list did not become every column:\n%s", sql)
	}
	if !strings.Contains(sql, `"tenant_id"`) {
		t.Fatalf("the upserted rows carry no tenant:\n%s", sql)
	}
}

func TestIncrementWritesTheStepIntoTheStatement(t *testing.T) {
	connection := &fakeConnection{affected: 1}

	if _, err := newTestBuilder(connection).Increment(context.Background(), grant(), "visits", 2, nil); err != nil {
		t.Fatal(err)
	}
	sql := connection.lastSQL()
	if !strings.Contains(sql, `"visits" = "visits" + 2`) {
		t.Fatalf("increment compiled as:\n%s", sql)
	}
	if !strings.Contains(sql, "tenant_id") {
		t.Fatalf("increment touched every tenant:\n%s", sql)
	}
}

func TestDecrementEachStepsEveryColumnAndCarriesTheExtras(t *testing.T) {
	connection := &fakeConnection{affected: 1}

	if _, err := newTestBuilder(connection).DecrementEach(context.Background(), grant(),
		map[string]float64{"credits": 5}, map[string]any{"role": "guest"}); err != nil {
		t.Fatal(err)
	}
	sql := connection.lastSQL()
	if !strings.Contains(sql, `"credits" = "credits" - 5`) {
		t.Fatalf("decrementEach compiled as:\n%s", sql)
	}
	if !strings.Contains(sql, `"role" = ?`) {
		t.Fatalf("the extra column was dropped:\n%s", sql)
	}
}

func TestDeleteFiltersByTenant(t *testing.T) {
	connection := &fakeConnection{affected: 1}

	affected, err := newTestBuilder(connection).Delete(context.Background(), grant(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("delete answered %d", affected)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Fatalf("the delete was not scoped:\n%s", sql)
	}
	if !strings.Contains(sql, `"users"."id" = ?`) {
		t.Fatalf("the delete did not name the row:\n%s", sql)
	}
	bindings := connection.lastBindings()
	if len(bindings) != 2 || bindings[0] != "acme" || bindings[1] != 4 {
		t.Fatalf("the delete bindings are %#v", bindings)
	}
}

func TestTruncateStillNeedsAGrant(t *testing.T) {
	connection := &fakeConnection{}
	if err := newTestBuilder(connection).Truncate(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}
	if len(connection.calls) != 1 || connection.calls[0].kind != "statement" {
		t.Fatalf("truncate ran %+v", connection.calls)
	}
	if !strings.HasPrefix(connection.lastSQL(), "truncate table") {
		t.Fatalf("truncate compiled as:\n%s", connection.lastSQL())
	}
}

func TestUpdateFromRefusesAGrammarThatCannotCompileIt(t *testing.T) {
	connection := &fakeConnection{}
	_, err := newTestBuilder(connection).UpdateFrom(context.Background(), grant(), map[string]any{"role": "admin"})
	if err == nil {
		t.Fatal("updateFrom ran on an engine that does not have it")
	}
	if !strings.Contains(err.Error(), "updateFrom") {
		t.Fatalf("the refusal does not name the method: %v", err)
	}
	if len(connection.calls) != 0 {
		t.Fatal("the unsupported statement reached the connection")
	}
}
