package model

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/database/query"
)

// Builder is the query builder that hands back rows of a table instead of
// records.
//
// Everything that runs takes an auth.Grant and filters by auth.Tenant(g) --
// reads exactly like writes. Everything that only builds does not: a Where or an
// OrderBy is a fragment of SQL, and a fragment authorizes nothing.
//
// It is one type for every table. A query an application writes against its own
// entity goes through the query type generated beside it, which holds one of
// these and converts what comes back.
type Builder struct {
	query *query.Builder
	table *Table
	conn  *conn

	// model is the prototype the query runs through: a model of the table that
	// stands for no row. It is made the first time something asks for it, which
	// most queries never do.
	model *Model

	eagerLoad     map[string]func(*query.Builder)
	scopes        map[string]Scope
	removedScopes []string

	onDelete            func(context.Context, *Builder, auth.Grant) (int64, error)
	afterQueryCallbacks []func(Rows) Rows
	onCloneCallbacks    []func(*Builder)
	pendingAttributes   map[string]any

	// prepared says that the scopes and the tenant filter are already on this
	// builder's query.
	//
	// It exists because prepare is called by every method that runs, and those
	// methods call each other: Get prepares and then asks GetModels, which
	// prepares too. Without the flag the tenant filter and every global scope
	// landed in the where clause twice -- the same rows, the same bindings, and a
	// query nobody could read.
	prepared bool

	// err is what a builder method could not report.
	//
	// A method that returned an error could not be chained, and a chain that has
	// to be broken every second call is a chain nobody writes -- so the error is
	// held and returned by the first method that runs. Nothing is ever executed
	// with an error waiting.
	err error
}

// fail records an error for the first method that runs to report. See
// Builder.err.
func fail(b *Builder, err error) *Builder {
	if b.err == nil {
		b.err = err
	}
	return b
}

// Table returns the table this builder queries.
func (b *Builder) Table() *Table { return b.table }

// GetModel returns the model the query runs through: a model of the table that
// stands for no row, the same one on every call.
//
// A relation reads the table, the key and the column names off it, and a
// relation built on it is unconstrained -- see RelationFunc.
func (b *Builder) GetModel() *Model {
	if b.model == nil {
		b.model = b.table.newModel(b.conn)
		b.model.r.prototype = true
	}
	return b.model
}

// tableName is the table the query reads: the table's own name, or the alias a
// relation joined it to itself under.
func (b *Builder) tableName() string {
	if b.model != nil {
		return tableNameOf(b.model)
	}
	return b.table.name
}

// qualify returns column qualified with the query's table, unless it already
// contains a dot.
func (b *Builder) qualify(column string) string {
	if containsDot(column) {
		return column
	}
	return b.tableName() + "." + column
}

// GetQuery returns the underlying query.Builder.
func (b *Builder) GetQuery() *query.Builder { return b.query }

// SetQuery replaces the underlying query.Builder.
func (b *Builder) SetQuery(q *query.Builder) *Builder {
	b.query = q
	return b
}

// ToBase returns the underlying query.Builder with the scopes applied.
//
// It takes the Grant because applying the scopes is also where the tenant
// filter goes on, and a base builder handed out without it is a query somebody
// will run.
func (b *Builder) ToBase(ctx context.Context, g auth.Grant) (*query.Builder, error) {
	prepared, err := prepare(b, g)
	if err != nil {
		return nil, err
	}
	return prepared.query, nil
}

// Qualify returns column qualified with the table.
func (b *Builder) Qualify(column string) string { return b.qualify(column) }

// NewModelInstance returns a new, unsaved row of the table, on the builder's
// connection, with attributes merged over any pending attributes from
// WithAttributes.
func (b *Builder) NewModelInstance(attributes map[string]any) (Entity, error) {
	instance, err := newModelInstance(b, attributes)
	if err != nil {
		return nil, err
	}
	return instance.r.self, nil
}

// newModelInstance is NewModelInstance with the model still in hand.
func newModelInstance(b *Builder, attributes map[string]any) (*Model, error) {
	merged := copyMap(b.pendingAttributes)
	for key, value := range attributes {
		merged[key] = value
	}
	instance := b.table.newModel(b.conn)
	if err := instance.Fill(merged); err != nil {
		return nil, err
	}
	return instance, nil
}

// WithAttributes records values that filter the query and then fill whatever
// the query creates.
//
// asConditions defaults to true; passing false keeps the values for
// NewModelInstance without adding the where clauses -- which is what a relation
// does with a foreign key it already constrained another way.
//
// There is no separate single-column form: a map with one entry is that call.
func (b *Builder) WithAttributes(attributes map[string]any, asConditions ...bool) *Builder {
	if optionalBool(asConditions) {
		for _, column := range sortedKeys(attributes) {
			b.Where(b.qualify(column), "=", attributes[column])
		}
	}
	if b.pendingAttributes == nil {
		b.pendingAttributes = map[string]any{}
	}
	for column, value := range attributes {
		b.pendingAttributes[column] = value
	}
	return b
}

// WithSavepointIfNeeded runs scope inside a savepoint when a transaction is
// already open, and plainly when none is.
//
// query.Connection does not declare a transaction level -- see Transactor for
// why this component does not widen it -- so the capability is asked for by a
// type assertion, and a connection that does not implement it runs the callback
// as if no transaction were open, the same as a level of zero.
func (b *Builder) WithSavepointIfNeeded(scope func() error) error {
	nested, ok := b.query.GetConnection().(Savepointer)
	if !ok || nested.TransactionLevel() <= 0 {
		return scope()
	}
	return nested.Transaction(scope)
}

// clone returns a copy of b with its own query, scopes and callback slices, so
// that mutating the copy never touches b.
func clone(b *Builder) *Builder {
	out := &Builder{
		query:               b.query.Clone(),
		table:               b.table,
		conn:                b.conn,
		model:               b.model,
		eagerLoad:           make(map[string]func(*query.Builder), len(b.eagerLoad)),
		scopes:              cloneScopes(b.scopes),
		removedScopes:       slices.Clone(b.removedScopes),
		onDelete:            b.onDelete,
		afterQueryCallbacks: slices.Clone(b.afterQueryCallbacks),
		pendingAttributes:   copyMap(b.pendingAttributes),
		prepared:            b.prepared,
		err:                 b.err,
	}
	for name, constraints := range b.eagerLoad {
		out.eagerLoad[name] = constraints
	}
	out.onCloneCallbacks = slices.Clone(b.onCloneCallbacks)
	for _, callback := range out.onCloneCallbacks {
		callback(out)
	}
	return out
}

// Clone returns a copy of b, safe to mutate independently.
func (b *Builder) Clone() *Builder { return clone(b) }

// prepare is where a query becomes runnable: the global scopes go on, and then
// the tenant filter.
//
// The tenant is read off the Grant and never from anywhere else. A Grant with no
// tenant -- the zero Grant, which is the only one constructible outside the auth
// package -- is refused here, before any SQL exists.
//
// The wheres already on the query are wrapped in one group before the tenant
// filter is appended, and that is not tidiness. `where a or b` with `and tenant
// = ?` appended reads as `a or (b and tenant = ?)`, so every row matching a
// comes back whoever it belongs to. Grouping first is what makes the filter mean
// what it says.
func prepare(b *Builder, g auth.Grant) (*Builder, error) {
	if b.err != nil {
		return nil, b.err
	}

	tenant := auth.Tenant(g)
	if tenant == "" || !auth.ValidTenant(tenant) {
		return nil, ErrNoTenant
	}
	if b.prepared {
		return b, nil
	}

	prepared := b.ApplyScopes()
	prepared.prepared = true

	if err := scopeToTenant(prepared, g, tenant); err != nil {
		return nil, err
	}
	return prepared, nil
}

// scopeToTenant is the only place a tenant lands on a model statement: the
// table's own filter, and then the nested half of the query builder's scoped.
//
// It is called on a builder nobody else holds -- prepare on the clone
// ApplyScopes made, ForceDelete on its own -- because the filter belongs to the
// statement and not to the query the caller kept: running the same builder twice
// under two Grants must not leave the first tenant's filter on the second
// tenant's statement.
//
// The second half is not a second filter, it is the rest of the same one.
// query.Builder.ScopeNested puts the tenant on the far side of a union, on the
// subquery of a `where exists`, a `where in` and a count comparison, and on the
// subqueries compiled into a from, a select or a join. Without it the filter
// named only the outer table, and that is what leaked:
// Users.WithCount("posts").Get(auth.SystemGrant("user.list", "acme")) emitted
// `select "users".*, (select count(*) from "posts" where "users"."id" =
// "posts"."user_id") as "posts_count" from "users" where "users"."tenant_id" =
// ?`, and every tenant's posts were counted into every row.
//
// The context is Background because the connection contract takes none, so a
// signature that accepted one could not pass it on. All ScopeNested does with it
// is refuse to build a statement whose context is already cancelled, and there
// is nothing here for that to cancel.
func scopeToTenant(b *Builder, g auth.Grant, tenant string) error {
	if column := b.table.tenantColumn; column != "" {
		isolateWheres(b.query)
		b.query.Where(b.qualify(column), "=", tenant)
	}
	return b.query.ScopeNested(context.Background(), g)
}

// isolateWheres wraps every where already on the query in a single group, so
// that a filter added after them cannot be swallowed by an or. See prepare.
func isolateWheres(q *query.Builder) {
	if len(q.Wheres) < 2 {
		return
	}
	hasOr := false
	for _, where := range q.Wheres {
		if strings.Contains(where.Boolean, "or") {
			hasOr = true
			break
		}
	}
	if !hasOr {
		return
	}

	group := q.ForNestedWhere()
	group.Wheres = q.Wheres
	boolean := strings.ReplaceAll(q.Wheres[0].Boolean, " not", "")
	if boolean == "" {
		boolean = "and"
	}
	q.Wheres = []query.Where{{Type: "Nested", Query: group, Boolean: boolean}}
}

// WithGlobalScope registers scope under identifier, for this query.
func (b *Builder) WithGlobalScope(identifier string, scope Scope) *Builder {
	if b.scopes == nil {
		b.scopes = map[string]Scope{}
	}
	b.scopes[identifier] = scope
	return b
}

// WithoutGlobalScope removes the scope registered under identifier, and
// records it as removed.
func (b *Builder) WithoutGlobalScope(identifier string) *Builder {
	delete(b.scopes, identifier)
	b.removedScopes = append(b.removedScopes, identifier)
	return b
}

// WithoutGlobalScopes removes the named scopes. With no argument it removes
// them all.
func (b *Builder) WithoutGlobalScopes(identifiers ...string) *Builder {
	if len(identifiers) == 0 {
		identifiers = sortedScopeNames(b.scopes)
	}
	for _, identifier := range identifiers {
		b.WithoutGlobalScope(identifier)
	}
	return b
}

// WithoutGlobalScopesExcept removes every registered scope except the named
// ones.
func (b *Builder) WithoutGlobalScopesExcept(identifiers ...string) *Builder {
	for _, identifier := range sortedScopeNames(b.scopes) {
		if !slices.Contains(identifiers, identifier) {
			b.WithoutGlobalScope(identifier)
		}
	}
	return b
}

// RemovedScopes returns the identifiers of the scopes removed from this
// builder.
func (b *Builder) RemovedScopes() []string { return slices.Clone(b.removedScopes) }

// ApplyScopes returns a copy of the builder with every registered scope
// applied.
//
// The wheres a scope adds are wrapped in a group when either side carries an or:
// without that, a scope's filter joins an or chain and stops filtering.
func (b *Builder) ApplyScopes() *Builder {
	out := clone(b)
	for _, identifier := range sortedScopeNames(b.scopes) {
		before := len(out.query.Wheres)
		b.scopes[identifier](out)
		groupNewWheres(out.query, before)
	}
	return out
}

// groupNewWheres wraps the wheres added since originalCount in their own group
// when needed, so a scope's filter cannot be absorbed by a surrounding or.
func groupNewWheres(q *query.Builder, originalCount int) {
	if len(q.Wheres) == originalCount {
		return
	}
	all := q.Wheres
	q.Wheres = nil
	groupWhereSliceForScope(q, all[:originalCount])
	groupWhereSliceForScope(q, all[originalCount:])
}

// groupWhereSliceForScope appends slice to q's wheres, wrapped in one nested
// group when any entry in it uses "or".
func groupWhereSliceForScope(q *query.Builder, slice []query.Where) {
	if len(slice) == 0 {
		return
	}
	hasOr := false
	for _, where := range slice {
		if strings.Contains(where.Boolean, "or") {
			hasOr = true
			break
		}
	}
	if !hasOr {
		q.Wheres = append(q.Wheres, slice...)
		return
	}
	group := q.ForNestedWhere()
	group.Wheres = slice
	q.Wheres = append(q.Wheres, query.Where{
		Type:    "Nested",
		Query:   group,
		Boolean: strings.ReplaceAll(slice[0].Boolean, " not", ""),
	})
}

// Where adds a where clause. Passing a func(*Builder) instead of a column name
// adds a group built by calling that function with a fresh builder, as
// WhereGroup does.
func (b *Builder) Where(column any, args ...any) *Builder {
	if nested, ok := column.(func(*Builder)); ok {
		return whereNested(b, nested, "and")
	}
	b.query.Where(column, args...)
	return b
}

// OrWhere adds an or-where clause. Passing a func(*Builder) instead of a column
// name adds a group built by calling that function with a fresh builder.
func (b *Builder) OrWhere(column any, args ...any) *Builder {
	if nested, ok := column.(func(*Builder)); ok {
		return whereNested(b, nested, "or")
	}
	b.query.OrWhere(column, args...)
	return b
}

// WhereGroup adds the wheres fn builds as one parenthesised group, joined to the
// query with boolean -- "and" or "or".
//
// fn is handed a fresh builder on the same table, and only what it adds to the
// where clause, the eager loads and the scopes it removes are carried over. It
// is what a generated query type's Where calls when it is handed a closure over
// itself, which is why it is spelled out rather than left to Where's type
// switch.
func (b *Builder) WhereGroup(boolean string, fn func(*Builder)) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	return whereNested(b, fn, boolean)
}

// whereNested runs callback against a fresh builder for the same table, and adds
// what it builds as one group joined with boolean.
func whereNested(b *Builder, callback func(*Builder), boolean string) *Builder {
	nested := b.table.query(b.conn, false)
	nested.model = b.model
	callback(nested)
	if nested.err != nil {
		// The nested builder is thrown away once its wheres are merged, so an
		// error left on it would never reach anybody.
		fail(b, nested.err)
	}
	for name, constraints := range nested.eagerLoad {
		b.eagerLoad[name] = constraints
	}
	b.WithoutGlobalScopes(nested.removedScopes...)
	b.query.AddNestedWhereQuery(nested.query, boolean)
	return b
}

// WhereNot adds a where clause wrapped in a negated group: NOT (column
// args...).
func (b *Builder) WhereNot(column any, args ...any) *Builder {
	before := len(b.query.Wheres)
	b.Where(func(nested *Builder) {
		nested.Where(column, args...)
	})
	return negateLastWhere(b, before)
}

// negateLastWhere flips the boolean of the group at index before to "... not",
// negating it.
//
// It negates nothing when the group turned out empty -- an empty nested where
// is dropped rather than compiled, and negating whatever came before it would
// change a clause the caller did not write.
func negateLastWhere(b *Builder, before int) *Builder {
	if len(b.query.Wheres) == before {
		return b
	}
	last := &b.query.Wheres[len(b.query.Wheres)-1]
	if !strings.Contains(last.Boolean, "not") {
		last.Boolean += " not"
	}
	return b
}

// OrWhereNot adds an or-joined, negated group: OR NOT (column args...).
func (b *Builder) OrWhereNot(column any, args ...any) *Builder {
	before := len(b.query.Wheres)
	b.OrWhere(func(nested *Builder) {
		nested.Where(column, args...)
	})
	return negateLastWhere(b, before)
}

// WhereKey filters by the table's primary key. A slice of ids adds a WHERE IN
// instead of an equality.
func (b *Builder) WhereKey(id any) *Builder {
	key := b.qualify(b.table.keyName)
	if ids, ok := id.([]any); ok {
		b.query.WhereIn(key, ids)
		return b
	}
	b.query.Where(key, "=", id)
	return b
}

// WhereKeyNot excludes the table's primary key. A slice of ids adds a WHERE NOT
// IN instead of an inequality.
func (b *Builder) WhereKeyNot(id any) *Builder {
	key := b.qualify(b.table.keyName)
	if ids, ok := id.([]any); ok {
		b.query.WhereNotIn(key, ids)
		return b
	}
	return b.Where(key, "!=", id)
}

// Latest orders the query by column, or the table's created-at column when none
// is given, newest first.
func (b *Builder) Latest(column ...string) *Builder {
	b.query.Latest(timestampColumn(b.table.createdAt, column))
	return b
}

// Oldest orders the query by column, or the table's created-at column when none
// is given, oldest first.
func (b *Builder) Oldest(column ...string) *Builder {
	b.query.Oldest(timestampColumn(b.table.createdAt, column))
	return b
}

func timestampColumn(fallback string, column []string) any {
	if len(column) > 0 && column[0] != "" {
		return column[0]
	}
	if fallback == "" {
		return "created_at"
	}
	return fallback
}

// Hydrate turns records into rows of the table: records in, rows out.
//
// Each row is allocated by the table's New and filled through the cached
// schema, and every row of one call shares one block of bookkeeping, so the
// cost per row is the entity and the values in it.
func (b *Builder) Hydrate(items []query.Record) (Rows, error) {
	return hydrate(b, items)
}

// hydrate is Hydrate, which every read in this package goes through.
func hydrate(b *Builder, items []query.Record) (Rows, error) {
	out := make(Rows, len(items))
	if len(items) == 0 {
		return out, nil
	}
	states := make([]row, len(items))
	for i, item := range items {
		e, err := hydrateRow(b, item, &states[i])
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

// hydrateRow is one record become one row: the entity allocated by the table's
// New, its model pointed at state, the columns assigned through the cached
// schema, the original synced and the Retrieved event fired.
func hydrateRow(b *Builder, record query.Record, state *row) (Entity, error) {
	t := b.table
	e := t.newEntity()
	m := e.base()
	state.table, state.conn, state.self, state.exists = t, b.conn, e, true
	m.r = state
	if err := setAttributes(m, record, true); err != nil {
		return nil, err
	}
	m.r.original = m.GetAttributes()
	if err := fireModelEvent(m, Retrieved); err != nil {
		return nil, err
	}
	return e, nil
}

// FromQuery returns rows from SQL somebody wrote by hand.
//
// It takes the Grant like every other read. The SQL is the caller's, so the
// tenant cannot be added to it -- which is exactly why the Grant is still
// required: a query nobody authorized does not run, and the where clause that
// scopes it is the caller's to write.
func (b *Builder) FromQuery(ctx context.Context, g auth.Grant, sql string, bindings []any) (Rows, error) {
	// The other method that skips prepare, and so the other one that has to read
	// the held error itself. See ForceDelete.
	if b.err != nil {
		return nil, b.err
	}
	if tenant := auth.Tenant(g); tenant == "" || !auth.ValidTenant(tenant) {
		return nil, ErrNoTenant
	}
	records, err := b.conn.connection.Select(ctx, sql, bindings, true)
	if err != nil {
		return nil, fmt.Errorf("model: selecting from %s: %w", b.table.name, err)
	}
	return b.Hydrate(records)
}

// Get runs the query and returns the matching rows, with their eager loads
// applied.
func (b *Builder) Get(ctx context.Context, g auth.Grant, columns ...any) (Rows, error) {
	found, err := b.get(ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	return b.ApplyAfterQueryCallbacks(found), nil
}

// get is Get without the after-query callbacks.
//
// The callbacks are the caller's, and they are applied where the result is
// handed to one; every read in this package that keeps working on the rows calls
// this.
func (b *Builder) get(ctx context.Context, g auth.Grant, columns ...any) (Rows, error) {
	prepared, err := prepare(b, g)
	if err != nil {
		return nil, err
	}
	found, err := getModels(prepared, ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		if err := eagerLoadRelations(prepared, ctx, g, found); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// result is the after-query callbacks for a terminal that matched at most one
// row.
//
// A First is a Get with a limit of one, so its row is a one-row result and the
// callbacks see it as one. Nothing matched stays nothing matched: a callback
// that replaced an empty result would be answering a question nobody asked.
func result(b *Builder, model *Model) Entity {
	if model == nil {
		return nil
	}
	if len(b.afterQueryCallbacks) == 0 {
		return model.r.self
	}
	return b.ApplyAfterQueryCallbacks(Rows{model.r.self}).First()
}

// GetModels returns the rows, hydrated, with nothing eager loaded.
//
// It is already prepared when Get calls it; called on its own it prepares
// itself, so there is no way to reach the rows without the Grant.
func (b *Builder) GetModels(ctx context.Context, g auth.Grant, columns ...any) (Rows, error) {
	return getModels(b, ctx, g, columns...)
}

// getModels is GetModels.
func getModels(b *Builder, ctx context.Context, g auth.Grant, columns ...any) (Rows, error) {
	prepared, err := prepare(b, g)
	if err != nil {
		return nil, err
	}
	if len(columns) > 0 && prepared.query.Columns == nil {
		prepared.query.Select(columns...)
	}
	records, err := runSelect(prepared, ctx)
	if err != nil {
		return nil, err
	}
	return hydrate(prepared, records)
}

// AfterQuery registers a callback run on the result of Get, allowed to replace
// it.
//
// It runs wherever rows are handed to a caller: Get, Chunk and the walks built
// on it, and the terminals that are a Get with a limit -- First, Sole, Find and
// their neighbours, whose row the callback sees as a result of one.
//
// It does not run on the reads this package makes for itself: the row Refresh
// reads back into a model it already holds, the aggregate LoadCount fills in,
// everything the relation tree reads through its own seam. Nor does it run on a
// page, which is counted from the rows the query returned.
func (b *Builder) AfterQuery(callback func(Rows) Rows) *Builder {
	b.afterQueryCallbacks = append(b.afterQueryCallbacks, callback)
	return b
}

// ApplyAfterQueryCallbacks runs the registered AfterQuery callbacks over result
// in order, threading each callback's replacement into the next.
func (b *Builder) ApplyAfterQueryCallbacks(result Rows) Rows {
	for _, callback := range b.afterQueryCallbacks {
		if next := callback(result); next != nil {
			result = next
		}
	}
	return result
}

// First returns the first row matching the query, or (nil, nil) when there is
// none: no row is not a failure, and FirstOrFail is the spelling for when it is.
func (b *Builder) First(ctx context.Context, g auth.Grant, columns ...any) (Entity, error) {
	model, err := first(b, ctx, g, columns...)
	return result(b, model), err
}

// first is First with the model still in hand, and without the after-query
// callbacks.
func first(b *Builder, ctx context.Context, g auth.Grant, columns ...any) (*Model, error) {
	b.query.Limit(1)
	found, err := b.get(ctx, g, columns...)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0].base(), nil
}

// FirstOrFail returns the first row matching the query, or an error when there
// is none.
func (b *Builder) FirstOrFail(ctx context.Context, g auth.Grant, columns ...any) (Entity, error) {
	model, err := firstOrFail(b, ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// firstOrFail is FirstOrFail with the model still in hand.
func firstOrFail(b *Builder, ctx context.Context, g auth.Grant, columns ...any) (*Model, error) {
	model, err := first(b, ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, modelNotFound(b.table.name)
	}
	return model, nil
}

// FirstOr returns the first row matching the query, or what callback makes when
// there is none.
func (b *Builder) FirstOr(ctx context.Context, g auth.Grant, callback func() (Entity, error), columns ...any) (Entity, error) {
	model, err := first(b, ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return result(b, model), nil
	}
	return callback()
}

// FirstWhere adds a where clause and returns the first matching row.
func (b *Builder) FirstWhere(ctx context.Context, g auth.Grant, column any, args ...any) (Entity, error) {
	return b.Where(column, args...).First(ctx, g)
}

// Sole returns the row matching the query, and fails unless it is the only one.
func (b *Builder) Sole(ctx context.Context, g auth.Grant, columns ...any) (Entity, error) {
	model, err := sole(b, ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// sole is Sole with the model still in hand.
func sole(b *Builder, ctx context.Context, g auth.Grant, columns ...any) (*Model, error) {
	found, err := b.Limit(2).get(ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	switch len(found) {
	case 0:
		return nil, modelNotFound(b.table.name)
	case 1:
		return found[0].base(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrMultipleRecordsFound, b.table.name)
	}
}

// Find returns the row with the given primary key, or the first of the rows for
// a slice of keys.
func (b *Builder) Find(ctx context.Context, g auth.Grant, id any, columns ...any) (Entity, error) {
	model, err := find(b, ctx, g, id, columns...)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// find is Find with the model still in hand.
func find(b *Builder, ctx context.Context, g auth.Grant, id any, columns ...any) (*Model, error) {
	if ids, ok := id.([]any); ok {
		found, err := findMany(b, ctx, g, ids, columns...)
		if err != nil || len(found) == 0 {
			return nil, err
		}
		return found[0].base(), nil
	}
	b.WhereKey(id)
	return first(b, ctx, g, columns...)
}

// FindMany returns the rows matching any of ids.
func (b *Builder) FindMany(ctx context.Context, g auth.Grant, ids []any, columns ...any) (Rows, error) {
	found, err := findMany(b, ctx, g, ids, columns...)
	if err != nil {
		return nil, err
	}
	return b.ApplyAfterQueryCallbacks(found), nil
}

// findMany is FindMany without the after-query callbacks.
func findMany(b *Builder, ctx context.Context, g auth.Grant, ids []any, columns ...any) (Rows, error) {
	if len(ids) == 0 {
		return Rows{}, nil
	}
	b.WhereKey(ids)
	return b.get(ctx, g, columns...)
}

// FindOrFail returns the row with the given primary key, or an error when there
// is none.
//
// Given a list it also fails when one id is missing: asking for three rows and
// getting two is not a shorter result, it is a wrong one.
func (b *Builder) FindOrFail(ctx context.Context, g auth.Grant, id any, columns ...any) (Entity, error) {
	model, err := findOrFail(b, ctx, g, id, columns...)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// findOrFail is FindOrFail with the model still in hand.
func findOrFail(b *Builder, ctx context.Context, g auth.Grant, id any, columns ...any) (*Model, error) {
	if ids, ok := id.([]any); ok {
		found, err := findMany(b, ctx, g, ids, columns...)
		if err != nil {
			return nil, err
		}
		if len(found) != len(uniqueValues(ids)) {
			return nil, modelNotFound(b.table.name, ids...)
		}
		return found[0].base(), nil
	}
	model, err := find(b, ctx, g, id, columns...)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, modelNotFound(b.table.name, id)
	}
	return model, nil
}

// FindOrNew returns the row with the given primary key, or a new unsaved row
// when there is none.
func (b *Builder) FindOrNew(ctx context.Context, g auth.Grant, id any, columns ...any) (Entity, error) {
	model, err := find(b, ctx, g, id, columns...)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return result(b, model), nil
	}
	return b.NewModelInstance(nil)
}

// FirstOrNew returns the first row matching attributes, or a new unsaved row
// built from attributes and values when there is none.
func (b *Builder) FirstOrNew(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	model, err := first(whereAll(clone(b), attributes), ctx, g)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return result(b, model), nil
	}
	return b.NewModelInstance(mergeMaps(attributes, values))
}

// FirstOrCreate returns the first row matching attributes, or creates and
// returns one from attributes and values when there is none.
func (b *Builder) FirstOrCreate(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	model, err := firstOrCreate(b, ctx, g, attributes, values)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// firstOrCreate is FirstOrCreate with the model still in hand.
func firstOrCreate(b *Builder, ctx context.Context, g auth.Grant, attributes, values map[string]any) (*Model, error) {
	model, err := first(whereAll(clone(b), attributes), ctx, g)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return createOrFirst(b, ctx, g, attributes, values)
}

// CreateOrFirst inserts a row from attributes and values, and if a unique index
// says somebody got there first, reads theirs instead.
//
// Nothing here classifies a driver error yet, so any insert failure sends it
// looking for the row, and the insert error is returned when there is none --
// which keeps the race safe and never swallows a real failure.
func (b *Builder) CreateOrFirst(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	model, err := createOrFirst(b, ctx, g, attributes, values)
	if err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// createOrFirst is CreateOrFirst with the model still in hand.
func createOrFirst(b *Builder, ctx context.Context, g auth.Grant, attributes, values map[string]any) (*Model, error) {
	model, err := create(b, ctx, g, mergeMaps(attributes, values))
	if err == nil {
		return model, nil
	}
	existing, findErr := first(whereAll(clone(b), attributes), ctx, g)
	if findErr != nil || existing == nil {
		return nil, err
	}
	return existing, nil
}

// UpdateOrCreate finds or creates a row matching attributes, then fills it with
// values and saves it.
func (b *Builder) UpdateOrCreate(ctx context.Context, g auth.Grant, attributes, values map[string]any) (Entity, error) {
	model, err := firstOrCreate(b, ctx, g, attributes, values)
	if err != nil {
		return nil, err
	}
	if model.r.recent {
		return result(b, model), nil
	}
	if err := model.Fill(values); err != nil {
		return nil, err
	}
	if _, err := model.Save(ctx, g); err != nil {
		return nil, err
	}
	return result(b, model), nil
}

// whereAll adds one equality where clause per entry in attributes.
func whereAll(b *Builder, attributes map[string]any) *Builder {
	for _, column := range sortedKeys(attributes) {
		b.Where(b.qualify(column), "=", attributes[column])
	}
	return b
}

// Value returns one column of the first row matching the query.
func (b *Builder) Value(ctx context.Context, g auth.Grant, column string) (any, error) {
	model, err := first(b, ctx, g, column)
	if err != nil || model == nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// ValueOrFail returns one column of the first row matching the query, or an
// error when there is none.
func (b *Builder) ValueOrFail(ctx context.Context, g auth.Grant, column string) (any, error) {
	model, err := firstOrFail(b, ctx, g, column)
	if err != nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// SoleValue returns one column of the row matching the query, and fails unless
// it is the only one.
func (b *Builder) SoleValue(ctx context.Context, g auth.Grant, column string) (any, error) {
	model, err := sole(b, ctx, g, column)
	if err != nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// Pluck returns one column of every row matching the query.
func (b *Builder) Pluck(ctx context.Context, g auth.Grant, column string) ([]any, error) {
	found, err := b.get(ctx, g, column)
	if err != nil {
		return nil, err
	}
	return found.Pluck(afterLastDot(column)), nil
}

// Count returns the row count of the query, or the count of columns when given.
func (b *Builder) Count(ctx context.Context, g auth.Grant, columns ...any) (int64, error) {
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	value, err := b.Aggregate(ctx, g, "count", columns...)
	if err != nil {
		return 0, err
	}
	return toInt64(value), nil
}

// Aggregate runs function (count, sum, min, max, avg) over columns and returns
// the result.
func (b *Builder) Aggregate(ctx context.Context, g auth.Grant, function string, columns ...any) (any, error) {
	prepared, err := prepare(b, g)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	return runAggregate(prepared, ctx, function, columns)
}

// Exists reports whether the query matches any row.
func (b *Builder) Exists(ctx context.Context, g auth.Grant) (bool, error) {
	count, err := b.Count(ctx, g)
	return count > 0, err
}

// Create returns a new row, filled with attributes and saved.
//
// A tenant in attributes is ignored, as Fill ignores it. The row is written with
// the Grant's tenant, and the entity returned carries that tenant.
func (b *Builder) Create(ctx context.Context, g auth.Grant, attributes map[string]any) (Entity, error) {
	instance, err := create(b, ctx, g, attributes)
	if err != nil {
		return nil, err
	}
	return instance.r.self, nil
}

// create is Create with the model still in hand.
func create(b *Builder, ctx context.Context, g auth.Grant, attributes map[string]any) (*Model, error) {
	instance, err := newModelInstance(b, attributes)
	if err != nil {
		return nil, err
	}
	if _, err := instance.Save(ctx, g); err != nil {
		return nil, err
	}
	return instance, nil
}

// ForceCreate returns a new row, filled with attributes via ForceFill and saved.
//
// There is no mass-assignment guard to turn off (see the package comment); what
// ForceCreate keeps from Create is ForceFill's behavior: an attribute the entity
// does not declare is carried through as a raw attribute instead of dropped.
func (b *Builder) ForceCreate(ctx context.Context, g auth.Grant, attributes map[string]any) (Entity, error) {
	instance := b.table.newModel(b.conn)
	if err := instance.ForceFill(mergeMaps(b.pendingAttributes, attributes)); err != nil {
		return nil, err
	}
	if _, err := instance.Save(ctx, g); err != nil {
		return nil, err
	}
	return instance.r.self, nil
}

// Insert writes values as new rows.
//
// The tenant column is written from the Grant on every row, overwriting whatever
// the caller put there: the tenant comes from the Grant and from nowhere else.
func (b *Builder) Insert(ctx context.Context, g auth.Grant, values ...map[string]any) (bool, error) {
	prepared, rows, err := prepareWrite(b, g, values)
	if err != nil {
		return false, err
	}
	return runInsert(prepared, ctx, rows)
}

// InsertGetID inserts values as one new row and returns the value generated for
// sequence.
func (b *Builder) InsertGetID(ctx context.Context, g auth.Grant, values map[string]any, sequence string) (int64, error) {
	prepared, rows, err := prepareWrite(b, g, []map[string]any{values})
	if err != nil {
		return 0, err
	}
	return runInsertGetID(prepared, ctx, rows[0], sequence)
}

// writable refuses a write with a Grant that only reads -- one issued to a
// subject somebody else is viewing as. Every statement this package issues
// that writes passes here first, so viewing an account cannot change it
// whatever the screen offered.
func writable(b *Builder, g auth.Grant) error {
	if err := auth.Writable(g); err != nil {
		return fmt.Errorf("%w (writing %s)", err, b.table.name)
	}
	return nil
}

// prepareWrite is prepare for a statement that carries values: the Grant is
// checked, the scopes are applied, and the tenant is written into every row.
func prepareWrite(b *Builder, g auth.Grant, values []map[string]any) (*Builder, []map[string]any, error) {
	if err := writable(b, g); err != nil {
		return nil, nil, err
	}
	prepared, err := prepare(b, g)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]map[string]any, 0, len(values))
	for _, row := range values {
		if column := b.table.tenantColumn; column != "" {
			row, _ = withoutColumn(row, column)
			row[column] = auth.Tenant(g)
		} else {
			row = copyMap(row)
		}
		rows = append(rows, row)
	}
	return prepared, rows, nil
}

// Update runs an UPDATE with values over the query as it stands, and returns
// the number of rows affected.
//
// A value under the table's tenant column, spelled any way query.NamesColumn
// recognises, is replaced by the Grant's tenant rather than written: the row
// stays with the tenant whose Grant reached it. It holds for Save, Increment and
// every other write that ends here.
func (b *Builder) Update(ctx context.Context, g auth.Grant, values map[string]any) (int64, error) {
	if err := writable(b, g); err != nil {
		return 0, err
	}
	prepared, err := prepare(b, g)
	if err != nil {
		return 0, err
	}
	if column := b.table.tenantColumn; column != "" {
		var named bool
		if values, named = withoutColumn(values, column); named {
			values[column] = auth.Tenant(g)
		}
	}
	return runUpdate(prepared, ctx, addUpdatedAtColumn(prepared, values))
}

// addUpdatedAtColumn adds the updated-at timestamp to values when the table
// uses timestamps and the caller did not already set it.
//
// The column is re-keyed to <table>.<column>, so that an update with a join
// names the table it means. The grammars that cannot take a qualified name on
// the left of a SET -- Postgres and SQLite -- strip it again before compiling.
func addUpdatedAtColumn(b *Builder, values map[string]any) map[string]any {
	column := b.table.updatedAt
	if !b.table.timestamps || column == "" {
		return values
	}

	out := copyMap(values)
	if _, ok := out[column]; !ok {
		if _, qualified := out[b.qualify(column)]; qualified {
			return out
		}
		out[column] = freshTimestamp()
	}

	value := out[column]
	delete(out, column)
	out[tableOf(b.query.GetFrom())+"."+column] = value
	return out
}

// tableOf returns the table name from a FROM value, dropping any " as alias"
// suffix.
func tableOf(from any) string {
	name := fmt.Sprint(from)
	if i := strings.Index(strings.ToLower(name), " as "); i >= 0 {
		return strings.TrimSpace(name[i+4:])
	}
	return strings.TrimSpace(name)
}

// Upsert inserts values, updating the columns in update on any row that
// conflicts on uniqueBy. With no update columns given, every column is updated.
func (b *Builder) Upsert(ctx context.Context, g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if len(uniqueBy) == 0 {
		return 0, fmt.Errorf("model: the unique columns must not be empty")
	}
	prepared, rows, err := prepareWrite(b, g, values)
	if err != nil {
		return 0, err
	}
	if update == nil {
		update = sortedKeys(rows[0])
	}
	if column := b.table.updatedAt; b.table.timestamps && column != "" {
		now := freshTimestamp()
		for _, row := range rows {
			if _, ok := row[column]; !ok {
				row[column] = now
			}
		}
		if !slices.Contains(update, column) {
			update = append(update, column)
		}
	}
	return runUpsert(prepared, ctx, rows, uniqueBy, update)
}

// Increment adds amount to column, plus any extra columns to set, and returns
// the number of rows affected.
func (b *Builder) Increment(ctx context.Context, g auth.Grant, column string, amount any, extra map[string]any) (int64, error) {
	return incrementOrDecrement(b, ctx, g, column, amount, extra, "+")
}

// Decrement subtracts amount from column, plus any extra columns to set, and
// returns the number of rows affected.
func (b *Builder) Decrement(ctx context.Context, g auth.Grant, column string, amount any, extra map[string]any) (int64, error) {
	return incrementOrDecrement(b, ctx, g, column, amount, extra, "-")
}

// incrementOrDecrement is the shared body of Increment and Decrement.
//
// The amount goes into the SQL rather than into a binding, because it is on the
// right of an assignment to the column itself. That is why a non-numeric amount
// is refused: one that came from a request would otherwise be SQL.
func incrementOrDecrement(b *Builder, ctx context.Context, g auth.Grant, column string, amount any, extra map[string]any, sign string) (int64, error) {
	if amount == nil {
		amount = 1
	}
	if !isNumeric(reflect.ValueOf(amount).Kind()) {
		return 0, fmt.Errorf("model: non-numeric value passed to increment or decrement on %s.%s: the amount is compiled into the statement, so it cannot come from anywhere a value can", b.table.name, column)
	}

	values := copyMap(extra)
	wrapped := b.conn.grammar.Wrap(column)
	values[column] = query.Raw(fmt.Sprintf("%s %s %v", wrapped, sign, amount))
	return b.Update(ctx, g, values)
}

// Delete removes the rows matching the query, or runs the registered onDelete
// callback instead.
//
// A table that soft deletes has an onDelete callback on its builder, so this
// runs the update that stamps the rows instead of a delete.
func (b *Builder) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	if err := writable(b, g); err != nil {
		return 0, err
	}
	if b.onDelete != nil {
		return b.onDelete(ctx, b, g)
	}
	prepared, err := prepare(b, g)
	if err != nil {
		return 0, err
	}
	return runDelete(prepared, ctx)
}

// ForceDelete removes the rows, whatever the soft delete would have done.
//
// It still applies the tenant filter. "Force" is about the soft delete, never
// about the tenant.
func (b *Builder) ForceDelete(ctx context.Context, g auth.Grant) (int64, error) {
	// The held error is read here rather than in prepare, because this is one of
	// the two methods that does not go through prepare. Without this line a
	// builder invalidated by WithTrashed on a table that does not soft delete
	// still issues the DELETE -- against the contract this file states about
	// itself, which is that nothing is ever executed with an error waiting.
	if b.err != nil {
		return 0, b.err
	}

	if err := writable(b, g); err != nil {
		return 0, err
	}
	tenant := auth.Tenant(g)
	if tenant == "" || !auth.ValidTenant(tenant) {
		return 0, ErrNoTenant
	}
	forced := clone(b)
	if err := scopeToTenant(forced, g, tenant); err != nil {
		return 0, err
	}
	return runDelete(forced, ctx)
}

// OnDelete registers callback as the override Delete runs instead of a plain
// delete.
func (b *Builder) OnDelete(callback func(context.Context, *Builder, auth.Grant) (int64, error)) *Builder {
	b.onDelete = callback
	return b
}

// Touch sets column, or the table's updated-at column when none is given, to
// the current time on every row matching the query.
func (b *Builder) Touch(ctx context.Context, g auth.Grant, column ...string) (int64, error) {
	name := b.table.updatedAt
	if len(column) > 0 && column[0] != "" {
		name = column[0]
	} else if !b.table.timestamps || name == "" {
		return 0, nil
	}
	return b.Update(ctx, g, map[string]any{name: freshTimestamp()})
}

// With marks relations to eager load.
func (b *Builder) With(relations ...string) *Builder {
	for _, relation := range relations {
		if relation == "" {
			continue
		}
		b.eagerLoad[relation] = nil
	}
	return b
}

// WithConstraints marks relation to eager load, constrained by callback.
//
// The callback takes a query.Builder, because the relation it constrains is a
// query on another table and a caller narrowing it is writing wheres.
func (b *Builder) WithConstraints(relation string, constraints func(*query.Builder)) *Builder {
	b.eagerLoad[relation] = constraints
	return b
}

// WithOnly replaces the eager load list with relations.
func (b *Builder) WithOnly(relations ...string) *Builder {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b.With(relations...)
}

// Without removes relations from the eager load list.
func (b *Builder) Without(relations ...string) *Builder {
	for _, relation := range relations {
		delete(b.eagerLoad, relation)
	}
	return b
}

// GetEagerLoads returns the names marked to eager load, sorted.
func (b *Builder) GetEagerLoads() []string {
	out := make([]string, 0, len(b.eagerLoad))
	for name := range b.eagerLoad {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// WithoutEagerLoads clears the eager load list.
func (b *Builder) WithoutEagerLoads() *Builder {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b
}

// WithoutEagerLoad removes these relations, and the ones nested under them,
// from the eager load list.
func (b *Builder) WithoutEagerLoad(relations ...string) *Builder {
	for name := range b.eagerLoad {
		for _, relation := range relations {
			if name == relation || strings.HasPrefix(name, relation+".") {
				delete(b.eagerLoad, name)
			}
		}
	}
	return b
}

// SetEagerLoads replaces the eager load list with relations.
func (b *Builder) SetEagerLoads(relations ...string) *Builder {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b.With(relations...)
}

// FindSole returns the row with the given primary key, and fails unless it is
// the only one.
func (b *Builder) FindSole(ctx context.Context, g auth.Grant, id any, columns ...any) (Entity, error) {
	return b.WhereKey(id).Sole(ctx, g, columns...)
}

// TouchQuietly touches the query's rows without firing model events.
func (b *Builder) TouchQuietly(ctx context.Context, g auth.Grant, column ...string) (touched int64, err error) {
	return touched, b.GetModel().WithoutEvents(func() error {
		touched, err = b.Touch(ctx, g, column...)
		return err
	})
}

// EagerLoadRelations loads every top-level relation marked with With, and
// attaches the matches to each row.
//
// Every row has to be one the framework built: a literal has no model to attach
// a relation to, and the load refuses with ErrUnwired rather than reporting
// success having loaded onto nothing.
func (b *Builder) EagerLoadRelations(ctx context.Context, g auth.Grant, rows Rows) error {
	if _, err := rows.modelsOrFail(); err != nil {
		return err
	}
	return eagerLoadRelations(b, ctx, g, rows)
}

// eagerLoadRelations is EagerLoadRelations on rows already known to be wired.
//
// The loading itself is relations.EagerLoadRelation, which is four calls and is
// where eager loading is implemented. Writing it again here would be the second
// implementation of the one thing that turns N queries into two, and the two
// would drift on the case neither author tested -- the morph-to that matches its
// own parents, the relation whose local key is not the primary key.
//
// The rows are handed over as refs, which is the same model seen through the
// interface a relation consumes: what the relation sets is set on the row the
// caller holds.
func eagerLoadRelations(b *Builder, ctx context.Context, g auth.Grant, rows Rows) error {
	if len(rows) == 0 {
		return nil
	}

	var refs []relations.Model
	for _, name := range b.GetEagerLoads() {
		if strings.Contains(name, ".") {
			// A nested eager load is loaded by the query that fetches its parent,
			// which is where the models it hangs off are hydrated.
			continue
		}
		relation, err := b.GetRelationWithoutConstraints(name)
		if err != nil {
			return err
		}
		if refs == nil {
			refs = refsOf(rows)
		}
		if _, err := relations.EagerLoadRelation(ctx, g, refs, name, relation, onRelationQuery(b.eagerLoad[name])); err != nil {
			return err
		}
	}
	return nil
}

// onRelationQuery adapts what With recorded to what the eager loader applies.
//
// WithConstraints takes func(*query.Builder) because a caller narrowing an eager
// load is writing wheres, and the loader hands the relation itself so that a
// constraint could reach more than its query. This is the step between, and it
// is applied after the batch's own `in (...)` for the reason the loader
// documents.
func onRelationQuery(callback func(*query.Builder)) func(relations.Relation) {
	if callback == nil {
		return nil
	}
	return func(rel relations.Relation) { callback(rel.GetQuery().GetQuery()) }
}

// Limit sets the row limit, forwarded to the underlying query.
func (b *Builder) Limit(value int) *Builder {
	b.query.Limit(value)
	return b
}

// Offset sets the row offset, forwarded to the underlying query.
func (b *Builder) Offset(value int) *Builder {
	b.query.Offset(value)
	return b
}

// ForPage sets the limit and offset for page, perPage rows at a time.
func (b *Builder) ForPage(page, perPage int) *Builder {
	b.query.ForPage(page, perPage)
	return b
}

// GetLimit returns the row limit, or nil when none is set.
func (b *Builder) GetLimit() *int { return b.query.GetLimit() }

// GetOffset returns the row offset, or nil when none is set.
func (b *Builder) GetOffset() *int { return b.query.GetOffset() }

// OrderBy adds an ordering, forwarded to the underlying query.
func (b *Builder) OrderBy(column any, direction ...string) *Builder {
	b.query.OrderBy(column, direction...)
	return b
}

// OrderByDesc adds a descending ordering, forwarded to the underlying query.
func (b *Builder) OrderByDesc(column any) *Builder {
	b.query.OrderByDesc(column)
	return b
}

// Select sets the columns to return, forwarded to the underlying query.
func (b *Builder) Select(columns ...any) *Builder {
	b.query.Select(columns...)
	return b
}

// AddSelect adds columns to the ones already selected.
func (b *Builder) AddSelect(columns ...any) *Builder {
	b.query.AddSelect(columns...)
	return b
}

// SelectRaw adds a raw select expression, forwarded to the underlying query.
func (b *Builder) SelectRaw(expression string, bindings ...any) *Builder {
	b.query.SelectRaw(expression, bindings...)
	return b
}

// enforceOrderBy adds an ascending order by the table's key, qualifiedKey, when
// the query has none: chunking without an order is chunking over a set the
// engine may return in a different order each time.
func enforceOrderBy(q *query.Builder, qualifiedKey string) {
	if len(q.Orders) == 0 && len(q.UnionOrders) == 0 {
		q.OrderBy(qualifiedKey, "asc")
	}
}

// DefaultKeyName returns the table's primary key name.
func (b *Builder) DefaultKeyName() string { return b.table.keyName }

// Chunk walks the query count rows at a time, calling callback for each chunk.
//
// The callback stops the walk by returning false, and stops it with a reason by
// returning an error.
func (b *Builder) Chunk(ctx context.Context, g auth.Grant, count int, callback func(Rows, int) (bool, error)) error {
	if count < 1 {
		return fmt.Errorf("model: the chunk size should be at least 1")
	}
	enforceOrderBy(b.query, b.qualify(b.table.keyName))

	skip := 0
	if offset := b.GetOffset(); offset != nil {
		skip = *offset
	}
	remaining := -1
	if limit := b.GetLimit(); limit != nil {
		remaining = *limit
	}

	for page := 1; ; page++ {
		size := count
		if remaining >= 0 {
			size = min(count, remaining)
		}
		if size == 0 {
			return nil
		}

		results, err := clone(b).Offset((page-1)*count+skip).Limit(size).Get(ctx, g)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			return nil
		}
		if remaining >= 0 {
			remaining = max(remaining-len(results), 0)
		}
		keepGoing, err := callback(results, page)
		if err != nil {
			return err
		}
		if !keepGoing || len(results) != count {
			return nil
		}
	}
}

// Each walks the query count rows at a time, calling callback for every row with
// its overall index.
func (b *Builder) Each(ctx context.Context, g auth.Grant, count int, callback func(Entity, int) (bool, error)) error {
	return b.Chunk(ctx, g, count, func(rows Rows, page int) (bool, error) {
		for i, e := range rows {
			keepGoing, err := callback(e, (page-1)*count+i)
			if err != nil || !keepGoing {
				return false, err
			}
		}
		return true, nil
	})
}

// ChunkById walks the query count rows at a time, ordered and paged by the key
// rather than by offset, so that a row inserted between two chunks cannot shift
// the window and hide a row.
func (b *Builder) ChunkById(ctx context.Context, g auth.Grant, count int, callback func(Rows, int) (bool, error), column ...string) error {
	if count < 1 {
		return fmt.Errorf("model: the chunk size should be at least 1")
	}
	name := b.table.keyName
	if len(column) > 0 && column[0] != "" {
		name = column[0]
	}

	var lastID any
	for page := 1; ; page++ {
		q := clone(b)
		if lastID != nil {
			q.Where(b.qualify(name), ">", lastID)
		}
		found, err := q.OrderBy(b.qualify(name), "asc").Limit(count).get(ctx, g)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return nil
		}
		// The key the next page resumes from is read before the callback sees
		// the rows, so a callback that replaces them cannot move the walk.
		lastID = found[len(found)-1].base().GetAttribute(name)
		keepGoing, err := callback(q.ApplyAfterQueryCallbacks(found), page)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
		if lastID == nil {
			return fmt.Errorf("model: chunkById stopped because %s is not in the result", name)
		}
		if len(found) != count {
			return nil
		}
	}
}

// Lazy walks the rows, fetched a chunk at a time, one row at a time.
//
// The error a chunk stopped on arrives beside the row it would have been, as
// iter.Seq2 carries it throughout this collection, because a walk can fail after
// it has already handed out rows.
func (b *Builder) Lazy(ctx context.Context, g auth.Grant, chunkSize int) iter.Seq2[Entity, error] {
	return func(yield func(Entity, error) bool) {
		stopped := false
		err := b.Chunk(ctx, g, chunkSize, func(rows Rows, _ int) (bool, error) {
			for _, e := range rows {
				if !yield(e, nil) {
					stopped = true
					return false, nil
				}
			}
			return true, nil
		})
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// LazyById walks the rows, fetched a chunk at a time and paged by the key
// rather than by offset, one row at a time. See Lazy for the error.
func (b *Builder) LazyById(ctx context.Context, g auth.Grant, chunkSize int, column ...string) iter.Seq2[Entity, error] {
	return func(yield func(Entity, error) bool) {
		stopped := false
		err := b.ChunkById(ctx, g, chunkSize, func(rows Rows, _ int) (bool, error) {
			for _, e := range rows {
				if !yield(e, nil) {
					stopped = true
					return false, nil
				}
			}
			return true, nil
		}, column...)
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// Cursor walks the result one row at a time.
//
// query.Connection hands back the records it read, so this is a walk over one
// result set rather than a second way to run a query -- and it stays lazy from
// the caller's side, which is what the method is for. A failure arrives beside
// the row, as the only value the walk yields.
func (b *Builder) Cursor(ctx context.Context, g auth.Grant) iter.Seq2[Entity, error] {
	return func(yield func(Entity, error) bool) {
		rows, err := b.Get(ctx, g)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, e := range rows {
			if !yield(e, nil) {
				return
			}
		}
	}
}

func uniqueValues(values []any) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		if !slices.ContainsFunc(out, func(seen any) bool { return seen == value }) {
			out = append(out, value)
		}
	}
	return out
}

func mergeMaps(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for key, value := range m {
			out[key] = value
		}
	}
	return out
}

func afterLastDot(column string) string {
	if i := strings.LastIndex(column, "."); i >= 0 {
		return column[i+1:]
	}
	return column
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case []byte:
		return parseInt64(string(v))
	case string:
		return parseInt64(v)
	}
	return 0
}

func parseInt64(s string) int64 {
	var out int64
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		out = out*10 + int64(c-'0')
	}
	if negative {
		return -out
	}
	return out
}

// withoutColumn copies values without any key that names column, as
// query.NamesColumn compares them, and reports whether one was there.
func withoutColumn(values map[string]any, column string) (map[string]any, bool) {
	out := make(map[string]any, len(values)+1)
	found := false
	for key, value := range values {
		if query.NamesColumn(key, column) {
			found = true
			continue
		}
		out[key] = value
	}
	return out, found
}
