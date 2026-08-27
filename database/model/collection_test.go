package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

func collectionOf(t *testing.T, model *Model[user], ids ...int64) Collection[user] {
	t.Helper()
	out := make(Collection[user], 0, len(ids))
	for _, id := range ids {
		instance, err := model.NewFromBuilder(map[string]any{"id": id, "name": "row"})
		if err != nil {
			t.Fatalf("NewFromBuilder: %v", err)
		}
		out = append(out, instance)
	}
	return out
}

func TestModelKeysAndFind(t *testing.T) {
	model, _ := newUserModel()
	models := collectionOf(t, model, 1, 2, 3)

	keys := models.ModelKeys()
	if len(keys) != 3 || keys[0] != int64(1) {
		t.Fatalf("ModelKeys() = %v", keys)
	}
	if found := models.Find(int64(2)); found == nil || found.Entity.ID != 2 {
		t.Errorf("Find(2) = %v", found)
	}
	if models.Find(int64(9)) != nil {
		t.Error("Find invented a model")
	}
	if _, err := models.FindOrFail(int64(9)); !errors.Is(err, ErrModelNotFound) {
		t.Errorf("FindOrFail error = %v, want ErrModelNotFound", err)
	}
	if !models.Contains(int64(3)) || models.DoesntContain(int64(3)) {
		t.Error("Contains does not answer for a key that is there")
	}
	if !models.Contains(models[0]) {
		t.Error("Contains does not answer for a model")
	}
}

func TestCollectionSetOperations(t *testing.T) {
	model, _ := newUserModel()
	left := collectionOf(t, model, 1, 2, 3)
	right := collectionOf(t, model, 2, 3, 4)

	if got := left.Diff(right).ModelKeys(); len(got) != 1 || got[0] != int64(1) {
		t.Errorf("Diff = %v, want [1]", got)
	}
	if got := left.Intersect(right).ModelKeys(); len(got) != 2 {
		t.Errorf("Intersect = %v, want two", got)
	}
	if got := left.Only(int64(1), int64(3)).ModelKeys(); len(got) != 2 {
		t.Errorf("Only = %v", got)
	}
	if got := left.Except(int64(1)).ModelKeys(); len(got) != 2 {
		t.Errorf("Except = %v", got)
	}

	duplicated := append(collectionOf(t, model, 1, 1), left...)
	if got := duplicated.Unique().ModelKeys(); len(got) != 3 {
		t.Errorf("Unique = %v, want one per key", got)
	}
}

func TestCollectionPluckAndToArray(t *testing.T) {
	model, _ := newUserModel()
	models := collectionOf(t, model, 1, 2)

	if got := models.Pluck("id"); len(got) != 2 || got[1] != int64(2) {
		t.Errorf("Pluck(id) = %v", got)
	}

	models.MakeHidden("name")
	array := models.ToArray()
	if len(array) != 2 {
		t.Fatalf("ToArray = %v", array)
	}
	if _, ok := array[0]["name"]; ok {
		t.Error("MakeHidden on the collection did not reach every model")
	}
}

func TestToQueryReadsBackExactlyTheseRows(t *testing.T) {
	model, _ := newUserModel()
	models := collectionOf(t, model, 1, 2)

	q, err := models.ToQuery()
	if err != nil {
		t.Fatalf("ToQuery: %v", err)
	}
	sql, err := q.ToBase(context.Background(), grant())
	if err != nil {
		t.Fatalf("ToBase: %v", err)
	}
	if !strings.Contains(sql.ToSQL(), `"users"."id" in (?, ?)`) {
		t.Errorf("SQL = %q, want the keys of the collection", sql.ToSQL())
	}

	if _, err := (Collection[user]{}).ToQuery(); !errors.Is(err, ErrEmptyCollection) {
		t.Errorf("ToQuery on an empty collection = %v, want ErrEmptyCollection", err)
	}
}

func TestCollectionFreshDropsWhatIsGone(t *testing.T) {
	model, conn := newUserModel()
	models := collectionOf(t, model, 1, 2)
	conn.queue(query.Record{"id": int64(1), "name": "reloaded"})

	fresh, err := models.Fresh(context.Background(), grant())
	if err != nil {
		t.Fatalf("Fresh: %v", err)
	}
	if len(fresh) != 1 || fresh[0].Entity.Name != "reloaded" {
		t.Fatalf("Fresh = %v rows", len(fresh))
	}
}

func TestLoadMissingSkipsWhatIsLoaded(t *testing.T) {
	model, _ := newUserModel()
	relation := &fakeRelation{
		table: "posts", foreign: "posts.user_id", local: "users.id",
		matched: map[any]any{int64(1): []string{"first"}, int64(2): []string{"second"}},
	}
	withPosts(model, relation)

	models := collectionOf(t, model, 1, 2)
	models[0].SetRelation("posts", []string{"already here"})

	if err := models.LoadMissing(context.Background(), grant(), "posts"); err != nil {
		t.Fatalf("LoadMissing: %v", err)
	}

	first, _ := models[0].GetRelation("posts")
	if first.([]string)[0] != "already here" {
		t.Errorf("LoadMissing overwrote a relation that was loaded: %v", first)
	}
	second, ok := models[1].GetRelation("posts")
	if !ok || second.([]string)[0] != "second" {
		t.Errorf("LoadMissing did not load the missing one: %v", second)
	}
}

func TestLoadCountFillsTheAggregateOntoEveryModel(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	models := collectionOf(t, model, 1, 2)

	conn.queue(
		query.Record{"id": int64(1), "posts_count": int64(2)},
		query.Record{"id": int64(2), "posts_count": int64(5)},
	)

	if err := models.LoadCount(context.Background(), grant(), "posts"); err != nil {
		t.Fatalf("LoadCount: %v", err)
	}
	if got := models[0].GetAttribute("posts_count"); got != int64(2) {
		t.Errorf("posts_count on the first model = %v, want 2", got)
	}
	if got := models[1].GetAttribute("posts_count"); got != int64(5) {
		t.Errorf("posts_count on the second model = %v, want 5", got)
	}
	if models[0].IsDirty() {
		t.Error("an aggregate loaded onto a model left it dirty, so the next save would try to write posts_count")
	}
}

func TestRelatedReadsALoadedRelationAsItsType(t *testing.T) {
	model, _ := newUserModel()
	other, _ := newUserModel()
	related, err := other.NewFromBuilder(map[string]any{"id": int64(9)})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}
	model.SetRelation("manager", Collection[user]{related})

	got, ok := Related[user, user](model, "manager")
	if !ok || len(got) != 1 || got[0].Entity.ID != 9 {
		t.Fatalf("Related = %v, %v", got, ok)
	}
	if _, ok := Related[user, user](model, "nothing"); ok {
		t.Error("Related answered for a relation that was never loaded")
	}
}

func TestCollectionPushSavesEveryModel(t *testing.T) {
	model, conn := newUserModel()
	models := collectionOf(t, model, 1, 2)
	models[0].Entity.Name = "changed"

	pushed, err := models.Push(context.Background(), grant())
	if err != nil || !pushed {
		t.Fatalf("Push = %v, %v", pushed, err)
	}
	if len(conn.sqls()) != 1 {
		t.Errorf("statements = %v, want only the model that was dirty", conn.sqls())
	}
}
