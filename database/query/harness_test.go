package query_test

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/database/query"
)

// The harness the execution tests run against: a connection that records what
// it was asked to run and answers with rows the test queued, and a grammar that
// compiles enough SQL for the statement to be read back and checked.
//
// The grammar is here rather than a stub returning a fixed string because the
// claim under test is about the statement -- that it carries the tenant, and
// that the tenant filter binds the whole where clause rather than its last
// term. A stub cannot answer that question.

type call struct {
	kind     string
	sql      string
	bindings []any
}

type fakeConnection struct {
	// results is queued one result set per select, in order.
	results  [][]query.Record
	calls    []call
	affected int64
	inserted bool
	err      error

	// streamed is what Cursor yields, and cursorErr what it refuses with.
	streamed  []query.Record
	cursorErr error
}

func (c *fakeConnection) Select(sql string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	c.calls = append(c.calls, call{kind: "select", sql: sql, bindings: bindings})
	if c.err != nil {
		return nil, c.err
	}
	if len(c.results) == 0 {
		return nil, nil
	}
	rows := c.results[0]
	c.results = c.results[1:]
	return rows, nil
}

func (c *fakeConnection) Insert(sql string, bindings []any) (bool, error) {
	c.calls = append(c.calls, call{kind: "insert", sql: sql, bindings: bindings})
	return c.inserted, c.err
}

func (c *fakeConnection) Update(sql string, bindings []any) (int64, error) {
	c.calls = append(c.calls, call{kind: "update", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Delete(sql string, bindings []any) (int64, error) {
	c.calls = append(c.calls, call{kind: "delete", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Statement(sql string, bindings []any) (bool, error) {
	c.calls = append(c.calls, call{kind: "statement", sql: sql, bindings: bindings})
	return true, c.err
}

func (c *fakeConnection) AffectingStatement(sql string, bindings []any) (int64, error) {
	c.calls = append(c.calls, call{kind: "affecting", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Cursor(sql string, bindings []any, useReadPDO bool) (func(yield func(query.Record, error) bool), error) {
	c.calls = append(c.calls, call{kind: "cursor", sql: sql, bindings: bindings})
	if c.cursorErr != nil {
		return nil, c.cursorErr
	}
	rows := c.streamed
	return func(yield func(query.Record, error) bool) {
		for _, row := range rows {
			if !yield(row, nil) {
				return
			}
		}
	}, nil
}

func (c *fakeConnection) lastSQL() string {
	if len(c.calls) == 0 {
		return ""
	}
	return c.calls[len(c.calls)-1].sql
}

func (c *fakeConnection) lastBindings() []any {
	if len(c.calls) == 0 {
		return nil
	}
	return c.calls[len(c.calls)-1].bindings
}

// fakeProcessor passes the rows through, which is what the base Processor does.
type fakeProcessor struct {
	insertedID int64
}

func (p *fakeProcessor) ProcessSelect(_ *query.Builder, results []query.Record) []query.Record {
	return results
}

func (p *fakeProcessor) ProcessInsertGetID(_ *query.Builder, sql string, values []any, sequence string) (int64, error) {
	return p.insertedID, nil
}

// fakeGrammar compiles the fragments the execution tests read back. It is the
// SQL standard spelling throughout, which is what BaseGrammar wraps with.
type fakeGrammar struct {
	query.BaseGrammar
}

func (g *fakeGrammar) CompileSelect(q *query.Builder) string {
	if aggregate := q.GetAggregate(); aggregate != nil {
		sql := "select " + aggregate.Function + "(" + g.Columnize(aggregate.Columns) + ") as aggregate from " + g.WrapTable(q.GetFrom())
		return sql + g.compileWheres(q)
	}

	columns := q.Columns
	if columns == nil {
		columns = []any{"*"}
	}
	sql := "select " + g.Columnize(columns) + " from " + g.WrapTable(q.GetFrom())
	sql += g.compileJoins(q)
	sql += g.compileWheres(q)

	if len(q.Orders) > 0 {
		parts := make([]string, 0, len(q.Orders))
		for _, order := range q.Orders {
			if order.SQL != nil {
				parts = append(parts, fmt.Sprint(order.SQL))
				continue
			}
			parts = append(parts, g.Wrap(order.Column)+" "+order.Direction)
		}
		sql += " order by " + strings.Join(parts, ", ")
	}
	if limit := q.GetLimit(); limit != nil {
		sql += fmt.Sprintf(" limit %d", *limit)
	}
	if offset := q.GetOffset(); offset != nil {
		sql += fmt.Sprintf(" offset %d", *offset)
	}
	for _, union := range q.Unions {
		if union.All {
			sql += " union all " + g.CompileSelect(union.Query)
			continue
		}
		sql += " union " + g.CompileSelect(union.Query)
	}
	return sql
}

// compileJoins is here because a join against a subquery compiles that subquery
// whole, and the tests about it read the statement to see whether the subquery
// carries a tenant of its own.
func (g *fakeGrammar) compileJoins(q *query.Builder) string {
	var out strings.Builder
	for _, join := range q.Joins {
		if join.Lateral {
			out.WriteString(" " + join.Type + " join lateral " + g.WrapTable(join.Table) + " on true")
			continue
		}
		out.WriteString(" " + join.Type + " join " + g.WrapTable(join.Table))
		if len(join.Wheres) > 0 {
			out.WriteString(" on " + g.whereClauses(join.Builder))
		}
	}
	return out.String()
}

func (g *fakeGrammar) compileWheres(q *query.Builder) string {
	if len(q.Wheres) == 0 {
		return ""
	}
	return " where " + g.whereClauses(q)
}

func (g *fakeGrammar) whereClauses(q *query.Builder) string {
	var out strings.Builder
	for i, where := range q.Wheres {
		if i > 0 {
			out.WriteString(" " + where.Boolean + " ")
		}
		out.WriteString(g.whereClause(where))
	}
	return out.String()
}

func (g *fakeGrammar) whereClause(where query.Where) string {
	switch where.Type {
	case "Basic":
		return g.Wrap(where.Column) + " " + where.Operator + " " + g.Parameter(where.Value)
	case "Null":
		if where.Not {
			return g.Wrap(where.Column) + " is not null"
		}
		return g.Wrap(where.Column) + " is null"
	case "In":
		if len(where.Values) == 0 {
			return "0 = 1"
		}
		return g.Wrap(where.Column) + " in (" + g.Parameterize(where.Values) + ")"
	case "Between":
		return g.Wrap(where.Column) + " between ? and ?"
	case "Column":
		return g.Wrap(where.First) + " " + where.Operator + " " + g.Wrap(where.Second)
	case "Raw":
		return fmt.Sprint(where.SQL)
	case "Nested":
		return "(" + g.whereClauses(where.Query) + ")"
	case "Exists":
		return "exists (" + g.CompileSelect(where.Query) + ")"
	default:
		return ""
	}
}

func (g *fakeGrammar) CompileInsert(q *query.Builder, values []map[string]any) string {
	if len(values) == 0 {
		return "insert into " + g.WrapTable(q.GetFrom()) + " default values"
	}
	columns := sortedColumns(values[0])
	rows := make([]string, len(values))
	for i, row := range values {
		placeholders := make([]string, len(columns))
		for j := range columns {
			placeholders[j] = g.Parameter(row[columns[j]])
		}
		rows[i] = "(" + strings.Join(placeholders, ", ") + ")"
	}
	wrapped := make([]any, len(columns))
	for i, column := range columns {
		wrapped[i] = column
	}
	return "insert into " + g.WrapTable(q.GetFrom()) + " (" + g.Columnize(wrapped) + ") values " + strings.Join(rows, ", ")
}

func (g *fakeGrammar) CompileInsertOrIgnore(q *query.Builder, values []map[string]any) string {
	return strings.Replace(g.CompileInsert(q, values), "insert into", "insert or ignore into", 1)
}

func (g *fakeGrammar) CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string {
	return g.CompileInsert(q, []map[string]any{values})
}

func (g *fakeGrammar) CompileInsertUsing(q *query.Builder, columns []any, sql string) string {
	return "insert into " + g.WrapTable(q.GetFrom()) + " (" + g.Columnize(columns) + ") " + sql
}

func (g *fakeGrammar) CompileInsertOrIgnoreUsing(q *query.Builder, columns []any, sql string) string {
	return strings.Replace(g.CompileInsertUsing(q, columns, sql), "insert into", "insert or ignore into", 1)
}

func (g *fakeGrammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	sets := make([]string, 0, len(values))
	for _, column := range sortedColumns(values) {
		sets = append(sets, g.Wrap(column)+" = "+g.Parameter(values[column]))
	}
	return "update " + g.WrapTable(q.GetFrom()) + " set " + strings.Join(sets, ", ") + g.compileWheres(q)
}

func (g *fakeGrammar) CompileUpsert(q *query.Builder, values []map[string]any, uniqueBy []string, update []string) string {
	return g.CompileInsert(q, values) + " on conflict (" + strings.Join(uniqueBy, ", ") + ") do update set " + strings.Join(update, ", ")
}

func (g *fakeGrammar) CompileDelete(q *query.Builder) string {
	return "delete from " + g.WrapTable(q.GetFrom()) + g.compileWheres(q)
}

func (g *fakeGrammar) CompileTruncate(q *query.Builder) map[string][]any {
	return map[string][]any{"truncate table " + g.WrapTable(q.GetFrom()): nil}
}

func (g *fakeGrammar) CompileExists(q *query.Builder) string {
	return "select exists(" + g.CompileSelect(q) + ") as " + g.Wrap("exists")
}

func (g *fakeGrammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0)
	out = append(out, bindings["join"]...)
	for _, column := range sortedColumns(values) {
		if nested, ok := values[column].([]any); ok {
			out = append(out, nested...)
			continue
		}
		out = append(out, values[column])
	}
	for _, key := range []string{"from", "where", "groupBy", "having", "order", "union", "unionOrder"} {
		out = append(out, bindings[key]...)
	}
	return out
}

func (g *fakeGrammar) PrepareBindingsForDelete(bindings map[string][]any) []any {
	out := make([]any, 0)
	for _, key := range []string{"from", "join", "where", "groupBy", "having", "order", "union", "unionOrder"} {
		out = append(out, bindings[key]...)
	}
	return out
}

// Escape is what ToRawSQL needs and BaseGrammar refuses to do, so the fake one
// quotes the way SQLite does.
func (g *fakeGrammar) Escape(value any, binary bool) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case int:
		return fmt.Sprint(v), nil
	case int64:
		return fmt.Sprint(v), nil
	case float64:
		return fmt.Sprint(v), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(v), "'", "''") + "'", nil
	}
}

func (g *fakeGrammar) SetTablePrefix(prefix string) query.Grammar {
	g.BaseGrammar.SetTablePrefix(prefix)
	return g
}

func sortedColumns(row map[string]any) []string {
	columns := make([]string, 0, len(row))
	for column := range row {
		columns = append(columns, column)
	}
	for i := 1; i < len(columns); i++ {
		for j := i; j > 0 && columns[j] < columns[j-1]; j-- {
			columns[j], columns[j-1] = columns[j-1], columns[j]
		}
	}
	return columns
}

// newTestBuilder is the builder every test starts from: a table, a recording
// connection, the grammar above and a pass-through processor.
func newTestBuilder(connection *fakeConnection) *query.Builder {
	return query.NewBuilder(connection, &fakeGrammar{}, &fakeProcessor{insertedID: 7}).From("users")
}
