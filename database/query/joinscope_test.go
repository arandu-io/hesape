package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// TestAJoinedTableCarriesTheTenantToo pins the fourth instance of the leak that
// took four rounds to close.
//
// A join contributes no filter of its own: the tenant clause names the table in
// the from, and everything reached by joining is read whole. A pivot row
// belonging to one customer granted a role in another -- the role was filtered,
// the row that granted it was not.
func TestAJoinedTableCarriesTheTenantToo(t *testing.T) {
	t.Parallel()

	db := &fakeConnection{}
	b := newTestBuilder(db).
		From("roles").
		Join("role_user", "roles.id", "=", "role_user.role_id").
		Where("role_user.user_id", "=", 1)

	if _, err := b.Get(context.Background(), auth.SystemGrant("role.list", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := db.lastSQL()
	if !strings.Contains(sql, `"role_user"."tenant_id" = ?`) {
		t.Fatalf("the joined table was read without a tenant:\n%s", sql)
	}
	if !strings.Contains(sql, `"roles"."tenant_id" = ?`) {
		t.Fatalf("the selected table lost its tenant:\n%s", sql)
	}
}

// TestAJoinedSubqueryIsNotFilteredOnATenantColumnItDoesNotHave covers the
// skip: a derived table has no tenant column, and the query inside it was
// already scoped on the way through.
func TestAJoinedSubqueryIsNotFilteredOnATenantColumnItDoesNotHave(t *testing.T) {
	t.Parallel()

	db := &fakeConnection{}
	sub := newTestBuilder(db).From("posts")
	b := newTestBuilder(db).
		From("users").
		JoinSub(sub, "recent", "users.id", "=", "recent.user_id", "inner", false)

	if _, err := b.Get(context.Background(), auth.SystemGrant("user.list", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := db.lastSQL()
	if strings.Contains(sql, `"recent"."tenant_id"`) {
		t.Fatalf("the derived table was filtered on a column it does not have:\n%s", sql)
	}
	// The subquery still carries its own, which is the whole point of the skip.
	if !strings.Contains(sql, `"posts"."tenant_id" = ?`) {
		t.Fatalf("the subquery inside the join lost its tenant:\n%s", sql)
	}
}

// TestScopingAJoinTwiceWritesTheFilterOnce keeps a query reusable: running the
// same builder under two Grants must not stack clauses.
func TestScopingAJoinTwiceWritesTheFilterOnce(t *testing.T) {
	t.Parallel()

	db := &fakeConnection{}
	b := newTestBuilder(db).
		From("roles").
		Join("role_user", "roles.id", "=", "role_user.role_id")

	for _, tenant := range []string{"acme", "globex"} {
		if _, err := b.Get(context.Background(), auth.SystemGrant("role.list", tenant)); err != nil {
			t.Fatalf("Get for %s: %v", tenant, err)
		}
		if n := strings.Count(db.lastSQL(), `"role_user"."tenant_id" = ?`); n != 1 {
			t.Fatalf("the join filter appears %d times for %s:\n%s", n, tenant, db.lastSQL())
		}
	}
}
