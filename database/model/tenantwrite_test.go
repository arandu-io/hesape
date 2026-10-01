package model

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// note is the row of the mass-assignment reproduction: a string key the
// application chooses, a tenant column, and one field a form edits.
type note struct {
	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`
	Body     string `db:"body"`
}

// sqliteNotes is a notes model on the SQLite grammar over a recording
// connection.
func sqliteNotes() (*Model[note], *testConnection) {
	conn := newTestConnection()
	m := NewModel[note]("notes", conn, grammars.NewSQLiteGrammar(), &testProcessor{conn: conn})
	m.KeyType = "string"
	m.Incrementing = false
	m.Timestamps = false
	return m, conn
}

var acmeNotes = auth.SystemGrant("notes.write", "acme")

// assertNotWritten fails when any statement the connection ran bound one of
// the values.
func assertNotWritten(t *testing.T, conn *testConnection, values ...any) {
	t.Helper()
	for _, s := range conn.statements {
		for _, binding := range s.Bindings {
			for _, value := range values {
				if binding == value {
					t.Fatalf("%v was written:\n%s\n%#v", value, s.SQL, s.Bindings)
				}
			}
		}
	}
}

// TestABuilderUpdateCannotMoveARowToAnotherTenant: Update(ctx, g, r.All())
// wrote whatever tenant the map carried, under the exact column name or any
// other spelling SQLite resolves to it, and the row left acme for globex.
func TestABuilderUpdateCannotMoveARowToAnotherTenant(t *testing.T) {
	for _, key := range []string{"tenant_id", "notes.tenant_id", "TENANT_ID"} {
		t.Run(key, func(t *testing.T) {
			m, conn := sqliteNotes()
			if _, err := m.Where("id", "=", "n1").Update(context.Background(), acmeNotes,
				map[string]any{"body": "edited", key: "globex"}); err != nil {
				t.Fatal(err)
			}
			assertNotWritten(t, conn, "globex")
		})
	}
}

// TestFillLeavesTheTenantAndTheKeyOfAnExistingRowAlone: Model.Update fills
// from the request's map, and that map carries whatever keys its sender added.
func TestFillLeavesTheTenantAndTheKeyOfAnExistingRowAlone(t *testing.T) {
	m, conn := sqliteNotes()
	loaded, err := m.NewInstance(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.SetRawAttributes(map[string]any{"id": "n1", "tenant_id": "acme", "body": "x"}, true); err != nil {
		t.Fatal(err)
	}

	if _, err := loaded.Update(context.Background(), acmeNotes,
		map[string]any{"body": "edited", "tenant_id": "globex", "id": "n9"}); err != nil {
		t.Fatal(err)
	}
	if loaded.Entity.TenantID != "acme" || loaded.Entity.ID != "n1" || loaded.Entity.Body != "edited" {
		t.Fatalf("entity = %+v, want body edited and the tenant and key untouched", *loaded.Entity)
	}
	assertNotWritten(t, conn, "globex", "n9")
}

// TestSaveCannotMoveARowWhoseFieldWasChanged: assigning the field directly is
// the explicit path, and the write still takes its tenant from the Grant.
func TestSaveCannotMoveARowWhoseFieldWasChanged(t *testing.T) {
	m, conn := sqliteNotes()
	loaded, _ := m.NewInstance(nil, true)
	_ = loaded.SetRawAttributes(map[string]any{"id": "n1", "tenant_id": "acme", "body": "x"}, true)

	loaded.Entity.TenantID = "globex"
	if _, err := loaded.Save(context.Background(), acmeNotes); err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex")
}

// TestUpdateOrCreateCannotMoveTheRowItFound: the values fill the row found and
// are saved, through the same Fill.
func TestUpdateOrCreateCannotMoveTheRowItFound(t *testing.T) {
	m, conn := sqliteNotes()
	conn.queue(query.Record{"id": "n1", "tenant_id": "acme", "body": "x"})

	if _, err := m.NewQuery().UpdateOrCreate(context.Background(), acmeNotes,
		map[string]any{"id": "n1"}, map[string]any{"body": "edited", "tenant_id": "globex"}); err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex")
}

// TestCreateTakesTheTenantFromTheGrantAndTheKeyFromTheMap: a new row's key
// still comes from the map, since nothing generates a string key, and its
// tenant still never does.
func TestCreateTakesTheTenantFromTheGrantAndTheKeyFromTheMap(t *testing.T) {
	m, conn := sqliteNotes()
	if _, err := m.Create(context.Background(), acmeNotes,
		map[string]any{"id": "n1", "body": "hello", "tenant_id": "globex", "TENANT_ID": "initech"}); err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex", "initech")
	found := false
	for _, binding := range conn.last().Bindings {
		if binding == "n1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the key the map named was not inserted: %#v", conn.last().Bindings)
	}
}
