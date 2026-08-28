package concerns

import (
	"context"
	"iter"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/database/query/processors"
	"github.com/arandu-io/hesape/pagination"
)

// grant is the Grant these tests read and write under. The tenant is not
// decoration: most of what this package does is put that string into a
// statement, so a test carrying an empty one would assert nothing.
func grant() auth.Grant { return auth.SystemGrant("roles.write", "acme") }

// statement is one call that reached the connection.
type statement struct {
	kind     string
	sql      string
	bindings []any
}

// fakeConnection is a query.Connection that records what it was asked to run
// and answers from canned rows.
//
// It never parses the SQL. What these tests check is the statement and the
// bindings the package produced, which is the pair a wrong answer comes from.
type fakeConnection struct {
	statements []statement
	rows       []query.Record
	affected   int64
	err        error
}

func (c *fakeConnection) Select(_ context.Context, sql string, bindings []any, _ bool) ([]query.Record, error) {
	c.statements = append(c.statements, statement{kind: "select", sql: sql, bindings: bindings})
	return c.rows, c.err
}

func (c *fakeConnection) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "insert", sql: sql, bindings: bindings})
	return c.err == nil, c.err
}

func (c *fakeConnection) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "update", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "delete", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "statement", sql: sql, bindings: bindings})
	return c.err == nil, c.err
}

// only returns the single statement the connection was given, and fails the
// test if it was given any other number.
func (c *fakeConnection) only(t testingT) statement {
	t.Helper()
	if len(c.statements) != 1 {
		t.Fatalf("the connection ran %d statements, want exactly 1: %#v", len(c.statements), c.statements)
	}
	return c.statements[0]
}

// testingT is the part of *testing.T the helpers here use. Declared so a helper
// can be called from a subtest without the file importing testing for a type it
// only names.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// newQuery returns a base query builder over conn, with a real grammar and
// processor: the SQL these tests assert on is the SQL the package ships.
func newQuery(conn *fakeConnection, table string) *query.Builder {
	b := query.NewBuilder(conn, grammars.NewSQLiteGrammar(), processors.NewSQLiteProcessor())
	if table != "" {
		b = b.From(table)
	}
	return b
}

// fakeModel is a Model that holds attributes in a map and records the calls
// that would have reached a database.
//
// Every method of the interface is answered, because the interface is the
// contract the whole relation tree is written against and a partial stand-in
// would compile only until somebody called the missing half.
type fakeModel struct {
	table      string
	keyName    string
	keyType    string
	morphClass string
	attributes map[string]any
	relations  map[string]any
	isRelation map[string]bool
	touches    map[string]bool
	exists     bool
	recent     bool
	timestamp  time.Time

	touched int
	saved   int
	deleted int
	saveErr error

	query Builder

	// queryFactory, when set, answers a fresh Builder on every NewQuery.
	// ofMany builds one subquery per column and joins them to each other, so
	// two calls handing back the same builder would make one query that is its
	// own join.
	queryFactory func() Builder
}

func newFakeModel(table string) *fakeModel {
	return &fakeModel{
		table:      table,
		keyName:    "id",
		keyType:    "int",
		morphClass: table,
		attributes: map[string]any{},
		relations:  map[string]any{},
		isRelation: map[string]bool{},
		touches:    map[string]bool{},
		timestamp:  time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}
}

func (m *fakeModel) GetTable() string      { return m.table }
func (m *fakeModel) SetTable(table string) { m.table = table }

func (m *fakeModel) QualifyColumn(column string) string {
	if column == "" {
		return column
	}
	for i := range column {
		if column[i] == '.' {
			return column
		}
	}
	return m.table + "." + column
}

func (m *fakeModel) GetKeyName() string    { return m.keyName }
func (m *fakeModel) GetKeyType() string    { return m.keyType }
func (m *fakeModel) GetKey() any           { return m.attributes[m.keyName] }
func (m *fakeModel) GetForeignKey() string { return m.table + "_" + m.keyName }
func (m *fakeModel) GetMorphClass() string { return m.morphClass }
func (m *fakeModel) Exists() bool          { return m.exists }

func (m *fakeModel) GetAttribute(key string) any         { return m.attributes[key] }
func (m *fakeModel) SetAttribute(key string, value any)  { m.attributes[key] = value }
func (m *fakeModel) GetAttributes() map[string]any       { return m.attributes }
func (m *fakeModel) UnsetAttribute(key string)           { delete(m.attributes, key) }
func (m *fakeModel) SetRelation(relation string, v any)  { m.relations[relation] = v }
func (m *fakeModel) RelationLoaded(relation string) bool { _, ok := m.relations[relation]; return ok }
func (m *fakeModel) UnsetRelation(relation string)       { delete(m.relations, relation) }
func (m *fakeModel) IsRelation(key string) bool          { return m.isRelation[key] }
func (m *fakeModel) Touches(relation string) bool        { return m.touches[relation] }

func (m *fakeModel) SetRawAttributes(attributes map[string]any, _ bool) {
	m.attributes = attributes
}

func (m *fakeModel) GetRelation(relation string) (any, bool) {
	value, ok := m.relations[relation]
	return value, ok
}

func (m *fakeModel) Touch(context.Context, auth.Grant) error {
	m.touched++
	return nil
}

func (m *fakeModel) NewInstance(attributes map[string]any) Model {
	instance := newFakeModel(m.table)
	instance.keyName, instance.keyType, instance.morphClass = m.keyName, m.keyType, m.morphClass
	for key, value := range attributes {
		instance.attributes[key] = value
	}
	return instance
}

func (m *fakeModel) Fill(attributes map[string]any) {
	for key, value := range attributes {
		m.attributes[key] = value
	}
}

func (m *fakeModel) ForceFill(attributes map[string]any) { m.Fill(attributes) }
func (m *fakeModel) WasRecentlyCreated() bool            { return m.recent }

func (m *fakeModel) WithoutEvents(callback func() error) error { return callback() }

func (m *fakeModel) Delete(context.Context, auth.Grant) (int64, error) {
	m.deleted++
	return 1, nil
}

func (m *fakeModel) NewQuery() Builder {
	if m.queryFactory != nil {
		return m.queryFactory()
	}
	return m.query
}

func (m *fakeModel) GetCreatedAtColumn() string { return "created_at" }
func (m *fakeModel) GetUpdatedAtColumn() string { return "updated_at" }
func (m *fakeModel) UsesTimestamps() bool       { return true }
func (m *fakeModel) FreshTimestamp() time.Time  { return m.timestamp }
func (m *fakeModel) Save(context.Context, auth.Grant) error {
	m.saved++
	return m.saveErr
}

// sharedModel is a Model whose rows are the same for every tenant. It is the
// opt-out Tenanted describes, and it is here so a test can prove the opt-out is
// honoured rather than assumed.
type sharedModel struct{ *fakeModel }

func (sharedModel) GetTenantColumn() string { return "" }

// columnModel names a tenant column of its own, which is the other thing
// Tenanted allows.
type columnModel struct {
	*fakeModel
	column string
}

func (m columnModel) GetTenantColumn() string { return m.column }

// fakeBuilder is a Builder over a real base query.
//
// Every clause method forwards to the query underneath as well as recording, so
// the statement a test compiles is the statement the package built. A stand-in
// that swallowed its clauses would make the interface look inhabited while
// measuring nothing, which is the shape a relation surface was already found
// unreachable behind.
//
// The read terminals answer zero values, and only those: what the tests using
// this builder measure is the statement, not what a read returned.
type fakeBuilder struct {
	base   *query.Builder
	model  Model
	wheres []whereCall
}

// whereCall is one Where the builder was asked for.
type whereCall struct {
	column any
	args   []any
}

func newFakeBuilder(model Model, base *query.Builder) *fakeBuilder {
	return &fakeBuilder{base: base, model: model}
}

// asScoper returns b answering OwnTenantScoper with scopes, which is how a
// builder says it filters its own table itself.
func (b *fakeBuilder) asScoper(scopes bool) *scopingBuilder {
	return &scopingBuilder{fakeBuilder: b, scopes: scopes}
}

func (b *fakeBuilder) GetModel() Model          { return b.model }
func (b *fakeBuilder) GetQuery() *query.Builder { return b.base }

func (b *fakeBuilder) Select(columns ...any) Builder {
	b.base.Select(columns...)
	return b
}

func (b *fakeBuilder) AddSelect(columns ...any) Builder {
	b.base.AddSelect(columns...)
	return b
}

func (b *fakeBuilder) WhereNotNull(columns ...any) Builder {
	b.base.WhereNotNull(columns...)
	return b
}

func (b *fakeBuilder) WhereColumn(first any, args ...any) Builder {
	b.base.WhereColumn(first, args...)
	return b
}

// WhereKey is the one clause with no counterpart on the base query: it is the
// typed builder's shorthand for the model's own key, and a base query has no
// model to ask for one.
func (b *fakeBuilder) WhereKey(ids ...any) Builder {
	if b.model == nil {
		return b
	}
	b.base.WhereIn(b.model.QualifyColumn(b.model.GetKeyName()), ids)
	return b
}

func (b *fakeBuilder) Join(table any, first any, args ...any) Builder {
	b.base.Join(table, first, args...)
	return b
}

func (b *fakeBuilder) GroupBy(groups ...any) Builder {
	b.base.GroupBy(groups...)
	return b
}

func (b *fakeBuilder) SelectRaw(expression string, bindings ...any) Builder {
	b.base.SelectRaw(expression, bindings...)
	return b
}

func (b *fakeBuilder) OrderBy(column any, direction ...string) Builder {
	b.base.OrderBy(column, direction...)
	return b
}

func (b *fakeBuilder) Limit(value int) Builder {
	b.base.Limit(value)
	return b
}

func (b *fakeBuilder) Offset(value int) Builder {
	b.base.Offset(value)
	return b
}

func (b *fakeBuilder) Where(column any, args ...any) Builder {
	b.wheres = append(b.wheres, whereCall{column: column, args: args})
	b.base.Where(column, args...)
	return b
}

func (b *fakeBuilder) WhereIn(column any, values []any) Builder {
	b.wheres = append(b.wheres, whereCall{column: column, args: []any{values}})
	b.base.WhereIn(column, values)
	return b
}

func (b *fakeBuilder) Cursor(context.Context, auth.Grant) iter.Seq2[Model, error] {
	return func(func(Model, error) bool) {}
}

func (b *fakeBuilder) Paginate(context.Context, auth.Grant, int, int, pagination.Options, ...any) (*pagination.LengthAwarePaginator[Model], error) {
	return nil, nil
}

func (b *fakeBuilder) SimplePaginate(context.Context, auth.Grant, int, int, pagination.Options, ...any) (*pagination.Paginator[Model], error) {
	return nil, nil
}

func (b *fakeBuilder) CursorPaginate(context.Context, auth.Grant, int, *pagination.Cursor, pagination.Options, ...any) (*pagination.CursorPaginator[Model], error) {
	return nil, nil
}

func (b *fakeBuilder) Get(context.Context, auth.Grant) ([]Model, error)           { return nil, nil }
func (b *fakeBuilder) First(context.Context, auth.Grant) (Model, error)           { return nil, nil }
func (b *fakeBuilder) Find(context.Context, auth.Grant, any) (Model, error)       { return nil, nil }
func (b *fakeBuilder) Insert(context.Context, auth.Grant, []map[string]any) error { return nil }

func (b *fakeBuilder) Update(context.Context, auth.Grant, map[string]any) (int64, error) {
	return 0, nil
}

func (b *fakeBuilder) Upsert(context.Context, auth.Grant, []map[string]any, []string, []string) (int64, error) {
	return 0, nil
}

func (b *fakeBuilder) Delete(context.Context, auth.Grant) (int64, error) { return 0, nil }
func (b *fakeBuilder) Clone() Builder {
	return &fakeBuilder{base: b.base.Clone(), model: b.model}
}

// scopingBuilder is a fakeBuilder that answers OwnTenantScoper.
type scopingBuilder struct {
	*fakeBuilder
	scopes bool
}

func (b *scopingBuilder) ScopesOwnTableByTenant() bool { return b.scopes }

// columnsWheredOn returns the columns the builder was asked to filter on, in
// the order they were asked for.
func (b *fakeBuilder) columnsWheredOn() []string {
	out := make([]string, 0, len(b.wheres))
	for _, where := range b.wheres {
		out = append(out, toString(where.column))
	}
	return out
}

func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
