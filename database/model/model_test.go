package model

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

func grant() auth.Grant { return auth.SystemGrant("users.write", "acme") }

func TestFillWritesDeclaredColumnsAndDropsTheRest(t *testing.T) {
	model, _ := newUserModel()

	if err := model.Fill(map[string]any{"name": "Ada", "nickname": "the countess"}); err != nil {
		t.Fatalf("Fill: %v", err)
	}

	if model.Entity.Name != "Ada" {
		t.Errorf("Entity.Name = %q, want Ada: a declared column is what Fill writes", model.Entity.Name)
	}
	if _, ok := model.attributes["nickname"]; ok {
		t.Error("Fill kept nickname, and it must drop what the entity does not declare -- that is fill() outside $fillable")
	}
}

func TestForceFillKeepsAnUndeclaredColumn(t *testing.T) {
	model, _ := newUserModel()

	if err := model.ForceFill(map[string]any{"name": "Ada", "posts_count": int64(3)}); err != nil {
		t.Fatalf("ForceFill: %v", err)
	}

	if got := model.GetAttribute("posts_count"); got != int64(3) {
		t.Errorf("GetAttribute(posts_count) = %v, want 3: unguarded mass assignment keeps a key with no property behind it", got)
	}
}

func TestFillCannotReachAnUnexportedField(t *testing.T) {
	model, _ := newUserModel()

	if err := model.Fill(map[string]any{"secret": "hunter2"}); err != nil {
		t.Fatalf("Fill: %v", err)
	}

	if model.Entity.secret != "" {
		t.Error("Fill wrote an unexported field, and the compiler is what stops that -- it is this framework's $guarded")
	}
	if _, ok := fieldByColumn(reflectTypeOfUser(), "secret"); ok {
		t.Error("an unexported field must not be a column at all")
	}
}

func TestFillRefusesAValueThatDoesNotFitTheField(t *testing.T) {
	model, _ := newUserModel()

	err := model.Fill(map[string]any{"name": 7})
	if err == nil {
		t.Fatal("Fill accepted an int for a string column; Go would convert it to a rune, which is silently the wrong value")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, and it has to name the column", err)
	}
}

func TestColumnNameFallsBackToSnakeCase(t *testing.T) {
	type untagged struct {
		ID        int64
		FirstName string
		Skipped   string `db:"-"`
	}
	columns := fieldsOf(reflectTypeOf[untagged]())

	var names []string
	for _, column := range columns {
		names = append(names, column.column)
	}
	want := []string{"id", "first_name"}
	if len(names) != len(want) {
		t.Fatalf("columns = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("columns = %v, want %v", names, want)
		}
	}
}

func TestDirtyTracking(t *testing.T) {
	model, _ := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(1), "name": "Ada"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	if model.IsDirty() {
		t.Error("a model that was just synced is clean")
	}

	model.Entity.Name = "Grace"

	if !model.IsDirty("name") {
		t.Error("IsDirty(name) = false after the field changed")
	}
	if !model.IsClean("email") {
		t.Error("IsClean(email) = false, and email did not change")
	}
	if got := model.GetDirty()["name"]; got != "Grace" {
		t.Errorf("GetDirty()[name] = %v, want Grace", got)
	}

	model.SyncChanges()
	if !model.WasChanged("name") {
		t.Error("WasChanged(name) = false after SyncChanges")
	}
	if got := model.GetPrevious()["name"]; got != "Ada" {
		t.Errorf("GetPrevious()[name] = %v, want Ada", got)
	}
}

func TestSaveInsertsWithTheTenantFromTheGrantAndSetsTheKey(t *testing.T) {
	model, conn := newUserModel()
	model.Entity.Name = "Ada"
	model.Entity.TenantID = "somebody-elses"

	saved, err := model.Save(context.Background(), grant())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saved {
		t.Fatal("Save reported nothing written")
	}

	last := conn.last()
	if !strings.HasPrefix(last.SQL, `insert into "users"`) {
		t.Fatalf("SQL = %q, want an insert", last.SQL)
	}
	if !strings.Contains(last.SQL, `"tenant_id"`) {
		t.Errorf("SQL = %q, and every insert carries the tenant column", last.SQL)
	}
	if model.Entity.TenantID == "acme" {
		t.Log("the entity keeps what the caller set; what is written is the grant's tenant")
	}
	if got := last.Bindings[4]; got != "acme" {
		t.Errorf("tenant binding = %v, want acme: the tenant comes from the Grant and nowhere else (RULE 14)", got)
	}
	if model.Entity.ID != 1 {
		t.Errorf("Entity.ID = %d, want 1: an incrementing insert sets the key", model.Entity.ID)
	}
	if !model.Exists || !model.WasRecentlyCreated {
		t.Error("after an insert the model exists and was recently created")
	}
	if model.Entity.CreatedAt.IsZero() || model.Entity.UpdatedAt.IsZero() {
		t.Error("timestamps were not written")
	}
}

func TestInsertLeavesTheIncrementingKeyOut(t *testing.T) {
	model, conn := newUserModel()
	model.Entity.Name = "Ada"

	if _, err := model.Save(context.Background(), grant()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if strings.Contains(conn.last().SQL, `"id"`) {
		t.Errorf("SQL = %q: an incrementing insert must not send a zero id, which PHP never has because an unset property is simply absent", conn.last().SQL)
	}
}

func TestSaveUpdatesOnlyTheDirtyColumns(t *testing.T) {
	model, conn := newUserModel()
	if err := model.SetRawAttributes(map[string]any{
		"id": int64(7), "name": "Ada", "email": "ada@example.com", "tenant_id": "acme",
	}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true
	model.Entity.Name = "Grace"

	if _, err := model.Save(context.Background(), grant()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sql := conn.last().SQL
	if !strings.HasPrefix(sql, `update "users" set`) {
		t.Fatalf("SQL = %q, want an update", sql)
	}
	if strings.Contains(sql, `"email"`) {
		t.Errorf("SQL = %q: an unchanged column is not in the SET list", sql)
	}
	if !strings.Contains(sql, `"name" = ?`) || !strings.Contains(sql, `"updated_at" = ?`) {
		t.Errorf("SQL = %q, want name and updated_at in the SET list", sql)
	}
	if !strings.Contains(sql, `"tenant_id" = ?`) {
		t.Errorf("SQL = %q: an update is scoped by tenant like every other statement (RULE 17)", sql)
	}
	if !strings.Contains(sql, `"users"."id" = ?`) {
		t.Errorf("SQL = %q, want the row named by its key", sql)
	}
}

func TestSaveOnACleanModelWritesNothing(t *testing.T) {
	model, conn := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7), "name": "Ada"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	saved, err := model.Save(context.Background(), grant())
	if err != nil || !saved {
		t.Fatalf("Save = %v, %v; a clean model saves as true with no statement", saved, err)
	}
	if len(conn.sqls()) != 0 {
		t.Errorf("statements = %v, want none", conn.sqls())
	}
}

func TestDeleteRemovesTheRowByKeyAndTenant(t *testing.T) {
	model, conn := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7)}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	deleted, err := model.Delete(context.Background(), grant())
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}

	sql := conn.last().SQL
	if !strings.HasPrefix(sql, `delete from "users"`) {
		t.Fatalf("SQL = %q, want a delete", sql)
	}
	if !strings.Contains(sql, `"tenant_id" = ?`) {
		t.Errorf("SQL = %q: a delete is scoped by tenant", sql)
	}
	if model.Exists {
		t.Error("the model still reports that it exists after a hard delete")
	}
}

func TestDeleteOnAModelThatWasNeverSavedIsNotAFailure(t *testing.T) {
	model, conn := newUserModel()

	deleted, err := model.Delete(context.Background(), grant())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted {
		t.Error("Delete reported a row deleted, and there was none")
	}
	if len(conn.sqls()) != 0 {
		t.Errorf("statements = %v, want none", conn.sqls())
	}
}

func TestEventCallbackStopsTheSave(t *testing.T) {
	model, conn := newUserModel()
	refused := errors.New("no")
	model.RegisterModelEvent(Saving, func(*Model[user]) error { return refused })

	if _, err := model.Save(context.Background(), grant()); !errors.Is(err, refused) {
		t.Fatalf("Save error = %v, want the callback's; returning false in PHP halts the save and says nothing", err)
	}
	if len(conn.sqls()) != 0 {
		t.Errorf("statements = %v, want none: a refused saving event writes nothing", conn.sqls())
	}
}

func TestWithoutEventsMutesTheCallback(t *testing.T) {
	model, _ := newUserModel()
	fired := false
	model.RegisterModelEvent(Creating, func(*Model[user]) error {
		fired = true
		return nil
	})

	if _, err := model.SaveQuietly(context.Background(), grant()); err != nil {
		t.Fatalf("SaveQuietly: %v", err)
	}
	if fired {
		t.Error("a quiet save fired a model event")
	}
}

func TestReplicateDropsTheKeyAndTheTimestamps(t *testing.T) {
	model, _ := newUserModel()
	if err := model.SetRawAttributes(map[string]any{
		"id": int64(7), "name": "Ada", "created_at": time.Now(), "updated_at": time.Now(),
	}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	copied, err := model.Replicate()
	if err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	if copied.ID != 0 {
		t.Errorf("copy carries id %d, and a replica is a new row", copied.ID)
	}
	if !copied.CreatedAt.IsZero() {
		t.Error("copy carries the original created_at")
	}
	if copied.Name != "Ada" {
		t.Errorf("copy lost the columns it should keep: name = %q", copied.Name)
	}

	// Whether the replica exists is the model's answer, and this entity does not
	// embed one, so the model-side form is what carries it.
	replica, err := model.replicate()
	if err != nil {
		t.Fatalf("replicate: %v", err)
	}
	if replica.Exists {
		t.Error("a replica does not exist yet")
	}
}

func TestIsComparesKeyTableAndConnection(t *testing.T) {
	model, _ := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7)}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	same, err := model.NewInstance(map[string]any{"id": int64(7)}, true)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	other, err := model.NewInstance(map[string]any{"id": int64(8)}, true)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	if !model.Is(same) {
		t.Error("Is = false for the same row of the same table")
	}
	if !model.IsNot(other) {
		t.Error("IsNot = false for another key")
	}
}

func TestHiddenAndAppendedAttributes(t *testing.T) {
	model, _ := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(1), "name": "Ada", "email": "ada@example.com"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.MakeHidden("email")

	array := model.ToArray()
	if _, ok := array["email"]; ok {
		t.Error("a hidden column is still serialised")
	}
	if _, ok := array["name"]; !ok {
		t.Error("a visible column was dropped")
	}

	model.MakeVisible("email")
	if _, ok := model.ToArray()["email"]; !ok {
		t.Error("MakeVisible did not bring the column back")
	}

	model.SetRelation("posts", []string{"one"})
	model.Append("posts")
	json, err := model.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if !strings.Contains(string(json), "posts") {
		t.Errorf("ToJSON = %s, want the appended relation in it", json)
	}
}

func TestOnlyAndExcept(t *testing.T) {
	model, _ := newUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(1), "name": "Ada", "email": "ada@example.com"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	only := model.Only("name")
	if len(only) != 1 || only["name"] != "Ada" {
		t.Errorf("Only(name) = %v", only)
	}
	if _, ok := model.Except("name")["name"]; ok {
		t.Error("Except kept the column it was told to drop")
	}
}

func TestGetTableFallsBackToThePluralOfTheType(t *testing.T) {
	conn := newTestConnection()
	model := NewModel[user]("", conn, newTestGrammar(), &testProcessor{conn: conn})

	if got := model.GetTable(); got != "users" {
		t.Errorf("GetTable() = %q, want users", got)
	}
}

func TestAssignParsesATimestampADriverWroteAsText(t *testing.T) {
	model, _ := newUserModel()

	if err := model.SetRawAttributes(map[string]any{"created_at": "2026-08-11 09:30:00"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	if got := model.Entity.CreatedAt.Format("2006-01-02 15:04:05"); got != "2026-08-11 09:30:00" {
		t.Errorf("created_at = %q, and SQLite has no date type -- it hands back text", got)
	}
}

func TestTransactionalSaveSaysSoWhenTheConnectionCannot(t *testing.T) {
	model, _ := newUserModel()

	_, err := model.SaveOrFail(context.Background(), grant())
	if err == nil || !strings.Contains(err.Error(), "transaction") {
		t.Fatalf("SaveOrFail error = %v, want a refusal naming the transaction: writing outside one the caller believes in is worse", err)
	}
}

func reflectTypeOfUser() reflect.Type { return reflect.TypeFor[user]() }

func reflectTypeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }
