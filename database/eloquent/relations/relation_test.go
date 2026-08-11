package relations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
)

// TestAMorphEagerLoadFiltersOnTheTypeColumn: the id alone is not enough. Post 1
// and video 1 both exist, and a relation that only matched the id would hand
// the video's comments to the post.
func TestAMorphEagerLoadFiltersOnTheTypeColumn(t *testing.T) {
	database := newDB()
	post := newModel(database, "posts", "post", map[string]any{"id": "1", "tenant_id": "acme"})

	database.seed("comments",
		map[string]any{"id": "1", "commentable_id": "1", "commentable_type": "post", "body": "on the post", "tenant_id": "acme"},
		map[string]any{"id": "2", "commentable_id": "1", "commentable_type": "video", "body": "on the video", "tenant_id": "acme"},
	)

	var relation *relations.MorphMany
	relations.NoConstraints(func() {
		relation = relations.NewMorphMany(
			newModel(database, "comments", "comment", nil).NewQuery(),
			post,
			"comments.commentable_type",
			"comments.commentable_id",
			"id",
		)
	})

	ctx, g := context.Background(), auth.SystemGrant("comment.view", "acme")
	models := []relations.Model{post}
	if _, err := relations.EagerLoadRelation(ctx, g, models, "comments", relation, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _ := post.GetRelation("comments")
	comments := loaded.([]relations.Model)
	if len(comments) != 1 {
		t.Fatalf("the post came back with %d comments, want only the one whose type is post", len(comments))
	}
	if comments[0].GetAttribute("body") != "on the post" {
		t.Errorf("the comment matched was %v", comments[0].GetAttribute("body"))
	}
}

// TestHasOneWithDefaultAnswersAModelRatherThanNil: withDefault is what keeps a
// template from having to test for null before every field.
func TestHasOneWithDefaultAnswersAModelRatherThanNil(t *testing.T) {
	database := newDB()
	user := newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"})

	relation := relations.NewHasOne(
		newModel(database, "profiles", "profile", nil).NewQuery(),
		user,
		"profiles.user_id",
		"id",
	)
	relation.WithDefaultAttributes(map[string]any{"theme": "light"})

	result, err := relation.GetResults(context.Background(), auth.SystemGrant("profile.view", "acme"))
	if err != nil {
		t.Fatal(err)
	}

	profile, ok := result.(relations.Model)
	if !ok || profile == nil {
		t.Fatalf("a relation with no row and withDefault answered %v, want an unsaved model", result)
	}
	if profile.GetAttribute("theme") != "light" {
		t.Errorf("the default was not filled: %v", profile.GetAttributes())
	}
	if profile.GetAttribute("user_id") != "1" {
		t.Errorf("the default model was not pointed back at its parent: %v", profile.GetAttributes())
	}
}

// TestChaperoneLinksTheChildrenBackToTheParent: without it, reading the inverse
// on a loaded child is one query per child -- an N+1 hiding inside the fix for
// an N+1.
func TestChaperoneLinksTheChildrenBackToTheParent(t *testing.T) {
	database, users, _ := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	relation := eagerPostsOf(database, users[0])
	relation.GetRelated().(*model).declared = []string{"user"}

	if err := relation.Inverse("user"); err != nil {
		t.Fatal(err)
	}

	models := asModels(users)
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", relation, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _ := models[0].GetRelation("posts")
	posts := loaded.([]relations.Model)
	if len(posts) == 0 {
		t.Fatal("no posts came back")
	}

	owner, ok := posts[0].GetRelation("user")
	if !ok {
		t.Fatal("the child came back without the inverse relation set")
	}
	if owner.(relations.Model).GetKey() != "1" {
		t.Errorf("the inverse pointed at %v, want the parent it came from", owner.(relations.Model).GetKey())
	}
}

// TestChaperoneRefusesARelationTheModelDoesNotDeclare answers
// RelationNotFoundException: naming a relation that is not there would set an
// attribute nobody reads.
func TestChaperoneRefusesARelationTheModelDoesNotDeclare(t *testing.T) {
	database, users, _ := seedBlog()

	if err := eagerPostsOf(database, users[0]).Inverse("autor"); err == nil {
		t.Fatal("chaperone accepted a relation the related model does not declare")
	}
}

// TestHasManyThroughReachesTheFarParentThroughTheJoin, and does it in one
// query: the intermediate table is joined, not queried.
func TestHasManyThroughReachesTheFarParentThroughTheJoin(t *testing.T) {
	database := newDB()

	country := newModel(database, "countries", "country", map[string]any{"id": "br", "tenant_id": "acme"})
	through := newModel(database, "users", "user", nil)

	database.seed("users",
		map[string]any{"id": "1", "country_id": "br", "tenant_id": "acme"},
		map[string]any{"id": "2", "country_id": "pt", "tenant_id": "acme"},
	)
	database.seed("posts",
		map[string]any{"id": "10", "user_id": "1", "title": "from brazil", "tenant_id": "acme"},
		map[string]any{"id": "11", "user_id": "2", "title": "from portugal", "tenant_id": "acme"},
	)

	var relation *relations.HasManyThrough
	relations.NoConstraints(func() {
		relation = relations.NewHasManyThrough(
			newModel(database, "posts", "post", nil).NewQuery(),
			country, through,
			"country_id", "user_id", "id", "id",
		)
	})

	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	before := database.count()
	models := []relations.Model{country}
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", relation, nil); err != nil {
		t.Fatal(err)
	}

	if ran := database.count() - before; ran != 1 {
		t.Fatalf("a has-many-through eager load ran %d queries, want 1", ran)
	}

	loaded, _ := country.GetRelation("posts")
	posts := loaded.([]relations.Model)
	if len(posts) != 1 || posts[0].GetAttribute("title") != "from brazil" {
		t.Fatalf("the country came back with %v, want only the post reached through its own user", posts)
	}
	if posts[0].GetAttribute(relations.ThroughKey) == nil {
		t.Error("the intermediate key was not selected, so nothing could have keyed the match")
	}
}

// TestSoleTellsTheTwoWrongAnswersApart.
func TestSoleTellsTheTwoWrongAnswersApart(t *testing.T) {
	database, users, _ := seedBlog()
	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	if _, err := postsOf(database, users[0]).Sole(ctx, g); !errors.Is(err, relations.ErrMultipleRecordsFound) {
		t.Errorf("sole over two rows answered %v, want the multiple-records error", err)
	}

	if _, err := postsOf(database, users[2]).Sole(ctx, g); !errors.Is(err, relations.ErrModelNotFound) {
		t.Errorf("sole over no rows answered %v, want the not-found error", err)
	}
}

// TestAssociateWritesTheKeyAndTheLoadedModel.
func TestAssociateWritesTheKeyAndTheLoadedModel(t *testing.T) {
	database, users, posts := seedBlog()

	relation := relations.NewBelongsTo(
		newModel(database, "users", "user", nil).NewQuery(),
		posts[0], "user_id", "id", "user",
	)

	relation.Associate(users[1])
	if posts[0].GetAttribute("user_id") != "2" {
		t.Errorf("associate wrote %v to the foreign key", posts[0].GetAttribute("user_id"))
	}
	if _, ok := posts[0].GetRelation("user"); !ok {
		t.Error("associate left the relation unloaded, so reading it would go to the database")
	}

	relation.Dissociate()
	if posts[0].GetAttribute("user_id") != nil {
		t.Error("dissociate left the foreign key set")
	}
}

// TestTheMorphAliasIsWhatTheColumnHolds.
func TestTheMorphAliasIsWhatTheColumnHolds(t *testing.T) {
	database := newDB()
	relations.MorphMap(map[string]func() relations.Model{
		"post": func() relations.Model { return newModel(database, "posts", "post", nil) },
	})

	instance, err := relations.CreateModelByType("post")
	if err != nil {
		t.Fatal(err)
	}
	if instance.GetTable() != "posts" {
		t.Errorf("the alias resolved to table %q", instance.GetTable())
	}

	alias, err := relations.GetMorphAlias(instance)
	if err != nil || alias != "post" {
		t.Errorf("the alias came back as %q (%v), want the registered one", alias, err)
	}

	if _, err := relations.CreateModelByType("nothing"); !errors.Is(err, relations.ErrMorphNotMapped) {
		t.Errorf("an unregistered alias answered %v", err)
	}
}

// TestNoConstraintsLeavesTheParentWhereOffTheQuery: it is what makes the eager
// query one statement for every parent rather than one narrowed to the first.
func TestNoConstraintsLeavesTheParentWhereOffTheQuery(t *testing.T) {
	database, users, _ := seedBlog()

	constrained := postsOf(database, users[0])
	if len(constrained.GetQuery().GetQuery().Wheres) == 0 {
		t.Fatal("a relation built normally carries no constraint at all")
	}

	unconstrained := eagerPostsOf(database, users[0])
	if len(unconstrained.GetQuery().GetQuery().Wheres) != 0 {
		t.Fatalf("a relation built inside NoConstraints carried %+v", unconstrained.GetQuery().GetQuery().Wheres)
	}
}
