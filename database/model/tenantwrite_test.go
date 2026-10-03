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
	Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`
	Body     string `db:"body"`
}

// sqliteNotes is a notes model on the SQLite grammar over a recording
// connection.
func sqliteNotes(configure ...func(*TableSpec)) (*note, *testConnection) {
	conn := newTestConnection()
	spec := TableSpec{Name: "notes", New: func() Entity { return new(note) }, ManualKey: true, NoTimestamps: true}
	for _, c := range configure {
		c(&spec)
	}
	db := grammarDB{conn, grammars.NewSQLiteGrammar(), &testProcessor{conn: conn}}
	return NewTable(spec).New(db).(*note), conn
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
			if _, err := newQuery(m.base()).Where("id", "=", "n1").Update(context.Background(), acmeNotes,
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
	loaded, err := instanceOf(m, nil, true)
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
	if loaded.TenantID != "acme" || loaded.ID != "n1" || loaded.Body != "edited" {
		t.Fatalf("entity = %+v, want body edited and the tenant and key untouched", loaded.GetAttributes())
	}
	assertNotWritten(t, conn, "globex", "n9")
}

// TestSaveCannotMoveARowWhoseFieldWasChanged: assigning the field directly is
// the explicit path, and the write still takes its tenant from the Grant.
func TestSaveCannotMoveARowWhoseFieldWasChanged(t *testing.T) {
	m, conn := sqliteNotes()
	loaded, _ := instanceOf(m, nil, true)
	_ = loaded.SetRawAttributes(map[string]any{"id": "n1", "tenant_id": "acme", "body": "x"}, true)

	loaded.TenantID = "globex"
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

	if _, err := newQuery(m.base()).UpdateOrCreate(context.Background(), acmeNotes,
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
	if _, err := newQuery(m.base()).Create(context.Background(), acmeNotes,
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

// TestCreateHandsBackTheTenantItWrote: Fill skips the tenant column, and the
// insert wrote the Grant's tenant into the row but not into the entity, so a
// caller reading TenantID off what Create returned read the empty string.
func TestCreateHandsBackTheTenantItWrote(t *testing.T) {
	m, _ := sqliteNotes()
	created, err := newQuery(m.base()).Create(context.Background(), acmeNotes,
		map[string]any{"id": "n1", "body": "hello", "tenant_id": "globex"})
	if err != nil {
		t.Fatal(err)
	}
	if got := created.(*note).TenantID; got != auth.Tenant(acmeNotes) {
		t.Fatalf("TenantID = %q, want %q", got, auth.Tenant(acmeNotes))
	}
}

// TestSavingANewStructHandsBackTheTenantItWrote: the struct path, with the
// field set by hand to another tenant. The row takes the Grant's tenant, and so
// does the value the caller holds.
func TestSavingANewStructHandsBackTheTenantItWrote(t *testing.T) {
	m, conn := sqliteNotes()
	fresh, err := instanceOf(m, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	fresh.ID, fresh.TenantID, fresh.Body = "n1", "globex", "hello"
	if _, err := fresh.Save(context.Background(), acmeNotes); err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex")
	if fresh.TenantID != "acme" {
		t.Fatalf("TenantID = %q, want acme", fresh.TenantID)
	}
	if fresh.IsDirty() {
		t.Fatalf("a saved row is dirty: %v", fresh.GetDirty())
	}
}

// TestACreatingListenerCannotLeaveTheEntityOnAnotherTenant: a listener runs
// after the first stamp, and the row is still written with the Grant's tenant.
func TestACreatingListenerCannotLeaveTheEntityOnAnotherTenant(t *testing.T) {
	m, conn := sqliteNotes(func(s *TableSpec) {
		s.Events = map[Event][]func(Entity) error{Creating: {func(e Entity) error {
			e.(*note).TenantID = "globex"
			return nil
		}}}
	})
	created, err := newQuery(m.base()).Create(context.Background(), acmeNotes, map[string]any{"id": "n1", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex")
	if got := created.(*note).TenantID; got != "acme" {
		t.Fatalf("TenantID = %q, want acme", got)
	}
}

// TestSaveOfAMovedFieldPutsTheEntityBack: the update keeps the row with the
// Grant's tenant, and the entity is put back to match it.
func TestSaveOfAMovedFieldPutsTheEntityBack(t *testing.T) {
	m, conn := sqliteNotes()
	loaded, _ := instanceOf(m, nil, true)
	_ = loaded.SetRawAttributes(map[string]any{"id": "n1", "tenant_id": "acme", "body": "x"}, true)

	loaded.TenantID = "globex"
	loaded.Body = "edited"
	if _, err := loaded.Save(context.Background(), acmeNotes); err != nil {
		t.Fatal(err)
	}
	assertNotWritten(t, conn, "globex")
	if loaded.TenantID != "acme" || loaded.Body != "edited" {
		t.Fatalf("entity = %+v, want body edited on tenant acme", loaded.GetAttributes())
	}
}

// TestAnEntityWithNoTenantFieldStillSaves: the tenant column is written from
// the Grant, and there is no field to hand it back in.
func TestAnEntityWithNoTenantFieldStillSaves(t *testing.T) {
	type bare struct {
		Model
		ID   string `db:"id"`
		Body string `db:"body"`
	}
	conn := newTestConnection()
	table := NewTable(TableSpec{Name: "notes", New: func() Entity { return new(bare) }, ManualKey: true, NoTimestamps: true})
	db := grammarDB{conn, grammars.NewSQLiteGrammar(), &testProcessor{conn: conn}}
	if _, err := table.Query(db).Create(context.Background(), acmeNotes, map[string]any{"id": "n1", "body": "x"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range conn.last().Bindings {
		if binding == "acme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the Grant's tenant was not inserted: %#v", conn.last().Bindings)
	}
}
