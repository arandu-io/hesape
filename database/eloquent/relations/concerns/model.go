package concerns

import (
	"context"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Model is what a relation asks of Illuminate\Database\Eloquent\Model.
//
// PHP passes the concrete Model class around and reaches whatever it needs
// through late static binding. Go has no such thing, so the surface a relation
// actually touches is written out as an interface -- and, following the rule
// that an interface belongs with its consumer, it is declared here rather than
// in the eloquent package that will implement it. The concrete model embeds the
// concerns in database/eloquent/concerns and satisfies this by doing so.
//
// It lives in this package, and not in relations, because Go forbids a
// subpackage from importing its parent: relations, relations/concerns and every
// relation type have to agree on one Model, and the only place all three can
// see is the leaf. relations aliases it back (type Model = concerns.Model), so
// relations.Model and concerns.Model are the same type and a reader who expects
// the PHP layout still finds the name where the PHP puts it.
type Model interface {
	// GetTable answers Model::getTable.
	GetTable() string

	// SetTable answers Model::setTable. A self-join renames the related model's
	// table to the join alias, which is why this is on the interface.
	SetTable(table string)

	// QualifyColumn answers Model::qualifyColumn: the column prefixed with the
	// table, unless it already carries one.
	QualifyColumn(column string) string

	// GetKeyName answers Model::getKeyName.
	GetKeyName() string

	// GetKeyType answers Model::getKeyType: "int" or "string".
	GetKeyType() string

	// GetKey answers Model::getKey.
	GetKey() any

	// GetForeignKey answers Model::getForeignKey: the conventional foreign key
	// pointing at this model, snake_case singular plus the key name.
	GetForeignKey() string

	// GetMorphClass answers Model::getMorphClass.
	//
	// In PHP it defaults to the class name and the morph map is optional. There
	// is no class name at run time here, so it is the alias this model was
	// registered under -- see the relations package doc for why that is the
	// better trade rather than a regrettable one.
	GetMorphClass() string

	// Exists answers Model::$exists. A property in PHP; a method here, because
	// the field name is taken by nothing and the value is read, never assigned,
	// by a relation.
	Exists() bool

	// GetAttribute answers Model::getAttribute.
	GetAttribute(key string) any

	// SetAttribute answers Model::setAttribute.
	SetAttribute(key string, value any)

	// GetAttributes answers Model::getAttributes.
	GetAttributes() map[string]any

	// SetRawAttributes answers Model::setRawAttributes.
	SetRawAttributes(attributes map[string]any, sync bool)

	// UnsetAttribute answers PHP's unset($model->$key), which BelongsToMany
	// performs on every pivot_ column it lifts off the related model.
	UnsetAttribute(key string)

	// GetRelation answers Model::getRelation, with the second result reporting
	// whether the relation was loaded, where PHP throws.
	GetRelation(relation string) (any, bool)

	// SetRelation answers Model::setRelation.
	SetRelation(relation string, value any)

	// RelationLoaded answers Model::relationLoaded.
	RelationLoaded(relation string) bool

	// UnsetRelation answers Model::unsetRelation.
	UnsetRelation(relation string)

	// IsRelation answers Model::isRelation: whether the name is one of this
	// model's relations. Chaperone asks before it writes an inverse nobody
	// declared.
	IsRelation(key string) bool

	// Touches answers Model::touches.
	Touches(relation string) bool

	// Touch answers Model::touch.
	Touch(ctx context.Context, g auth.Grant) error

	// NewInstance answers Model::newInstance.
	NewInstance(attributes map[string]any) Model

	// Fill answers Model::fill: mass assignment, through the guard.
	Fill(attributes map[string]any)

	// ForceFill answers Model::forceFill: mass assignment, past the guard.
	ForceFill(attributes map[string]any)

	// WasRecentlyCreated answers Model::$wasRecentlyCreated, which
	// updateOrCreate reads to tell a create from a find.
	WasRecentlyCreated() bool

	// WithoutEvents answers Model::withoutEvents.
	//
	// Static in PHP, a method here: Go has no late static binding, so the only
	// way to reach the model's own event registry is through the model. It is
	// what the Quietly variants of save and create are made of.
	WithoutEvents(callback func() error) error

	// Delete answers Model::delete, returning rows affected where the PHP
	// returns bool|null.
	Delete(ctx context.Context, g auth.Grant) (int64, error)

	// NewQuery answers Model::newQuery.
	NewQuery() Builder

	// GetCreatedAtColumn answers Model::getCreatedAtColumn.
	GetCreatedAtColumn() string

	// GetUpdatedAtColumn answers Model::getUpdatedAtColumn.
	GetUpdatedAtColumn() string

	// UsesTimestamps answers Model::usesTimestamps.
	UsesTimestamps() bool

	// FreshTimestamp answers Model::freshTimestamp.
	FreshTimestamp() time.Time

	// Save answers Model::save.
	//
	// It carries the context and the Grant that every write in this collection
	// carries: a relation that could save without one would be the hole RULE 17
	// exists to close, and it would be on the write side, where it is easiest
	// to spot and therefore least excusable to leave open.
	Save(ctx context.Context, g auth.Grant) error
}

// Builder is what a relation asks of Illuminate\Database\Eloquent\Builder.
//
// Narrow on purpose: it is the set of methods the sixteen relation types call,
// not the whole Eloquent builder. The concrete builder lives in the eloquent
// package and satisfies this; declaring the contract here is what keeps
// relations from importing it and closing a cycle.
//
// Every method that reaches the database takes a context and an auth.Grant.
// That is not decoration. A relation is a read path, List and Find and Get and
// Paginate are read paths, and RULE 17 says a read path without a Policy is a
// tenant leak with a technical name -- so the Grant is in the signature, where
// forgetting it does not compile.
type Builder interface {
	// GetModel answers Builder::getModel.
	GetModel() Model

	// GetQuery answers Builder::getQuery: the base query builder underneath.
	GetQuery() *query.Builder

	// Select answers Builder::select.
	Select(columns ...any) Builder

	// AddSelect answers Builder::addSelect.
	AddSelect(columns ...any) Builder

	// Where answers Builder::where.
	Where(column any, args ...any) Builder

	// WhereIn answers Builder::whereIn.
	WhereIn(column any, values []any) Builder

	// WhereNotNull answers Builder::whereNotNull.
	WhereNotNull(columns ...any) Builder

	// WhereColumn answers Builder::whereColumn.
	WhereColumn(first any, args ...any) Builder

	// WhereKey answers Builder::whereKey.
	WhereKey(ids ...any) Builder

	// Join answers Builder::join.
	Join(table any, first any, args ...any) Builder

	// GroupBy answers Builder::groupBy.
	GroupBy(groups ...any) Builder

	// SelectRaw answers Builder::selectRaw.
	SelectRaw(expression string, bindings ...any) Builder

	// OrderBy answers Builder::orderBy.
	OrderBy(column any, direction ...string) Builder

	// Limit answers Builder::limit.
	Limit(value int) Builder

	// Get answers Builder::get.
	Get(ctx context.Context, g auth.Grant) ([]Model, error)

	// First answers Builder::first. A miss is (nil, nil), where the PHP returns
	// null; ErrNotFound belongs to firstOrFail, which is a different question.
	First(ctx context.Context, g auth.Grant) (Model, error)

	// Find answers Builder::find. A miss is (nil, nil), as it is for First.
	Find(ctx context.Context, g auth.Grant, id any) (Model, error)

	// Insert answers Builder::insert.
	Insert(ctx context.Context, g auth.Grant, values []map[string]any) error

	// Update answers Builder::update, returning rows affected.
	Update(ctx context.Context, g auth.Grant, values map[string]any) (int64, error)

	// Upsert answers Builder::upsert, returning rows affected.
	//
	// It is here because HasOneOrMany::upsert and MorphOneOrMany::upsert stamp
	// the foreign key -- and the morph type -- onto every row and then hand the
	// batch straight to the builder. Doing that one row at a time through Insert
	// would turn one statement into N, which is the opposite of what a caller
	// reaches for upsert to get.
	Upsert(ctx context.Context, g auth.Grant, values []map[string]any, uniqueBy, update []string) (int64, error)

	// Delete answers Builder::delete, returning rows affected.
	Delete(ctx context.Context, g auth.Grant) (int64, error)

	// Clone answers PHP's `clone $builder`.
	Clone() Builder
}

// TenantColumn is the column a tenant-scoped table carries.
//
// One name, not a setting: every query this package emits filters on it, and a
// per-model column name that could be misspelled is a filter that silently
// matches nothing -- or, worse, a filter the grammar drops.
const TenantColumn = "tenant_id"

// Tenanted is how a model says its rows are not partitioned by tenant.
//
// Almost nothing should implement it. A currency table, a country list, a
// shared taxonomy -- rows that are the same for every customer of the system --
// return the empty string and are read unfiltered. Everything else does not
// implement it at all and is filtered on TenantColumn.
//
// The shape is deliberate: opting out is a method somebody wrote, with a name
// that greps, in the model's own file. The alternative -- a column name that
// defaults to empty -- makes the leak the default and the safety the thing that
// has to be remembered.
type Tenanted interface {
	// GetTenantColumn answers the column this model is partitioned by, or the
	// empty string for a table that is shared across tenants.
	GetTenantColumn() string
}

// TenantColumnFor answers the column m is partitioned by.
func TenantColumnFor(m Model) string {
	if t, ok := m.(Tenanted); ok {
		return t.GetTenantColumn()
	}
	return TenantColumn
}

// RequireTenant answers the tenant carried by g, and refuses a Grant that has
// none.
//
// The zero Grant carries the empty tenant. Filtering on it would compile to
// `tenant_id = ”`, which matches nothing on the read side and writes an
// unreachable row on the write side -- a bug that looks like an empty result
// set and gets debugged as a missing fixture. Refusing is louder and is the
// same answer auth.Grant.Check gives: this is not authorized.
func RequireTenant(g auth.Grant) (string, error) {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return "", fmt.Errorf("%w: this relation was loaded with a grant that carries no tenant. The tenant comes from the Grant (RULE 14), so an empty one means nothing authorized this read", auth.ErrForbidden)
	}
	return tenant, nil
}

// ScopeTenant returns b filtered by the tenant g carries.
//
// Every relation calls it on the way to the database, on the eager path exactly
// as on the lazy one. The eager path is the one that matters: a with() whose
// parent query is correctly scoped and whose child query is not returns the
// right parents carrying another customer's children, and nothing in the result
// looks wrong.
func ScopeTenant(b Builder, m Model, g auth.Grant) (Builder, error) {
	tenant, err := RequireTenant(g)
	if err != nil {
		return nil, err
	}
	column := TenantColumnFor(m)
	if column == "" {
		return b, nil
	}
	return b.Where(m.QualifyColumn(column), tenant), nil
}

// ScopeTenantQuery is ScopeTenant for a base query builder.
//
// The pivot table is reached with the base builder rather than an Eloquent one
// -- newPivotStatement returns Illuminate\Database\Query\Builder -- so the
// intermediate table needs its own scoping call. It needs it just as much: a
// pivot row is what says this customer's user has that customer's role.
func ScopeTenantQuery(q *query.Builder, table string, g auth.Grant) (*query.Builder, error) {
	tenant, err := RequireTenant(g)
	if err != nil {
		return nil, err
	}
	return q.Where(table+"."+TenantColumn, tenant), nil
}
