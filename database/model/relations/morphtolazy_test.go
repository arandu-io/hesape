package relations_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations"
)

// Reading a MorphTo one row at a time.
//
// The eager path resolves the morph type first and then queries each type's
// table once, and it has had a test since it was written. The lazy path had
// none, and it was wrong: MorphTo declared no GetResults of its own, so it
// inherited BelongsTo's, which reads the query the relation was constructed
// with. For a MorphTo that query is over a placeholder model -- one the
// constructor takes only to have a connection to start from -- so a lazy read
// selected from the placeholder's table with no morph type in the where clause.
//
// A comment on a video came back as a post, or as nothing, depending on which
// table the placeholder happened to name. Both are well-formed queries.

// TestReadingAMorphToOneRowAtATimeUsesTheResolvedType is the test the lazy path
// never had.
func TestReadingAMorphToOneRowAtATimeUsesTheResolvedType(t *testing.T) {
	database := newDB()
	database.seed("posts", map[string]any{"id": "1", "title": "a post", "tenant_id": "acme"})
	database.seed("videos", map[string]any{"id": "7", "title": "a video", "tenant_id": "acme"})

	relations.MorphMap(map[string]func() relations.Model{
		"post":  func() relations.Model { return newModel(database, "posts", "post", nil) },
		"video": func() relations.Model { return newModel(database, "videos", "video", nil) },
	})

	ctx, g := context.Background(), auth.SystemGrant("comment.view", "acme")

	// The comment points at a video. The relation is constructed over a posts
	// placeholder, which is what the eager path would have replaced and the lazy
	// path used to keep.
	comment := newModel(database, "comments", "comment", map[string]any{
		"id": "12", "commentable_id": "7", "commentable_type": "video", "tenant_id": "acme",
	})

	relation := relations.NewMorphTo(
		newModel(database, "posts", "post", nil).NewQuery(),
		comment, "commentable_id", "", "commentable_type", "commentable",
	)

	result, err := relation.GetResults(ctx, g)
	if err != nil {
		t.Fatalf("GetResults: %v", err)
	}

	owner, ok := result.(relations.Model)
	if !ok || owner == nil {
		t.Fatalf("GetResults returned %T, want the video the comment points at", result)
	}
	if owner.GetTable() != "videos" {
		t.Fatalf("the lazy read went to %s, want videos -- the placeholder's table won", owner.GetTable())
	}
	if got := owner.GetAttribute("id"); got != "7" {
		t.Fatalf("id = %v, want 7", got)
	}
}

// TestAMorphToWithNoTypeHasNoOwner: a child that names no type has no owner,
// rather than an owner of whatever kind the placeholder was.
func TestAMorphToWithNoTypeHasNoOwner(t *testing.T) {
	database := newDB()
	database.seed("posts", map[string]any{"id": "1", "title": "a post", "tenant_id": "acme"})

	relations.MorphMap(map[string]func() relations.Model{
		"post": func() relations.Model { return newModel(database, "posts", "post", nil) },
	})

	comment := newModel(database, "comments", "comment", map[string]any{
		"id": "13", "commentable_id": "1", "tenant_id": "acme",
	})

	relation := relations.NewMorphTo(
		newModel(database, "posts", "post", nil).NewQuery(),
		comment, "commentable_id", "", "commentable_type", "commentable",
	)

	before := database.count()
	result, err := relation.GetResults(context.Background(), auth.SystemGrant("comment.view", "acme"))
	if err != nil {
		t.Fatalf("GetResults: %v", err)
	}
	if result != nil {
		t.Fatalf("a comment with no commentable_type came back with %v", result)
	}
	if ran := database.count() - before; ran != 0 {
		t.Fatalf("it ran %d queries for a row that names no type, want 0", ran)
	}
}
