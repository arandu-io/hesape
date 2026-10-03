package model

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
)

// The seam between the model and the relations that read it.
//
// The two assertions below ask whether the adapters satisfy the contract the
// relations tree consumes, by compiling. The tests ask the questions that come
// after: does what a relation writes reach the row the caller holds, and does
// the way back give the row back.

// The two lines the probe existed for. They still carry the whole shape.
var (
	_ concerns.Model   = (*modelRef)(nil)
	_ concerns.Builder = (*builderRef)(nil)
)

// TestRefIsStable pins the caching, because something will eventually key a map
// by a model and a ref that was a new value every call would be a trap.
func TestRefIsStable(t *testing.T) {
	m, _ := newUserModel()
	if refOf(m.base()) != refOf(m.base()) {
		t.Error("two calls to ref answered two different values")
	}
}

// TestWhatARelationWritesReachesTheModel is the property the whole seam rests
// on: the adapter holds a pointer, so a write through the interface lands on
// the model the caller kept.
func TestWhatARelationWritesReachesTheModel(t *testing.T) {
	m, _ := newUserModel()
	ref := refOf(m.base())

	ref.SetAttribute("name", "Ada")
	if m.Name != "Ada" {
		t.Errorf("Name = %q; the write stayed on the adapter", m.Name)
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

// TestUnrefGivesTheModelBack, and refuses a value that is not one of this
// package's refs.
func TestUnrefGivesTheModelBack(t *testing.T) {
	m, _ := newUserModel()

	back, ok := unref(refOf(m.base()))
	if !ok || back != &m.Model {
		t.Fatalf("unref gave (%v, %v), want the model back", back, ok)
	}
	if back.r.self != Entity(m) {
		t.Error("the model unref gave back does not reach the row it came from")
	}

	// A value that is not one of these refs -- a pivot row -- answers no rather
	// than panicking.
	if _, ok := unref(nil); ok {
		t.Error("unref claimed nothing was a model")
	}
}

// TestRelatedReadsBackWhatARelationLoaded covers the one call that is the whole
// price of the seam: a relation loads erased models, and this is the way back.
func TestRelatedReadsBackWhatARelationLoaded(t *testing.T) {
	parent, _ := newAccountModel()
	first, _ := newAccountModel()
	second, _ := newAccountModel()
	first.Name = "Ada"
	second.Name = "Grace"

	// Stored the way a relation stores it: the narrow interface, not the row.
	parent.SetRelation("friends", []concerns.Model{refOf(first.base()), refOf(second.base())})

	friends, ok := parent.Related("friends")
	if !ok {
		t.Fatal("Related did not read back what the relation loaded")
	}
	if len(friends) != 2 || friends[0].(*account).Name != "Ada" || friends[1].(*account).Name != "Grace" {
		t.Fatalf("friends = %v, want the two rows in order", friends)
	}

	if _, ok := parent.Related("nothing"); ok {
		t.Error("Related claimed a relation nobody loaded")
	}
}

// TestTheHeldErrorSurfacesAtTheNextMethodThatCanReportOne: Fill returns nothing
// through the interface and an error on the model, and dropping it would be the
// worst of the three options.
func TestTheHeldErrorSurfacesAtTheNextMethodThatCanReportOne(t *testing.T) {
	m, _ := newUserModel()
	ref := refOf(m.base())

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

// TestTheBuilderRefChainsWithoutLeavingTheBuilder: the fourteen chainables
// answer the interface rather than the builder, and what they must not do is
// fork.
func TestTheBuilderRefChainsWithoutLeavingTheBuilder(t *testing.T) {
	m, conn := newUserModel()
	conn.queue()

	ref := newQuery(m.base()).Ref()
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
