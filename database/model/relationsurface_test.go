package model

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// The four things a relation asks of a model that nothing else does.

func TestUnsetAttributeReachesTheRawAttributesAndNotTheFields(t *testing.T) {
	m, _ := newUserModel()
	if err := m.SetRawAttributes(map[string]any{"name": "Ada", "posts_count": 3}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	refOf(m.base()).UnsetAttribute("posts_count")
	if got := m.GetAttribute("posts_count"); got != nil {
		t.Errorf("posts_count = %v after UnsetAttribute, want nil", got)
	}

	// A struct field is not a raw attribute and cannot be removed. Setting it to
	// its zero value would be a different thing said with the same word, so the
	// call is a no-op rather than a surprise.
	refOf(m.base()).UnsetAttribute("name")
	if got := m.GetAttribute("name"); got != "Ada" {
		t.Errorf("name = %v after UnsetAttribute, want it untouched", got)
	}
}

func TestIsRelationReadsTheDeclarationAndNotTheLoadedValue(t *testing.T) {
	m, _ := newUserModel()
	declared(m, "posts")

	// Declared and not loaded: still a relation. That is the question being
	// asked, and answering it from the loaded values would say no.
	if !refOf(m.base()).IsRelation("posts") {
		t.Error("a declared relation that is not loaded read as not a relation")
	}
	if refOf(m.base()).IsRelation("name") {
		t.Error("a column read as a relation")
	}
}

func TestTouchesReadsTheListTheTableNames(t *testing.T) {
	m, _ := newUserModel()

	if refOf(m.base()).Touches("posts") {
		t.Error("a model touches something by default; it must not")
	}

	touching, _ := newUserModel(func(s *TableSpec) { s.Touches = []string{"posts"} })
	if !refOf(touching.base()).Touches("posts") {
		t.Error("Touches did not read the list the spec named")
	}
	if refOf(touching.base()).Touches("comments") {
		t.Error("Touches answered for a relation that is not in the list")
	}
}

func TestTouchStampsTheUpdatedAtColumn(t *testing.T) {
	m, conn := newUserModel()
	conn.queue()

	instance, err := fromRecord(m, map[string]any{
		"id": int64(1), "name": "Ada", "tenant_id": "acme",
		"updated_at": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	if err := instance.Touch(context.Background(), auth.SystemGrant("users.write", "acme")); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	if instance.UpdatedAt.Year() == 2020 {
		t.Error("Touch left the old timestamp in place")
	}
	if len(conn.statements) == 0 {
		t.Fatal("Touch stamped the model and never saved it")
	}
}

// TestTouchIsANoOpOnAModelWithNothingToStamp: not an error, because there is
// nothing wrong with a model that carries no timestamps -- there is just
// nothing to do.
func TestTouchIsANoOpOnAModelWithNothingToStamp(t *testing.T) {
	m, conn := newUserModel(func(s *TableSpec) { s.NoTimestamps = true })

	if err := m.Touch(context.Background(), auth.SystemGrant("users.write", "acme")); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("it wrote %d statements for a model with no timestamps", len(conn.statements))
	}
}
