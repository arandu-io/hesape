package eloquent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/database/query"
)

// testGrammar is a grammar good enough to read the compiled SQL back in a test.
//
// It exists because query/grammars is empty while this is being written, and
// because a test that asserts against a fake compiler is asserting against a
// fake. What it compiles is the subset these tests use, in the shape a real
// grammar compiles it -- including the two things the eloquent side depends
// on: the sorted column order of an insert, and a SET column stripped of its
// table, which is what the Postgres and SQLite grammars do.
type testGrammar struct {
	query.BaseGrammar
}

func newTestGrammar() *testGrammar { return &testGrammar{} }

func (g *testGrammar) CompileSelect(q *query.Builder) string {
	var b strings.Builder
	b.WriteString("select ")
	if aggregate := q.GetAggregate(); aggregate != nil {
		b.WriteString(aggregate.Function + "(" + g.Columnize(aggregate.Columns) + ") as aggregate")
	} else if len(q.Columns) == 0 {
		b.WriteString("*")
	} else {
		b.WriteString(g.Columnize(q.Columns))
	}
	b.WriteString(" from " + g.WrapTable(q.GetFrom()))

	if where := g.compileWheres(q); where != "" {
		b.WriteString(" where " + where)
	}
	if len(q.Orders) > 0 {
		parts := make([]string, 0, len(q.Orders))
		for _, order := range q.Orders {
			if order.SQL != nil {
				parts = append(parts, fmt.Sprint(order.SQL))
				continue
			}
			parts = append(parts, g.Wrap(order.Column)+" "+order.Direction)
		}
		b.WriteString(" order by " + strings.Join(parts, ", "))
	}
	if limit := q.GetLimit(); limit != nil {
		b.WriteString(fmt.Sprintf(" limit %d", *limit))
	}
	if offset := q.GetOffset(); offset != nil {
		b.WriteString(fmt.Sprintf(" offset %d", *offset))
	}
	return b.String()
}

func (g *testGrammar) compileWheres(q *query.Builder) string {
	var parts []string
	for i, where := range q.Wheres {
		fragment := g.compileWhere(where)
		if fragment == "" {
			continue
		}
		if i == 0 {
			parts = append(parts, fragment)
			continue
		}
		boolean := where.Boolean
		if boolean == "" {
			boolean = "and"
		}
		parts = append(parts, boolean+" "+fragment)
	}
	return strings.Join(parts, " ")
}

func (g *testGrammar) compileWhere(where query.Where) string {
	switch where.Type {
	case "Basic":
		return g.Wrap(where.Column) + " " + where.Operator + " " + g.Parameter(where.Value)
	case "Column":
		return g.Wrap(where.First) + " " + where.Operator + " " + g.Wrap(where.Second)
	case "Raw":
		return fmt.Sprint(where.SQL)
	case "Null":
		if where.Not {
			return g.Wrap(where.Column) + " is not null"
		}
		return g.Wrap(where.Column) + " is null"
	case "In":
		if len(where.Values) == 0 {
			if where.Not {
				return "1 = 1"
			}
			return "0 = 1"
		}
		operator := " in ("
		if where.Not {
			operator = " not in ("
		}
		return g.Wrap(where.Column) + operator + g.Parameterize(where.Values) + ")"
	case "Between":
		operator := " between "
		if where.Not {
			operator = " not between "
		}
		return g.Wrap(where.Column) + operator + g.Parameter(where.Values[0]) + " and " + g.Parameter(where.Values[1])
	case "Nested":
		return "(" + g.compileWheres(where.Query) + ")"
	case "Exists":
		if where.Not {
			return "not exists (" + g.CompileSelect(where.Query) + ")"
		}
		return "exists (" + g.CompileSelect(where.Query) + ")"
	}
	return ""
}

func (g *testGrammar) CompileInsert(q *query.Builder, values []map[string]any) string {
	if len(values) == 0 {
		return "insert into " + g.WrapTable(q.GetFrom()) + " default values"
	}
	columns := sortedKeys(values[0])
	wrapped := make([]any, len(columns))
	for i, column := range columns {
		wrapped[i] = column
	}
	rows := make([]string, 0, len(values))
	for _, row := range values {
		placeholders := make([]any, 0, len(columns))
		for _, column := range columns {
			placeholders = append(placeholders, row[column])
		}
		rows = append(rows, "("+g.Parameterize(placeholders)+")")
	}
	return "insert into " + g.WrapTable(q.GetFrom()) + " (" + g.Columnize(wrapped) + ") values " + strings.Join(rows, ", ")
}

func (g *testGrammar) CompileInsertOrIgnore(q *query.Builder, values []map[string]any) string {
	return strings.Replace(g.CompileInsert(q, values), "insert into", "insert or ignore into", 1)
}

func (g *testGrammar) CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string {
	return g.CompileInsert(q, []map[string]any{values})
}

func (g *testGrammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	sets := make([]string, 0, len(values))
	for _, column := range sortedKeys(values) {
		// The table is stripped, as the SQLite and Postgres grammars do.
		name := column
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		sets = append(sets, g.Wrap(name)+" = "+g.Parameter(values[column]))
	}
	out := "update " + g.WrapTable(q.GetFrom()) + " set " + strings.Join(sets, ", ")
	if where := g.compileWheres(q); where != "" {
		out += " where " + where
	}
	return out
}

func (g *testGrammar) CompileUpsert(q *query.Builder, values []map[string]any, uniqueBy, update []string) string {
	out := g.CompileInsert(q, values)
	wrapped := make([]any, len(uniqueBy))
	for i, column := range uniqueBy {
		wrapped[i] = column
	}
	sets := make([]string, 0, len(update))
	for _, column := range update {
		sets = append(sets, g.Wrap(column)+" = excluded."+g.Wrap(column))
	}
	return out + " on conflict (" + g.Columnize(wrapped) + ") do update set " + strings.Join(sets, ", ")
}

func (g *testGrammar) CompileDelete(q *query.Builder) string {
	out := "delete from " + g.WrapTable(q.GetFrom())
	if where := g.compileWheres(q); where != "" {
		out += " where " + where
	}
	return out
}

func (g *testGrammar) CompileTruncate(q *query.Builder) map[string][]any {
	return map[string][]any{"truncate table " + g.WrapTable(q.GetFrom()): nil}
}

func (g *testGrammar) CompileExists(q *query.Builder) string {
	return "select exists(" + g.CompileSelect(q) + ") as " + g.Wrap("exists")
}

func (g *testGrammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0, len(values))
	for _, column := range sortedKeys(values) {
		out = append(out, values[column])
	}
	for _, key := range []string{"join", "from", "where", "groupBy", "having", "order", "union", "unionOrder"} {
		out = append(out, bindings[key]...)
	}
	return out
}

func (g *testGrammar) PrepareBindingsForDelete(bindings map[string][]any) []any {
	out := make([]any, 0)
	for _, key := range []string{"join", "from", "where", "groupBy", "having", "order", "union", "unionOrder"} {
		out = append(out, bindings[key]...)
	}
	return out
}

func (g *testGrammar) SetTablePrefix(prefix string) query.Grammar {
	g.BaseGrammar.SetTablePrefix(prefix)
	return g
}

// statement is one thing the fake connection was asked to run.
type statement struct {
	SQL      string
	Bindings []any
}

// testConnection records what it was asked to run and returns the rows the
// test queued.
type testConnection struct {
	mu         sync.Mutex
	statements []statement
	rows       [][]query.Record
	affected   int64
	lastID     int64
	failInsert error
}

func newTestConnection() *testConnection { return &testConnection{lastID: 1} }

func (c *testConnection) queue(rows ...query.Record) {
	c.rows = append(c.rows, rows)
}

func (c *testConnection) record(sql string, bindings []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, statement{SQL: sql, Bindings: bindings})
}

func (c *testConnection) last() statement {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.statements) == 0 {
		return statement{}
	}
	return c.statements[len(c.statements)-1]
}

func (c *testConnection) sqls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.statements))
	for _, s := range c.statements {
		out = append(out, s.SQL)
	}
	return out
}

func (c *testConnection) Select(sql string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	c.record(sql, bindings)
	if len(c.rows) == 0 {
		return nil, nil
	}
	rows := c.rows[0]
	c.rows = c.rows[1:]
	return rows, nil
}

func (c *testConnection) Insert(sql string, bindings []any) (bool, error) {
	c.record(sql, bindings)
	if c.failInsert != nil {
		return false, c.failInsert
	}
	return true, nil
}

func (c *testConnection) Update(sql string, bindings []any) (int64, error) {
	c.record(sql, bindings)
	return c.affected, nil
}

func (c *testConnection) Delete(sql string, bindings []any) (int64, error) {
	c.record(sql, bindings)
	return c.affected, nil
}

func (c *testConnection) Statement(sql string, bindings []any) (bool, error) {
	c.record(sql, bindings)
	return true, nil
}

// testProcessor implements the two hooks a driver gets.
type testProcessor struct{ conn *testConnection }

func (p *testProcessor) ProcessSelect(q *query.Builder, results []query.Record) []query.Record {
	return results
}

func (p *testProcessor) ProcessInsertGetID(q *query.Builder, sql string, values []any, sequence string) (int64, error) {
	if _, err := p.conn.Insert(sql, values); err != nil {
		return 0, err
	}
	id := p.conn.lastID
	p.conn.lastID++
	return id, nil
}

// user is the entity the tests model. It is deliberately ordinary: exported
// fields with db tags, one unexported field to prove Fill cannot reach it.
type user struct {
	ID        int64      `db:"id"`
	Name      string     `db:"name"`
	Email     string     `db:"email"`
	TenantID  string     `db:"tenant_id"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`

	secret string
}

func newUserModel() (*Model[user], *testConnection) {
	conn := newTestConnection()
	model := NewModel[user]("users", conn, newTestGrammar(), &testProcessor{conn: conn})
	return model, conn
}
