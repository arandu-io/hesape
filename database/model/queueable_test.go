package model

import (
	"errors"
	"reflect"
	"testing"
)

func TestModelQueueableIDAndConnection(t *testing.T) {
	model, _ := newUserModel()
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
	model, _ := newUserModel()
	model.RelationResolvers = map[string]func(*Model[user]) Relation{"posts": nil}
	model.SetRelation("posts", Collection[user]{})
	model.SetRelation("stray", "not a relation")

	if got := model.GetQueueableRelations(); !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("GetQueueableRelations = %v, want [posts]: a loaded key with nothing behind it is skipped, as method_exists does there", got)
	}
}

func TestModelQueueableRelationsNestsWithADot(t *testing.T) {
	model, _ := newUserModel()
	model.RelationResolvers = map[string]func(*Model[user]) Relation{"posts": nil}

	child, _ := newUserModel()
	child.RelationResolvers = map[string]func(*Model[user]) Relation{"comments": nil}
	child.SetRelation("comments", Collection[user]{})
	model.SetRelation("posts", Collection[user]{child})

	want := []string{"posts", "posts.comments"}
	if got := model.GetQueueableRelations(); !reflect.DeepEqual(got, want) {
		t.Errorf("GetQueueableRelations = %v, want %v", got, want)
	}
}

func TestCollectionQueueableClassAndIDs(t *testing.T) {
	first, _ := newUserModel()
	first.Entity.ID = 1
	second, _ := newUserModel()
	second.Entity.ID = 2
	c := Collection[user]{first, second}

	if got := c.GetQueueableClass(); got != "user" {
		t.Errorf("GetQueueableClass = %q, want user: a Collection[T] holds one type and that type is the class", got)
	}
	if got := c.GetQueueableIDs(); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("GetQueueableIDs = %v, want [1 2]", got)
	}
	if got := (Collection[user]{}).GetQueueableClass(); got != "" {
		t.Errorf("GetQueueableClass on an empty collection = %q, want the empty string", got)
	}
}

func TestCollectionQueueableRelationsIsTheIntersection(t *testing.T) {
	resolvers := map[string]func(*Model[user]) Relation{"posts": nil, "roles": nil}

	first, _ := newUserModel()
	first.RelationResolvers = resolvers
	first.SetRelation("posts", Collection[user]{})
	first.SetRelation("roles", Collection[user]{})

	second, _ := newUserModel()
	second.RelationResolvers = resolvers
	second.SetRelation("posts", Collection[user]{})

	got := Collection[user]{first, second}.GetQueueableRelations()
	if !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("GetQueueableRelations = %v, want [posts]: a relation loaded on one row only cannot be restored for the collection", got)
	}
}

func TestCollectionQueueableConnectionRefusesAMix(t *testing.T) {
	first, _ := newUserModel()
	first.SetConnection("primary", nil)
	second, _ := newUserModel()
	second.SetConnection("reporting", nil)

	if _, err := (Collection[user]{first, second}).GetQueueableConnection(); !errors.Is(err, ErrMixedQueueableConnections) {
		t.Fatalf("error = %v, want ErrMixedQueueableConnections: the PHP throws a LogicException here", err)
	}

	got, err := (Collection[user]{first}).GetQueueableConnection()
	if err != nil || got != "primary" {
		t.Errorf("GetQueueableConnection = %q, %v, want primary and no error", got, err)
	}
}
