package relations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
)

// TestEagerLoadingRunsOneQueryForEveryParent is the measurement the whole
// package exists for.
//
// It counts the statements the connection was asked for, because the values
// come out right either way -- that is exactly why an N+1 gets merged. Three
// parents lazily read their relation three times; eagerly, once.
func TestEagerLoadingRunsOneQueryForEveryParent(t *testing.T) {
	database, users, _ := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	before := database.count()
	for _, user := range users {
		relation := postsOf(database, user)
		if _, err := relation.GetResults(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	lazy := database.count() - before

	if lazy != len(users) {
		t.Fatalf("reading the relation off each parent ran %d queries, want one per parent (%d)", lazy, len(users))
	}

	before = database.count()
	models := asModels(users)
	relation := eagerPostsOf(database, users[0])
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", relation, nil); err != nil {
		t.Fatal(err)
	}
	eager := database.count() - before

	if eager != 1 {
		t.Fatalf("the eager load ran %d queries, want exactly 1.\n%s", eager, strings.Join(database.log[len(database.log)-eager:], "\n"))
	}
}

// TestEagerLoadingMatchesEveryChildToItsParent is the other half: one query is
// only useful if the rows land on the right models.
func TestEagerLoadingMatchesEveryChildToItsParent(t *testing.T) {
	database, users, _ := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	models := asModels(users)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", eagerPostsOf(database, users[0]), nil); err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		"1": {"a", "b"},
		"2": {"c"},
		"3": {},
	}

	for _, user := range models {
		loaded, ok := user.GetRelation("posts")
		if !ok {
			t.Fatalf("user %v came back with no posts relation at all: a parent with no children must be an empty collection, or reading it goes to the database", user.GetKey())
		}

		posts, ok := loaded.([]relations.Model)
		if !ok {
			t.Fatalf("posts came back as %T, want a collection", loaded)
		}

		titles := make([]string, 0, len(posts))
		for _, post := range posts {
			titles = append(titles, post.GetAttribute("title").(string))
		}

		expected := want[user.GetKey().(string)]
		if strings.Join(titles, ",") != strings.Join(expected, ",") {
			t.Errorf("user %v got posts %v, want %v", user.GetKey(), titles, expected)
		}
	}
}

// TestTheEagerQueryFiltersByTenant is RULE 17 on the path where breaking it is
// invisible.
//
// The parent query in this test is correct: three users of one tenant. If the
// eager load does not filter, the posts of the other tenant come back attached
// to them, and every row on the screen looks like it belongs there.
func TestTheEagerQueryFiltersByTenant(t *testing.T) {
	database, users, _ := seedBlog()
	database.seed("posts", map[string]any{
		"id": "999", "user_id": "1", "title": "another customer's post", "tenant_id": "other",
	})

	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	models := asModels(users)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", eagerPostsOf(database, users[0]), nil); err != nil {
		t.Fatal(err)
	}

	loaded, _ := models[0].GetRelation("posts")
	for _, post := range loaded.([]relations.Model) {
		if post.GetAttribute("tenant_id") != "acme" {
			t.Fatalf("the eager load returned a row of tenant %v to a grant for acme", post.GetAttribute("tenant_id"))
		}
	}
}

// TestAGrantWithNoTenantCannotLoadARelation is the same rule, refused earlier.
//
// The zero Grant carries no tenant. Filtering on it would compile to
// `tenant_id = ”`, which returns nothing and reads like a missing fixture; the
// refusal says what actually happened.
func TestAGrantWithNoTenantCannotLoadARelation(t *testing.T) {
	database, users, _ := seedBlog()

	_, err := postsOf(database, users[0]).GetResults(context.Background(), auth.Grant{})
	if err == nil {
		t.Fatal("a relation loaded with the zero Grant returned rows")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("the refusal was %v, want it to be an authorization failure", err)
	}
}

// TestAnEagerLoadWithNoParentKeysRunsNoQuery answers Relation::$eagerKeysWereEmpty.
//
// `where in ()` is a syntax error on some engines and matches nothing on
// others. Neither is worth a round trip.
func TestAnEagerLoadWithNoParentKeysRunsNoQuery(t *testing.T) {
	database, _, _ := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	orphan := newModel(database, "users", "user", nil)
	models := []relations.Model{orphan}

	before := database.count()
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", eagerPostsOf(database, orphan), nil); err != nil {
		t.Fatal(err)
	}

	if ran := database.count() - before; ran != 0 {
		t.Fatalf("an eager load over parents with no keys ran %d queries, want none", ran)
	}
	if _, ok := orphan.GetRelation("posts"); !ok {
		t.Fatal("the parent was left without an initialized relation, so reading it would lazy load")
	}
}

// TestTheEagerConstraintIsOneInClause reads the clause the eager load put on
// the query, because "one query" is only true if it asks for every parent at
// once.
func TestTheEagerConstraintIsOneInClause(t *testing.T) {
	database, users, _ := seedBlog()

	relation := eagerPostsOf(database, users[0])
	if err := relation.AddEagerConstraints(asModels(users)); err != nil {
		t.Fatal(err)
	}

	wheres := relation.GetQuery().GetQuery().Wheres
	var found bool
	for _, where := range wheres {
		if where.Type != "In" {
			continue
		}
		found = true
		if len(where.Values) != 3 {
			t.Fatalf("the in clause carried %d keys, want one per parent", len(where.Values))
		}
		// Sorted, so that the same batch of parents compiles to the same
		// statement between requests.
		if where.Values[0] != "1" || where.Values[2] != "3" {
			t.Errorf("the keys came out %v, want them sorted", where.Values)
		}
	}
	if !found {
		t.Fatalf("no in clause was added: the eager load would be one query per parent. Clauses: %+v", wheres)
	}
}

// TestBelongsToEagerLoadingSharesOneOwner: many children, one owner each, and
// the same owner handed to every child that points at it.
func TestBelongsToEagerLoadingSharesOneOwner(t *testing.T) {
	database, _, posts := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("user.view", "acme")

	var relation *relations.BelongsTo
	relations.NoConstraints(func() {
		relation = relations.NewBelongsTo(
			newModel(database, "users", "user", nil).NewQuery(),
			posts[0],
			"user_id",
			"id",
			"user",
		)
	})

	before := database.count()
	models := asModels(posts)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "user", relation, nil); err != nil {
		t.Fatal(err)
	}

	if ran := database.count() - before; ran != 1 {
		t.Fatalf("belongsTo eager loading ran %d queries, want 1", ran)
	}

	first, _ := models[0].GetRelation("user")
	second, _ := models[1].GetRelation("user")
	if first == nil || second == nil {
		t.Fatal("a post came back without its user")
	}
	if first.(relations.Model).GetKey() != "1" || second.(relations.Model).GetKey() != "1" {
		t.Fatalf("posts a and b belong to user 1, got %v and %v", first.(relations.Model).GetKey(), second.(relations.Model).GetKey())
	}
}

// TestMorphToEagerLoadingIsOneQueryPerType is the exception, and the reason it
// is one: no dialect selects from a table named by a column.
func TestMorphToEagerLoadingIsOneQueryPerType(t *testing.T) {
	database := newDB()
	database.seed("posts", map[string]any{"id": "1", "title": "a post", "tenant_id": "acme"})
	database.seed("videos", map[string]any{"id": "7", "title": "a video", "tenant_id": "acme"})

	relations.MorphMap(map[string]func() relations.Model{
		"post":  func() relations.Model { return newModel(database, "posts", "post", nil) },
		"video": func() relations.Model { return newModel(database, "videos", "video", nil) },
	})

	comments := []*model{
		newModel(database, "comments", "comment", map[string]any{"id": "10", "commentable_id": "1", "commentable_type": "post", "tenant_id": "acme"}),
		newModel(database, "comments", "comment", map[string]any{"id": "11", "commentable_id": "1", "commentable_type": "post", "tenant_id": "acme"}),
		newModel(database, "comments", "comment", map[string]any{"id": "12", "commentable_id": "7", "commentable_type": "video", "tenant_id": "acme"}),
	}

	ctx, g := context.Background(), auth.SystemGrant("comment.view", "acme")

	var relation *relations.MorphTo
	relations.NoConstraints(func() {
		relation = relations.NewMorphTo(
			newModel(database, "posts", "post", nil).NewQuery(),
			comments[0],
			"commentable_id",
			"",
			"commentable_type",
			"commentable",
		)
	})

	before := database.count()
	models := asModels(comments)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "commentable", relation, nil); err != nil {
		t.Fatal(err)
	}

	if ran := database.count() - before; ran != 2 {
		t.Fatalf("three comments over two morph types ran %d queries, want 2 -- one per type, not one per row", ran)
	}

	first, _ := models[0].GetRelation("commentable")
	third, _ := models[2].GetRelation("commentable")
	if first == nil || third == nil {
		t.Fatal("a comment came back without its commentable")
	}
	if first.(relations.Model).GetTable() != "posts" || third.(relations.Model).GetTable() != "videos" {
		t.Fatalf("the types were matched to the wrong tables: %s and %s", first.(relations.Model).GetTable(), third.(relations.Model).GetTable())
	}
}

// TestAnUnregisteredMorphTypeIsRefused: the morph map is the mechanism here, not
// a recommendation, so a type nobody registered is an error with the alias in
// it rather than a nil dereference three frames away.
func TestAnUnregisteredMorphTypeIsRefused(t *testing.T) {
	database := newDB()
	relations.MorphMap(map[string]func() relations.Model{
		"post": func() relations.Model { return newModel(database, "posts", "post", nil) },
	})

	comment := newModel(database, "comments", "comment", map[string]any{
		"id": "1", "commentable_id": "1", "commentable_type": "podcast", "tenant_id": "acme",
	})

	var relation *relations.MorphTo
	relations.NoConstraints(func() {
		relation = relations.NewMorphTo(
			newModel(database, "posts", "post", nil).NewQuery(),
			comment, "commentable_id", "", "commentable_type", "commentable",
		)
	})

	_, err := relations.EagerLoadRelation(context.Background(), auth.SystemGrant("comment.view", "acme"),
		[]relations.Model{comment}, "commentable", relation, nil)
	if err == nil {
		t.Fatal("an unregistered morph type loaded without complaint")
	}
	if !strings.Contains(err.Error(), "podcast") {
		t.Fatalf("the error was %v, want it to name the alias that is missing", err)
	}
}

// seedBlog is three users and three posts of one tenant.
func seedBlog() (*db, []*model, []*model) {
	database := newDB()

	users := []*model{
		newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"}),
		newModel(database, "users", "user", map[string]any{"id": "2", "tenant_id": "acme"}),
		newModel(database, "users", "user", map[string]any{"id": "3", "tenant_id": "acme"}),
	}
	for _, user := range users {
		database.seed("users", user.GetAttributes())
	}

	rows := []map[string]any{
		{"id": "10", "user_id": "1", "title": "a", "tenant_id": "acme"},
		{"id": "11", "user_id": "1", "title": "b", "tenant_id": "acme"},
		{"id": "12", "user_id": "2", "title": "c", "tenant_id": "acme"},
	}
	database.seed("posts", rows...)

	posts := make([]*model, 0, len(rows))
	for _, row := range rows {
		posts = append(posts, newModel(database, "posts", "post", row))
	}

	return database, users, posts
}

// postsOf is the relation as a lazy read builds it: constrained to one parent.
func postsOf(database *db, parent relations.Model) *relations.HasMany {
	return relations.NewHasMany(
		newModel(database, "posts", "post", nil).NewQuery(),
		parent,
		"posts.user_id",
		"id",
	)
}

// eagerPostsOf is the relation as an eager load builds it: with constraints
// off, so the parent's own where is not on the query the batch will run.
func eagerPostsOf(database *db, parent relations.Model) *relations.HasMany {
	var relation *relations.HasMany
	relations.NoConstraints(func() { relation = postsOf(database, parent) })
	return relation
}

func asModels(models []*model) []relations.Model {
	out := make([]relations.Model, 0, len(models))
	for _, m := range models {
		out = append(out, m)
	}
	return out
}
