package eloquent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func activeScope() map[string]NamedScope[user] {
	return map[string]NamedScope[user]{
		"active": func(b *Builder[user], parameters ...any) *Builder[user] {
			if len(parameters) > 0 {
				return b.Where("state", "=", parameters[0])
			}
			return b.Where("state", "=", "active")
		},
	}
}

func TestHasNamedScopeReadsTheRegistry(t *testing.T) {
	model, _ := newUserModel()
	model.NamedScopes = activeScope()

	if !model.HasNamedScope("active") {
		t.Error("a registered scope is what method_exists('scopeActive') answers to")
	}
	if model.HasNamedScope("archived") {
		t.Error("an unregistered scope is a missing method there")
	}
	if !model.NewQuery().HasNamedScope("active") {
		t.Error("Builder::hasNamedScope asks the model")
	}
}

func TestScopesAppliesTheNamedScope(t *testing.T) {
	model, conn := newUserModel()
	model.NamedScopes = activeScope()
	conn.queue()

	if _, err := model.NewQuery().Scopes("active").Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `"state" = ?`) {
		t.Errorf("sql = %q, want the scope's where clause", sql)
	}
}

func TestCallNamedScopeTakesParameters(t *testing.T) {
	model, conn := newUserModel()
	model.NamedScopes = activeScope()
	conn.queue()

	q := model.CallNamedScope("active", model.NewQuery(), "archived")
	if _, err := q.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := conn.last().Bindings[0]; got != "archived" {
		t.Errorf("binding = %v, want archived: the parameters reach the scope", got)
	}
}

func TestCallNamedScopeReportsAnUnknownScope(t *testing.T) {
	model, _ := newUserModel()

	_, err := model.CallNamedScope("archived", model.NewQuery()).Get(context.Background(), grant())
	if !errors.Is(err, ErrNamedScopeNotFound) {
		t.Fatalf("error = %v, want ErrNamedScopeNotFound", err)
	}
}

func TestScopeWheresAreGroupedSoAnOrCannotEscape(t *testing.T) {
	model, conn := newUserModel()
	model.NamedScopes = map[string]NamedScope[user]{
		"eitherName": func(b *Builder[user], _ ...any) *Builder[user] {
			return b.Where("name", "=", "Ada").OrWhere("name", "=", "Grace")
		},
	}
	conn.queue()

	q := model.NewQuery().Where("email", "=", "ada@example.com").Scopes("eitherName")
	if _, err := q.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `("name" = ? or "name" = ?)`) {
		t.Errorf("sql = %q: addNewWheresWithinGroup wraps a scope that carries an or, or the scope stops filtering", sql)
	}
}
