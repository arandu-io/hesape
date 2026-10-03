package model

import (
	"errors"
	"reflect"
	"testing"
)

// declared registers names on the table of model as relations nothing calls,
// which is all a queued job asks: whether a loaded key is one of them.
func declared(model Entity, names ...string) {
	for _, name := range names {
		model.base().Table().Relate(name, func(*Model) Relation { return nil })
	}
}

func TestRowsQueueableIDAndConnection(t *testing.T) {
	model, _ := newAccountModel()
	model.ID = 7
	onConnection(model, "reporting")

	if got := (Rows{model}).GetQueueableIDs(); !reflect.DeepEqual(got, []any{int64(7)}) {
		t.Errorf("GetQueueableIDs = %v, want [7]: the PHP returns getKey", got)
	}
	if got, err := (Rows{model}).GetQueueableConnection(); err != nil || got != "reporting" {
		t.Errorf("GetQueueableConnection = %q, %v; want reporting", got, err)
	}
}

func TestQueueableRelationsSkipsWhatIsNotDeclared(t *testing.T) {
	model, _ := newAccountModel()
	declared(model, "posts")
	model.SetRelation("posts", Rows{})
	model.SetRelation("stray", "not a relation")

	if got := queueableRelations(model.base()); !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("queueableRelations = %v, want [posts]: a loaded key with nothing behind it is skipped, as method_exists does there", got)
	}
}

func TestQueueableRelationsNestsWithADot(t *testing.T) {
	model, _ := newAccountModel()
	declared(model, "posts")

	child, _ := newAccountModel()
	declared(child, "comments")
	child.SetRelation("comments", Rows{})
	model.SetRelation("posts", Rows{child})

	want := []string{"posts", "posts.comments"}
	if got := queueableRelations(model.base()); !reflect.DeepEqual(got, want) {
		t.Errorf("queueableRelations = %v, want %v", got, want)
	}
}

func TestCollectionQueueableClassAndIDs(t *testing.T) {
	first, _ := newAccountModel()
	first.ID = 1
	second, _ := newAccountModel()
	second.ID = 2
	c := Rows{first, second}

	if got := c.GetQueueableClass(); got != "account" {
		t.Errorf("GetQueueableClass = %q, want account: the rows of one table are of one type, and that type is the class", got)
	}
	if got := c.GetQueueableIDs(); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("GetQueueableIDs = %v, want [1 2]", got)
	}
	if got := (Rows{}).GetQueueableClass(); got != "" {
		t.Errorf("GetQueueableClass on an empty collection = %q, want the empty string", got)
	}
}

func TestCollectionQueueableRelationsIsTheIntersection(t *testing.T) {
	first, _ := newAccountModel()
	declared(first, "posts", "roles")
	first.SetRelation("posts", Rows{})
	first.SetRelation("roles", Rows{})

	second, _ := instanceOf(first, nil, true)
	second.SetRelation("posts", Rows{})

	got := Rows{first, second}.GetQueueableRelations()
	if !reflect.DeepEqual(got, []string{"posts"}) {
		t.Errorf("GetQueueableRelations = %v, want [posts]: a relation loaded on one row only cannot be restored for the collection", got)
	}
}

func TestCollectionQueueableConnectionRefusesAMix(t *testing.T) {
	first, _ := newAccountModel()
	onConnection(first, "primary")
	second, _ := newAccountModel()
	onConnection(second, "reporting")

	if _, err := (Rows{first, second}).GetQueueableConnection(); !errors.Is(err, ErrMixedQueueableConnections) {
		t.Fatalf("error = %v, want ErrMixedQueueableConnections: the PHP throws a LogicException here", err)
	}

	got, err := (Rows{first}).GetQueueableConnection()
	if err != nil || got != "primary" {
		t.Errorf("GetQueueableConnection = %q, %v, want primary and no error", got, err)
	}
}
