package eloquent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// This file is the proof that a relation subquery carries the tenant, and it
// proves it by counting rows rather than by reading SQL.
//
// The audit that found the leak read the emitted statement and saw no
// posts.tenant_id in the subselect of a withCount. A test written the same way
// asserts the shape of a string, and the shape of a string is what the four
// tests in builder_test.go asserted while the leak was in place -- they passed
// on SQL that counted every tenant's posts. So the tables here hold rows of two
// tenants, the query runs against them, and what is asserted is the number that
// only comes out right when the subquery is filtered.
//
// # How a query is run without a database
//
// evalGrammar compiles every select to a handle -- {{q0}} -- and keeps the
// builder it compiled beside it. What reaches the connection is therefore not
// SQL but the name of a query, and evalDB evaluates the builder that name stands
// for: its wheres against the seeded rows, its columns per row, and any subquery
// it names the same way, recursively.
//
// The handle is what makes this honest. A subquery frozen into a raw column --
// the bug -- freezes the handle of the builder as it was at that moment, so the
// column still names the UNSCOPED query and the count comes out wrong. Nothing
// here can be satisfied by a filter that was added to a copy nobody selected
// from.

// evalDB is the seeded database and the registry of compiled queries.
type evalDB struct {
	tables     map[string][]map[string]any
	registered []*query.Builder
}

func newEvalDB() *evalDB {
	return &evalDB{tables: map[string][]map[string]any{}}
}

// seed adds one row to a table.
func (db *evalDB) seed(table string, row map[string]any) {
	db.tables[table] = append(db.tables[table], row)
}

// register records a compiled query and returns the handle that stands for
// it.
func (db *evalDB) register(q *query.Builder) string {
	db.registered = append(db.registered, q)
	return fmt.Sprintf("{{q%d}}", len(db.registered)-1)
}

// resolve returns the query a handle stands for. The text may carry more
// than the handle -- "(  {{q3}} ) as \"posts_count\"" -- so it is searched
// for.
func (db *evalDB) resolve(text string) (*query.Builder, bool) {
	start := strings.Index(text, "{{q")
	if start < 0 {
		return nil, false
	}
	end := strings.Index(text[start:], "}}")
	if end < 0 {
		return nil, false
	}
	index, err := strconv.Atoi(text[start+3 : start+end])
	if err != nil || index >= len(db.registered) {
		return nil, false
	}
	return db.registered[index], true
}

// Select implements query.Connection: the statement is a handle, and the
// query it names is evaluated against the seeded rows.
func (db *evalDB) Select(_ context.Context, statement string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	q, ok := db.resolve(statement)
	if !ok {
		return nil, fmt.Errorf("evalDB: %q is not a query this grammar compiled", statement)
	}
	return db.evaluate(q, nil)
}

func (db *evalDB) Insert(context.Context, string, []any) (bool, error)  { return true, nil }
func (db *evalDB) Update(context.Context, string, []any) (int64, error) { return 0, nil }
func (db *evalDB) Delete(context.Context, string, []any) (int64, error) { return 0, nil }
func (db *evalDB) Statement(context.Context, string, []any) (bool, error) {
	return true, nil
}

// scope is one table and one of its rows, for resolving a qualified column.
type scope struct {
	table string
	row   map[string]any
}

// evaluate runs a query against the seeded rows, with outer holding the rows of
// the queries this one is nested in -- which is what makes a correlated
// subquery, `posts.user_id = users.id`, mean anything.
func (db *evalDB) evaluate(q *query.Builder, outer []scope) ([]query.Record, error) {
	table := tableOf(q.GetFrom())
	rows, ok := db.tables[table]
	if !ok {
		return nil, fmt.Errorf("evalDB: no table named %q was seeded", table)
	}

	matched := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		scopes := append([]scope{{table: table, row: row}}, outer...)
		keep, err := db.matches(q.Wheres, scopes)
		if err != nil {
			return nil, err
		}
		if keep {
			matched = append(matched, row)
		}
	}

	matched = window(matched, q.GetOffset(), q.GetLimit())

	if aggregate := q.GetAggregate(); aggregate != nil {
		value, err := db.aggregate(aggregate.Function, renderSQL(aggregate.Columns[0]), matched)
		if err != nil {
			return nil, err
		}
		return []query.Record{{"aggregate": value}}, nil
	}

	// A relation's existence query carries its aggregate as its whole select
	// list -- `select count(*) from posts ...` -- which is how withAggregate
	// writes it.
	if function, column, ok := aggregateSelect(q); ok {
		value, err := db.aggregate(function, column, matched)
		if err != nil {
			return nil, err
		}
		return []query.Record{{function: value}}, nil
	}

	out := make([]query.Record, 0, len(matched))
	for _, row := range matched {
		scopes := append([]scope{{table: table, row: row}}, outer...)
		record, err := db.project(q, row, scopes)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

// window applies the limit and the offset, over the rows in the order they were
// seeded. Ordering is not evaluated: a chunked walk orders by the key, and the
// rows are seeded in key order, so the two agree and there is nothing for a sort
// to decide.
func window(rows []map[string]any, offset, limit *int) []map[string]any {
	if offset != nil {
		if *offset >= len(rows) {
			return nil
		}
		rows = rows[*offset:]
	}
	if limit != nil && *limit < len(rows) {
		rows = rows[:*limit]
	}
	return rows
}

// project builds one result row out of the query's select list.
func (db *evalDB) project(q *query.Builder, row map[string]any, scopes []scope) (query.Record, error) {
	if len(q.Columns) == 0 {
		return copyRecord(row), nil
	}

	record := query.Record{}
	for _, column := range q.Columns {
		text := renderSQL(column)
		if text == "*" || strings.HasSuffix(text, ".*") {
			for key, value := range row {
				record[key] = value
			}
			continue
		}

		if sub, ok := db.resolve(text); ok {
			alias, err := aliasOf(text)
			if err != nil {
				return nil, err
			}
			value, err := db.scalar(sub, text, scopes)
			if err != nil {
				return nil, err
			}
			record[alias] = value
			continue
		}

		value, err := lookup(text, scopes)
		if err != nil {
			return nil, err
		}
		record[strings.TrimPrefix(text, tableOf(q.GetFrom())+".")] = value
	}
	return record, nil
}

// aggregateSelect reads a select list that is one aggregate call and
// nothing else: count(*), sum("total"). It returns the function and the
// column.
func aggregateSelect(q *query.Builder) (function, column string, ok bool) {
	if len(q.Columns) != 1 {
		return "", "", false
	}
	text := strings.TrimSpace(renderSQL(q.Columns[0]))
	open := strings.Index(text, "(")
	if open <= 0 || !strings.HasSuffix(text, ")") {
		return "", "", false
	}
	function = strings.ToLower(text[:open])
	switch function {
	case "count", "sum", "min", "max", "avg":
	default:
		return "", "", false
	}
	return function, strings.Trim(text[open+1:len(text)-1], `"`), true
}

// scalar returns what a subquery in a select list contributes to one row:
// the aggregate it selects, or -- when the column was written with the
// exists operator -- whether it matched anything at all.
func (db *evalDB) scalar(sub *query.Builder, text string, scopes []scope) (any, error) {
	rows, err := db.evaluate(sub, scopes)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(text), "exists") {
		return len(rows) > 0, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("evalDB: a subquery in a select list answered %d rows", len(rows))
	}
	for _, value := range rows[0] {
		return value, nil
	}
	return nil, nil
}

// matches reports whether one row satisfies a list of where clauses.
//
// Only the clause types these tests build are understood, and anything else is
// an error rather than a row that quietly passes: a filter this cannot read is a
// filter it must not silently drop.
func (db *evalDB) matches(wheres []query.Where, scopes []scope) (bool, error) {
	result := true
	for i, where := range wheres {
		got, err := db.matchOne(where, scopes)
		if err != nil {
			return false, err
		}
		switch {
		case i == 0:
			result = got
		case strings.Contains(where.Boolean, "or"):
			result = result || got
		default:
			result = result && got
		}
	}
	return result, nil
}

func (db *evalDB) matchOne(where query.Where, scopes []scope) (bool, error) {
	switch where.Type {
	case "Basic":
		left, err := db.columnOperand(where.Column, scopes)
		if err != nil {
			return false, err
		}
		right, err := db.valueOperand(where.Value, scopes)
		if err != nil {
			return false, err
		}
		return compare(left, where.Operator, right)

	case "Column":
		left, err := lookup(renderSQL(where.First), scopes)
		if err != nil {
			return false, err
		}
		right, err := lookup(renderSQL(where.Second), scopes)
		if err != nil {
			return false, err
		}
		return compare(left, where.Operator, right)

	case "Exists":
		if where.Query == nil {
			return false, fmt.Errorf("evalDB: an exists clause with no subquery")
		}
		rows, err := db.evaluate(where.Query, scopes)
		if err != nil {
			return false, err
		}
		return (len(rows) > 0) != where.Not, nil

	case "Nested":
		if where.Query == nil {
			return false, fmt.Errorf("evalDB: a nested clause with no group")
		}
		got, err := db.matches(where.Query.Wheres, scopes)
		if err != nil {
			return false, err
		}
		return got != where.Not, nil

	default:
		return false, fmt.Errorf("evalDB: no rule for a %q where clause", where.Type)
	}
}

// columnOperand reads the left side of a comparison: a subquery handle is
// evaluated, and anything else names a column of a row in scope.
func (db *evalDB) columnOperand(column any, scopes []scope) (any, error) {
	if value, ok, err := db.subqueryOperand(column, scopes); ok || err != nil {
		return value, err
	}
	return lookup(renderSQL(column), scopes)
}

// valueOperand reads the right side: a subquery handle is evaluated, and
// anything else is the value itself. It is not looked up as a column, which is
// the whole difference from the left side -- the tenant is bound as the string
// "acme", and a tenant read as a column name matches every row.
func (db *evalDB) valueOperand(value any, scopes []scope) (any, error) {
	if resolved, ok, err := db.subqueryOperand(value, scopes); ok || err != nil {
		return resolved, err
	}
	if query.IsExpression(value) {
		return renderSQL(value), nil
	}
	return value, nil
}

// subqueryOperand returns the value of an operand that is a compiled
// subquery, and reports whether it was one.
func (db *evalDB) subqueryOperand(operand any, scopes []scope) (any, bool, error) {
	if !query.IsExpression(operand) {
		return nil, false, nil
	}
	text := renderSQL(operand)
	sub, ok := db.resolve(text)
	if !ok {
		return nil, false, nil
	}
	value, err := db.scalar(sub, text, scopes)
	return value, true, err
}

// aggregate returns count, sum, min, max and avg over the matched rows.
func (db *evalDB) aggregate(function, column string, rows []map[string]any) (any, error) {
	switch strings.ToLower(function) {
	case "count":
		return int64(len(rows)), nil
	case "sum", "min", "max", "avg":
		total, low, high := 0.0, 0.0, 0.0
		for i, row := range rows {
			value, ok := castNumber(row[column])
			if !ok {
				return nil, fmt.Errorf("evalDB: %s over %q found %v, which is not a number", function, column, row[column])
			}
			total += value
			if i == 0 || value < low {
				low = value
			}
			if i == 0 || value > high {
				high = value
			}
		}
		switch strings.ToLower(function) {
		case "sum":
			return total, nil
		case "min":
			return low, nil
		case "max":
			return high, nil
		default:
			if len(rows) == 0 {
				return nil, nil
			}
			return total / float64(len(rows)), nil
		}
	}
	return nil, fmt.Errorf("evalDB: no rule for the aggregate %q", function)
}

// lookup reads a column off the innermost scope that owns it.
func lookup(name string, scopes []scope) (any, error) {
	table, column := "", name
	if i := strings.LastIndex(name, "."); i >= 0 {
		table, column = name[:i], name[i+1:]
	}
	for _, s := range scopes {
		if table != "" && s.table != table {
			continue
		}
		if value, ok := s.row[column]; ok {
			return value, nil
		}
	}
	if table != "" {
		return nil, fmt.Errorf("evalDB: no column %q in any table in scope", name)
	}
	return nil, nil
}

func compare(left any, operator string, right any) (bool, error) {
	leftNumber, leftIsNumber := castNumber(left)
	rightNumber, rightIsNumber := castNumber(right)
	if leftIsNumber && rightIsNumber {
		switch operator {
		case "=", "":
			return leftNumber == rightNumber, nil
		case "!=", "<>":
			return leftNumber != rightNumber, nil
		case ">":
			return leftNumber > rightNumber, nil
		case ">=":
			return leftNumber >= rightNumber, nil
		case "<":
			return leftNumber < rightNumber, nil
		case "<=":
			return leftNumber <= rightNumber, nil
		}
		return false, fmt.Errorf("evalDB: no rule for the operator %q", operator)
	}

	switch operator {
	case "=", "":
		return fmt.Sprint(left) == fmt.Sprint(right), nil
	case "!=", "<>":
		return fmt.Sprint(left) != fmt.Sprint(right), nil
	}
	return false, fmt.Errorf("evalDB: no rule for %v %s %v", left, operator, right)
}

// castNumber reads a number however the clause carried it. A number compiled
// into the statement rather than bound arrives as its text -- which is how the
// count comparison used to write the number it compares against -- and reading
// it as a number is what makes a test of that shape fail on the count it got
// rather than on a comparison this could not make.
func castNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	}
	return 0, false
}

func copyRecord(row map[string]any) query.Record {
	out := query.Record{}
	for key, value := range row {
		out[key] = value
	}
	return out
}

// renderSQL renders a column, expression or table however it was written.
func renderSQL(value any) string {
	if expression, ok := value.(query.Expression); ok {
		return fmt.Sprint(expression.Value())
	}
	return fmt.Sprint(value)
}

// aliasOf reads the name a select item was given: `({{q3}}) as "posts_count"`.
func aliasOf(text string) (string, error) {
	i := strings.LastIndex(text, " as ")
	if i < 0 {
		return "", fmt.Errorf("evalDB: the select item %q carries no alias", text)
	}
	return strings.Trim(strings.TrimSpace(text[i+4:]), `"`), nil
}

// evalGrammar is testGrammar with one method replaced: a select compiles to the
// handle of the query it was compiled from. See the file comment.
type evalGrammar struct {
	*testGrammar
	db *evalDB
}

func (g *evalGrammar) CompileSelect(q *query.Builder) string { return g.db.register(q) }

func (g *evalGrammar) SetTablePrefix(prefix string) query.Grammar {
	g.testGrammar.SetTablePrefix(prefix)
	return g
}

// evalProcessor is the driver hook, with nothing to do.
type evalProcessor struct{}

func (evalProcessor) ProcessSelect(q *query.Builder, results []query.Record) []query.Record {
	return results
}

func (evalProcessor) ProcessInsertGetID(ctx context.Context, q *query.Builder, sql string, values []any, sequence string) (int64, error) {
	return 0, nil
}

// twoTenants builds the model over a users table, a posts table and an orders
// table, each holding rows of acme and of globex.
//
// The numbers are chosen so that an unscoped subquery cannot answer by accident:
// globex has more posts than acme on the same user id, and its order totals are
// an order of magnitude larger.
func twoTenants(t *testing.T) (*Model[user], *evalDB) {
	t.Helper()

	db := newEvalDB()
	grammar := &evalGrammar{testGrammar: newTestGrammar(), db: db}
	model := NewModel[user]("users", db, grammar, evalProcessor{})
	model.RelationResolvers = map[string]func(*Model[user]) Relation{
		"posts": func(*Model[user]) Relation {
			return &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"}
		},
		"orders": func(*Model[user]) Relation {
			return &fakeRelation{table: "orders", foreign: "orders.user_id", local: "users.id"}
		},
	}

	// One user per tenant, both with id 1, because a correlated subquery joins
	// on that id and nothing else: if the tenant is missing from the subquery,
	// the two tenants' children fall into the same bucket.
	db.seed("users", map[string]any{"id": int64(1), "name": "Ada", "tenant_id": "acme"})
	db.seed("users", map[string]any{"id": int64(1), "name": "Grace", "tenant_id": "globex"})
	db.seed("users", map[string]any{"id": int64(2), "name": "Alan", "tenant_id": "acme"})

	// acme's user 1 has two posts, globex's has three. acme's user 2 has none,
	// and globex has none for id 2 either.
	db.seed("posts", map[string]any{"id": int64(10), "user_id": int64(1), "tenant_id": "acme", "published": true})
	db.seed("posts", map[string]any{"id": int64(11), "user_id": int64(1), "tenant_id": "acme", "published": false})
	db.seed("posts", map[string]any{"id": int64(12), "user_id": int64(1), "tenant_id": "globex", "published": true})
	db.seed("posts", map[string]any{"id": int64(13), "user_id": int64(1), "tenant_id": "globex", "published": true})
	db.seed("posts", map[string]any{"id": int64(14), "user_id": int64(1), "tenant_id": "globex", "published": true})

	db.seed("orders", map[string]any{"id": int64(20), "user_id": int64(1), "tenant_id": "acme", "total": int64(30)})
	db.seed("orders", map[string]any{"id": int64(21), "user_id": int64(1), "tenant_id": "acme", "total": int64(12)})
	db.seed("orders", map[string]any{"id": int64(22), "user_id": int64(1), "tenant_id": "globex", "total": int64(500)})
	// Below acme's smallest, so that a min over both tenants is not acme's min
	// either -- every aggregate has a globex row that changes its answer.
	db.seed("orders", map[string]any{"id": int64(23), "user_id": int64(1), "tenant_id": "globex", "total": int64(5)})

	return model, db
}

func acme() auth.Grant { return auth.SystemGrant("user.list", "acme") }

// TestWithCountCountsOnlyTheGrantsTenant is the audit's first finding.
//
// `Users.WithCount("posts").Get(gA)` came back with posts_count over every
// tenant's posts, because withAggregate froze the subquery into a raw column and
// nothing scoped it: acme's user 1 was told it had 5 posts, 3 of which belong to
// globex.
func TestWithCountCountsOnlyTheGrantsTenant(t *testing.T) {
	model, _ := twoTenants(t)

	models, err := model.NewQuery().WithCount("posts").Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Get returned %d users, want acme's two", len(models))
	}

	byName := map[string]any{}
	for _, m := range models {
		byName[m.Entity.Name] = m.GetAttribute("posts_count")
	}
	if got := byName["Ada"]; got != int64(2) {
		t.Errorf("posts_count for acme's user 1 = %v, want 2 -- globex has three posts on the same user id", got)
	}
	if got := byName["Alan"]; got != int64(0) {
		t.Errorf("posts_count for acme's user 2 = %v, want 0", got)
	}
}

// TestWithSumSumsOnlyTheGrantsTenant is the audit's second finding: a scalar of
// another customer's revenue. acme's user 1 has 30 and 12; globex's has 500 and
// 5 on the same user id.
func TestWithSumSumsOnlyTheGrantsTenant(t *testing.T) {
	model, _ := twoTenants(t)

	models, err := model.NewQuery().WithSum("orders", "total").Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := models[0].GetAttribute("orders_sum_total"); got != 42.0 {
		t.Errorf("orders_sum_total = %v, want 42 -- 547 is acme reading globex's revenue", got)
	}
}

// TestWithAggregateScopesEveryFunction runs the general form the four named
// helpers go through, so a function nobody wrote a test for is covered too.
func TestWithAggregateScopesEveryFunction(t *testing.T) {
	for _, tc := range []struct {
		function string
		alias    string
		want     any
	}{
		{function: "max", alias: "orders_max_total", want: 30.0},
		{function: "min", alias: "orders_min_total", want: 12.0},
		{function: "avg", alias: "orders_avg_total", want: 21.0},
	} {
		t.Run(tc.function, func(t *testing.T) {
			model, _ := twoTenants(t)

			models, err := model.NewQuery().WithAggregate([]string{"orders"}, "total", tc.function).Get(context.Background(), acme())
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got := models[0].GetAttribute(tc.alias); got != tc.want {
				t.Errorf("%s = %v, want %v -- globex's orders of 500 and 5 are not acme's", tc.alias, got, tc.want)
			}
		})
	}
}

// TestWithExistsAsksOnlyAboutTheGrantsTenant covers the one aggregate written
// with its own operator. acme's user 2 has no posts; globex has none for that id
// either, so what the unscoped column answered was still "no" -- the row that
// tells the two apart is user 1, whose globex posts must not make an acme user
// look like it has any beyond its own.
func TestWithExistsAsksOnlyAboutTheGrantsTenant(t *testing.T) {
	model, db := twoTenants(t)
	// A post that belongs to globex only, on a user id acme has and has no
	// posts for.
	db.seed("posts", map[string]any{"id": int64(15), "user_id": int64(2), "tenant_id": "globex", "published": true})

	models, err := model.NewQuery().WithExists("posts").Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	byName := map[string]bool{}
	for _, m := range models {
		value, _ := m.GetAttribute("posts_exists").(bool)
		byName[m.Entity.Name] = value
	}
	if !byName["Ada"] {
		t.Errorf("posts_exists for acme's user 1 = false, want true -- it has two posts of its own")
	}
	if byName["Alan"] {
		t.Errorf("posts_exists for acme's user 2 = true, and the only post on that user id belongs to globex")
	}
}

// TestWhereHasFiltersByTheGrantsTenantsRows is the audit's third finding: the
// list of one tenant filtered by the existence of another tenant's rows, which
// tells the caller which rows the other tenant has.
func TestWhereHasFiltersByTheGrantsTenantsRows(t *testing.T) {
	model, db := twoTenants(t)
	db.seed("posts", map[string]any{"id": int64(15), "user_id": int64(2), "tenant_id": "globex", "published": true})

	models, err := model.NewQuery().WhereHas("posts", nil).Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 1 || models[0].Entity.Name != "Ada" {
		names := make([]string, 0, len(models))
		for _, m := range models {
			names = append(names, m.Entity.Name)
		}
		t.Fatalf("whereHas returned %v, want only Ada -- Alan's only post belongs to globex", names)
	}
}

// TestWhereHasWithAConstraintScopesTheConstrainedSubquery keeps the caller's own
// filter and the tenant filter on the same subquery. acme has one published post
// on user 1; globex has three.
func TestWhereHasWithAConstraintScopesTheConstrainedSubquery(t *testing.T) {
	model, _ := twoTenants(t)

	models, err := model.NewQuery().WhereHasCount("posts", func(sub *query.Builder) {
		sub.Where("published", "=", true)
	}, ">=", 2).Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("has(published) >= 2 returned %d users, want none -- acme has one published post, globex has three", len(models))
	}
}

// TestHasWithACountComparesOnlyTheGrantsTenantsRows is the count comparison,
// which took the other route through the builder: not an exists but a subquery
// on the left of an operator, frozen into a raw column. acme's user 1 has two
// posts, and every tenant together has five.
func TestHasWithACountComparesOnlyTheGrantsTenantsRows(t *testing.T) {
	model, _ := twoTenants(t)

	models, err := model.NewQuery().Has("posts", ">", 2, "and", nil).Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("has > 2 returned %d users, want none -- only globex has more than two posts on that user id", len(models))
	}

	models, err = model.NewQuery().Has("posts", ">", 1, "and", nil).Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 1 || models[0].Entity.Name != "Ada" {
		t.Fatalf("has > 1 returned %d users, want acme's user 1, which has exactly two posts of its own", len(models))
	}
}

// TestWhereDoesntHaveAsksAboutTheGrantsTenantsRows is the negated form, and it
// is the one that fails in the direction that hides rows: an unscoped not-exists
// drops a row because ANOTHER tenant has a child for it.
func TestWhereDoesntHaveAsksAboutTheGrantsTenantsRows(t *testing.T) {
	model, db := twoTenants(t)
	db.seed("posts", map[string]any{"id": int64(15), "user_id": int64(2), "tenant_id": "globex", "published": true})

	models, err := model.NewQuery().WhereDoesntHave("posts", nil).Get(context.Background(), acme())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 1 || models[0].Entity.Name != "Alan" {
		names := make([]string, 0, len(models))
		for _, m := range models {
			names = append(names, m.Entity.Name)
		}
		t.Fatalf("whereDoesntHave returned %v, want Alan -- the post on its id belongs to globex", names)
	}
}

// TestEveryRelationSubqueryNamesTheTenantColumn is the cheap check the audit
// warned is not enough on its own, kept for what it adds: it names the column,
// so a failure says which clause lost it rather than only that a number was
// wrong.
func TestEveryRelationSubqueryNamesTheTenantColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*Builder[user]) *Builder[user]
	}{
		{name: "WithCount", build: func(b *Builder[user]) *Builder[user] { return b.WithCount("posts") }},
		{name: "WithSum", build: func(b *Builder[user]) *Builder[user] { return b.WithSum("orders", "total") }},
		{name: "WithExists", build: func(b *Builder[user]) *Builder[user] { return b.WithExists("posts") }},
		{name: "WhereHas", build: func(b *Builder[user]) *Builder[user] { return b.WhereHas("posts", nil) }},
		{name: "Has", build: func(b *Builder[user]) *Builder[user] { return b.Has("posts", ">", 3, "and", nil) }},
		{name: "WhereDoesntHave", build: func(b *Builder[user]) *Builder[user] { return b.WhereDoesntHave("posts", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, conn := newUserModel()
			withPostsAndOrders(model)
			conn.queue()

			if _, err := tc.build(model.NewQuery()).Get(context.Background(), acme()); err != nil {
				t.Fatalf("Get: %v", err)
			}

			sql := conn.last().SQL
			inner := `"posts"."tenant_id" = ?`
			if strings.Contains(tc.name, "Sum") {
				inner = `"orders"."tenant_id" = ?`
			}
			if !strings.Contains(sql, inner) {
				t.Errorf("SQL = %q, want the relation subquery filtered by %s", sql, inner)
			}
			if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
				t.Errorf("SQL = %q, want the outer query filtered too", sql)
			}
			if got, want := strings.Count(sql, "?"), len(conn.last().Bindings); got != want {
				t.Errorf("SQL = %q has %d placeholders for %d bindings %v", sql, got, want, conn.last().Bindings)
			}
		})
	}
}

func withPostsAndOrders(model *Model[user]) *Model[user] {
	model.RelationResolvers = map[string]func(*Model[user]) Relation{
		"posts": func(*Model[user]) Relation {
			return &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"}
		},
		"orders": func(*Model[user]) Relation {
			return &fakeRelation{table: "orders", foreign: "orders.user_id", local: "users.id"}
		},
	}
	return model
}

// A relation subquery is scoped once per statement, however many statements a
// builder issues. Chunk builds one per page off the same query, and a filter
// that accumulated would make each page's SQL longer than the last -- which is
// the shape a leak fix takes when it writes onto the builder the caller kept
// instead of onto the statement.
func TestAChunkedWalkScopesEachPageOnceAndAnswersTheSame(t *testing.T) {
	model, _ := twoTenants(t)

	pages := 0
	seen := map[string]any{}
	err := model.NewQuery().
		Where(func(group *Builder[user]) { group.Where("name", "!=", "") }).
		WithCount("posts").
		Chunk(context.Background(), acme(), 1, func(models Collection[user], page int) (bool, error) {
			pages++
			for _, m := range models {
				seen[m.Entity.Name] = m.GetAttribute("posts_count")
			}
			return true, nil
		})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if pages != 2 {
		t.Fatalf("the walk took %d pages over acme's two users", pages)
	}
	if got := seen["Ada"]; got != int64(2) {
		t.Errorf("posts_count for Ada on a chunked walk = %v, want 2", got)
	}
	if got := seen["Alan"]; got != int64(0) {
		t.Errorf("posts_count for Alan on a chunked walk = %v, want 0", got)
	}
}
