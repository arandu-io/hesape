package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

// collectionOf builds a collection of the shape an application writes: an
// entity that embeds its model, so that the collection's model-side methods --
// keyed by key, hidden per row, reloaded from the table -- have a model to
// reach. See Collection.models.
func collectionOf(t *testing.T, model *Model[account], ids ...int64) Collection[account] {
	t.Helper()
	out := make(Collection[account], 0, len(ids))
	for _, id := range ids {
		instance, err := model.NewFromBuilder(map[string]any{"id": id, "name": "row"})
		if err != nil {
			t.Fatalf("NewFromBuilder: %v", err)
		}
		out = append(out, instance.Entity)
	}
	return out
}

func TestModelKeysAndFind(t *testing.T) {
	model, _ := newAccountModel()
	models := collectionOf(t, model, 1, 2, 3)

	keys := models.ModelKeys()
	if len(keys) != 3 || keys[0] != int64(1) {
		t.Fatalf("ModelKeys() = %v", keys)
	}
	if found := models.Find(int64(2)); found == nil || found.ID != 2 {
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
	model, _ := newAccountModel()
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
	model, _ := newAccountModel()
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
	model, _ := newAccountModel()
	models := collectionOf(t, model, 1, 2)

	q, err := models.ToQuery()
	if err != nil {
		t.Fatalf("ToQuery: %v", err)
	}
	sql, err := q.ToBase(context.Background(), grant())
	if err != nil {
		t.Fatalf("ToBase: %v", err)
	}
	if !strings.Contains(sql.ToSQL(), `"accounts"."id" in (?, ?)`) {
		t.Errorf("SQL = %q, want the keys of the collection", sql.ToSQL())
	}

	if _, err := (Collection[account]{}).ToQuery(); !errors.Is(err, ErrEmptyCollection) {
		t.Errorf("ToQuery on an empty collection = %v, want ErrEmptyCollection", err)
	}
}

func TestCollectionFreshDropsWhatIsGone(t *testing.T) {
	model, conn := newAccountModel()
	models := collectionOf(t, model, 1, 2)
	conn.queue(query.Record{"id": int64(1), "name": "reloaded"})

	fresh, err := models.Fresh(context.Background(), grant())
	if err != nil {
		t.Fatalf("Fresh: %v", err)
	}
	if len(fresh) != 1 || fresh[0].Name != "reloaded" {
		t.Fatalf("Fresh = %v rows", len(fresh))
	}
}

func TestLoadMissingSkipsWhatIsLoaded(t *testing.T) {
	model, _ := newAccountModel()
	relation := &fakeRelation{
		table: "posts", foreign: "posts.user_id", local: "users.id",
		matched: map[any]any{int64(1): []string{"first"}, int64(2): []string{"second"}},
	}
	withPostsOn(model, relation)

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

// TestARelationLoadRefusesRowsThatCarryNoModel.
//
// A relation is attached to the model behind a row. Rows that reach no model --
// a T that does not embed one, a struct written as a literal -- used to make the
// load a query that ran and attached its result to nothing, reported as success.
func TestARelationLoadRefusesRowsThatCarryNoModel(t *testing.T) {
	plain, conn := newUserModel()
	withPosts(plain, &fakeRelation{
		table: "posts", foreign: "posts.user_id", local: "users.id",
		matched: map[any]any{int64(1): []string{"first"}},
	})
	rows := Collection[user]{plain.Entity}

	for name, load := range map[string]func() error{
		"Load":          func() error { return rows.Load(context.Background(), grant(), "posts") },
		"LoadMissing":   func() error { return rows.LoadMissing(context.Background(), grant(), "posts") },
		"LoadCount":     func() error { return rows.LoadCount(context.Background(), grant(), "posts") },
		"LoadAggregate": func() error { return rows.LoadAggregate(context.Background(), grant(), []string{"posts"}, "*", "sum") },
		"EagerLoad": func() error {
			return plain.NewQuery().With("posts").EagerLoadRelations(context.Background(), grant(), rows)
		},
	} {
		if err := load(); !errors.Is(err, ErrRowHasNoModel) {
			t.Errorf("%s on rows with no model = %v, want ErrRowHasNoModel", name, err)
		}
	}
	if got := len(conn.sqls()); got != 0 {
		t.Errorf("a refused load ran %d statements", got)
	}

	// Nothing to load is nothing to refuse: the shape of the rows is only a
	// problem when a relation was named.
	if err := rows.Load(context.Background(), grant()); err != nil {
		t.Errorf("Load with no relations = %v, want nil", err)
	}
}

// TestARelationLoadRefusesOneLiteralAmongHydratedRows: the count in the message
// is what tells the two mistakes apart.
func TestARelationLoadRefusesOneLiteralAmongHydratedRows(t *testing.T) {
	model, _ := newAccountModel()
	withPostsOn(model, &fakeRelation{table: "posts", foreign: "posts.account_id", local: "accounts.id"})

	rows := append(collectionOf(t, model, 1, 2), &account{ID: 3})
	err := rows.Load(context.Background(), grant(), "posts")
	if !errors.Is(err, ErrRowHasNoModel) {
		t.Fatalf("Load = %v, want ErrRowHasNoModel", err)
	}
	if !strings.Contains(err.Error(), "1 of 3 rows") {
		t.Errorf("the message does not say how many rows were unreachable: %v", err)
	}
}

func TestLoadCountFillsTheAggregateOntoEveryModel(t *testing.T) {
	model, conn := newAccountModel()
	withPostsOn(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
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
	model, _ := newAccountModel()
	other, _ := newAccountModel()
	related, err := other.NewFromBuilder(map[string]any{"id": int64(9)})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}
	model.SetRelation("manager", Collection[account]{related.Entity})

	// Read off the row, which is what a terminal hands back: the model the
	// relation was set on is the one inside it.
	got, ok := Related[account, account](model.Entity, "manager")
	if !ok || len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("Related = %v, %v", got, ok)
	}
	if _, ok := Related[account, account](model.Entity, "nothing"); ok {
		t.Error("Related answered for a relation that was never loaded")
	}
}

// TestRelatedAnswersNoForARowThatCarriesNoModel.
//
// A T that does not embed Model[T] has no field pointing back at the model the
// relation was attached to, and reading one used to dereference nothing. False
// is the answer: the relation is not there to be read.
func TestRelatedAnswersNoForARowThatCarriesNoModel(t *testing.T) {
	plain, _ := newUserModel()
	plain.SetRelation("manager", Collection[user]{&user{ID: 9}})

	if _, ok := Related[user, user](plain.Entity, "manager"); ok {
		t.Error("Related read a relation off a row that carries no model")
	}
	if _, ok := Related[account, account](&account{}, "manager"); ok {
		t.Error("Related read a relation off a literal")
	}
}

func TestCollectionPushSavesEveryModel(t *testing.T) {
	model, conn := newAccountModel()
	models := collectionOf(t, model, 1, 2)
	models[0].Name = "changed"

	pushed, err := models.Push(context.Background(), grant())
	if err != nil || !pushed {
		t.Fatalf("Push = %v, %v", pushed, err)
	}
	if len(conn.sqls()) != 1 {
		t.Errorf("statements = %v, want only the model that was dirty", conn.sqls())
	}
}
