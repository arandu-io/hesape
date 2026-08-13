package relations_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/database/query"
)

// The fakes below are a database small enough to reason about and honest about
// the one thing these tests measure: every statement a relation asks for is
// appended to a log, so a test can assert the COUNT and not only the values.
//
// They filter rows for real -- basic equality, in, not null, and one join --
// because a fake that returned everything would pass a tenant test that a real
// database would fail.

// db is the whole database: table name to rows.
type db struct {
	tables map[string][]map[string]any

	// log is every statement that reached the connection, in order. The eager
	// loading tests are assertions about its length.
	log []string
}

func newDB() *db { return &db{tables: map[string][]map[string]any{}} }

func (d *db) seed(table string, rows ...map[string]any) {
	d.tables[table] = append(d.tables[table], rows...)
}

func (d *db) count() int { return len(d.log) }

// model is a model with attributes, relations and nothing else.
type model struct {
	table      string
	keyName    string
	keyType    string
	morphClass string
	relations  map[string]any
	attributes map[string]any
	declared   []string
	exists     bool
	tenant     string
	database   *db
	saved      bool
}

func newModel(database *db, table, morphClass string, attributes map[string]any) *model {
	m := &model{
		table:      table,
		keyName:    "id",
		keyType:    "string",
		morphClass: morphClass,
		relations:  map[string]any{},
		attributes: map[string]any{},
		database:   database,
		tenant:     "tenant_id",
	}
	for key, value := range attributes {
		m.attributes[key] = value
	}
	m.exists = len(attributes) > 0
	return m
}

func (m *model) GetTable() string   { return m.table }
func (m *model) SetTable(t string)  { m.table = t }
func (m *model) GetKeyName() string { return m.keyName }
func (m *model) GetKeyType() string { return m.keyType }
func (m *model) GetKey() any        { return m.attributes[m.keyName] }
func (m *model) GetForeignKey() string {
	return m.morphClass + "_" + m.keyName
}
func (m *model) GetMorphClass() string { return m.morphClass }
func (m *model) Exists() bool          { return m.exists }
func (m *model) GetTenantColumn() string {
	return m.tenant
}

func (m *model) QualifyColumn(column string) string {
	if strings.Contains(column, ".") {
		return column
	}
	return m.table + "." + column
}

func (m *model) GetAttribute(key string) any        { return m.attributes[key] }
func (m *model) SetAttribute(key string, value any) { m.attributes[key] = value }
func (m *model) GetAttributes() map[string]any      { return m.attributes }
func (m *model) UnsetAttribute(key string)          { delete(m.attributes, key) }

func (m *model) SetRawAttributes(attributes map[string]any, sync bool) {
	m.attributes = map[string]any{}
	for key, value := range attributes {
		m.attributes[key] = value
	}
}

func (m *model) GetRelation(relation string) (any, bool) {
	value, ok := m.relations[relation]
	return value, ok
}
func (m *model) SetRelation(relation string, value any) { m.relations[relation] = value }
func (m *model) RelationLoaded(relation string) bool {
	_, ok := m.relations[relation]
	return ok
}
func (m *model) UnsetRelation(relation string) { delete(m.relations, relation) }

func (m *model) IsRelation(key string) bool {
	for _, declared := range m.declared {
		if declared == key {
			return true
		}
	}
	return false
}

func (m *model) Touches(string) bool { return false }
func (m *model) Touch(context.Context, auth.Grant) error {
	return nil
}

func (m *model) NewInstance(attributes map[string]any) relations.Model {
	instance := newModel(m.database, m.table, m.morphClass, attributes)
	instance.declared = m.declared
	instance.exists = false
	return instance
}

func (m *model) Fill(attributes map[string]any) { m.ForceFill(attributes) }
func (m *model) ForceFill(attributes map[string]any) {
	for key, value := range attributes {
		m.attributes[key] = value
	}
}
func (m *model) WasRecentlyCreated() bool                  { return m.saved }
func (m *model) WithoutEvents(callback func() error) error { return callback() }
func (m *model) UsesTimestamps() bool                      { return true }
func (m *model) GetCreatedAtColumn() string                { return "created_at" }
func (m *model) GetUpdatedAtColumn() string                { return "updated_at" }
func (m *model) FreshTimestamp() time.Time                 { return time.Unix(0, 0).UTC() }

func (m *model) NewQuery() relations.Builder {
	return newBuilder(m.database, m)
}

func (m *model) Save(ctx context.Context, g auth.Grant) error {
	row := map[string]any{}
	for key, value := range m.attributes {
		row[key] = value
	}
	if _, ok := row[m.tenant]; !ok && m.tenant != "" {
		row[m.tenant] = auth.Tenant(g)
	}
	m.database.log = append(m.database.log, "insert into "+m.table)
	m.database.tables[m.table] = append(m.database.tables[m.table], row)
	m.exists, m.saved = true, true
	return nil
}

func (m *model) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	m.database.log = append(m.database.log, "delete from "+m.table)
	return 1, nil
}

// builder is the Eloquent builder, narrowed to what a relation calls, over the
// base query builder this collection already has.
type builder struct {
	database *db
	model    *model
	base     *query.Builder
}

func newBuilder(database *db, m *model) *builder {
	b := &builder{database: database, model: m, base: query.NewBuilder(&connection{database: database}, &grammar{}, nil)}
	b.base.From(m.GetTable())
	return b
}

func (b *builder) GetModel() relations.Model { return b.model }
func (b *builder) GetQuery() *query.Builder  { return b.base }

func (b *builder) Select(columns ...any) relations.Builder {
	b.base.Select(columns...)
	return b
}

func (b *builder) AddSelect(columns ...any) relations.Builder {
	b.base.AddSelect(columns...)
	return b
}

func (b *builder) SelectRaw(expression string, bindings ...any) relations.Builder {
	b.base.SelectRaw(expression, bindings...)
	return b
}

func (b *builder) Where(column any, args ...any) relations.Builder {
	b.base.Where(column, args...)
	return b
}

func (b *builder) WhereIn(column any, values []any) relations.Builder {
	b.base.WhereIn(column, values)
	return b
}

func (b *builder) WhereNotNull(columns ...any) relations.Builder {
	b.base.WhereNotNull(columns...)
	return b
}

func (b *builder) WhereColumn(first any, args ...any) relations.Builder {
	b.base.WhereColumn(first, args...)
	return b
}

func (b *builder) WhereKey(ids ...any) relations.Builder {
	b.base.WhereIn(b.model.QualifyColumn(b.model.GetKeyName()), ids)
	return b
}

func (b *builder) Join(table any, first any, args ...any) relations.Builder {
	b.base.Join(table, first, args...)
	return b
}

func (b *builder) GroupBy(groups ...any) relations.Builder {
	b.base.GroupBy(groups...)
	return b
}

func (b *builder) OrderBy(column any, direction ...string) relations.Builder {
	b.base.OrderBy(column, direction...)
	return b
}

func (b *builder) Limit(value int) relations.Builder {
	b.base.Limit(value)
	return b
}

func (b *builder) Clone() relations.Builder {
	return &builder{database: b.database, model: b.model, base: b.base.Clone()}
}

func (b *builder) Get(ctx context.Context, g auth.Grant) ([]relations.Model, error) {
	rows := b.run()

	models := make([]relations.Model, 0, len(rows))
	for _, row := range rows {
		instance := newModel(b.database, b.model.GetTable(), b.model.GetMorphClass(), row)
		instance.declared = b.model.declared
		instance.exists = true
		models = append(models, instance)
	}
	return models, nil
}

func (b *builder) First(ctx context.Context, g auth.Grant) (relations.Model, error) {
	models, err := b.Get(ctx, g)
	if err != nil || len(models) == 0 {
		return nil, err
	}
	return models[0], nil
}

func (b *builder) Find(ctx context.Context, g auth.Grant, id any) (relations.Model, error) {
	return b.Clone().Where(b.model.QualifyColumn(b.model.GetKeyName()), id).First(ctx, g)
}

func (b *builder) Insert(ctx context.Context, g auth.Grant, values []map[string]any) error {
	table := fmt.Sprint(b.base.GetFrom())
	b.database.log = append(b.database.log, "insert into "+table)
	b.database.tables[table] = append(b.database.tables[table], values...)
	return nil
}

// Upsert is the fake's Builder::upsert. It matches on uniqueBy rather than on a
// real index, which is all a relation test needs: what is under test is which
// columns the relation stamped onto each row, not what the engine does with a
// conflict.
func (b *builder) Upsert(ctx context.Context, g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error) {
	table := fmt.Sprint(b.base.GetFrom())
	b.database.log = append(b.database.log, "upsert into "+table)

	affected := int64(0)
	for _, row := range values {
		existing := b.matchOnUnique(table, row, uniqueBy)
		if existing == nil {
			b.database.tables[table] = append(b.database.tables[table], row)
			affected++
			continue
		}
		columns := update
		if columns == nil {
			for column := range row {
				columns = append(columns, column)
			}
			sort.Strings(columns)
		}
		for _, column := range columns {
			if value, ok := row[column]; ok {
				existing[column] = value
			}
		}
		affected++
	}
	return affected, nil
}

func (b *builder) matchOnUnique(table string, row map[string]any, uniqueBy []string) map[string]any {
	if len(uniqueBy) == 0 {
		return nil
	}
	for _, candidate := range b.database.tables[table] {
		same := true
		for _, column := range uniqueBy {
			if !sameValue(candidate[column], row[column]) {
				same = false
				break
			}
		}
		if same {
			return candidate
		}
	}
	return nil
}

func (b *builder) Update(ctx context.Context, g auth.Grant, values map[string]any) (int64, error) {
	rows := b.run()
	for _, row := range rows {
		for key, value := range values {
			row[key] = value
		}
	}
	b.database.log = append(b.database.log, "update "+fmt.Sprint(b.base.GetFrom()))
	return int64(len(rows)), nil
}

func (b *builder) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	table := fmt.Sprint(b.base.GetFrom())
	keep := make([]map[string]any, 0, len(b.database.tables[table]))
	removed := 0
	for _, row := range b.database.tables[table] {
		if matches(b.base.Wheres, row) {
			removed++
			continue
		}
		keep = append(keep, row)
	}
	b.database.tables[table] = keep
	b.database.log = append(b.database.log, "delete from "+table)
	return int64(removed), nil
}

// run is the select: it logs the statement, joins if asked, filters, and
// applies the column aliases so that `pivot_user_id` comes back the way a real
// engine would return it.
func (b *builder) run() []map[string]any {
	b.base.ApplyBeforeQueryCallbacks()
	b.database.log = append(b.database.log, b.base.ToSQL())

	table := fmt.Sprint(b.base.GetFrom())
	rows := b.database.tables[table]

	for _, join := range b.base.Joins {
		rows = joinRows(b.database, rows, join)
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !matches(b.base.Wheres, row) {
			continue
		}
		out = append(out, project(row, b.base.GetColumns()))
	}
	return out
}

func joinRows(database *db, left []map[string]any, join *query.JoinClause) []map[string]any {
	table := fmt.Sprint(join.Table)

	out := make([]map[string]any, 0, len(left))
	for _, leftRow := range left {
		for _, rightRow := range database.tables[table] {
			if !joinMatches(join.Wheres, leftRow, rightRow, table) {
				continue
			}
			merged := map[string]any{}
			for key, value := range leftRow {
				merged[key] = value
			}
			for key, value := range rightRow {
				merged[table+"."+key] = value
			}
			out = append(out, merged)
		}
	}
	return out
}

func joinMatches(wheres []query.Where, left, right map[string]any, table string) bool {
	for _, where := range wheres {
		if where.Type != "Column" {
			continue
		}
		first := resolve(left, right, table, fmt.Sprint(where.First))
		second := resolve(left, right, table, fmt.Sprint(where.Second))
		if first == nil || second == nil || !sameValue(first, second) {
			return false
		}
	}
	return true
}

// resolve reads a qualified column off whichever side of the join owns it.
func resolve(left, right map[string]any, table, column string) any {
	if strings.HasPrefix(column, table+".") {
		return right[plain(column)]
	}
	if value, ok := left[column]; ok {
		return value
	}
	return left[plain(column)]
}

// matches evaluates the clauses this fake understands. Anything else is treated
// as satisfied, so a test that depends on a clause has to be a clause listed
// here -- an unsupported filter cannot silently pass a leak.
func matches(wheres []query.Where, row map[string]any) bool {
	for _, where := range wheres {
		// A joined row holds the other table's columns under their qualified
		// name, so the qualified lookup is tried first and the plain one second.
		column := fmt.Sprint(where.Column)
		value, present := row[column]
		if !present {
			value, present = row[plain(column)]
		}

		switch where.Type {
		case "Basic":
			if where.Operator != "=" {
				continue
			}
			if !sameValue(value, where.Value) {
				return false
			}
		case "In":
			found := false
			for _, candidate := range where.Values {
				if sameValue(value, candidate) {
					found = true
					break
				}
			}
			if found == where.Not {
				return false
			}
		case "Null":
			isNull := !present || value == nil
			if isNull == where.Not {
				return false
			}
		}
	}
	return true
}

// project applies the select list: a plain column is kept, and `x as y` is
// copied under y, which is how the pivot columns arrive.
func project(row map[string]any, columns []any) map[string]any {
	out := map[string]any{}
	for key, value := range row {
		out[key] = value
	}

	for _, column := range columns {
		text, ok := column.(string)
		if !ok || !strings.Contains(text, " as ") {
			continue
		}
		parts := strings.SplitN(text, " as ", 2)
		source := strings.TrimSpace(parts[0])
		alias := strings.TrimSpace(parts[1])
		if value, ok := row[source]; ok {
			out[alias] = value
			continue
		}
		if value, ok := row[plain(source)]; ok {
			out[alias] = value
		}
	}
	return out
}

func plain(column string) string {
	segments := strings.Split(column, ".")
	return segments[len(segments)-1]
}

func sameValue(left, right any) bool {
	return fmt.Sprint(left) == fmt.Sprint(right)
}

// connection is the query.Connection the base builder runs pivot statements
// through. Only the pivot path uses it; everything else goes through builder.
//
// It sees the SQL and the bindings and not the clauses, which is what a real
// driver sees. So it keeps a row when every binding appears somewhere in it:
// crude, but exact enough for the statements a pivot emits -- and it means the
// tenant binding has to be there for the row to come back, which is the thing
// the pivot tests are about.
type connection struct{ database *db }

func (c *connection) Select(sql string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	c.database.log = append(c.database.log, sql)

	out := make([]query.Record, 0)
	for _, row := range c.database.tables[tableOfSQL(sql)] {
		if carriesAll(row, bindings) {
			out = append(out, row)
		}
	}
	return out, nil
}

// Insert rebuilds the row from the column list the grammar wrote and the
// bindings that follow it, which line up because both are key-sorted.
func (c *connection) Insert(sql string, bindings []any) (bool, error) {
	c.database.log = append(c.database.log, sql)

	table, columns := tableOfSQL(sql), columnsOfSQL(sql)
	if len(columns) == 0 || len(bindings)%len(columns) != 0 {
		return true, nil
	}

	for offset := 0; offset < len(bindings); offset += len(columns) {
		row := map[string]any{}
		for index, column := range columns {
			row[column] = bindings[offset+index]
		}
		c.database.tables[table] = append(c.database.tables[table], row)
	}
	return true, nil
}

func (c *connection) Update(sql string, bindings []any) (int64, error) {
	c.database.log = append(c.database.log, sql)
	return 1, nil
}

func (c *connection) Delete(sql string, bindings []any) (int64, error) {
	c.database.log = append(c.database.log, sql)

	table := tableOfSQL(sql)
	keep := make([]map[string]any, 0, len(c.database.tables[table]))
	removed := int64(0)
	for _, row := range c.database.tables[table] {
		if carriesAll(row, bindings) {
			removed++
			continue
		}
		keep = append(keep, row)
	}
	c.database.tables[table] = keep
	return removed, nil
}

func carriesAll(row map[string]any, bindings []any) bool {
	for _, binding := range bindings {
		found := false
		for _, value := range row {
			if sameValue(value, binding) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func columnsOfSQL(sql string) []string {
	open := strings.Index(sql, "(")
	closing := strings.LastIndex(sql, ")")
	if open < 0 || closing < open {
		return nil
	}

	columns := strings.Split(sql[open+1:closing], ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

func (c *connection) Statement(sql string, bindings []any) (bool, error) {
	c.database.log = append(c.database.log, sql)
	return true, nil
}

// tableOfSQL reads the table out of the four statements this grammar writes.
func tableOfSQL(sql string) string {
	fields := strings.Fields(sql)
	for index, field := range fields {
		switch field {
		case "from", "into":
			if index+1 < len(fields) {
				return fields[index+1]
			}
		case "update":
			if index+1 < len(fields) {
				return fields[index+1]
			}
		}
	}
	return ""
}

// grammar renders enough SQL to read in a log and to assert on. The real
// grammars live in query/grammars and are somebody else's slice; this one
// exists so that these tests do not have to wait for them.
type grammar struct{ query.BaseGrammar }

func (g *grammar) CompileSelect(q *query.Builder) string {
	columns := "*"
	if len(q.GetColumns()) > 0 {
		rendered := make([]string, 0, len(q.GetColumns()))
		for _, column := range q.GetColumns() {
			rendered = append(rendered, fmt.Sprint(column))
		}
		columns = strings.Join(rendered, ", ")
	}

	sql := "select " + columns + " from " + fmt.Sprint(q.GetFrom())
	for _, join := range q.Joins {
		sql += " inner join " + fmt.Sprint(join.Table)
	}
	if clause := renderWheres(q.Wheres); clause != "" {
		sql += " where " + clause
	}
	return sql
}

func (g *grammar) CompileInsert(q *query.Builder, values []map[string]any) string {
	if len(values) == 0 {
		return "insert into " + fmt.Sprint(q.GetFrom())
	}
	columns := make([]string, 0, len(values[0]))
	for column := range values[0] {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return "insert into " + fmt.Sprint(q.GetFrom()) + " (" + strings.Join(columns, ", ") + ")"
}

func (g *grammar) CompileInsertOrIgnore(q *query.Builder, values []map[string]any) string {
	return g.CompileInsert(q, values)
}

func (g *grammar) CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string {
	return g.CompileInsert(q, []map[string]any{values})
}

func (g *grammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	sort.Strings(columns)

	sql := "update " + fmt.Sprint(q.GetFrom()) + " set " + strings.Join(columns, ", ")
	if clause := renderWheres(q.Wheres); clause != "" {
		sql += " where " + clause
	}
	return sql
}

func (g *grammar) CompileUpsert(q *query.Builder, values []map[string]any, uniqueBy []string, update []string) string {
	return g.CompileInsert(q, values)
}

func (g *grammar) CompileDelete(q *query.Builder) string {
	sql := "delete from " + fmt.Sprint(q.GetFrom())
	if clause := renderWheres(q.Wheres); clause != "" {
		sql += " where " + clause
	}
	return sql
}

func (g *grammar) CompileTruncate(q *query.Builder) map[string][]any {
	return map[string][]any{"truncate " + fmt.Sprint(q.GetFrom()): nil}
}

func (g *grammar) CompileExists(q *query.Builder) string { return g.CompileSelect(q) }

func (g *grammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0, len(values))
	for _, column := range sortedColumns(values) {
		out = append(out, values[column])
	}
	return append(out, bindings["where"]...)
}

func (g *grammar) PrepareBindingsForDelete(bindings map[string][]any) []any {
	return bindings["where"]
}

func sortedColumns(values map[string]any) []string {
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return columns
}

func renderWheres(wheres []query.Where) string {
	rendered := make([]string, 0, len(wheres))
	for _, where := range wheres {
		switch where.Type {
		case "Basic":
			rendered = append(rendered, fmt.Sprint(where.Column)+" "+where.Operator+" ?")
		case "In":
			placeholders := make([]string, len(where.Values))
			for index := range where.Values {
				placeholders[index] = "?"
			}
			operator := " in ("
			if where.Not {
				operator = " not in ("
			}
			rendered = append(rendered, fmt.Sprint(where.Column)+operator+strings.Join(placeholders, ", ")+")")
		case "Null":
			if where.Not {
				rendered = append(rendered, fmt.Sprint(where.Column)+" is not null")
				continue
			}
			rendered = append(rendered, fmt.Sprint(where.Column)+" is null")
		case "Column":
			rendered = append(rendered, fmt.Sprint(where.First)+" "+where.Operator+" "+fmt.Sprint(where.Second))
		}
	}
	return strings.Join(rendered, " and ")
}
