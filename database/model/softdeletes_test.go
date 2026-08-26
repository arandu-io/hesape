package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database/query"
)

func newSoftDeletingUserModel() (*Model[user], *testConnection) {
	model, conn := newUserModel()
	model.SoftDeletes = true
	return model, conn
}

func TestASoftDeletingModelFiltersTheDeletedRowsOut(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	conn.queue()

	if _, err := model.NewQuery().Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(conn.last().SQL, `"users"."deleted_at" is null`) {
		t.Fatalf("SQL = %q, want the soft delete scope applied", conn.last().SQL)
	}
}

func TestWithTrashedTakesTheScopeOff(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	conn.queue()

	if _, err := model.NewQuery().WithTrashed().Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(conn.last().SQL, "deleted_at") {
		t.Fatalf("SQL = %q, want no deleted_at filter", conn.last().SQL)
	}
	if !strings.Contains(conn.last().SQL, `"users"."tenant_id" = ?`) {
		t.Error("withTrashed removed the tenant filter too, and it must not: the tenant is not a scope anybody takes off")
	}
}

func TestOnlyTrashedKeepsTheDeletedRows(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	conn.queue()

	if _, err := model.NewQuery().OnlyTrashed().Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(conn.last().SQL, `"users"."deleted_at" is not null`) {
		t.Fatalf("SQL = %q, want only the deleted rows", conn.last().SQL)
	}
}

func TestWithoutTrashedPutsTheFilterBack(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	conn.queue()

	if _, err := model.NewQuery().WithTrashed().WithoutTrashed().Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(conn.last().SQL, `"users"."deleted_at" is null`) {
		t.Fatalf("SQL = %q, want the filter back", conn.last().SQL)
	}
}

func TestDeleteMarksTheRowInsteadOfRemovingIt(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7), "name": "Ada"}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	deleted, err := model.Delete(context.Background(), grant())
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}

	sql := conn.last().SQL
	if !strings.HasPrefix(sql, `update "users" set`) {
		t.Fatalf("SQL = %q, want an update", sql)
	}
	if !strings.Contains(sql, `"deleted_at" = ?`) {
		t.Errorf("SQL = %q, want deleted_at written", sql)
	}
	if !model.Trashed() {
		t.Error("Trashed() = false on the model that was just soft deleted")
	}
	if !model.Exists {
		t.Error("a soft deleted model still exists, and PHP keeps exists true for exactly that reason")
	}
}

func TestForceDeleteRemovesTheRow(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7)}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	deleted, err := model.ForceDelete(context.Background(), grant())
	if err != nil || !deleted {
		t.Fatalf("ForceDelete = %v, %v", deleted, err)
	}
	sql := conn.last().SQL
	if !strings.HasPrefix(sql, `delete from "users"`) {
		t.Fatalf("SQL = %q, want a real delete", sql)
	}
	if !strings.Contains(sql, `"tenant_id" = ?`) {
		t.Error(`SQL is missing the tenant: "force" is about the soft delete, never about the tenant`)
	}
	if model.Exists {
		t.Error("the model still reports that it exists")
	}
}

func TestForceDeleteFiresItsOwnEvents(t *testing.T) {
	model, _ := newSoftDeletingUserModel()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7)}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	var fired []Event
	for _, event := range []Event{ForceDeleting, Deleting, Deleted, ForceDeleted} {
		event := event
		model.RegisterModelEvent(event, func(*Model[user]) error {
			fired = append(fired, event)
			return nil
		})
	}

	if _, err := model.ForceDelete(context.Background(), grant()); err != nil {
		t.Fatalf("ForceDelete: %v", err)
	}
	want := []Event{ForceDeleting, Deleting, Deleted, ForceDeleted}
	if len(fired) != len(want) {
		t.Fatalf("fired %v, want %v", fired, want)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Fatalf("fired %v, want %v", fired, want)
		}
	}
}

func TestRestoreClearsTheColumn(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	deletedAt := time.Now()
	if err := model.SetRawAttributes(map[string]any{"id": int64(7), "deleted_at": deletedAt}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}
	model.Exists = true

	restored, err := model.Restore(context.Background(), grant())
	if err != nil || !restored {
		t.Fatalf("Restore = %v, %v", restored, err)
	}
	if model.Trashed() {
		t.Error("Trashed() = true after a restore")
	}
	if !strings.Contains(conn.last().SQL, `"deleted_at" = ?`) {
		t.Fatalf("SQL = %q, want deleted_at cleared", conn.last().SQL)
	}
	if conn.last().Bindings[0] != (*time.Time)(nil) {
		t.Errorf("bindings = %v, want a null for deleted_at", conn.last().Bindings)
	}
}

func TestBuilderRestoreUpdatesEveryTrashedRowItMatches(t *testing.T) {
	model, conn := newSoftDeletingUserModel()

	if _, err := model.NewQuery().Where("name", "=", "Ada").Restore(context.Background(), grant()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	sql := conn.last().SQL
	if !strings.HasPrefix(sql, `update "users" set`) || !strings.Contains(sql, `"deleted_at" = ?`) {
		t.Fatalf("SQL = %q, want the update that un-deletes", sql)
	}
	if strings.Contains(sql, "deleted_at\" is null") {
		t.Error("restore ran with the soft delete scope still on, so it could only match rows that are not deleted")
	}
}

func TestSoftDeleteMethodsRefuseAModelThatDoesNotSoftDelete(t *testing.T) {
	model, _ := newUserModel()
	conn := model.connection.(*testConnection)
	conn.queue()

	if _, err := model.NewQuery().WithTrashed().Get(context.Background(), grant()); err == nil {
		t.Fatal("withTrashed on a model without soft deletes has to say so, not filter nothing quietly")
	}
	if _, err := model.NewQuery().Restore(context.Background(), grant()); err == nil {
		t.Fatal("restore on a model without soft deletes has to say so")
	}
}

func TestDeletingThroughTheBuilderGoesThroughTheScope(t *testing.T) {
	model, conn := newSoftDeletingUserModel()

	if _, err := model.NewQuery().Where("name", "=", "Ada").Delete(context.Background(), grant()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.HasPrefix(conn.last().SQL, `update "users" set`) {
		t.Fatalf("SQL = %q: a delete on a soft deleting model is the update the scope registered", conn.last().SQL)
	}
}

func TestBuilderForceDeleteIgnoresTheScope(t *testing.T) {
	model, conn := newSoftDeletingUserModel()

	if _, err := model.NewQuery().Where("name", "=", "Ada").ForceDelete(context.Background(), grant()); err != nil {
		t.Fatalf("ForceDelete: %v", err)
	}
	if !strings.HasPrefix(conn.last().SQL, `delete from "users"`) {
		t.Fatalf("SQL = %q, want a real delete", conn.last().SQL)
	}
}

func TestSoftDeletedRowsAreStillReadableWithTrashed(t *testing.T) {
	model, conn := newSoftDeletingUserModel()
	deletedAt := time.Now()
	conn.queue(query.Record{"id": int64(7), "deleted_at": deletedAt})

	models, err := model.NewQuery().WithTrashed().Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 1 || !models[0].Trashed() {
		t.Fatalf("got %d models, trashed = %v", len(models), len(models) == 1 && models[0].Trashed())
	}
}
