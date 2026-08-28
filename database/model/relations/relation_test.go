package relations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
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

	relation := relations.NewMorphManyUnconstrained(
		newModel(database, "comments", "comment", nil).NewQuery(),
		post,
		"comments.commentable_type",
		"comments.commentable_id",
		"id",
	)

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

	relation := relations.NewHasManyThroughUnconstrained(
		newModel(database, "posts", "post", nil).NewQuery(),
		country, through,
		"country_id", "user_id", "id", "id",
	)

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

// TestHasManyThroughDoesNotReachThroughAnotherTenantsRow covers the intermediate
// table.
//
// The country is this customer's and so is the post. What is not is the user
// row that links them: this customer's user 1 lives in another country
// entirely, and the row that says user 1 is Brazilian belongs to the other
// customer. The join used to bring it in unfiltered -- constrainThroughParents
// added `users.deleted_at is null` and no tenant -- so the country came back
// with a post it can only be reached through somebody else's row, carrying that
// row's key under ThroughKey.
func TestHasManyThroughDoesNotReachThroughAnotherTenantsRow(t *testing.T) {
	database := newDB()

	country := newModel(database, "countries", "country", map[string]any{"id": "br", "tenant_id": "acme"})
	through := newModel(database, "users", "user", nil)

	database.seed("users",
		map[string]any{"id": "1", "country_id": "pt", "tenant_id": "acme"},
		map[string]any{"id": "1", "country_id": "br", "tenant_id": "other"},
	)
	database.seed("posts",
		map[string]any{"id": "10", "user_id": "1", "title": "acme post", "tenant_id": "acme"},
	)

	relation := relations.NewHasManyThroughUnconstrained(
		newModel(database, "posts", "post", nil).NewQuery(),
		country, through,
		"country_id", "user_id", "id", "id",
	)

	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	models := []relations.Model{country}
	if _, err := relations.EagerLoadRelation(ctx, g, models, "posts", relation, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _ := country.GetRelation("posts")
	posts := loaded.([]relations.Model)
	if len(posts) != 0 {
		t.Fatalf("the country came back with %d posts, want none: the only path to them runs through another customer's user row", len(posts))
	}
}

// TestLatestOfManyAggregatesOnlyTheGrantsTenant covers inside the subquery a
// one-of-many relation joins.
//
// latestOfMany picks a row with `max(posts.id)`, taken in a grouped subquery and
// joined back. The subquery used to be compiled to raw SQL and joined by hand
// inside a beforeQuery callback, which put it out of reach of every tenant pass
// -- so the maximum was taken over every customer's posts. The post below with
// the higher id belongs to another customer, and the aggregate that ignores the
// tenant answers with its id, which this customer's relation then resolves to a
// post it does not have.
//
// What is asserted is what the statement bound, and it is asserted rather than
// counted for a reason worth stating: a derived table is SQL by the time it
// reaches a connection, so this fake has no way to evaluate one, and the value
// bound inside it is the only place the filter shows. The statement binds the
// tenant twice -- once for the table it reads from, once inside the subquery --
// and the subquery's comes first, because the join is compiled before the where.
func TestLatestOfManyAggregatesOnlyTheGrantsTenant(t *testing.T) {
	database := newDB()

	user := newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"})
	database.seed("posts",
		map[string]any{"id": "10", "user_id": "1", "title": "the one acme wrote", "tenant_id": "acme"},
		map[string]any{"id": "99", "user_id": "1", "title": "another customer's", "tenant_id": "other"},
	)

	relation := relations.NewHasOne(
		newModel(database, "posts", "post", nil).NewQuery(), user, "user_id", "id")
	if err := relation.LatestOfMany("id", "latest_post"); err != nil {
		t.Fatal(err)
	}

	ctx, g := context.Background(), auth.SystemGrant("post.view", "acme")

	rows, err := relation.GetQuery().GetQuery().Get(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !sameValue(rows[0]["id"], "10") {
		t.Fatalf("the relation read %v, want this customer's only post", rows)
	}

	bound := database.bound[len(database.bound)-1]
	tenants := 0
	for _, binding := range bound {
		if binding == "acme" {
			tenants++
		}
	}
	if tenants != 2 {
		t.Fatalf("the statement bound the tenant %d times (%v), want 2: one for posts and one inside the aggregate subquery", tenants, bound)
	}
	if len(bound) == 0 || bound[0] != "acme" {
		t.Fatalf("the statement begins with the binding %v, want the tenant: the subquery is joined before the where clause, so the first value belongs to the aggregate", bound)
	}
}

// TestOneOfManyExistenceQueryCarriesTheSameJoin is what MergeOneOfManyJoinsTo
// owes the existence query: a whereHas over a latestOfMany relation has to join
// the same aggregate the relation joins, or it asks whether the parent has any
// post rather than whether it has that one.
//
// It is here because the mechanism changed: the join used to be a deferred
// callback the merge copied, and is now the join itself, repeated.
func TestOneOfManyExistenceQueryCarriesTheSameJoin(t *testing.T) {
	database := newDB()

	user := newModel(database, "users", "user", map[string]any{"id": "1", "tenant_id": "acme"})
	relation := relations.NewHasOne(
		newModel(database, "posts", "post", nil).NewQuery(), user, "user_id", "id")
	if err := relation.LatestOfMany("id", "latest_post"); err != nil {
		t.Fatal(err)
	}

	existence := newModel(database, "posts", "post", nil).NewQuery()
	parent := newModel(database, "users", "user", nil).NewQuery()
	relation.GetRelationExistenceQuery(existence, parent)

	if joins := existence.GetQuery().Joins; len(joins) != 1 {
		t.Fatalf("the existence query carries %d joins, want the one the relation aggregates through", len(joins))
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

// TestTheUnconstrainedConstructorLeavesTheParentWhereOffTheQuery: it is what
// makes the eager query one statement for every parent rather than one narrowed
// to the first.
//
// The property used to belong to a process-wide flag. It belongs to the
// constructor now, which is why this test reads two constructors instead of one
// constructor and a switch.
func TestTheUnconstrainedConstructorLeavesTheParentWhereOffTheQuery(t *testing.T) {
	database, users, _ := seedBlog()

	constrained := postsOf(database, users[0])
	if len(constrained.GetQuery().GetQuery().Wheres) == 0 {
		t.Fatal("a relation built normally carries no constraint at all")
	}

	unconstrained := eagerPostsOf(database, users[0])
	if len(unconstrained.GetQuery().GetQuery().Wheres) != 0 {
		t.Fatalf("an unconstrained relation carried %+v", unconstrained.GetQuery().GetQuery().Wheres)
	}
}

// TestABuilderThatMakesNoTenantPromiseIsFilteredByTheRelation is the safe
// direction of the split written down.
//
// ScopeTenant asks the builder whether it already filters its own table, and
// skips its own clause when the answer is yes. This is the other answer: a
// builder that says nothing -- because it never heard of the question, which is
// the state every builder starts in -- is filtered here.
//
// The failure this guards is the one that leaves no filter at all. Somebody
// reading two identical tenant clauses deletes one, and if the default here were
// to skip, deleting the wrong one would mean a relation over an ordinary builder
// reading every customer's rows in a statement that looks finished.
func TestABuilderThatMakesNoTenantPromiseIsFilteredByTheRelation(t *testing.T) {
	database := newDB()
	posts := newModel(database, "posts", "post", nil)

	if _, promises := any(posts.NewQuery()).(concerns.OwnTenantScoper); promises {
		t.Fatal("this fake now answers OwnTenantScoper, so it no longer stands for a builder that makes no promise")
	}

	scoped, err := concerns.ScopeTenant(posts.NewQuery(), posts, auth.SystemGrant("posts.read", "acme"))
	if err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	filtered := false
	for _, where := range scoped.GetQuery().Wheres {
		if where.Column == "posts.tenant_id" {
			filtered = true
		}
	}
	if !filtered {
		t.Fatalf("a builder that promises nothing was left unfiltered: %+v", scoped.GetQuery().Wheres)
	}
}
