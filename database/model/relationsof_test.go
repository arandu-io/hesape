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
	Model

	ID        int64  `db:"id"`
	UserID    int64  `db:"user_id"`
	AccountID int64  `db:"account_id"`
	Title     string `db:"title"`
	TenantID  string `db:"tenant_id"`
}

func newPostTable(configure ...func(*TableSpec)) *Table {
	spec := TableSpec{Name: "posts", New: func() Entity { return new(post) }}
	for _, c := range configure {
		c(&spec)
	}
	return NewTable(spec)
}

func newPostModel() (*post, *testConnection) {
	conn := newTestConnection()
	return newPostTable().New(conn).(*post), conn
}

// withPostsOn registers "posts" on the table of model: a has-many to a posts
// table keyed by foreignKey, read through the connection the parent came from.
func withPostsOn(model Entity, foreignKey string) *Table {
	posts := newPostTable()
	model.base().Table().Relate("posts", func(m *Model) Relation {
		return HasMany(m, posts, foreignKey, "")
	})
	return posts
}

// TestAHasManyBetweenTwoRealModels is the end of the seam.
//
// Until this compiled there was a relations tree with no producer for the model
// it consumes: twelve constructors that took an interface nothing satisfied,
// and a typed model that satisfied nothing. What it proves is small and it is
// the thing the whole adapter exists for -- two real models, one relation, one
// statement, scoped.
func TestAHasManyBetweenTwoRealModels(t *testing.T) {
	users, conn := newUserModel()
	posts := newPostTable()

	parent, err := fromRecord(users, map[string]any{
		"id": int64(1), "name": "Ada", "tenant_id": "acme",
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	// The conventional keys: user_id on posts, id on users. Naming them here
	// would be noise, and getting them wrong is what the convention prevents.
	relation := HasMany(&parent.Model, posts, "", "")

	conn.queue()
	ctx, g := context.Background(), auth.SystemGrant("posts.read", "acme")
	if _, err := relation.Get(ctx, g); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// The relation runs on the connection the parent came from.
	last := conn.last()
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

// TestARelationOnARowIsConstrainedAndOnThePrototypeIsNot: the same factory
// narrows the relation to the row it was handed and leaves it open on the model
// a query runs through, which is the one the eager loader resolves -- a
// narrowing there would be to a parent that does not exist.
func TestARelationOnARowIsConstrainedAndOnThePrototypeIsNot(t *testing.T) {
	users, conn := newUserModel()
	posts := newPostTable()

	parent, err := fromRecord(users, map[string]any{"id": int64(1), "tenant_id": "acme"})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	ctx, g := context.Background(), auth.SystemGrant("posts.read", "acme")
	conn.queue()
	if _, err := HasMany(&parent.Model, posts, "", "").Get(ctx, g); err != nil {
		t.Fatalf("Get on the row's relation: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `"posts"."user_id" = ?`) {
		t.Errorf("SQL = %q, want the row's relation narrowed to its key", sql)
	}

	prototype := newQuery(users.base()).GetModel()
	relation := HasMany(prototype, posts, "", "")
	if wheres := relation.GetQuery().GetQuery().Wheres; len(wheres) != 0 {
		t.Errorf("the prototype's relation carries %d wheres, want none: it stands for no row", len(wheres))
	}
}

// TestABelongsToBetweenTwoRealModels is the inverse, and it reads the other
// conventional key.
func TestABelongsToBetweenTwoRealModels(t *testing.T) {
	posts, conn := newPostModel()
	users := newUserTable()

	child, err := fromRecord(posts, map[string]any{
		"id": int64(9), "user_id": int64(1), "title": "a post", "tenant_id": "acme",
	})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	relation := BelongsTo(&child.Model, users, "user_id", "", "user")

	conn.queue()
	if _, err := relation.GetResults(context.Background(), auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("GetResults: %v", err)
	}

	last := conn.last()
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
	users, conn := newUserModel()
	posts := newPostTable()

	parent, err := fromRecord(users, map[string]any{"id": int64(1), "tenant_id": "acme"})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}

	if _, err := HasMany(&parent.Model, posts, "", "").Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("a relation ran under a grant that carries no tenant")
	}
	if len(conn.statements) != 0 {
		t.Fatalf("it reached the connection anyway: %v", conn.sqls())
	}
}

// TestARelationOnARowNothingBuiltPanicsWithTheReason: a relation reads its
// connection off the parent, and a literal has none -- the factory is called
// while declaring a relation, where there is no error to return.
func TestARelationOnARowNothingBuiltPanicsWithTheReason(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), "did not build") {
			t.Fatalf("recovered %v, want a panic naming the unwired row", r)
		}
	}()
	HasMany(&(&user{}).Model, newPostTable(), "", "")
}

// A module, and the relation surface it reaches.
//
// This is the same program as testdata/relation_surface, running: that one
// proves an application can write it, this one proves what it emits and what
// comes back. They are two tests because they fail differently -- a surface that
// compiles and returns the wrong rows is the failure the compile fixture cannot
// see.

// blog is what a module keeps: its tables, built once.
type blog struct {
	users *Table
	posts *Table
	conn  *testConnection
}

func newBlog(conn *testConnection) *blog {
	users := NewTable(TableSpec{Name: "users", New: func() Entity { return new(account) }})
	posts := newPostTable()

	users.Relate("posts", func(u *Model) Relation {
		return HasMany(u, posts, "user_id", "id")
	})
	return &blog{users: users, posts: posts, conn: conn}
}

// TestAModuleEagerLoadsAndReadsBackTheRelation is the whole of what the surface
// is for: one call registers it, one query loads it, and the row a terminal
// handed back carries it.
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
	rows, err := blog.users.Query(conn).With("posts").Get(ctx, g)
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

	// Read back off the row the terminal handed over.
	posts, ok := rows[0].base().Related("posts")
	if !ok {
		t.Fatal("the loaded relation is not reachable from the row")
	}
	if len(posts) != 2 || posts[0].(*post).Title != "first" {
		t.Fatalf("posts = %v, want the two rows whose user_id is this parent's key", posts)
	}

	// The parent that matched nothing is loaded and empty, not unloaded.
	empty, ok := rows[1].base().Related("posts")
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
	_, err := blog.users.Query(conn).
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

	if _, err := blog.users.Query(conn).With("posts").Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("an eager load ran under a grant that carries no tenant")
	}
	if got := len(conn.sqls()); got != 0 {
		t.Fatalf("it reached the connection anyway: %v", conn.sqls())
	}
}

// TestRelateAfterTheFirstQueryPanics: a table is shared by every request at
// once, so a relation added after it served one would be a write racing every
// read.
func TestRelateAfterTheFirstQueryPanics(t *testing.T) {
	conn := newTestConnection()
	blog := newBlog(conn)
	_ = blog.users.Query(conn)

	defer func() {
		if recover() == nil {
			t.Fatal("Relate after a query did not panic")
		}
	}()
	blog.users.Relate("comments", func(u *Model) Relation { return HasMany(u, blog.posts, "", "") })
}
