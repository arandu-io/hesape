package eloquent

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Builder answers Illuminate\Database\Eloquent\Builder: the query builder that
// hands back models instead of rows.
//
// Everything that runs takes an auth.Grant and filters by auth.Tenant(g) --
// reads exactly like writes (RULE 17). Everything that only builds does not: a
// Where or an OrderBy is a fragment of SQL, and a fragment authorizes nothing.
type Builder[T any] struct {
	query *query.Builder
	model *Model[T]

	eagerLoad     map[string]func(*query.Builder)
	scopes        map[string]Scope[T]
	removedScopes []string

	onDelete            func(*Builder[T], auth.Grant) (int64, error)
	afterQueryCallbacks []func(Collection[T]) Collection[T]
	onCloneCallbacks    []func(*Builder[T])
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
	// PHP throws RelationNotFoundException while the query is being built. A Go
	// method that returned an error could not be chained, and a chain that has to
	// be broken every second call is a chain nobody writes -- so the error is
	// held and returned by the first method that runs. Nothing is ever executed
	// with an error waiting.
	err error
}

// fail records an error for the first method that runs to report. See
// Builder.err.
func (b *Builder[T]) fail(err error) *Builder[T] {
	if b.err == nil {
		b.err = err
	}
	return b
}

// NewBuilder answers Builder::__construct. The model comes after, through
// SetModel, exactly as it does there.
func NewBuilder[T any](q *query.Builder) *Builder[T] {
	return &Builder[T]{query: q, eagerLoad: map[string]func(*query.Builder){}}
}

// NewModelQuery answers Model::newModelQuery: a builder with no global scopes
// and no eager loads.
func (m *Model[T]) NewModelQuery() *Builder[T] {
	q := query.NewBuilder(m.Connection, m.Grammar, m.Processor)
	q.From(m.GetTable())
	return NewBuilder[T](q).SetModel(m)
}

// NewQuery answers Model::newQuery: the model's query with its global scopes on.
func (m *Model[T]) NewQuery() *Builder[T] {
	return m.RegisterGlobalScopes(m.NewQueryWithoutScopes())
}

// Query answers Model::query. In PHP it is the static form of newQuery; a Go
// generic type has no statics, so the two are the same call on the model the
// application built.
func (m *Model[T]) Query() *Builder[T] { return m.NewQuery() }

// NewQueryWithoutScopes answers Model::newQueryWithoutScopes.
func (m *Model[T]) NewQueryWithoutScopes() *Builder[T] { return m.NewModelQuery() }

// NewQueryWithoutRelationships answers Model::newQueryWithoutRelationships.
func (m *Model[T]) NewQueryWithoutRelationships() *Builder[T] {
	return m.RegisterGlobalScopes(m.NewModelQuery())
}

// NewQueryWithoutScope answers Model::newQueryWithoutScope.
func (m *Model[T]) NewQueryWithoutScope(identifier string) *Builder[T] {
	return m.NewQuery().WithoutGlobalScope(identifier)
}

// On answers Model::on: the same query, on another connection.
//
// The PHP takes the connection's name and resolves it; with no resolver
// (ADR 0001) the connection comes with the name.
func (m *Model[T]) On(name string, connection query.Connection) *Builder[T] {
	instance, err := m.NewInstance(nil, false)
	if err != nil {
		// NewInstance can only fail on a value that does not fit a field, and
		// nothing is being filled here.
		instance = m
	}
	return instance.SetConnection(name, connection).NewQuery()
}

// RegisterGlobalScopes answers Model::registerGlobalScopes.
//
// It also registers the SoftDeletingScope when the model soft deletes, which in
// PHP is what the trait's bootSoftDeletes does at class boot. A Go value has no
// boot, so the registration happens where the scopes are collected.
func (m *Model[T]) RegisterGlobalScopes(b *Builder[T]) *Builder[T] {
	if m.SoftDeletes && !m.HasGlobalScope(SoftDeletingScopeName) {
		m.AddGlobalScope(SoftDeletingScopeName, &SoftDeletingScope[T]{})
	}
	for identifier, scope := range m.globalScopes {
		b.WithGlobalScope(identifier, scope)
	}
	return b
}

// All answers Model::all.
func (m *Model[T]) All(g auth.Grant, columns ...any) (Collection[T], error) {
	return m.NewQuery().Get(g, columns...)
}

// SetModel answers Builder::setModel.
func (b *Builder[T]) SetModel(model *Model[T]) *Builder[T] {
	b.model = model
	b.query.From(model.GetTable())
	return b
}

// GetModel answers Builder::getModel.
func (b *Builder[T]) GetModel() *Model[T] { return b.model }

// GetQuery answers Builder::getQuery: the underlying query builder.
func (b *Builder[T]) GetQuery() *query.Builder { return b.query }

// SetQuery answers Builder::setQuery.
func (b *Builder[T]) SetQuery(q *query.Builder) *Builder[T] {
	b.query = q
	return b
}

// ToBase answers Builder::toBase: the query builder with the scopes applied.
//
// It takes the Grant because applying the scopes is also where the tenant filter
// goes on, and a base builder handed out without it is a query somebody will run
// (RULE 17).
func (b *Builder[T]) ToBase(g auth.Grant) (*query.Builder, error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return nil, err
	}
	return prepared.query, nil
}

// Qualify answers Builder::qualifyColumn.
func (b *Builder[T]) Qualify(column string) string { return b.model.QualifyColumn(column) }

// NewModelInstance answers Builder::newModelInstance.
func (b *Builder[T]) NewModelInstance(attributes map[string]any) (*Model[T], error) {
	merged := copyMap(b.pendingAttributes)
	for key, value := range attributes {
		merged[key] = value
	}
	return b.model.NewInstance(merged, false)
}

// clone answers Builder::__clone.
func (b *Builder[T]) clone() *Builder[T] {
	out := &Builder[T]{
		query:               b.query.Clone(),
		model:               b.model,
		eagerLoad:           map[string]func(*query.Builder){},
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

// Clone answers Builder::clone.
func (b *Builder[T]) Clone() *Builder[T] { return b.clone() }

// prepare is where a query becomes runnable: the global scopes go on, and then
// the tenant filter (RULE 14, RULE 17).
//
// The tenant is read off the Grant and never from anywhere else. A Grant with no
// tenant -- the zero Grant, which is the only one constructible outside the auth
// package -- is refused here, before any SQL exists.
//
// The wheres already on the query are wrapped in one group before the tenant
// filter is appended, and that is not tidiness. `where a or b` with `and
// tenant = ?` appended reads as `a or (b and tenant = ?)`, so every row matching
// a comes back whoever it belongs to. Grouping first is what makes the filter
// mean what it says.
func (b *Builder[T]) prepare(g auth.Grant) (*Builder[T], error) {
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

	if column := prepared.model.TenantColumn; column != "" {
		isolateWheres(prepared.query)
		prepared.query.Where(prepared.model.QualifyColumn(column), "=", tenant)
	}
	return prepared, nil
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

// WithGlobalScope answers Builder::withGlobalScope.
func (b *Builder[T]) WithGlobalScope(identifier string, scope Scope[T]) *Builder[T] {
	if b.scopes == nil {
		b.scopes = map[string]Scope[T]{}
	}
	b.scopes[identifier] = scope
	if extender, ok := scope.(ScopeExtender[T]); ok {
		extender.Extend(b)
	}
	return b
}

// WithoutGlobalScope answers Builder::withoutGlobalScope.
func (b *Builder[T]) WithoutGlobalScope(identifier string) *Builder[T] {
	delete(b.scopes, identifier)
	b.removedScopes = append(b.removedScopes, identifier)
	return b
}

// WithoutGlobalScopes answers Builder::withoutGlobalScopes. With no argument it
// removes them all.
func (b *Builder[T]) WithoutGlobalScopes(identifiers ...string) *Builder[T] {
	if len(identifiers) == 0 {
		for identifier := range b.scopes {
			identifiers = append(identifiers, identifier)
		}
	}
	for _, identifier := range identifiers {
		b.WithoutGlobalScope(identifier)
	}
	return b
}

// RemovedScopes answers Builder::removedScopes.
func (b *Builder[T]) RemovedScopes() []string { return slices.Clone(b.removedScopes) }

// ApplyScopes answers Builder::applyScopes: a copy of the builder with every
// registered scope applied.
//
// The wheres a scope adds are wrapped in a group when either side carries an or,
// which is addNewWheresWithinGroup: without it a scope's filter joins an or
// chain and stops filtering.
func (b *Builder[T]) ApplyScopes() *Builder[T] {
	if len(b.scopes) == 0 {
		return b.clone()
	}
	out := b.clone()
	for _, identifier := range sortedScopeNames(b.scopes) {
		scope := b.scopes[identifier]
		before := len(out.query.Wheres)
		scope.Apply(out, out.model)
		groupNewWheres(out.query, before)
	}
	return out
}

// sortedScopeNames keeps the order scopes are applied in stable. PHP iterates an
// ordered array; a Go map has no order, and two scopes that disagree about the
// order they run in are a bug that only shows up sometimes.
func sortedScopeNames[T any](scopes map[string]Scope[T]) []string {
	out := make([]string, 0, len(scopes))
	for identifier := range scopes {
		out = append(out, identifier)
	}
	slices.Sort(out)
	return out
}

// groupNewWheres answers Builder::addNewWheresWithinGroup.
func groupNewWheres(q *query.Builder, originalCount int) {
	if len(q.Wheres) == originalCount {
		return
	}
	all := q.Wheres
	q.Wheres = nil
	groupWhereSliceForScope(q, all[:originalCount])
	groupWhereSliceForScope(q, all[originalCount:])
}

// groupWhereSliceForScope answers Builder::groupWhereSliceForScope.
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

// Where answers Builder::where.
func (b *Builder[T]) Where(column any, args ...any) *Builder[T] {
	if nested, ok := column.(func(*Builder[T])); ok {
		return b.whereNested(nested, "and")
	}
	b.query.Where(column, args...)
	return b
}

// OrWhere answers Builder::orWhere.
func (b *Builder[T]) OrWhere(column any, args ...any) *Builder[T] {
	if nested, ok := column.(func(*Builder[T])); ok {
		return b.whereNested(nested, "or")
	}
	b.query.OrWhere(column, args...)
	return b
}

// whereNested answers the Closure branch of Builder::where: the callback gets a
// builder of this model, and what it adds becomes one group.
func (b *Builder[T]) whereNested(callback func(*Builder[T]), boolean string) *Builder[T] {
	nested := b.model.NewQueryWithoutRelationships()
	callback(nested)
	if nested.err != nil {
		// The nested builder is thrown away once its wheres are merged, so an
		// error left on it would never reach anybody.
		b.fail(nested.err)
	}
	for name, constraints := range nested.eagerLoad {
		b.eagerLoad[name] = constraints
	}
	b.WithoutGlobalScopes(nested.removedScopes...)
	b.query.AddNestedWhereQuery(nested.query, boolean)
	return b
}

// WhereNot answers Builder::whereNot.
func (b *Builder[T]) WhereNot(column any, args ...any) *Builder[T] {
	before := len(b.query.Wheres)
	b.Where(func(nested *Builder[T]) {
		nested.Where(column, args...)
	})
	return b.negateLastWhere(before)
}

// negateLastWhere flips the group WhereNot just added, which PHP spells by
// passing "and not" as the boolean.
//
// It negates nothing when the group turned out empty -- an empty nested where is
// dropped rather than compiled, and negating whatever came before it would
// change a clause the caller did not write.
func (b *Builder[T]) negateLastWhere(before int) *Builder[T] {
	if len(b.query.Wheres) == before {
		return b
	}
	last := &b.query.Wheres[len(b.query.Wheres)-1]
	if !strings.Contains(last.Boolean, "not") {
		last.Boolean += " not"
	}
	return b
}

// OrWhereNot answers Builder::orWhereNot.
func (b *Builder[T]) OrWhereNot(column any, args ...any) *Builder[T] {
	before := len(b.query.Wheres)
	b.OrWhere(func(nested *Builder[T]) {
		nested.Where(column, args...)
	})
	return b.negateLastWhere(before)
}

// WhereKey answers Builder::whereKey.
func (b *Builder[T]) WhereKey(id any) *Builder[T] {
	if ids, ok := id.([]any); ok {
		b.query.WhereIn(b.model.GetQualifiedKeyName(), ids)
		return b
	}
	return b.Where(b.model.GetQualifiedKeyName(), "=", id)
}

// WhereKeyNot answers Builder::whereKeyNot.
func (b *Builder[T]) WhereKeyNot(id any) *Builder[T] {
	if ids, ok := id.([]any); ok {
		b.query.WhereNotIn(b.model.GetQualifiedKeyName(), ids)
		return b
	}
	return b.Where(b.model.GetQualifiedKeyName(), "!=", id)
}

// Latest answers Builder::latest.
func (b *Builder[T]) Latest(column ...string) *Builder[T] {
	b.query.Latest(timestampColumn(b.model.GetCreatedAtColumn(), column))
	return b
}

// Oldest answers Builder::oldest.
func (b *Builder[T]) Oldest(column ...string) *Builder[T] {
	b.query.Oldest(timestampColumn(b.model.GetCreatedAtColumn(), column))
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

// Hydrate answers Builder::hydrate: rows in, models out.
func (b *Builder[T]) Hydrate(items []query.Record) (Collection[T], error) {
	out := make(Collection[T], 0, len(items))
	for _, item := range items {
		model, err := b.model.NewFromBuilder(item)
		if err != nil {
			return nil, err
		}
		out = append(out, model)
	}
	return out, nil
}

// FromQuery answers Builder::fromQuery: models from SQL somebody wrote by hand.
//
// It takes the Grant like every other read. The SQL is the caller's, so the
// tenant cannot be added to it -- which is exactly why the Grant is still
// required: a query nobody authorized does not run, and the where clause that
// scopes it is the caller's to write (RULE 17).
func (b *Builder[T]) FromQuery(g auth.Grant, sql string, bindings []any) (Collection[T], error) {
	if tenant := auth.Tenant(g); tenant == "" || !auth.ValidTenant(tenant) {
		return nil, ErrNoTenant
	}
	rows, err := b.model.Connection.Select(sql, bindings, true)
	if err != nil {
		return nil, fmt.Errorf("eloquent: selecting from %s: %w", b.model.GetTable(), err)
	}
	return b.Hydrate(rows)
}

// Get answers Builder::get.
func (b *Builder[T]) Get(g auth.Grant, columns ...any) (Collection[T], error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return nil, err
	}
	models, err := prepared.GetModels(g, columns...)
	if err != nil {
		return nil, err
	}
	if len(models) > 0 {
		if err := prepared.EagerLoadRelations(g, models); err != nil {
			return nil, err
		}
	}
	return prepared.ApplyAfterQueryCallbacks(models), nil
}

// GetModels answers Builder::getModels: the rows, hydrated, with nothing eager
// loaded.
//
// It is already prepared when Get calls it; called on its own it prepares
// itself, so there is no way to reach the rows without the Grant.
func (b *Builder[T]) GetModels(g auth.Grant, columns ...any) (Collection[T], error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return nil, err
	}
	if len(columns) > 0 && prepared.query.Columns == nil {
		prepared.query.Select(columns...)
	}
	rows, err := prepared.runSelect()
	if err != nil {
		return nil, err
	}
	return prepared.Hydrate(rows)
}

// AfterQuery answers Builder::afterQuery.
func (b *Builder[T]) AfterQuery(callback func(Collection[T]) Collection[T]) *Builder[T] {
	b.afterQueryCallbacks = append(b.afterQueryCallbacks, callback)
	return b
}

// ApplyAfterQueryCallbacks answers Builder::applyAfterQueryCallbacks.
func (b *Builder[T]) ApplyAfterQueryCallbacks(result Collection[T]) Collection[T] {
	for _, callback := range b.afterQueryCallbacks {
		if next := callback(result); next != nil {
			result = next
		}
	}
	return result
}

// First answers BuildsQueries::first.
//
// It answers (nil, nil) where the PHP answers null: no row is not a failure, and
// FirstOrFail is the spelling for when it is.
func (b *Builder[T]) First(g auth.Grant, columns ...any) (*Model[T], error) {
	models, err := b.Limit(1).Get(g, columns...)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	return models[0], nil
}

// FirstOrFail answers Builder::firstOrFail.
func (b *Builder[T]) FirstOrFail(g auth.Grant, columns ...any) (*Model[T], error) {
	model, err := b.First(g, columns...)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, modelNotFound(b.model.GetTable())
	}
	return model, nil
}

// FirstOr answers Builder::firstOr: the first row, or what the callback makes.
func (b *Builder[T]) FirstOr(g auth.Grant, callback func() (*Model[T], error), columns ...any) (*Model[T], error) {
	model, err := b.First(g, columns...)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return callback()
}

// FirstWhere answers Builder::firstWhere.
func (b *Builder[T]) FirstWhere(g auth.Grant, column any, args ...any) (*Model[T], error) {
	return b.Where(column, args...).First(g)
}

// Sole answers Builder::sole: the row, when it is the only one.
func (b *Builder[T]) Sole(g auth.Grant, columns ...any) (*Model[T], error) {
	models, err := b.Limit(2).Get(g, columns...)
	if err != nil {
		return nil, err
	}
	switch len(models) {
	case 0:
		return nil, modelNotFound(b.model.GetTable())
	case 1:
		return models[0], nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrMultipleRecordsFound, b.model.GetTable())
	}
}

// Find answers Builder::find.
func (b *Builder[T]) Find(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	if ids, ok := id.([]any); ok {
		models, err := b.FindMany(g, ids, columns...)
		if err != nil || len(models) == 0 {
			return nil, err
		}
		return models[0], nil
	}
	return b.WhereKey(id).First(g, columns...)
}

// FindMany answers Builder::findMany.
func (b *Builder[T]) FindMany(g auth.Grant, ids []any, columns ...any) (Collection[T], error) {
	if len(ids) == 0 {
		return Collection[T]{}, nil
	}
	return b.WhereKey(ids).Get(g, columns...)
}

// FindOrFail answers Builder::findOrFail.
//
// Given a list it also fails when one id is missing, as the PHP does: asking for
// three rows and getting two is not a shorter answer, it is a wrong one.
func (b *Builder[T]) FindOrFail(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	if ids, ok := id.([]any); ok {
		models, err := b.FindMany(g, ids, columns...)
		if err != nil {
			return nil, err
		}
		if len(models) != len(uniqueValues(ids)) {
			return nil, modelNotFound(b.model.GetTable(), ids...)
		}
		return models[0], nil
	}
	model, err := b.Find(g, id, columns...)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, modelNotFound(b.model.GetTable(), id)
	}
	return model, nil
}

// FindOrNew answers Builder::findOrNew.
func (b *Builder[T]) FindOrNew(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	model, err := b.Find(g, id, columns...)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return b.NewModelInstance(nil)
}

// FirstOrNew answers Builder::firstOrNew.
func (b *Builder[T]) FirstOrNew(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	model, err := b.clone().whereAll(attributes).First(g)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return b.NewModelInstance(mergeMaps(attributes, values))
}

// FirstOrCreate answers Builder::firstOrCreate.
func (b *Builder[T]) FirstOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	model, err := b.clone().whereAll(attributes).First(g)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return b.CreateOrFirst(g, attributes, values)
}

// CreateOrFirst answers Builder::createOrFirst: insert, and if the unique index
// says somebody got there first, read theirs.
//
// The PHP catches UniqueConstraintViolationException. Nothing here classifies a
// driver error yet, so any insert failure sends it looking for the row, and the
// insert error is returned when there is none -- which keeps the race safe and
// never swallows a real failure.
func (b *Builder[T]) CreateOrFirst(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	model, err := b.Create(g, mergeMaps(attributes, values))
	if err == nil {
		return model, nil
	}
	existing, findErr := b.clone().whereAll(attributes).First(g)
	if findErr != nil || existing == nil {
		return nil, err
	}
	return existing, nil
}

// UpdateOrCreate answers Builder::updateOrCreate.
func (b *Builder[T]) UpdateOrCreate(g auth.Grant, attributes, values map[string]any) (*Model[T], error) {
	model, err := b.FirstOrCreate(g, attributes, values)
	if err != nil {
		return nil, err
	}
	if model.WasRecentlyCreated {
		return model, nil
	}
	if err := model.Fill(values); err != nil {
		return nil, err
	}
	if _, err := model.Save(g); err != nil {
		return nil, err
	}
	return model, nil
}

// whereAll adds one equality per entry, which is what passing an array to
// where() does in PHP.
func (b *Builder[T]) whereAll(attributes map[string]any) *Builder[T] {
	for _, column := range sortedKeys(attributes) {
		b.Where(b.model.QualifyColumn(column), "=", attributes[column])
	}
	return b
}

// Value answers Builder::value: one column of the first row.
func (b *Builder[T]) Value(g auth.Grant, column string) (any, error) {
	model, err := b.First(g, column)
	if err != nil || model == nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// ValueOrFail answers Builder::valueOrFail.
func (b *Builder[T]) ValueOrFail(g auth.Grant, column string) (any, error) {
	model, err := b.FirstOrFail(g, column)
	if err != nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// SoleValue answers Builder::soleValue.
func (b *Builder[T]) SoleValue(g auth.Grant, column string) (any, error) {
	model, err := b.Sole(g, column)
	if err != nil {
		return nil, err
	}
	return model.GetAttribute(afterLastDot(column)), nil
}

// Pluck answers Builder::pluck: one column of every row.
func (b *Builder[T]) Pluck(g auth.Grant, column string) ([]any, error) {
	models, err := b.Get(g, column)
	if err != nil {
		return nil, err
	}
	return models.Pluck(afterLastDot(column)), nil
}

// Count answers the aggregate Illuminate reaches through __call: count(*) over
// the query as it stands.
func (b *Builder[T]) Count(g auth.Grant, columns ...any) (int64, error) {
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	value, err := b.Aggregate(g, "count", columns...)
	if err != nil {
		return 0, err
	}
	return toInt64(value), nil
}

// Aggregate answers Builder::aggregate.
func (b *Builder[T]) Aggregate(g auth.Grant, function string, columns ...any) (any, error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	return prepared.runAggregate(function, columns)
}

// Exists answers Builder::exists.
func (b *Builder[T]) Exists(g auth.Grant) (bool, error) {
	count, err := b.Count(g)
	return count > 0, err
}

// Create answers Builder::create: a new model, filled and saved.
func (b *Builder[T]) Create(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	instance, err := b.NewModelInstance(attributes)
	if err != nil {
		return nil, err
	}
	if _, err := instance.Save(g); err != nil {
		return nil, err
	}
	return instance, nil
}

// ForceCreate answers Builder::forceCreate.
//
// In PHP it is create() with the guard off. There is no guard (see the package
// comment), so the difference it keeps is Fill's: an attribute the entity does
// not declare is carried through instead of dropped.
func (b *Builder[T]) ForceCreate(g auth.Grant, attributes map[string]any) (*Model[T], error) {
	instance, err := b.model.NewInstance(nil, false)
	if err != nil {
		return nil, err
	}
	if err := instance.ForceFill(mergeMaps(b.pendingAttributes, attributes)); err != nil {
		return nil, err
	}
	if _, err := instance.Save(g); err != nil {
		return nil, err
	}
	return instance, nil
}

// Insert answers the base builder's insert, reached through the Eloquent one.
//
// The tenant column is written from the Grant on every row, overwriting whatever
// the caller put there: the tenant comes from the Grant and from nowhere else
// (RULE 14).
func (b *Builder[T]) Insert(g auth.Grant, values ...map[string]any) (bool, error) {
	prepared, rows, err := b.prepareWrite(g, values)
	if err != nil {
		return false, err
	}
	return prepared.runInsert(rows)
}

// InsertGetID answers Builder::insertGetId. The PHP spells the last word Id.
func (b *Builder[T]) InsertGetID(g auth.Grant, values map[string]any, sequence string) (int64, error) {
	prepared, rows, err := b.prepareWrite(g, []map[string]any{values})
	if err != nil {
		return 0, err
	}
	return prepared.runInsertGetID(rows[0], sequence)
}

// prepareWrite is prepare() for a statement that carries values: the Grant is
// checked, the scopes are applied, and the tenant is written into every row.
func (b *Builder[T]) prepareWrite(g auth.Grant, values []map[string]any) (*Builder[T], []map[string]any, error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]map[string]any, 0, len(values))
	for _, row := range values {
		row = copyMap(row)
		if column := b.model.TenantColumn; column != "" {
			row[column] = auth.Tenant(g)
		}
		rows = append(rows, row)
	}
	return prepared, rows, nil
}

// Update answers Builder::update.
func (b *Builder[T]) Update(g auth.Grant, values map[string]any) (int64, error) {
	prepared, err := b.prepare(g)
	if err != nil {
		return 0, err
	}
	return prepared.runUpdate(prepared.addUpdatedAtColumn(values))
}

// addUpdatedAtColumn answers Builder::addUpdatedAtColumn.
//
// The column is re-keyed to <table>.<column>, as the PHP does, so that an update
// with a join names the table it means. The grammars that cannot take a
// qualified name on the left of a SET -- Postgres and SQLite -- strip it, which
// is what their compileUpdateColumns overrides are for.
func (b *Builder[T]) addUpdatedAtColumn(values map[string]any) map[string]any {
	column := b.model.GetUpdatedAtColumn()
	if !b.model.UsesTimestamps() || column == "" {
		return values
	}

	out := copyMap(values)
	if _, ok := out[column]; !ok {
		if _, qualified := out[b.model.QualifyColumn(column)]; qualified {
			return out
		}
		out[column] = b.model.FreshTimestamp()
	}

	value := out[column]
	delete(out, column)
	out[tableOf(b.query.GetFrom())+"."+column] = value
	return out
}

// tableOf answers `last(preg_split('/\s+as\s+/i', $this->query->from))`.
func tableOf(from any) string {
	name := fmt.Sprint(from)
	if i := strings.Index(strings.ToLower(name), " as "); i >= 0 {
		return strings.TrimSpace(name[i+4:])
	}
	return strings.TrimSpace(name)
}

// Upsert answers Builder::upsert.
func (b *Builder[T]) Upsert(g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if len(uniqueBy) == 0 {
		return 0, fmt.Errorf("eloquent: the unique columns must not be empty")
	}
	prepared, rows, err := b.prepareWrite(g, values)
	if err != nil {
		return 0, err
	}
	if update == nil {
		update = sortedKeys(rows[0])
	}
	if column := b.model.GetUpdatedAtColumn(); b.model.UsesTimestamps() && column != "" {
		now := b.model.FreshTimestamp()
		for _, row := range rows {
			if _, ok := row[column]; !ok {
				row[column] = now
			}
		}
		if !slices.Contains(update, column) {
			update = append(update, column)
		}
	}
	return prepared.runUpsert(rows, uniqueBy, update)
}

// Increment answers Builder::increment.
func (b *Builder[T]) Increment(g auth.Grant, column string, amount any, extra map[string]any) (int64, error) {
	return b.incrementOrDecrement(g, column, amount, extra, "+")
}

// Decrement answers Builder::decrement.
func (b *Builder[T]) Decrement(g auth.Grant, column string, amount any, extra map[string]any) (int64, error) {
	return b.incrementOrDecrement(g, column, amount, extra, "-")
}

// incrementOrDecrement answers the shared body of Builder::increment and
// Builder::decrement.
//
// The amount goes into the SQL rather than into a binding, because it is on the
// right of an assignment to the column itself. That is why the PHP refuses a
// non-numeric one -- `is_numeric($amount)` guards an InvalidArgumentException --
// and why this does too: an amount that came from a request would otherwise be
// SQL.
func (b *Builder[T]) incrementOrDecrement(g auth.Grant, column string, amount any, extra map[string]any, sign string) (int64, error) {
	if amount == nil {
		amount = 1
	}
	if !isNumeric(reflect.ValueOf(amount).Kind()) {
		return 0, fmt.Errorf("eloquent: non-numeric value passed to increment or decrement on %s.%s: the amount is compiled into the statement, so it cannot come from anywhere a value can", b.model.GetTable(), column)
	}

	values := copyMap(extra)
	wrapped := b.model.Grammar.Wrap(column)
	values[column] = query.Raw(fmt.Sprintf("%s %s %v", wrapped, sign, amount))
	return b.Update(g, values)
}

// Delete answers Builder::delete.
//
// A model that soft deletes has an onDelete callback on its builder, so this
// runs the update the SoftDeletingScope registered instead of a delete -- the
// same indirection the PHP uses.
func (b *Builder[T]) Delete(g auth.Grant) (int64, error) {
	if b.onDelete != nil {
		return b.onDelete(b, g)
	}
	prepared, err := b.prepare(g)
	if err != nil {
		return 0, err
	}
	return prepared.runDelete()
}

// ForceDelete answers Builder::forceDelete: the row goes, whatever the scope
// would have done.
//
// It still applies the tenant filter. "Force" is about the soft delete, never
// about the tenant (RULE 14).
func (b *Builder[T]) ForceDelete(g auth.Grant) (int64, error) {
	tenant := auth.Tenant(g)
	if tenant == "" || !auth.ValidTenant(tenant) {
		return 0, ErrNoTenant
	}
	forced := b.clone()
	if column := b.model.TenantColumn; column != "" {
		isolateWheres(forced.query)
		forced.query.Where(b.model.QualifyColumn(column), "=", tenant)
	}
	return forced.runDelete()
}

// OnDelete answers Builder::onDelete.
func (b *Builder[T]) OnDelete(callback func(*Builder[T], auth.Grant) (int64, error)) *Builder[T] {
	b.onDelete = callback
	return b
}

// Touch answers Builder::touch.
func (b *Builder[T]) Touch(g auth.Grant, column ...string) (int64, error) {
	name := b.model.GetUpdatedAtColumn()
	if len(column) > 0 && column[0] != "" {
		name = column[0]
	} else if !b.model.UsesTimestamps() || name == "" {
		return 0, nil
	}
	return b.Update(g, map[string]any{name: b.model.FreshTimestamp()})
}

// With answers Builder::with: relations to eager load.
func (b *Builder[T]) With(relations ...string) *Builder[T] {
	for _, relation := range relations {
		if relation == "" {
			continue
		}
		b.eagerLoad[relation] = nil
	}
	return b
}

// WithConstraints answers the array form of Builder::with, where the value is a
// callback that constrains the relation's own query.
//
// The callback takes a query builder and not an eloquent one, because the
// relation it constrains belongs to another model type and Go cannot spell "a
// builder of some other model" in this signature.
func (b *Builder[T]) WithConstraints(relation string, constraints func(*query.Builder)) *Builder[T] {
	b.eagerLoad[relation] = constraints
	return b
}

// WithOnly answers Builder::withOnly.
func (b *Builder[T]) WithOnly(relations ...string) *Builder[T] {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b.With(relations...)
}

// Without answers Builder::without.
func (b *Builder[T]) Without(relations ...string) *Builder[T] {
	for _, relation := range relations {
		delete(b.eagerLoad, relation)
	}
	return b
}

// GetEagerLoads answers Builder::getEagerLoads.
func (b *Builder[T]) GetEagerLoads() []string {
	out := make([]string, 0, len(b.eagerLoad))
	for name := range b.eagerLoad {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// WithoutEagerLoads answers Builder::withoutEagerLoads.
func (b *Builder[T]) WithoutEagerLoads() *Builder[T] {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b
}

// WithoutEagerLoad answers Builder::withoutEagerLoad: these relations, and the
// ones nested under them, come off the list.
func (b *Builder[T]) WithoutEagerLoad(relations ...string) *Builder[T] {
	for name := range b.eagerLoad {
		for _, relation := range relations {
			if name == relation || strings.HasPrefix(name, relation+".") {
				delete(b.eagerLoad, name)
			}
		}
	}
	return b
}

// SetEagerLoads answers Builder::setEagerLoads.
func (b *Builder[T]) SetEagerLoads(relations ...string) *Builder[T] {
	b.eagerLoad = map[string]func(*query.Builder){}
	return b.With(relations...)
}

// WithoutGlobalScopesExcept answers Builder::withoutGlobalScopesExcept.
func (b *Builder[T]) WithoutGlobalScopesExcept(identifiers ...string) *Builder[T] {
	for identifier := range b.scopes {
		if !slices.Contains(identifiers, identifier) {
			b.WithoutGlobalScope(identifier)
		}
	}
	return b
}

// FindSole answers Builder::findSole: the row with this key, when it is the only
// one -- which it is, unless the key is not one.
func (b *Builder[T]) FindSole(g auth.Grant, id any, columns ...any) (*Model[T], error) {
	return b.WhereKey(id).Sole(g, columns...)
}

// TouchQuietly answers Builder::touchQuietly.
func (b *Builder[T]) TouchQuietly(g auth.Grant, column ...string) (touched int64, err error) {
	return touched, b.model.WithoutEvents(func() error {
		touched, err = b.Touch(g, column...)
		return err
	})
}

// EagerLoadRelations answers Builder::eagerLoadRelations.
func (b *Builder[T]) EagerLoadRelations(g auth.Grant, models Collection[T]) error {
	if len(models) == 0 {
		return nil
	}
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
		matched, err := relation.Match(g, models.ModelKeys(), b.eagerLoad[name])
		if err != nil {
			return err
		}
		for _, model := range models {
			model.SetRelation(name, matched[model.GetKey()])
		}
	}
	return nil
}

// Limit answers Builder::limit, forwarded to the query builder.
func (b *Builder[T]) Limit(value int) *Builder[T] {
	b.query.Limit(value)
	return b
}

// Offset answers Builder::offset.
func (b *Builder[T]) Offset(value int) *Builder[T] {
	b.query.Offset(value)
	return b
}

// ForPage answers Builder::forPage.
func (b *Builder[T]) ForPage(page, perPage int) *Builder[T] {
	b.query.ForPage(page, perPage)
	return b
}

// GetLimit answers Builder::getLimit.
func (b *Builder[T]) GetLimit() *int { return b.query.GetLimit() }

// GetOffset answers Builder::getOffset.
func (b *Builder[T]) GetOffset() *int { return b.query.GetOffset() }

// OrderBy answers Builder::orderBy.
func (b *Builder[T]) OrderBy(column any, direction ...string) *Builder[T] {
	b.query.OrderBy(column, direction...)
	return b
}

// OrderByDesc answers Builder::orderByDesc.
func (b *Builder[T]) OrderByDesc(column any) *Builder[T] {
	b.query.OrderByDesc(column)
	return b
}

// Select answers Builder::select.
func (b *Builder[T]) Select(columns ...any) *Builder[T] {
	b.query.Select(columns...)
	return b
}

// AddSelect answers Builder::addSelect.
func (b *Builder[T]) AddSelect(columns ...any) *Builder[T] {
	b.query.AddSelect(columns...)
	return b
}

// SelectRaw answers Builder::selectRaw.
func (b *Builder[T]) SelectRaw(expression string, bindings ...any) *Builder[T] {
	b.query.SelectRaw(expression, bindings...)
	return b
}

// enforceOrderBy answers Builder::enforceOrderBy: chunking without an order is
// chunking over a set the engine may return in a different order each time.
func (b *Builder[T]) enforceOrderBy() {
	if len(b.query.Orders) == 0 && len(b.query.UnionOrders) == 0 {
		b.OrderBy(b.model.GetQualifiedKeyName(), "asc")
	}
}

// DefaultKeyName answers Builder::defaultKeyName.
func (b *Builder[T]) DefaultKeyName() string { return b.model.GetKeyName() }

// Chunk answers BuildsQueries::chunk.
//
// The callback stops the walk by returning false, as the PHP's does, and stops
// it with a reason by returning an error.
func (b *Builder[T]) Chunk(g auth.Grant, count int, callback func(Collection[T], int) (bool, error)) error {
	if count < 1 {
		return fmt.Errorf("eloquent: the chunk size should be at least 1")
	}
	b.enforceOrderBy()

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

		results, err := b.clone().Offset((page-1)*count + skip).Limit(size).Get(g)
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

// Each answers BuildsQueries::each.
func (b *Builder[T]) Each(g auth.Grant, count int, callback func(*Model[T], int) (bool, error)) error {
	return b.Chunk(g, count, func(models Collection[T], page int) (bool, error) {
		for i, model := range models {
			keepGoing, err := callback(model, (page-1)*count+i)
			if err != nil || !keepGoing {
				return false, err
			}
		}
		return true, nil
	})
}

// ChunkById answers BuildsQueries::chunkById: chunking by the key rather than by
// the offset, so that a row inserted between two chunks cannot shift the window
// and hide a row.
func (b *Builder[T]) ChunkById(g auth.Grant, count int, callback func(Collection[T], int) (bool, error), column ...string) error {
	if count < 1 {
		return fmt.Errorf("eloquent: the chunk size should be at least 1")
	}
	name := b.DefaultKeyName()
	if len(column) > 0 && column[0] != "" {
		name = column[0]
	}

	var lastID any
	for page := 1; ; page++ {
		q := b.clone()
		if lastID != nil {
			q.Where(b.model.QualifyColumn(name), ">", lastID)
		}
		results, err := q.OrderBy(b.model.QualifyColumn(name), "asc").Limit(count).Get(g)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			return nil
		}
		keepGoing, err := callback(results, page)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
		lastID = results[len(results)-1].GetAttribute(name)
		if lastID == nil {
			return fmt.Errorf("eloquent: chunkById stopped because %s is not in the result", name)
		}
		if len(results) != count {
			return nil
		}
	}
}

// Lazy answers BuildsQueries::lazy: the rows one at a time, a chunk at a time.
//
// The PHP returns a LazyCollection built on a generator; Go has range-over-func,
// so it returns the iterator itself. The error the walk stopped on is reported
// through the pointer the caller passes, because an iterator has nowhere to put
// one.
func (b *Builder[T]) Lazy(g auth.Grant, chunkSize int, err *error) func(func(*Model[T]) bool) {
	return func(yield func(*Model[T]) bool) {
		stopped := false
		chunkErr := b.Chunk(g, chunkSize, func(models Collection[T], _ int) (bool, error) {
			for _, model := range models {
				if !yield(model) {
					stopped = true
					return false, nil
				}
			}
			return true, nil
		})
		_ = stopped
		if chunkErr != nil && err != nil {
			*err = chunkErr
		}
	}
}

// LazyById answers BuildsQueries::lazyById.
func (b *Builder[T]) LazyById(g auth.Grant, chunkSize int, err *error, column ...string) func(func(*Model[T]) bool) {
	return func(yield func(*Model[T]) bool) {
		chunkErr := b.ChunkById(g, chunkSize, func(models Collection[T], _ int) (bool, error) {
			for _, model := range models {
				if !yield(model) {
					return false, nil
				}
			}
			return true, nil
		}, column...)
		if chunkErr != nil && err != nil {
			*err = chunkErr
		}
	}
}

// Cursor answers Builder::cursor.
//
// Illuminate streams the rows off an unbuffered PDO cursor. query.Connection
// hands back the rows it read, so this is the same walk over one result set
// rather than a second way to run a query -- and it stays lazy from the caller's
// side, which is what the method is for.
func (b *Builder[T]) Cursor(g auth.Grant, err *error) func(func(*Model[T]) bool) {
	return func(yield func(*Model[T]) bool) {
		models, getErr := b.Get(g)
		if getErr != nil {
			if err != nil {
				*err = getErr
			}
			return
		}
		for _, model := range models {
			if !yield(model) {
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
