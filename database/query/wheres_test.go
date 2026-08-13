package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// The clause tests read the statement a real grammar compiles, because the
// claim is about the SQL and about the order of the bindings under it. The
// MySQL grammar is the one that spells the most of them -- JSON, fulltext and
// straight joins -- so it is the one these compile against.

func mysqlBuilder() *query.Builder {
	return query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("users")
}

func assertSQL(t *testing.T, b *query.Builder, want string) {
	t.Helper()
	if got := b.ToSQL(); got != want {
		t.Errorf("the statement is\n  %s\nwant\n  %s", got, want)
	}
}

func assertBindings(t *testing.T, b *query.Builder, want ...any) {
	t.Helper()
	got := b.GetBindings()
	if !sameBindings(got, want) {
		t.Errorf("the bindings are %v, want %v", got, want)
	}
}

func TestWhereDateComparesTheDatePartAndBindsTheFormattedValue(t *testing.T) {
	b := mysqlBuilder().WhereDate("created_at", ">=", time.Date(2026, 8, 13, 15, 4, 5, 0, time.UTC))
	assertSQL(t, b, "select * from `users` where date(`created_at`) >= ?")
	assertBindings(t, b, "2026-08-13")
}

func TestWhereDateTakesTheValueAloneAsAnEquals(t *testing.T) {
	b := mysqlBuilder().WhereDate("created_at", "2026-08-13")
	assertSQL(t, b, "select * from `users` where date(`created_at`) = ?")
	assertBindings(t, b, "2026-08-13")
}

func TestTheDayAndTheMonthArePaddedToTwoDigits(t *testing.T) {
	b := mysqlBuilder().WhereDay("created_at", 5).OrWhereMonth("created_at", 9)
	assertSQL(t, b, "select * from `users` where day(`created_at`) = ? or month(`created_at`) = ?")
	assertBindings(t, b, "05", "09")
}

func TestWhereTimeAndWhereYearReadTheirPartOfTheTimestamp(t *testing.T) {
	moment := time.Date(2026, 8, 13, 15, 4, 5, 0, time.UTC)
	b := mysqlBuilder().WhereTime("created_at", "<", moment).WhereYear("created_at", moment)
	assertSQL(t, b, "select * from `users` where time(`created_at`) < ? and year(`created_at`) = ?")
	assertBindings(t, b, "15:04:05", "2026")
}

func TestWhereLikeAndItsNegationCompileTheOperator(t *testing.T) {
	b := mysqlBuilder().WhereLike("name", "tay%", false, "and", false).OrWhereNotLike("email", "%@example.com", false)
	assertSQL(t, b, "select * from `users` where `name` like ? or `email` not like ?")
	assertBindings(t, b, "tay%", "%@example.com")
}

// whereIntegerInRaw writes the values into the statement, so every one of them
// is cast to an integer first: an integer has no quoting to escape.
func TestWhereIntegerInRawWritesIntegersIntoTheStatement(t *testing.T) {
	b := mysqlBuilder().WhereIntegerInRaw("id", []any{1, "2", 3.7}, "and", false)
	assertSQL(t, b, "select * from `users` where `id` in (1, 2, 3)")
	assertBindings(t, b)
}

func TestWhereIntegerNotInRawNegatesIt(t *testing.T) {
	b := mysqlBuilder().OrWhereIntegerNotInRaw("id", []any{4})
	assertSQL(t, b, "select * from `users` where `id` not in (4)")
}

func TestWhereBetweenColumnsBindsNothing(t *testing.T) {
	b := mysqlBuilder().WhereBetweenColumns("created_at", []any{"starts_at", "ends_at"}, "and", false)
	assertSQL(t, b, "select * from `users` where `created_at` between `starts_at` and `ends_at`")
	assertBindings(t, b)
}

func TestWhereValueNotBetweenComparesTheOtherWayRound(t *testing.T) {
	b := mysqlBuilder().WhereValueNotBetween(42, []any{"low", "high"}, "and")
	assertSQL(t, b, "select * from `users` where ? not between `low` and `high`")
	assertBindings(t, b, 42)
}

func TestWhereRowValuesComparesTuples(t *testing.T) {
	b := mysqlBuilder().WhereRowValues([]any{"last_name", "id"}, "<", []any{"smith", 10}, "and")
	assertSQL(t, b, "select * from `users` where (`last_name`, `id`) < (?, ?)")
	assertBindings(t, b, "smith", 10)
}

func TestWhereRowValuesRefusesMismatchedLengths(t *testing.T) {
	b := mysqlBuilder().WhereRowValues([]any{"a", "b"}, "<", []any{1}, "and")
	if sql := b.ToSQL(); !strings.Contains(sql, "1 = 0") {
		t.Errorf("a mismatched row value comparison did not compile to a false clause:\n%s", sql)
	}
	assertBindings(t, b)
}

func TestWhereJSONContainsBindsTheValueAsJSON(t *testing.T) {
	b := mysqlBuilder().WhereJSONContains("options->languages", "en", "and", false)
	assertSQL(t, b, "select * from `users` where json_contains(`options`, ?, '$.\"languages\"')")
	assertBindings(t, b, `"en"`)
}

func TestWhereJSONDoesntContainNegatesTheClause(t *testing.T) {
	b := mysqlBuilder().OrWhereJSONDoesntContain("options->languages", "en")
	if got := b.ToSQL(); !strings.Contains(got, "not json_contains") {
		t.Errorf("the clause was not negated:\n%s", got)
	}
}

func TestWhereJSONLengthBindsAnInteger(t *testing.T) {
	b := mysqlBuilder().WhereJSONLength("options->languages", ">", "3")
	assertSQL(t, b, "select * from `users` where json_length(`options`, '$.\"languages\"') > ?")
	assertBindings(t, b, int64(3))
}

func TestWhereFullTextCompilesTheEnginesSearch(t *testing.T) {
	b := mysqlBuilder().WhereFullText([]any{"title", "body"}, "hesape", map[string]any{"mode": "boolean"}, "and")
	assertSQL(t, b, "select * from `users` where match (`title`, `body`) against (? in boolean mode)")
	assertBindings(t, b, "hesape")
}

// whereAny puts its comparisons in one group, which is what keeps it safe next
// to another clause: `state = ? and (name like ? or email like ?)` rather than
// an or that reaches back over everything before it.
func TestWhereAnyGroupsItsColumns(t *testing.T) {
	b := mysqlBuilder().Where("state", "=", "active").WhereAny([]any{"name", "email"}, "like", "tay%")
	assertSQL(t, b, "select * from `users` where `state` = ? and (`name` like ? or `email` like ?)")
	assertBindings(t, b, "active", "tay%", "tay%")
}

func TestWhereAllJoinsItsColumnsWithAnd(t *testing.T) {
	b := mysqlBuilder().WhereAll([]any{"name", "email"}, "like", "tay%")
	assertSQL(t, b, "select * from `users` where (`name` like ? and `email` like ?)")
}

func TestWhereNoneNegatesTheGroup(t *testing.T) {
	b := mysqlBuilder().Where("state", "=", "active").WhereNone([]any{"name", "email"}, "like", "tay%")
	assertSQL(t, b, "select * from `users` where `state` = ? and not (`name` like ? or `email` like ?)")
}

func TestDynamicWhereReadsTheColumnsOutOfTheMethodName(t *testing.T) {
	b := mysqlBuilder().DynamicWhere("whereFirstNameAndLastNameOrEmail", []any{"taylor", "otwell", "taylor@laravel.com"})
	assertSQL(t, b, "select * from `users` where `first_name` = ? and `last_name` = ? or `email` = ?")
	assertBindings(t, b, "taylor", "otwell", "taylor@laravel.com")
}

func TestMergeWheresAppendsClausesAndBindings(t *testing.T) {
	b := mysqlBuilder().Where("state", "=", "active")
	other := mysqlBuilder().Where("plan", "=", "pro")

	b.MergeWheres(other.Wheres, other.GetBindings())
	assertSQL(t, b, "select * from `users` where `state` = ? and `plan` = ?")
	assertBindings(t, b, "active", "pro")
}

func TestPrepareValueAndOperatorRefusesAnOperatorWithNoValue(t *testing.T) {
	b := mysqlBuilder()
	if _, _, err := b.PrepareValueAndOperator(nil, ">", false); err == nil {
		t.Error("an operator with no value was accepted")
	}
	value, operator, err := b.PrepareValueAndOperator(nil, 100, true)
	if err != nil {
		t.Fatalf("PrepareValueAndOperator: %v", err)
	}
	if value != 100 || operator != "=" {
		t.Errorf("the shorthand read as %v %v, want 100 =", operator, value)
	}
}

// The or twins are the half most likely to be written by copying the and one,
// so the conjunction is worth pinning on its own.
func TestTheOrTwinsUseAnOrConjunction(t *testing.T) {
	b := mysqlBuilder().Where("id", "=", 1).
		OrWhereColumn("first_name", "=", "last_name").
		OrWhereBetweenColumns("created_at", []any{"starts_at", "ends_at"}).
		OrWhereValueBetween(7, []any{"low", "high"}).
		OrWhereJSONContainsKey("options->pro").
		OrWhereRowValues([]any{"a"}, "=", []any{1})

	want := "select * from `users` where `id` = ? " +
		"or `first_name` = `last_name` " +
		"or `created_at` between `starts_at` and `ends_at` " +
		"or ? between `low` and `high` " +
		"or ifnull(json_contains_path(`options`, 'one', '$.\"pro\"'), 0) " +
		"or (`a`) = (?)"
	assertSQL(t, b, want)
	assertBindings(t, b, 1, 7, 1)
}

// Every clause type that carries a value has to be known to WhereBindings, or
// the tenant scoping rebuilds the where segment without it -- silently, and
// with every later value on the wrong placeholder.
func TestTheNewClauseTypesRebuildTheirOwnBindings(t *testing.T) {
	b := mysqlBuilder().
		WhereLike("name", "tay%", false, "and", false).
		WhereDate("created_at", "2026-08-13").
		WhereDay("created_at", 5).
		WhereMonth("created_at", 9).
		WhereYear("created_at", 2026).
		WhereTime("created_at", "15:04:05").
		WhereValueBetween(42, []any{"low", "high"}, "and", false).
		WhereRowValues([]any{"a", "b"}, "<", []any{1, 2}, "and").
		WhereJSONContains("options->pro", true, "and", false).
		WhereJSONLength("options->tags", ">", 2).
		WhereFullText([]any{"body"}, "hesape", nil, "and")

	flat := b.GetBindings()
	rebuilt := b.WhereBindings()
	if !sameBindings(flat, rebuilt) {
		t.Errorf("the clauses rebuild %v, and the builder collected %v", rebuilt, flat)
	}
}
