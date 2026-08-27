package model

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
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
