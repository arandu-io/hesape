package model

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

type modelCompileSpyGrammar struct {
	query.Grammar
	updateCalls int
}

func (g *modelCompileSpyGrammar) CompileUpdate(builder *query.Builder, values map[string]any) string {
	g.updateCalls++
	return g.Grammar.CompileUpdate(builder, values)
}

func TestModelWritesValidateOperatorsAgainstTheGrammarThatCompilesThem(t *testing.T) {
	connection := newTestConnection()
	model := NewModel[user]("users", connection, grammars.NewMySQLGrammar(), &testProcessor{conn: connection})
	foreign := query.NewBuilder(connection, grammars.NewPostgresGrammar(), &testProcessor{conn: connection}).
		From("users").
		Where("metadata", "@>", "{}")
	builder := model.NewQuery().SetQuery(foreign)

	_, err := builder.Update(context.Background(), auth.SystemGrant("users.write", "acme"), map[string]any{"name": "Ada"})
	if !errors.Is(err, query.ErrInvalidOperator) {
		t.Fatalf("Update() error = %v, want ErrInvalidOperator", err)
	}
	if statements := connection.sqls(); len(statements) != 0 {
		t.Fatalf("the connection received statements: %v", statements)
	}
}

func TestModelWriteCallbackCannotReplaceTheCompilerOperatorPolicy(t *testing.T) {
	connection := newTestConnection()
	compiler := &modelCompileSpyGrammar{Grammar: grammars.NewMySQLGrammar()}
	model := NewModel[user]("users", connection, compiler, &testProcessor{conn: connection})
	foreign := query.NewBuilder(connection, grammars.NewPostgresGrammar(), &testProcessor{conn: connection}).
		From("users")
	foreign.Wheres = append(foreign.Wheres, query.Where{
		Type: "Basic", Column: "metadata", Operator: "@>", Value: "{}", Boolean: "and",
	})
	foreign.BeforeQuery(func(builder *query.Builder) {
		builder.Grammar = grammars.NewPostgresGrammar()
	})
	builder := model.NewQuery().SetQuery(foreign)
	// A prepared builder reaches the write sink without another ScopeNested
	// preflight; the sink must independently enforce its compiler's policy.
	builder.prepared = true

	_, err := builder.Update(context.Background(), auth.SystemGrant("users.write", "acme"), map[string]any{"name": "Ada"})
	if !errors.Is(err, query.ErrInvalidOperator) {
		t.Fatalf("Update() error = %v, want ErrInvalidOperator", err)
	}
	if compiler.updateCalls != 0 {
		t.Fatalf("the model compiler was called %d times", compiler.updateCalls)
	}
	if statements := connection.sqls(); len(statements) != 0 {
		t.Fatalf("the connection received statements: %v", statements)
	}
}
