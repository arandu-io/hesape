package model

import (
	"errors"
	"reflect"
	"testing"
)

func TestModelQueueableIDAndConnection(t *testing.T) {
	model, _ := newAccountModel()
	model.Entity.ID = 7
	model.SetConnection("reporting", nil)

	if got := model.GetQueueableID(); got != int64(7) {
		t.Errorf("GetQueueableID = %v, want 7: the PHP returns getKey", got)
	}
	if got := model.GetQueueableConnection(); got != "reporting" {
		t.Errorf("GetQueueableConnection = %q, want reporting", got)
	}
}

func TestModelQueueableRelationsSkipsWhatIsNotDeclared(t *testing.T) {
	model, _ := newAccountModel()
	model.RelationResolvers = map[string]func(*Model[account]) Relation{"posts": nil}
	model.SetRelation("posts", Collection[account]{})
	model.SetRelation("stray", "not a relation")

	if got := model.GetQueueableRelations(); !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("GetQueueableRelations = %v, want [posts]: a loaded key with nothing behind it is skipped, as method_exists does there", got)
	}
}

func TestModelQueueableRelationsNestsWithADot(t *testing.T) {
	model, _ := newAccountModel()
	model.RelationResolvers = map[string]func(*Model[account]) Relation{"posts": nil}

	child, _ := newAccountModel()
	child.RelationResolvers = map[string]func(*Model[account]) Relation{"comments": nil}
	child.SetRelation("comments", Collection[account]{})
	model.SetRelation("posts", Collection[account]{child.Entity})

	want := []string{"posts", "posts.comments"}
	if got := model.GetQueueableRelations(); !reflect.DeepEqual(got, want) {
		t.Errorf("GetQueueableRelations = %v, want %v", got, want)
	}
}

func TestCollectionQueueableClassAndIDs(t *testing.T) {
	first, _ := newAccountModel()
	first.Entity.ID = 1
	second, _ := newAccountModel()
	second.Entity.ID = 2
	c := Collection[account]{first.Entity, second.Entity}

	if got := c.GetQueueableClass(); got != "account" {
		t.Errorf("GetQueueableClass = %q, want account: a Collection[T] holds one type and that type is the class", got)
	}
	if got := c.GetQueueableIDs(); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("GetQueueableIDs = %v, want [1 2]", got)
	}
	if got := (Collection[account]{}).GetQueueableClass(); got != "" {
		t.Errorf("GetQueueableClass on an empty collection = %q, want the empty string", got)
	}
}

func TestCollectionQueueableRelationsIsTheIntersection(t *testing.T) {
	resolvers := map[string]func(*Model[account]) Relation{"posts": nil, "roles": nil}

	first, _ := newAccountModel()
	first.RelationResolvers = resolvers
	first.SetRelation("posts", Collection[account]{})
	first.SetRelation("roles", Collection[account]{})

	second, _ := newAccountModel()
	second.RelationResolvers = resolvers
	second.SetRelation("posts", Collection[account]{})

	got := Collection[account]{first.Entity, second.Entity}.GetQueueableRelations()
	if !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("GetQueueableRelations = %v, want [posts]: a relation loaded on one row only cannot be restored for the collection", got)
	}
}

func TestCollectionQueueableConnectionRefusesAMix(t *testing.T) {
	first, _ := newAccountModel()
	first.SetConnection("primary", nil)
	second, _ := newAccountModel()
	second.SetConnection("reporting", nil)

	if _, err := (Collection[account]{first.Entity, second.Entity}).GetQueueableConnection(); !errors.Is(err, ErrMixedQueueableConnections) {
		t.Fatalf("error = %v, want ErrMixedQueueableConnections: the PHP throws a LogicException here", err)
	}

	got, err := (Collection[account]{first.Entity}).GetQueueableConnection()
	if err != nil || got != "primary" {
		t.Errorf("GetQueueableConnection = %q, %v, want primary and no error", got, err)
	}
}
