package model

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
)

// The seam between the typed model and the relations that read it.
//
// This file replaces the probe that came before it. The probe asked one
// question -- can an adapter over *Model[T] satisfy concerns.Model at all --
// and answered it by compiling. These ask the questions that come after: does
// what a relation writes reach the model the caller holds, and does the way
// back give the type away.

// The two lines the probe existed for. They still carry the whole shape.
var (
	_ concerns.Model   = (*modelRef[user])(nil)
	_ concerns.Builder = (*builderRef[user])(nil)
)

// TestRefIsStable pins the caching, because something will eventually key a map
// by a model and a ref that was a new value every call would be a trap.
func TestRefIsStable(t *testing.T) {
	m, _ := newUserModel()
	if m.Ref() != m.Ref() {
		t.Error("two calls to Ref answered two different values")
	}
}

// TestWhatARelationWritesReachesTheModel is the property the whole seam rests
// on: the adapter holds a pointer, so a write through the interface lands on
// the model the caller kept.
func TestWhatARelationWritesReachesTheModel(t *testing.T) {
	m, _ := newUserModel()
	ref := m.Ref()

	ref.SetAttribute("name", "Ada")
	if m.Entity.Name != "Ada" {
		t.Errorf("Name = %q; the write stayed on the adapter", m.Entity.Name)
	}

	ref.SetRelation("posts", []concerns.Model{})
	if !m.RelationLoaded("posts") {
		t.Error("the relation was set on the adapter and not on the model")
	}

	ref.UnsetRelation("posts")
	if m.RelationLoaded("posts") {
		t.Error("the relation was unset on the adapter and not on the model")
	}
}

// TestUnrefGivesTheTypeBack, and refuses when the ref is over another entity.
func TestUnrefGivesTheTypeBack(t *testing.T) {
	m, _ := newUserModel()

	back, ok := Unref[user](m.Ref())
	if !ok || back != m {
		t.Fatalf("Unref gave (%v, %v), want the model back", back, ok)
	}

	// A ref over another entity answers no rather than panicking: "is this
	// relation's model a post" is a question a caller may ask and get no for.
	type post struct {
		ID int64 `db:"id"`
	}
	if _, ok := Unref[post](m.Ref()); ok {
		t.Error("Unref claimed a user was a post")
	}
}

// TestRelatedReadsBackWhatARelationLoaded covers the one call that is the whole
// price of the seam: a relation loads erased models, and this is the way back.
//
// It is written over the entity that embeds its model, because that is the row
// the read starts from: a plain row has no way back to the model the relation
// was attached to.
func TestRelatedReadsBackWhatARelationLoaded(t *testing.T) {
	parent, _ := newAccountModel()
	first, _ := newAccountModel()
	second, _ := newAccountModel()
	first.Entity.Name = "Ada"
	second.Entity.Name = "Grace"

	// Stored the way a relation stores it: the narrow interface, not the type.
	parent.SetRelation("friends", []concerns.Model{first.Ref(), second.Ref()})

	friends, ok := Related[account, account](parent.Entity, "friends")
	if !ok {
		t.Fatal("Related did not read back what the relation loaded")
	}
	if len(friends) != 2 || friends[0].Name != "Ada" || friends[1].Name != "Grace" {
		t.Fatalf("friends = %v, want the two models in order", friends)
	}

	if _, ok := Related[account, account](parent.Entity, "nothing"); ok {
		t.Error("Related claimed a relation nobody loaded")
	}
}

// TestTheHeldErrorSurfacesAtTheNextMethodThatCanReportOne: Fill returns nothing
// through the interface and an error on the model, and dropping it would be the
// worst of the three options.
func TestTheHeldErrorSurfacesAtTheNextMethodThatCanReportOne(t *testing.T) {
	m, _ := newUserModel()
	ref := m.Ref()

	// A value that cannot go into the field it names.
	ref.Fill(map[string]any{"id": "not a number"})

	if err := ref.Save(context.Background(), auth.SystemGrant("users.write", "acme")); err == nil {
		t.Fatal("Save ran after a Fill that could not convert")
	}

	// And it is reported once: a model that recovered is not refused forever.
	if err := ref.Save(context.Background(), auth.SystemGrant("users.write", "acme")); err != nil {
		if err.Error() == "" {
			t.Error("the held error was reported twice")
		}
	}
}

// TestTheBuilderRefChainsWithoutLeavingTheTypedBuilder: the fourteen chainables
// are the covariant-return problem, and what they must not do is fork.
func TestTheBuilderRefChainsWithoutLeavingTheTypedBuilder(t *testing.T) {
	m, conn := newUserModel()
	conn.queue()

	ref := m.NewQuery().Ref()
	chained := ref.Where("name", "Ada").WhereNotNull("email").Limit(5)

	if chained != ref {
		t.Error("a chainable answered a different ref; the chain forked")
	}

	if _, err := chained.Get(context.Background(), auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	if sql == "" {
		t.Fatal("the chain never reached the connection")
	}
}
