package query_test

import (
	"context"
	"errors"
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
	b := mysqlBuilder().DynamicWhere("whereFirstNameAndLastNameOrEmail", []any{"ada", "lovelace", "ada@example.com"})
	assertSQL(t, b, "select * from `users` where `first_name` = ? and `last_name` = ? or `email` = ?")
	assertBindings(t, b, "ada", "lovelace", "ada@example.com")
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

func TestPrepareValueAndOperatorRefusesWithoutDisablingTheBuilder(t *testing.T) {
	b := mysqlBuilder()

	if _, _, err := b.PrepareValueAndOperator(1, "OR 1=1 --", false); !errors.Is(err, query.ErrInvalidOperator) {
		t.Fatalf("PrepareValueAndOperator() error = %v, want ErrInvalidOperator", err)
	}
	if b.Err() != nil {
		t.Fatalf("Err() = %v: screening a combination disabled the builder", b.Err())
	}

	assertSQL(t, b.Where("id", "=", 1), "select * from `users` where `id` = ?")
}

func TestWhereRejectsAnOperatorFragmentBeforeItReachesSQL(t *testing.T) {
	b := mysqlBuilder().Where("id", "= ? OR 1=1 --", 7)

	if got := b.ToSQL(); got != "" {
		t.Fatalf("a hostile operator reached SQL: %s", got)
	}
	if !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
	}
	var invalid *query.InvalidOperatorError
	if !errors.As(b.Err(), &invalid) || invalid.Operator != "= ? OR 1=1 --" {
		t.Fatalf("Err() = %#v, want the refused operator", b.Err())
	}
}

func TestOperatorNormalizationIsCaseInsensitiveButNeverRepairsMalformedInput(t *testing.T) {
	assertSQL(t, mysqlBuilder().Where("name", "LiKe", "a%"), "select * from `users` where `name` like ?")

	malformed := []string{
		" =", "= ", "=\t", "=\n", "not  like", "=/**/", "= --", "=; delete", "or", "unknown",
	}
	for _, operator := range malformed {
		t.Run(strings.ReplaceAll(operator, " ", "_"), func(t *testing.T) {
			b := mysqlBuilder().Where("id", operator, 1)
			if got := b.ToSQL(); got != "" {
				t.Fatalf("operator %q reached SQL: %s", operator, got)
			}
			if !errors.Is(b.Err(), query.ErrInvalidOperator) {
				t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
			}
		})
	}
}

func TestOperatorErrorsAreStickyAndTheTwoArgumentWhereStaysAValue(t *testing.T) {
	b := mysqlBuilder().Where("id", "first bad", 1).Where("id", "second bad", 2)
	var invalid *query.InvalidOperatorError
	if !errors.As(b.Err(), &invalid) || invalid.Operator != "first bad" {
		t.Fatalf("Err() = %#v, want the first invalid operator", b.Err())
	}

	assertSQL(t, mysqlBuilder().Where("status", ">"), "select * from `users` where `status` = ?")
	assertSQL(t, mysqlBuilder().WhereRaw("id = 1 OR 1 = 1 --"), "select * from `users` where id = 1 OR 1 = 1 --")
}

func TestFinalBarrierRejectsDirectMergeAndBeforeQueryMutations(t *testing.T) {
	tests := map[string]func(*query.Builder){
		"direct": func(b *query.Builder) {
			b.Wheres = append(b.Wheres, query.Where{Type: "Basic", Column: "id", Operator: "", Value: 1, Boolean: "and"})
		},
		"merge": func(b *query.Builder) {
			b.MergeWheres([]query.Where{{Type: "Basic", Column: "id", Operator: "= OR true", Value: 1, Boolean: "and"}}, []any{1})
		},
		"callback": func(b *query.Builder) {
			b.Where("id", "=", 1).BeforeQuery(func(mutated *query.Builder) {
				mutated.Wheres[0].Operator = "=; select"
			})
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			b := mysqlBuilder()
			mutate(b)
			if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
				t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
			}
		})
	}
}

func TestAllCallbacksRunBeforeTheFinalOperatorBarrier(t *testing.T) {
	parent := mysqlBuilder().Where("id", "=", 1)
	child := mysqlBuilder()
	child.BeforeQuery(func(*query.Builder) {
		parent.Wheres[0].Operator = "= OR 1=1"
	})
	parent.Union(child)

	if got := parent.ToSQL(); got != "" || !errors.Is(parent.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, parent.Err())
	}
}

func TestAnInvalidEmptyNestedWherePropagatesItsError(t *testing.T) {
	sub := mysqlBuilder().SelectRaw("count(*)")
	b := mysqlBuilder().Where(func(group *query.Builder) {
		group.WhereSubCount(sub, "bogus", 1, "and")
	})

	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

type compileSpyGrammar struct {
	*fakeGrammar
	selectCalls          int
	compileCalls         int
	updateCalls          int
	updateFromCalls      int
	operatorCalls        int
	bitwiseOperatorCalls int
}

func (g *compileSpyGrammar) GetOperators() []string {
	g.operatorCalls++
	return g.fakeGrammar.GetOperators()
}

func (g *compileSpyGrammar) GetBitwiseOperators() []string {
	g.bitwiseOperatorCalls++
	return g.fakeGrammar.GetBitwiseOperators()
}

func (g *compileSpyGrammar) CompileSelect(b *query.Builder) string {
	g.selectCalls++
	g.compileCalls++
	return g.fakeGrammar.CompileSelect(b)
}

func (g *compileSpyGrammar) CompileInsert(b *query.Builder, values []map[string]any) string {
	g.compileCalls++
	return g.fakeGrammar.CompileInsert(b, values)
}

func (g *compileSpyGrammar) CompileInsertOrIgnore(b *query.Builder, values []map[string]any) string {
	g.compileCalls++
	return g.fakeGrammar.CompileInsertOrIgnore(b, values)
}

func (g *compileSpyGrammar) CompileInsertGetID(b *query.Builder, values map[string]any, sequence string) string {
	g.compileCalls++
	return g.fakeGrammar.CompileInsertGetID(b, values, sequence)
}

func (g *compileSpyGrammar) CompileInsertUsing(b *query.Builder, columns []any, sql string) string {
	g.compileCalls++
	return g.fakeGrammar.CompileInsertUsing(b, columns, sql)
}

func (g *compileSpyGrammar) CompileInsertOrIgnoreUsing(b *query.Builder, columns []any, sql string) string {
	g.compileCalls++
	return g.fakeGrammar.CompileInsertOrIgnoreUsing(b, columns, sql)
}

func (g *compileSpyGrammar) CompileUpdate(b *query.Builder, values map[string]any) string {
	g.compileCalls++
	g.updateCalls++
	return g.fakeGrammar.CompileUpdate(b, values)
}

func (g *compileSpyGrammar) CompileUpdateFrom(b *query.Builder, values map[string]any) string {
	g.compileCalls++
	g.updateFromCalls++
	return g.fakeGrammar.CompileUpdateFrom(b, values)
}

func (g *compileSpyGrammar) CompileUpsert(b *query.Builder, values []map[string]any, uniqueBy, update []string) string {
	g.compileCalls++
	return g.fakeGrammar.CompileUpsert(b, values, uniqueBy, update)
}

func (g *compileSpyGrammar) CompileDelete(b *query.Builder) string {
	g.compileCalls++
	return g.fakeGrammar.CompileDelete(b)
}

func (g *compileSpyGrammar) CompileTruncate(b *query.Builder) map[string][]any {
	g.compileCalls++
	return g.fakeGrammar.CompileTruncate(b)
}

func (g *compileSpyGrammar) CompileExists(b *query.Builder) string {
	g.compileCalls++
	return g.fakeGrammar.CompileExists(b)
}

func TestInvalidOperatorCallsNeitherCompilerNorConnection(t *testing.T) {
	connection := &fakeConnection{}
	grammar := &compileSpyGrammar{fakeGrammar: &fakeGrammar{}}
	b := query.NewBuilder(connection, grammar, &fakeProcessor{}).
		From("users").
		Where("id", "= OR 1=1", 1)
	if grammar.operatorCalls != 0 {
		t.Fatalf("an obviously malformed operator reached GetOperators %d times", grammar.operatorCalls)
	}

	_, err := b.Get(context.Background(), grant())
	if !errors.Is(err, query.ErrInvalidOperator) {
		t.Fatalf("Get() error = %v, want ErrInvalidOperator", err)
	}
	if grammar.compileCalls != 0 {
		t.Fatalf("a compiler was called %d times", grammar.compileCalls)
	}
	if len(connection.calls) != 0 {
		t.Fatalf("the connection received %d calls", len(connection.calls))
	}
}

func TestFinalBarrierConsultsOnlyTheLiveOperatorPolicyForASafeToken(t *testing.T) {
	grammar := &compileSpyGrammar{fakeGrammar: &fakeGrammar{}}
	b := query.NewBuilder(nil, grammar, nil).From("users")
	b.Wheres = append(b.Wheres, query.Where{
		Type: "Basic", Column: "id", Operator: "bogus", Value: 1, Boolean: "and",
	})

	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
	if grammar.operatorCalls != 1 || grammar.compileCalls != 0 {
		t.Fatalf("grammar calls: GetOperators=%d compile=%d", grammar.operatorCalls, grammar.compileCalls)
	}
}

type phraseGrammar struct{ *fakeGrammar }

func (g *phraseGrammar) GetOperators() []string {
	return append(g.fakeGrammar.GetOperators(), "matches phrase")
}

func TestExternalGrammarMayDeclareASafeMultiwordOperator(t *testing.T) {
	b := query.NewBuilder(nil, &phraseGrammar{fakeGrammar: &fakeGrammar{}}, nil).
		From("documents").
		Where("body", "MATCHES PHRASE", "secure query")

	if got := b.ToSQL(); got != `select * from "documents" where "body" matches phrase ?` {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

func TestCallbacksRegisteredByCallbacksDrainBeforeCompilation(t *testing.T) {
	b := mysqlBuilder().Where("tenant_id", "=", "acme")
	calls := 0
	b.BeforeQuery(func(q *query.Builder) {
		calls++
		q.BeforeQuery(func(next *query.Builder) {
			calls++
			next.Wheres[0].Operator = "not an operator"
		})
	})

	if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
	if calls != 2 || len(b.BeforeQueryCallbacks) != 0 {
		t.Fatalf("callback calls = %d, queued = %d", calls, len(b.BeforeQueryCallbacks))
	}
}

func TestSelfRegisteringBeforeQueryCallbackFailsClosed(t *testing.T) {
	b := mysqlBuilder().Where("tenant_id", "=", "acme")
	var callback func(*query.Builder)
	callback = func(q *query.Builder) { q.BeforeQuery(callback) }
	b.BeforeQuery(callback)

	if got := b.ToSQL(); got != "" || b.Err() == nil {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

func TestFinalBarrierUsesTheLiveGrammarAfterPublicOrCallbackMutation(t *testing.T) {
	t.Run("public mutation rejects an old dialect operator", func(t *testing.T) {
		b := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil).
			From("documents").
			Where("metadata", "@>", "{}")
		b.Grammar = grammars.NewMySQLGrammar()

		if got := b.ToSQL(); got != "" || !errors.Is(b.Err(), query.ErrInvalidOperator) {
			t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
		}
	})

	t.Run("before query mutation admits the new dialect operator", func(t *testing.T) {
		b := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("documents")
		b.Wheres = append(b.Wheres, query.Where{
			Type: "Basic", Column: "metadata", Operator: "@>", Value: "{}", Boolean: "and",
		})
		b.BeforeQuery(func(mutated *query.Builder) {
			mutated.Grammar = grammars.NewPostgresGrammar()
		})

		if got := b.ToSQL(); got == "" || b.Err() != nil {
			t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
		}
	})
}

func TestScopedExecutionRunsEachBeforeQueryCallbackExactlyOnce(t *testing.T) {
	connection := &fakeConnection{}
	b := query.NewBuilder(connection, &fakeGrammar{}, &fakeProcessor{}).
		From("users").
		Where("active", true)
	calls := 0
	b.BeforeQuery(func(*query.Builder) { calls++ })

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if calls != 1 {
		t.Fatalf("BeforeQuery callback ran %d times, want 1", calls)
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
