package eloquent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

func TestGetRouteKeyIsTheKeyByDefault(t *testing.T) {
	model, _ := newUserModel()
	model.Entity.ID = 7

	if got := model.GetRouteKeyName(); got != "id" {
		t.Errorf("GetRouteKeyName = %q, want id: the PHP returns getKeyName", got)
	}
	if got := model.GetRouteKey(); got != int64(7) {
		t.Errorf("GetRouteKey = %v, want 7: the PHP reads the route key name off the attributes", got)
	}
}

func TestResolveRouteBindingFiltersByTenant(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(map[string]any{"id": int64(9), "name": "Ada"})

	found, err := model.ResolveRouteBinding(context.Background(), grant(), "9", "")
	if err != nil {
		t.Fatalf("ResolveRouteBinding: %v", err)
	}
	if found == nil || found.Entity.Name != "Ada" {
		t.Fatalf("ResolveRouteBinding found %v, want the row", found)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `"id" = ?`) {
		t.Errorf("sql = %q, want the route key in the where clause", sql)
	}
	if !strings.Contains(sql, `"tenant_id" = ?`) {
		t.Errorf("sql = %q: a bound id with no tenant filter is another customer's row (RULE 17)", sql)
	}
}

func TestResolveRouteBindingRefusesAGrantWithoutATenant(t *testing.T) {
	model, _ := newUserModel()

	if _, err := model.ResolveRouteBinding(context.Background(), auth.Grant{}, "9", ""); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("error = %v, want ErrNoTenant: the id came off a URL and nothing else scopes it", err)
	}
}

func TestResolveRouteBindingUsesTheGivenField(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	if _, err := model.ResolveRouteBinding(context.Background(), grant(), "ada@example.com", "email"); err != nil {
		t.Fatalf("ResolveRouteBinding: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `"email" = ?`) {
		t.Errorf("sql = %q, want the named field: $field ?? getRouteKeyName()", sql)
	}
}

func TestResolveSoftDeletableRouteBindingKeepsTrashedRows(t *testing.T) {
	model, conn := newUserModel()
	model.SoftDeletes = true
	conn.queue()

	if _, err := model.ResolveSoftDeletableRouteBinding(context.Background(), grant(), "9", ""); err != nil {
		t.Fatalf("ResolveSoftDeletableRouteBinding: %v", err)
	}
	if sql := conn.last().SQL; strings.Contains(sql, "deleted_at") {
		t.Errorf("sql = %q, want no soft delete filter: withTrashed is what the method adds", sql)
	}
}

func TestChildRouteBindingRelationshipNameIsPluralCamel(t *testing.T) {
	if got := ChildRouteBindingRelationshipName("blog_post"); got != "blogPosts" {
		t.Errorf("ChildRouteBindingRelationshipName = %q, want blogPosts: Str::plural(Str::camel($childType))", got)
	}
}

func TestResolveChildRouteBindingRefusesAnUndeclaredRelation(t *testing.T) {
	parent, _ := newUserModel()
	children, _ := newUserModel()

	_, err := ResolveChildRouteBinding(context.Background(), parent, children.NewQuery(), grant(), "post", "3", "")
	if !errors.Is(err, ErrRelationNotFound) {
		t.Fatalf("error = %v, want ErrRelationNotFound: the PHP calls $this->{$name}() and there is no such method", err)
	}
}

func TestResolveChildRouteBindingQualifiesTheFieldAndCarriesTheTenant(t *testing.T) {
	parent, _ := newUserModel()
	parent.RelationResolvers = map[string]func(*Model[user]) Relation{"posts": nil}

	children, conn := newUserModel()
	children.Table = "posts"
	conn.queue()

	if _, err := ResolveChildRouteBinding(context.Background(), parent, children.NewQuery(), grant(), "post", "3", "slug"); err != nil {
		t.Fatalf("ResolveChildRouteBinding: %v", err)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `"posts"."slug" = ?`) {
		t.Errorf("sql = %q, want the field qualified with the child table", sql)
	}
	if !strings.Contains(sql, `"tenant_id" = ?`) {
		t.Errorf("sql = %q: a nested resource is still a row somebody owns (RULE 17)", sql)
	}
}
