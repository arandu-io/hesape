package concerns

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

// oneOfMany is a CanBeOneOfMany wired the way a has-one-of-many relation wires
// it: a relation query over the related table, a related model that answers a
// fresh builder per subquery, and the four callbacks the trait calls back into.
type oneOfMany struct {
	*CanBeOneOfMany

	conn     *fakeConnection
	related  *fakeModel
	relation *fakeBuilder

	constraints int
	subQueries  []Builder
	joins       int
}

func newOneOfMany(t *testing.T) *oneOfMany {
	t.Helper()

	conn := &fakeConnection{}
	related := newFakeModel("posts")
	relation := newFakeBuilder(related, newQuery(conn, "posts"))

	related.queryFactory = func() Builder {
		return newFakeBuilder(related, newQuery(conn, "posts"))
	}

	host := &oneOfMany{CanBeOneOfMany: &CanBeOneOfMany{}, conn: conn, related: related, relation: relation}

	host.OneOfManyQuery = func() Builder { return relation }
	host.OneOfManyRelated = func() Model { return related }
	host.OneOfManyAddConstraints = func() { host.constraints++ }
	host.GetOneOfManySubQuerySelectColumns = func() []any { return []any{"user_id"} }
	host.AddOneOfManySubQueryConstraints = func(sub Builder, _, _ string) {
		host.subQueries = append(host.subQueries, sub)
	}
	host.AddOneOfManyJoinSubQueryConstraints = func(*query.JoinClause) { host.joins++ }

	return host
}

// TestOfManyNeedsTheRelationName.
//
// The PHP reads it off a debug_backtrace three frames up. There is no
// equivalent, and a name read off a stack frame is a join alias that changes
// when somebody renames a method.
func TestOfManyNeedsTheRelationName(t *testing.T) {
	host := newOneOfMany(t)

	err := host.OfMany([]OfManyColumn{{Column: "id", Aggregate: "MAX"}}, "")
	if err == nil {
		t.Fatal("ofMany was accepted with no relation name, so the join alias would be empty")
	}
	if !strings.Contains(err.Error(), "join alias") {
		t.Fatalf("OfMany: %v, and the error has to say what the name is for", err)
	}
	if host.IsOneOfMany() {
		t.Fatal("a refused ofMany marked the relation as one-of-many")
	}
	if len(host.relation.GetQuery().Joins) != 0 {
		t.Fatal("a refused ofMany joined something")
	}
}

// TestOfManyRefusesAnAggregateThatIsNotMinOrMax, in either case.
//
// Anything else is a value the subquery would compare a key against, and a key
// chosen by SUM is not a key.
//
// The receiver is left marked as one-of-many by a refusal, because the flag is
// set before the loop that validates. Pinned as it behaves: a caller that
// ignores the error gets a relation that thinks it has a subquery it never
// built.
func TestOfManyRefusesAnAggregateThatIsNotMinOrMax(t *testing.T) {
	host := newOneOfMany(t)

	err := host.OfMany([]OfManyColumn{{Column: "id", Aggregate: "SUM"}}, "latest_post")
	if err == nil {
		t.Fatal("ofMany accepted SUM, and a key chosen by a sum is not a key")
	}
	if !strings.Contains(err.Error(), "MIN, MAX") {
		t.Fatalf("OfMany: %v, and the error has to name what is available", err)
	}

	lower := newOneOfMany(t)
	if err := lower.OfMany([]OfManyColumn{{Column: "id", Aggregate: "max"}}, "latest_post"); err != nil {
		t.Fatalf("ofMany refused a lower-case aggregate: %v", err)
	}
}

// TestOfManyDefaultsToTheHighestKey: ofMany with no columns is "the latest one",
// and the key is what orders it.
func TestOfManyDefaultsToTheHighestKey(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if !host.IsOneOfMany() {
		t.Fatal("ofMany did not mark the relation")
	}

	sub := host.GetOneOfManySubQuery()
	if sub == nil {
		t.Fatal("ofMany built no subquery")
	}
	sql := sub.GetQuery().ToSQL()
	if !strings.Contains(sql, `MAX("posts"."id") as "id_aggregate"`) {
		t.Fatalf("the subquery is %q, want the key aggregated as id_aggregate", sql)
	}
	if !strings.Contains(sql, `group by "posts"."user_id"`) {
		t.Fatalf("the subquery is %q, want it grouped by the parent key", sql)
	}
}

// TestOfManyAddsTheKeyAsTheTieBreak.
//
// Two posts written in the same second are one row each in the subquery unless
// something breaks the tie, and without the key the aggregate returns a
// timestamp two rows share -- so the join matches both and a has-one comes back
// with two.
func TestOfManyAddsTheKeyAsTheTieBreak(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany([]OfManyColumn{{Column: "published_at", Aggregate: "MAX"}}, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	if len(host.subQueries) != 2 {
		t.Fatalf("ofMany built %d subqueries, want one per column with the key appended", len(host.subQueries))
	}

	first := host.subQueries[0].GetQuery().ToSQL()
	if !strings.Contains(first, `MAX("posts"."published_at") as "published_at_aggregate"`) {
		t.Fatalf("the first subquery is %q, want the named column aggregated", first)
	}

	second := host.subQueries[1].GetQuery().ToSQL()
	if !strings.Contains(second, `MAX("posts"."id") as "id_aggregate"`) {
		t.Fatalf("the second subquery is %q, want the key appended as the tie-break", second)
	}
}

// TestOfManyBreaksTiesWithMinOnTheEarlierColumns.
//
// The first column of each subquery carries the aggregate the caller asked for,
// and every column carried over from the round before is taken with min. That is
// what makes the tie-break deterministic: the same row wins on every run rather
// than whichever the engine happened to group.
func TestOfManyBreaksTiesWithMinOnTheEarlierColumns(t *testing.T) {
	host := newOneOfMany(t)

	err := host.OfMany([]OfManyColumn{
		{Column: "published_at", Aggregate: "MAX"},
		{Column: "id", Aggregate: "MAX"},
	}, "latest_post")
	if err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	second := host.subQueries[1].GetQuery().ToSQL()
	if !strings.Contains(second, `MAX("posts"."id") as "id_aggregate"`) {
		t.Fatalf("the second subquery is %q, want its own column with the asked aggregate", second)
	}
	if !strings.Contains(second, `min("posts"."published_at") as "published_at_aggregate"`) {
		t.Fatalf("the second subquery is %q, want the earlier column carried over with min", second)
	}
}

// TestOfManyWritesTheAggregateAsTheCallerSpelledIt.
//
// The validation is case-insensitive and the emitted SQL is not: the caller's
// spelling goes into the statement verbatim, while the tie-break columns are
// always taken with a lower-case min. So one subquery can carry both MAX and
// min. Pinned because it is what the statements in a query log look like, and a
// reader comparing two of them should know the difference is the caller's
// spelling rather than two code paths.
func TestOfManyWritesTheAggregateAsTheCallerSpelledIt(t *testing.T) {
	host := newOneOfMany(t)

	err := host.OfMany([]OfManyColumn{
		{Column: "published_at", Aggregate: "mAx"},
		{Column: "id", Aggregate: "MAX"},
	}, "latest_post")
	if err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	if sql := host.subQueries[0].GetQuery().ToSQL(); !strings.Contains(sql, `mAx("posts"."published_at")`) {
		t.Fatalf("the subquery is %q, and the caller wrote mAx", sql)
	}
	if sql := host.subQueries[1].GetQuery().ToSQL(); !strings.Contains(sql, `min("posts"."published_at")`) {
		t.Fatalf("the carried-over column is %q, and the tie-break is always min", sql)
	}
}

// TestOfManyRegistersTheJoinWhenItIsCalledAndNotAtCompileTime.
//
// It used to write the join inside a beforeQuery callback, which runs at compile
// time -- after the tenant pass has already looked at the query. A join that is
// not there when anything goes looking is a join nothing can filter, whatever
// else is fixed.
func TestOfManyRegistersTheJoinWhenItIsCalledAndNotAtCompileTime(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	joins := host.relation.GetQuery().Joins
	if len(joins) != 1 {
		t.Fatalf("the relation query carries %d joins before anything compiled it, want 1", len(joins))
	}
	if joins[0].Type != "inner" {
		t.Fatalf("the join is a %q join", joins[0].Type)
	}
	if host.joins != 1 {
		t.Fatalf("the host's join constraints ran %d times", host.joins)
	}
}

// TestOfManySubqueryIsFilteredByTenant is the leak.
//
// The subquery used to be compiled to SQL and joined as a raw table expression.
// A raw expression is already SQL by the time it reaches the statement, so
// nothing could add a tenant to it: the aggregate ran over every customer's
// rows, and user.LatestPost() resolved to whichever post shared that id with
// this customer -- or to none, which is the quieter half of the same bug.
//
// JoinSub records the subquery as a pending one, which is what the query
// builder's nested pass replaces with the same query, scoped. This runs that
// pass and reads the statement it produced.
func TestOfManySubqueryIsFilteredByTenant(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	compiled := host.relation.GetQuery()
	if err := compiled.ScopeNested(context.Background(), grant()); err != nil {
		t.Fatalf("ScopeNested: %v", err)
	}

	sql := compiled.ToSQL()
	inner := innerSelect(t, sql)
	if !strings.Contains(inner, "tenant_id") {
		t.Fatalf("the joined subquery runs unfiltered:\n%s\nsubquery: %s", sql, inner)
	}

	bindings := compiled.GetBindings()
	if !containsValue(bindings, "acme") {
		t.Fatalf("the statement carries %#v, and the tenant is not among them:\n%s", bindings, sql)
	}
}

// innerSelect returns the text between the first parentheses that open a
// subselect, which is the joined derived table.
func innerSelect(t *testing.T, sql string) string {
	t.Helper()

	start := strings.Index(sql, "(select")
	if start < 0 {
		t.Fatalf("the statement joins no subselect at all:\n%s", sql)
	}

	depth := 0
	for i := start; i < len(sql); i++ {
		switch sql[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return sql[start : i+1]
			}
		}
	}
	t.Fatalf("the subselect is never closed:\n%s", sql)
	return ""
}

func containsValue(values []any, want any) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestOfManySelectsTheRelatedTableWhenNothingWasSelected.
//
// The join brings the subquery's aggregate columns into the row. Without an
// explicit selection the statement would carry them beside the model's own, and
// a hydrated model would hold columns that are not its.
func TestOfManySelectsTheRelatedTableWhenNothingWasSelected(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	columns := host.relation.GetQuery().GetColumns()
	if !reflect.DeepEqual(columns, []any{"posts.*"}) {
		t.Fatalf("the relation selects %#v, want the related table's own columns", columns)
	}
}

// TestOfManyKeepsAnExplicitSelection: a caller that chose its columns chose
// them, and only a bare star is treated as nothing having been chosen.
func TestOfManyKeepsAnExplicitSelection(t *testing.T) {
	host := newOneOfMany(t)
	host.relation.Select("posts.title")

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	columns := host.relation.GetQuery().GetColumns()
	if !reflect.DeepEqual(columns, []any{"posts.title"}) {
		t.Fatalf("the relation selects %#v, and the caller had chosen its columns", columns)
	}

	star := newOneOfMany(t)
	star.relation.Select("*")
	if err := star.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if columns := star.relation.GetQuery().GetColumns(); !reflect.DeepEqual(columns, []any{"posts.*"}) {
		t.Fatalf("a bare star selection was kept as %#v, and it says nothing was chosen", columns)
	}
}

// TestOfManyReRunsTheRelationsOwnConstraintsAfterTheJoin.
//
// The relation's constraints name the parent key, and the join changes which
// rows that key reaches. Applying them before the join would constrain a query
// the join then widened.
func TestOfManyReRunsTheRelationsOwnConstraints(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if host.constraints != 1 {
		t.Fatalf("the relation's constraints ran %d times, want once after the join", host.constraints)
	}
}

// TestTheJoinAliasAvoidsCollidingWithTheRelatedTable.
//
// A relation named after its own table -- posts() on a model of posts -- would
// join `posts` as `posts`, and every qualified column in the statement would
// then name two things.
func TestTheJoinAliasAvoidsCollidingWithTheRelatedTable(t *testing.T) {
	host := newOneOfMany(t)

	if err := host.OfMany(nil, "posts"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if got := host.GetRelationName(); got != "posts_of_many" {
		t.Fatalf("the join alias is %q, and it is the same as the table", got)
	}

	other := newOneOfMany(t)
	if err := other.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if got := other.GetRelationName(); got != "latest_post" {
		t.Fatalf("the join alias is %q, want the relation name", got)
	}
}

// TestQualifySubSelectColumnUsesTheAliasAndTheLastSegment: the subquery's
// columns are reached through the join alias, whatever they were qualified by
// where they were written.
func TestQualifySubSelectColumnUsesTheAliasAndTheLastSegment(t *testing.T) {
	host := newOneOfMany(t)
	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	for _, c := range []struct{ in, want string }{
		{"id_aggregate", "latest_post.id_aggregate"},
		{"posts.id_aggregate", "latest_post.id_aggregate"},
		{"public.posts.id_aggregate", "latest_post.id_aggregate"},
	} {
		if got := host.QualifySubSelectColumn(c.in); got != c.want {
			t.Errorf("QualifySubSelectColumn(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if got := host.QualifyRelatedColumn("id"); got != "posts.id" {
		t.Fatalf("QualifyRelatedColumn = %q, want the related table's own qualification", got)
	}
}

// TestGetRelationQueryPutsConstraintsOnTheSubquery.
//
// A where on the outer query filters after the aggregate has already chosen the
// row: "the latest published post" becomes "the latest post, if it happens to be
// published", which is a different relation and usually an empty one.
func TestGetRelationQueryPutsConstraintsOnTheSubquery(t *testing.T) {
	host := newOneOfMany(t)
	fallback := newFakeBuilder(host.related, newQuery(host.conn, "posts"))

	if got := host.GetRelationQuery(fallback); got != Builder(fallback) {
		t.Fatal("a relation that is not one-of-many sent its constraints somewhere other than its own query")
	}

	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}
	if got := host.GetRelationQuery(fallback); got != host.GetOneOfManySubQuery() {
		t.Fatal("the constraints go on the outer query, where they filter after the aggregate has chosen")
	}
}

// TestMergeOneOfManyJoinsToRepeatsTheJoinOnTheExistenceQuery.
//
// whereHas and withCount build their own query, and without the same join the
// existence check counts every row of the has-many rather than the one the
// aggregate chose.
func TestMergeOneOfManyJoinsToRepeatsTheJoinOnTheExistenceQuery(t *testing.T) {
	host := newOneOfMany(t)
	if err := host.OfMany(nil, "latest_post"); err != nil {
		t.Fatalf("OfMany: %v", err)
	}

	existence := newFakeBuilder(host.related, newQuery(host.conn, "posts"))
	host.MergeOneOfManyJoinsTo(existence)

	joins := existence.GetQuery().Joins
	if len(joins) != 1 {
		t.Fatalf("the existence query carries %d joins, want the relation's own", len(joins))
	}

	sql := existence.GetQuery().ToSQL()
	if !strings.Contains(sql, `"latest_post"`) {
		t.Fatalf("the repeated join does not use the relation's alias:\n%s", sql)
	}
	if !strings.Contains(sql, "id_aggregate") {
		t.Fatalf("the repeated join does not carry the aggregate:\n%s", sql)
	}
}

// TestMergeOneOfManyJoinsToRepeatsNothingForAPlainRelation, which is what makes
// it safe to call unconditionally.
func TestMergeOneOfManyJoinsToRepeatsNothingForAPlainRelation(t *testing.T) {
	host := newOneOfMany(t)
	existence := newFakeBuilder(host.related, newQuery(host.conn, "posts"))

	host.MergeOneOfManyJoinsTo(existence)

	if len(existence.GetQuery().Joins) != 0 {
		t.Fatal("a relation that never called ofMany merged a join into the existence query")
	}
}

// TestLatestOfManyAndOldestOfManyPickOppositeAggregates, and default to the key
// when no column is named.
func TestLatestOfManyAndOldestOfManyPickOppositeAggregates(t *testing.T) {
	latest := newOneOfMany(t)
	if err := latest.LatestOfMany("published_at", "latest_post"); err != nil {
		t.Fatalf("LatestOfMany: %v", err)
	}
	if sql := latest.subQueries[0].GetQuery().ToSQL(); !strings.Contains(sql, `MAX("posts"."published_at")`) {
		t.Fatalf("LatestOfMany compiled %q, want max", sql)
	}

	oldest := newOneOfMany(t)
	if err := oldest.OldestOfMany("published_at", "oldest_post"); err != nil {
		t.Fatalf("OldestOfMany: %v", err)
	}
	if sql := oldest.subQueries[0].GetQuery().ToSQL(); !strings.Contains(sql, `MIN("posts"."published_at")`) {
		t.Fatalf("OldestOfMany compiled %q, want min", sql)
	}

	bare := newOneOfMany(t)
	if err := bare.LatestOfMany("", "latest_post"); err != nil {
		t.Fatalf("LatestOfMany: %v", err)
	}
	if sql := bare.subQueries[0].GetQuery().ToSQL(); !strings.Contains(sql, `MAX("posts"."id")`) {
		t.Fatalf("LatestOfMany with no column compiled %q, want the key", sql)
	}
}

// TestTheOfManyHelpers covers the three functions the ordering rests on, apart
// from the query they end up in.
func TestTheOfManyHelpers(t *testing.T) {
	columns := []OfManyColumn{{Column: "published_at", Aggregate: "MAX"}}

	if !hasColumn(columns, "published_at") {
		t.Error("hasColumn missed a column that is there")
	}
	if hasColumn(columns, "id") {
		t.Error("hasColumn reported a column that is not there, so the tie-break would not be appended")
	}

	if got := defaultOfManyColumn(""); got != "id" {
		t.Errorf("defaultOfManyColumn(\"\") = %q, want the key", got)
	}
	if got := defaultOfManyColumn("published_at"); got != "published_at" {
		t.Errorf("defaultOfManyColumn = %q, and the caller named a column", got)
	}

	if !isStar([]any{"*"}) {
		t.Error("a bare star was not read as nothing having been selected")
	}
	if isStar([]any{"posts.*"}) {
		t.Error("a qualified star was read as nothing having been selected")
	}
	if isStar([]any{"*", "posts.title"}) {
		t.Error("a star beside a named column was read as nothing having been selected")
	}
	if isStar(nil) {
		t.Error("an empty selection was read as a star, which is a different question")
	}
}
