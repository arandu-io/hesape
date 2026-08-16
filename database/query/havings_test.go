package query_test

import (
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

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
