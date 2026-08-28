package model

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// post is the other end of the relation under test.
type post struct {
	ID       int64  `db:"id"`
	UserID   int64  `db:"user_id"`
	Title    string `db:"title"`
	TenantID string `db:"tenant_id"`
}

func newPostModel() (*Model[post], *testConnection) {
	conn := newTestConnection()
	return NewModel[post]("posts", conn, newTestGrammar(), &testProcessor{conn: conn}), conn
}

// TestAHasManyBetweenTwoRealModels is the end of the seam.
//
// Until this compiled there was a relations tree with no producer for the model
// it consumes: twelve constructors that took an interface nothing satisfied,
// and a typed model that satisfied nothing. What it proves is small and it is
// the thing the whole adapter exists for -- two real models, one relation, one
// statement, scoped.
func TestAHasManyBetweenTwoRealModels(t *testing.T) {
	users, _ := newUserModel()
	posts, postsConn := newPostModel()

	parent, err := users.NewFromBuilder(map[string]any{
		"id": int64(1), "name": "Ada", "tenant_id": "acme",
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	// The conventional keys: user_id on posts, id on users. Naming them here
	// would be noise, and getting them wrong is what the convention prevents.
	relation := HasManyOf(parent, posts, "", "")

	postsConn.queue()
	ctx, g := context.Background(), auth.SystemGrant("posts.read", "acme")
	if _, err := relation.Get(ctx, g); err != nil {
		t.Fatalf("Get: %v", err)
	}

	last := postsConn.last()
	if !strings.Contains(last.SQL, "posts") {
		t.Errorf("the relation did not read the related table: %s", last.SQL)
	}
	if !strings.Contains(strings.ToLower(last.SQL), "user_id") {
		t.Errorf("the conventional foreign key is not in the statement: %s", last.SQL)
	}

	// The half that matters most. A relation is a read path, and a read path
	// that loses the tenant is the leak this framework exists to make
	// impossible.
	found := false
	for _, binding := range last.Bindings {
		if binding == "acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("the relation ran without the tenant: %s %v", last.SQL, last.Bindings)
	}
}

// TestABelongsToBetweenTwoRealModels is the inverse, and it reads the other
// conventional key.
func TestABelongsToBetweenTwoRealModels(t *testing.T) {
	users, usersConn := newUserModel()
	posts, _ := newPostModel()

	child, err := posts.NewFromBuilder(map[string]any{
		"id": int64(9), "user_id": int64(1), "title": "a post", "tenant_id": "acme",
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	relation := BelongsToModel(child, users, "user_id", "", "user")

	usersConn.queue()
	if _, err := relation.GetResults(context.Background(), auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("GetResults: %v", err)
	}

	last := usersConn.last()
	if !strings.Contains(last.SQL, "users") {
		t.Errorf("the relation did not read the owner's table: %s", last.SQL)
	}
	if !strings.Contains(strings.ToLower(last.SQL), "id") {
		t.Errorf("the owner key is not in the statement: %s", last.SQL)
	}
}

// TestARelationRefusesAGrantWithNoTenant: whatever else a relation is, it is a
// read path, and a read path without authorization does not run.
func TestARelationRefusesAGrantWithNoTenant(t *testing.T) {
	users, _ := newUserModel()
	posts, postsConn := newPostModel()

	parent, err := users.NewFromBuilder(map[string]any{"id": int64(1), "tenant_id": "acme"})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	if _, err := HasManyOf(parent, posts, "", "").Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("a relation ran under a grant that carries no tenant")
	}
	if len(postsConn.statements) != 0 {
		t.Fatalf("it reached the connection anyway: %v", postsConn.sqls())
	}
}

// A module, and the relation surface it reaches.
//
// This is the same program as testdata/relation_surface, running: that one
// proves an application can write it, this one proves what it emits and what
// comes back. They are two tests because they fail differently -- a surface that
// compiles and returns the wrong rows is the failure the compile fixture cannot
// see.

// blog is what a module constructor keeps.
type blog struct {
	users *Model[account]
	posts *Model[post]
}

func newBlog(conn *testConnection) *blog {
	users := NewModel[account]("users", conn, newTestGrammar(), &testProcessor{conn: conn})
	posts := NewModel[post]("posts", conn, newTestGrammar(), &testProcessor{conn: conn})

	users.RelationResolvers = map[string]func(*Model[account]) Relation{
		"posts": func(u *Model[account]) Relation {
			return HasManyOfUnconstrained(u, posts, "user_id", "id")
		},
	}
	return &blog{users: users, posts: posts}
}

// TestAModuleEagerLoadsAndReadsBackTheRelation is the whole of what the surface
// is for: one call registers it, one query loads it, and the row a terminal
// handed back carries it typed.
func TestAModuleEagerLoadsAndReadsBackTheRelation(t *testing.T) {
	conn := newTestConnection()
	blog := newBlog(conn)

	conn.queue(
		query.Record{"id": int64(1), "name": "Ada", "tenant_id": "acme"},
		query.Record{"id": int64(2), "name": "Alan", "tenant_id": "acme"},
	)
	conn.queue(
		query.Record{"id": int64(10), "user_id": int64(1), "title": "first", "tenant_id": "acme"},
		query.Record{"id": int64(11), "user_id": int64(1), "title": "second", "tenant_id": "acme"},
	)

	ctx, g := context.Background(), auth.SystemGrant("users.list", "acme")
	rows, err := blog.users.NewQuery().With("posts").Get(ctx, g)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Get returned %d rows", len(rows))
	}

	// Two statements for two parents, and the second one asked for every
	// parent's children at once.
	if got := len(conn.sqls()); got != 2 {
		t.Fatalf("ran %d statements, want 2: %v", got, conn.sqls())
	}
	relation := conn.sqls()[1]
	if !strings.Contains(relation, `"posts"."user_id" in (?, ?)`) {
		t.Errorf("the relation query = %q, want every parent's key in one in ()", relation)
	}
	if !strings.Contains(relation, `"posts"."tenant_id" = ?`) {
		t.Errorf("the relation query = %q, and a relation is a read path (RULE 17)", relation)
	}
	if got := strings.Count(relation, `"posts"."tenant_id"`); got != 1 {
		t.Errorf("the relation query names the tenant %d times, want 1: %s", got, relation)
	}

	// Read back typed, off the row the terminal handed over.
	posts, ok := Related[account, post](rows[0], "posts")
	if !ok {
		t.Fatal("the loaded relation is not reachable from the row")
	}
	if len(posts) != 2 || posts[0].Title != "first" {
		t.Fatalf("posts = %v, want the two rows whose user_id is this parent's key", posts)
	}

	// The parent that matched nothing is loaded and empty, not unloaded.
	empty, ok := Related[account, post](rows[1], "posts")
	if !ok {
		t.Fatal("the parent with no children reads as a relation that was never loaded")
	}
	if len(empty) != 0 {
		t.Errorf("posts = %v, want none", empty)
	}
}

// TestAModuleFiltersByWhatTheRelationContains is the existence half: WhereHas
// and WithCount compile to a correlated subquery over the related table, scoped
// on its own.
func TestAModuleFiltersByWhatTheRelationContains(t *testing.T) {
	conn := newTestConnection()
	blog := newBlog(conn)
	conn.queue()

	ctx, g := context.Background(), auth.SystemGrant("users.list", "acme")
	_, err := blog.users.NewQuery().
		WhereHas("posts", func(sub *query.Builder) { sub.Where("published", "=", true) }).
		WithCount("posts").
		Get(ctx, g)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL

	// The correlation is the parent's key against the foreign key pointing back
	// at it. It was the parent's key against the child's own key until the
	// compare key stopped being answered by the wrong type -- `users.id =
	// posts.id`, which runs, and answers with whichever rows share an id.
	if !strings.Contains(sql, `"users"."id" = "posts"."user_id"`) {
		t.Errorf("SQL = %q, want the parent correlated against the foreign key", sql)
	}
	if !strings.Contains(sql, "exists (") {
		t.Errorf("SQL = %q, want whereHas compiled to an exists", sql)
	}
	if !strings.Contains(sql, `as "posts_count"`) {
		t.Errorf("SQL = %q, want the aggregate aliased onto the row", sql)
	}
	if !strings.Contains(sql, `"published" = ?`) {
		t.Errorf("SQL = %q, want the caller's constraint inside the subquery", sql)
	}

	// Both subqueries carry the related table's tenant, and the outer query
	// carries its own. A subquery that does not is one tenant's users selected
	// by another tenant's posts.
	if got := strings.Count(sql, `"posts"."tenant_id" = ?`); got != 2 {
		t.Errorf("SQL = %q has %d scoped subqueries, want 2", sql, got)
	}
	if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Errorf("SQL = %q, want the outer query scoped too", sql)
	}
	if got, want := strings.Count(sql, "?"), len(conn.last().Bindings); got != want {
		t.Errorf("SQL = %q has %d placeholders for %d bindings %v", sql, got, want, conn.last().Bindings)
	}
}

// TestAModuleRefusesToLoadARelationWithoutATenant: a relation is a read path,
// and it goes through the same door every other read does.
func TestAModuleRefusesToLoadARelationWithoutATenant(t *testing.T) {
	conn := newTestConnection()
	blog := newBlog(conn)
	conn.queue(query.Record{"id": int64(1), "name": "Ada", "tenant_id": "acme"})

	if _, err := blog.users.NewQuery().With("posts").Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("an eager load ran under a grant that carries no tenant")
	}
	if got := len(conn.sqls()); got != 0 {
		t.Fatalf("it reached the connection anyway: %v", conn.sqls())
	}
}
