package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// tenantSpellings are the keys that reach the tenant column on SQLite without
// being spelled "tenant_id": the grammar strips the qualifier off the left of a
// SET, and SQLite resolves a column name in any case.
var tenantSpellings = []string{"tenant_id", "notes.tenant_id", "TENANT_ID", " tenant_id", `"tenant_id"`}

// sqliteNotes is a builder over notes on the SQLite grammar, recording what it
// is asked to run.
func sqliteNotes(connection *fakeConnection) *query.Builder {
	return query.NewBuilder(connection, grammars.NewSQLiteGrammar(), &fakeProcessor{connection: connection}).From("notes")
}

// TestAnUpdateCannotMoveARowToAnotherTenantUnderAnySpelling: Update replaced a
// value under the exact key "tenant_id" with the Grant's tenant, and wrote
// "notes.tenant_id" and "TENANT_ID" as given -- each one moving the row out of
// the tenant whose Grant reached it, past a where clause that had already
// matched.
func TestAnUpdateCannotMoveARowToAnotherTenantUnderAnySpelling(t *testing.T) {
	for _, key := range tenantSpellings {
		t.Run(key, func(t *testing.T) {
			connection := &fakeConnection{affected: 1}
			if _, err := sqliteNotes(connection).Where("id", "=", "n1").
				Update(context.Background(), grant(), map[string]any{key: "globex", "body": "y"}); err != nil {
				t.Fatal(err)
			}
			for _, binding := range connection.lastBindings() {
				if binding == "globex" {
					t.Fatalf("%q wrote another tenant into the row:\n%s\n%#v", key, connection.lastSQL(), connection.lastBindings())
				}
			}
			if strings.Count(strings.ToLower(connection.lastSQL()), "tenant_id") != 2 {
				t.Fatalf("want one SET of the tenant column and one filter on it:\n%s", connection.lastSQL())
			}
		})
	}
}

// TestAnInsertCarriesOneTenantUnderAnySpelling: Insert stamped "tenant_id" over
// the exact key and left "TENANT_ID" beside it, a second value for the same
// column.
func TestAnInsertCarriesOneTenantUnderAnySpelling(t *testing.T) {
	for _, key := range tenantSpellings {
		t.Run(key, func(t *testing.T) {
			connection := &fakeConnection{inserted: true}
			if _, err := sqliteNotes(connection).Insert(context.Background(), grant(),
				map[string]any{"id": "n2", key: "globex"}); err != nil {
				t.Fatal(err)
			}
			for _, binding := range connection.lastBindings() {
				if binding == "globex" {
					t.Fatalf("%q inserted the row into another tenant:\n%s", key, connection.lastSQL())
				}
			}
		})
	}
}

func TestNamesColumnIgnoresQualifierQuotesAndCase(t *testing.T) {
	for _, key := range []string{"tenant_id", "notes.tenant_id", `"notes"."tenant_id"`, "TENANT_ID", "`tenant_id`", " tenant_id "} {
		if !query.NamesColumn(key, "tenant_id") {
			t.Errorf("NamesColumn(%q, tenant_id) = false", key)
		}
	}
	for _, key := range []string{"tenant", "tenant_ids", "owner_tenant_id", ""} {
		if query.NamesColumn(key, "tenant_id") {
			t.Errorf("NamesColumn(%q, tenant_id) = true", key)
		}
	}
}
