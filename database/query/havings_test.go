package query_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

func TestHavingUsesTheSameOperatorBarrierAsWhere(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").Having("total", "> OR 1=1", 100)
	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

func TestInvalidHavingStopsBeforeTheBitwisePolicyAndCompiler(t *testing.T) {
	grammar := &compileSpyGrammar{fakeGrammar: &fakeGrammar{}}
	b := query.NewBuilder(nil, grammar, nil).
		From("invoices").
		GroupBy("plan").
		Having("total", "bogus", 100)

	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
	if grammar.operatorCalls != 1 {
		t.Fatalf("GetOperators() calls = %d, want 1", grammar.operatorCalls)
	}
	if grammar.bitwiseOperatorCalls != 0 {
		t.Fatalf("GetBitwiseOperators() calls = %d, want 0", grammar.bitwiseOperatorCalls)
	}
	if grammar.compileCalls != 0 {
		t.Fatalf("compiler calls = %d, want 0", grammar.compileCalls)
	}
}

func TestAnInvalidEmptyNestedHavingPropagatesItsError(t *testing.T) {
	sub := mysqlBuilder().SelectRaw("count(*)")
	b := mysqlBuilder().HavingNested(func(group *query.Builder) {
		group.WhereSubCount(sub, "bogus", 1, "and")
	}, "and")

	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

// A having filters the groups a group by produced, and its bindings live in
// their own segment: they are read after the where's and before the order's, so
// a having written before a where still binds after it.

func TestOrHavingUsesAnOrConjunction(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").Having("total", ">", 100).OrHaving("count", "<", 5)
	assertSQL(t, b, "select * from `users` group by `plan` having `total` > ? or `count` < ?")
	assertBindings(t, b, 100, 5)
}

func TestHavingBetweenBindsTwoBoundsAndNoMore(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").HavingBetween("total", []any{1, 100, 999}, "and", false)
	assertSQL(t, b, "select * from `users` group by `plan` having `total` between ? and ?")
	assertBindings(t, b, 1, 100)
}

func TestOrHavingNotBetweenNegatesAndOrs(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").Having("total", ">", 1).OrHavingNotBetween("count", []any{2, 3})
	assertSQL(t, b, "select * from `users` group by `plan` having `total` > ? or `count` not between ? and ?")
	assertBindings(t, b, 1, 2, 3)
}

func TestHavingNullAndItsNegationTakeSeveralColumns(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").HavingNull([]any{"total", "count"}, "and", false).OrHavingNotNull("label")
	assertSQL(t, b, "select * from `users` group by `plan` having `total` is null and `count` is null or `label` is not null")
	assertBindings(t, b)
}

func TestHavingNestedGroupsItsClauses(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").
		Having("total", ">", 100).
		HavingNested(func(group *query.Builder) {
			group.Having("count", "<", 5).OrHaving("count", ">", 50)
		}, "and")

	assertSQL(t, b, "select * from `users` group by `plan` having `total` > ? and (`count` < ? or `count` > ?)")
	assertBindings(t, b, 100, 5, 50)
}

// A group with nothing in it is dropped rather than compiled: "()" is a syntax
// error on every engine, and an empty callback is an ordinary outcome of a
// conditional filter.
func TestAnEmptyNestedHavingIsDropped(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").HavingNested(func(group *query.Builder) {}, "and")
	assertSQL(t, b, "select * from `users` group by `plan`")
}

func TestOrHavingRawWritesTheSQLAsGiven(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").HavingRaw("sum(total) > ?", 100).OrHavingRaw("count(*) < ?", 5)
	assertSQL(t, b, "select * from `users` group by `plan` having sum(total) > ? or count(*) < ?")
	assertBindings(t, b, 100, 5)
}

// A having opened with a callback is a nested group.
func TestHavingWithACallbackNests(t *testing.T) {
	b := mysqlBuilder().GroupBy("plan").Having(func(group *query.Builder) {
		group.Having("count", ">", 1)
	})
	assertSQL(t, b, "select * from `users` group by `plan` having (`count` > ?)")
}
