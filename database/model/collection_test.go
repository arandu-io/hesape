package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/database/query"
)

// collectionOf builds rows the way a query hands them back: hydrated, so that
// the methods keyed by key, hidden per row and reloaded from the table have a
// model to reach.
func collectionOf(t *testing.T, model *account, ids ...int64) Rows {
	t.Helper()
	out := make(Rows, 0, len(ids))
	for _, id := range ids {
		instance, err := fromRecord(model, map[string]any{"id": id, "name": "row"})
		if err != nil {
			t.Fatalf("NewFromBuilder: %v", err)
		}
		out = append(out, instance)
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
	if found := models.Find(int64(2)); found == nil || found.(*account).ID != 2 {
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

	if _, err := (Rows{}).ToQuery(); !errors.Is(err, ErrEmptyCollection) {
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
	if len(fresh) != 1 || fresh[0].(*account).Name != "reloaded" {
		t.Fatalf("Fresh = %v rows", len(fresh))
	}
}

func TestLoadMissingSkipsWhatIsLoaded(t *testing.T) {
	model, conn := newAccountModel()
	withPostsOn(model, "")

	models := collectionOf(t, model, 1, 2)
	models[0].base().SetRelation("posts", []string{"already here"})

	conn.queue(query.Record{"id": int64(20), "account_id": int64(2), "title": "second"})

	if err := models.LoadMissing(context.Background(), grant(), "posts"); err != nil {
		t.Fatalf("LoadMissing: %v", err)
	}

	first, _ := getRelation(models[0].base(), "posts")
	if loaded, ok := first.([]string); !ok || loaded[0] != "already here" {
		t.Errorf("LoadMissing overwrote a relation that was loaded: %v", first)
	}
	second, ok := getRelation(models[1].base(), "posts")
	if !ok {
		t.Fatalf("LoadMissing did not load the missing one: %v", second)
	}
	matched, ok := second.([]relations.Model)
	if !ok || len(matched) != 1 {
		t.Fatalf("the missing one loaded %#v, want the row whose key points at it", second)
	}
	if loaded, ok := unref(matched[0]); !ok || loaded.r.self.(*post).Title != "second" {
		t.Errorf("the missing one loaded the wrong row: %v", second)
	}
}

// TestARelationLoadRefusesALiteralAmongHydratedRows.
//
// A relation is attached to the model inside a row. A struct written as a
// literal has none, and a load over it used to be a query that ran and attached
// its result to nothing, reported as success.
func TestARelationLoadRefusesALiteralAmongHydratedRows(t *testing.T) {
	model, conn := newAccountModel()
	withPostsOn(model, "")

	rows := append(collectionOf(t, model, 1, 2), &account{ID: 3})
	for name, load := range map[string]func() error{
		"Load":          func() error { return rows.Load(context.Background(), grant(), "posts") },
		"LoadMissing":   func() error { return rows.LoadMissing(context.Background(), grant(), "posts") },
		"LoadCount":     func() error { return rows.LoadCount(context.Background(), grant(), "posts") },
		"LoadAggregate": func() error { return rows.LoadAggregate(context.Background(), grant(), []string{"posts"}, "*", "sum") },
		"EagerLoad": func() error {
			return newQuery(model.base()).With("posts").EagerLoadRelations(context.Background(), grant(), rows)
		},
	} {
		if err := load(); !errors.Is(err, ErrUnwired) {
			t.Errorf("%s over a literal = %v, want ErrUnwired", name, err)
		}
	}
	if got := len(conn.sqls()); got != 0 {
		t.Errorf("a refused load ran %d statements", got)
	}

	// Nothing to load is nothing to refuse: the rows are only a problem when a
	// relation was named.
	if err := rows.Load(context.Background(), grant()); err != nil {
		t.Errorf("Load with no relations = %v, want nil", err)
	}
}

func TestLoadCountFillsTheAggregateOntoEveryModel(t *testing.T) {
	model, conn := newAccountModel()
	withPostsOn(model, "")
	models := collectionOf(t, model, 1, 2)

	conn.queue(
		query.Record{"id": int64(1), "posts_count": int64(2)},
		query.Record{"id": int64(2), "posts_count": int64(5)},
	)

	if err := models.LoadCount(context.Background(), grant(), "posts"); err != nil {
		t.Fatalf("LoadCount: %v", err)
	}
	first, second := models[0].(*account), models[1].(*account)
	if got := first.GetAttribute("posts_count"); got != int64(2) {
		t.Errorf("posts_count on the first model = %v, want 2", got)
	}
	if got := second.GetAttribute("posts_count"); got != int64(5) {
		t.Errorf("posts_count on the second model = %v, want 5", got)
	}
	if first.IsDirty() {
		t.Error("an aggregate loaded onto a model left it dirty, so the next save would try to write posts_count")
	}
	if first.Name != "row" {
		t.Errorf("name = %q after LoadCount, want the column the aggregate query did not select left alone", first.Name)
	}
}

func TestRelatedReadsALoadedRelationAsRows(t *testing.T) {
	model, _ := newAccountModel()
	other, _ := newAccountModel()
	related, err := fromRecord(other, map[string]any{"id": int64(9)})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}
	model.SetRelation("manager", Rows{related})

	got, ok := model.Related("manager")
	if !ok || len(got) != 1 || got[0].(*account).ID != 9 {
		t.Fatalf("Related = %v, %v", got, ok)
	}
	if _, ok := model.Related("nothing"); ok {
		t.Error("Related answered for a relation that was never loaded")
	}

	// One row set as the relation reads back as one row.
	model.SetRelation("manager", related)
	if got, ok := model.Related("manager"); !ok || len(got) != 1 || got[0] != Entity(related) {
		t.Fatalf("Related over a single row = %v, %v", got, ok)
	}
}

// TestRelatedAnswersNoForALiteral: a struct written by hand has no model to
// have loaded anything onto, and reading one answers false rather than
// dereferencing nothing.
func TestRelatedAnswersNoForALiteral(t *testing.T) {
	if _, ok := (&account{}).Related("manager"); ok {
		t.Error("Related read a relation off a literal")
	}
}

func TestCollectionPushSavesEveryModel(t *testing.T) {
	model, conn := newAccountModel()
	models := collectionOf(t, model, 1, 2)
	models[0].(*account).Name = "changed"

	pushed, err := models.Push(context.Background(), grant())
	if err != nil || !pushed {
		t.Fatalf("Push = %v, %v", pushed, err)
	}
	if len(conn.sqls()) != 1 {
		t.Errorf("statements = %v, want only the model that was dirty", conn.sqls())
	}
}
