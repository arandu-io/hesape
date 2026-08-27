package model

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// The four methods a relation asks of a model, and the two accessors that
// carry the list one of them reads.

func TestUnsetAttributeReachesTheRawAttributesAndNotTheFields(t *testing.T) {
	m, _ := newUserModel()
	if err := m.SetRawAttributes(map[string]any{"name": "Ada", "posts_count": 3}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	m.UnsetAttribute("posts_count")
	if got := m.GetAttribute("posts_count"); got != nil {
		t.Errorf("posts_count = %v after UnsetAttribute, want nil", got)
	}

	// A struct field is not a raw attribute and cannot be removed. Setting it to
	// its zero value would be a different thing said with the same word, so the
	// call is a no-op rather than a surprise.
	m.UnsetAttribute("name")
	if got := m.GetAttribute("name"); got != "Ada" {
		t.Errorf("name = %v after UnsetAttribute, want it untouched", got)
	}
}

func TestIsRelationReadsTheDeclarationAndNotTheLoadedValue(t *testing.T) {
	m, _ := newUserModel()
	m.RelationResolvers = map[string]func(*Model[user]) Relation{
		"posts": func(*Model[user]) Relation { return nil },
	}

	// Declared and not loaded: still a relation. That is the question being
	// asked, and answering it from the loaded values would say no.
	if !m.IsRelation("posts") {
		t.Error("a declared relation that is not loaded read as not a relation")
	}
	if m.IsRelation("name") {
		t.Error("a column read as a relation")
	}
}

func TestTouchesReadsTheListTheApplicationSet(t *testing.T) {
	m, _ := newUserModel()

	if m.Touches("posts") {
		t.Error("a model touches something by default; it must not")
	}

	m.SetTouchedRelations([]string{"posts"})
	if !m.Touches("posts") {
		t.Error("Touches did not read the list that was set")
	}
	if m.Touches("comments") {
		t.Error("Touches answered for a relation that is not in the list")
	}
	if got := m.GetTouchedRelations(); len(got) != 1 || got[0] != "posts" {
		t.Errorf("GetTouchedRelations = %v", got)
	}
}

func TestTouchStampsTheUpdatedAtColumn(t *testing.T) {
	m, conn := newUserModel()
	conn.queue()

	instance, err := m.NewFromBuilder(map[string]any{
		"id": int64(1), "name": "Ada", "tenant_id": "acme",
		"updated_at": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	if err := instance.Touch(context.Background(), auth.SystemGrant("users.write", "acme")); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	if instance.Entity.UpdatedAt.Year() == 2020 {
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
	m, conn := newUserModel()
	m.Timestamps = false

	if err := m.Touch(context.Background(), auth.SystemGrant("users.write", "acme")); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("it wrote %d statements for a model with no timestamps", len(conn.statements))
	}
}
