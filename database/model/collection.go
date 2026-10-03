package model

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/collections"
	"github.com/arandu-io/hesape/database/model/relations/concerns"
)

// Collection is the rows a query came back with.
//
// The items are the application's own struct -- the same value a terminal hands
// back one of -- so reading a column off one is reading a field, and there is
// nothing to unwrap first.
//
// The general collection vocabulary lives in hesape/collections, and ToBase
// converts to it. Only the methods that know the items are rows of a table are
// here: keyed by their key, reloaded from their table, hidden and appended per
// row. Those go through the model behind each row, so they need a T that embeds
// Model[T] -- see models for what a T that does not gets instead.
type Collection[T any] []*T

// models is the same list, held as the models rather than as the rows.
//
// It carries the implementation, and Collection is one line per method over it.
// The split is not a duplication: this side is what the package hands itself --
// eager loading, Fresh, the key a chunk resumes from -- and it keeps working for
// a T that does not embed Model[T], where a row has no way back to the model
// that hydrated it. See embed.go for the two shapes.
//
// It is not generic, and that is what it is for. A generic type has every
// method compiled again for each type argument, in every package that names
// it, so a list type over *Model[T] cost each model a copy of the whole list.
// The elements are the models seen through row, which *Model[T] satisfies for
// every T, and the few operations that need T back are generic functions,
// compiled only where they are called.
type models []row

// row is a model as the code that does not know T sees it: the list here, the
// ref a relation holds, the paginators. Every method but the last three is one
// *Model[T] already has with this exact signature, so holding a model as a row
// costs nothing; the last three are the hooks only the typed model can answer.
type row interface {
	GetKey() any
	GetKeyName() string
	GetKeyType() string
	GetQualifiedKeyName() string
	GetTable() string
	QualifyColumn(column string) string
	GetForeignKey() string
	GetMorphClass() string
	GetConnectionName() string
	GetPerPage() int
	GetAttribute(key string) any
	GetAttributes() map[string]any
	SetAttribute(key string, value any) error
	SetRawAttributes(attributes map[string]any, sync bool) error
	UnsetAttribute(key string)
	Fill(attributes map[string]any) error
	ForceFill(attributes map[string]any) error
	GetRelation(name string) (any, bool)
	RelationLoaded(name string) bool
	IsRelation(key string) bool
	Touches(relation string) bool
	GetCreatedAtColumn() string
	GetUpdatedAtColumn() string
	UsesTimestamps() bool
	FreshTimestamp() time.Time
	Trashed() bool
	ToArray() map[string]any
	WithoutEvents(callback func() error) error
	Save(ctx context.Context, g auth.Grant) (bool, error)
	Delete(ctx context.Context, g auth.Grant) (bool, error)
	Touch(ctx context.Context, g auth.Grant) error
	Push(ctx context.Context, g auth.Grant) (bool, error)
	GetQueueableID() any
	GetQueueableRelations() []string
	Ref() concerns.Model

	// refState hands out the fields the ref writes. See refState.
	refState() refState

	// newRow is NewInstance(attributes, false), held as a row.
	newRow(attributes map[string]any) (row, error)

	// newQueryRef is NewQuery, as the builder a relation takes.
	newQueryRef() concerns.Builder
}

// entitiesOf returns the row of every model.
//
// Every model has one, whichever shape T is: NewInstance allocates it, so the
// two sides are always the same length.
func entitiesOf[T any](ms models) Collection[T] {
	out := make(Collection[T], len(ms))
	for i, model := range ms {
		out[i] = model.(*Model[T]).Entity
	}
	return out
}

// modelsOf holds the given models as the list.
func modelsOf[T any](ms ...*Model[T]) models {
	out := make(models, len(ms))
	for i, model := range ms {
		out[i] = model
	}
	return out
}

// models returns the model behind every row.
//
// This is where the shape of T shows. A T that embeds Model[T] carries its model
// inside itself, so the row is the model and this finds it. A T that does not
// has no field to point back with: the model that hydrated it is a value of its
// own beside it, the row is the columns and nothing else, and this answers
// nothing.
//
// So every method below that reaches a model is a method for the embedding
// shape. Nothing in this package calls one -- it holds the models already, and
// reads them through models. See embed.go and ModelOf.
func rowModels[T any](c Collection[T]) models {
	out := make(models, 0, len(c))
	for _, entity := range c {
		if model := ModelOf(entity); model != nil {
			out = append(out, model)
		}
	}
	return out
}

// modelsOrFail returns the model behind every row, and refuses when a row has
// none.
//
// models drops what it cannot resolve, which is the right answer for the reads
// that return a value -- Find over a row with no model is a miss, and a miss is
// nil. It is the wrong answer for a relation load, which would report success
// having attached the relation to nothing. So the loads come through here and
// the rest do not.
func rowModelsOrFail[T any](c Collection[T]) (models, error) {
	found := rowModels(c)
	if len(found) != len(c) {
		return nil, fmt.Errorf("%w: %d of %d rows", ErrRowHasNoModel, len(c)-len(found), len(c))
	}
	return found, nil
}

// ToBase returns the rows as a collections.Collection.
func (c Collection[T]) ToBase() collections.Collection[*T] { return collections.Collect([]*T(c)) }

// All returns the rows as a plain slice.
func (c Collection[T]) All() []*T { return []*T(c) }

// Count returns the number of rows.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty reports whether there are no rows.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// IsNotEmpty reports the opposite of IsEmpty.
func (c Collection[T]) IsNotEmpty() bool { return len(c) > 0 }

// First returns the first row, or nil when there is none.
func (c Collection[T]) First() *T {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// ModelKeys returns the primary key of every row.
func (c Collection[T]) ModelKeys() []any { return rowModels(c).ModelKeys() }

// Find returns the row with this key, out of the ones already in hand.
func (c Collection[T]) Find(key any) *T { return entityOfRow[T](rowModels(c).Find(key)) }

// FindOrFail returns the row with this key, or an error when none matches.
func (c Collection[T]) FindOrFail(key any) (*T, error) {
	model, err := rowModels(c).FindOrFail(key)
	return entityOfRow[T](model), err
}

// Contains reports whether key -- a key value, a row, or the model behind one,
// to compare by key -- matches one of the rows.
func (c Collection[T]) Contains(key any) bool {
	switch typed := key.(type) {
	case *T:
		model := ModelOf(typed)
		return model != nil && rowModels(c).containsModel(model)
	case *Model[T]:
		return rowModels(c).containsModel(typed)
	}
	return rowModels(c).Find(key) != nil
}

// DoesntContain reports the opposite of Contains.
func (c Collection[T]) DoesntContain(key any) bool { return !c.Contains(key) }

// Pluck returns one attribute of every row, as a slice of any: the value
// is whatever that column holds.
func (c Collection[T]) Pluck(column string) []any { return rowModels(c).Pluck(column) }

// GetDictionary returns the rows keyed by their key, which is how every
// set operation here compares them.
func (c Collection[T]) GetDictionary() map[any]*T {
	found := rowModels(c)
	out := make(map[any]*T, len(found))
	for _, model := range found {
		out[model.GetKey()] = model.(*Model[T]).Entity
	}
	return out
}

// Merge returns the other rows added, with a key that is already here
// replaced rather than repeated.
func (c Collection[T]) Merge(items Collection[T]) Collection[T] {
	return entitiesOf[T](rowModels(c).Merge(rowModels(items)))
}

// Load eager loads these relations onto every row.
//
// It reaches the model behind each row, so it needs a T that embeds Model[T];
// rows that carry no model are ErrRowHasNoModel rather than a load that quietly
// attaches nothing.
func (c Collection[T]) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	found, err := rowModelsOrFail(c)
	if err != nil {
		return err
	}
	return loadModels[T](ctx, g, found, relations...)
}

// LoadMissing eager loads these relations onto every row, skipping the
// ones already loaded. See Load for the shape of T it needs.
func (c Collection[T]) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	found, err := rowModelsOrFail(c)
	if err != nil {
		return err
	}
	return loadMissingModels[T](ctx, g, found, relations...)
}

// LoadAggregate loads function over column of each relation onto every
// row. See Load for the shape of T it needs.
func (c Collection[T]) LoadAggregate(ctx context.Context, g auth.Grant, relations []string, column, function string) error {
	if len(relations) == 0 {
		return nil
	}
	found, err := rowModelsOrFail(c)
	if err != nil {
		return err
	}
	return loadAggregateModels[T](ctx, g, found, relations, column, function)
}

// LoadCount loads the count of each relation onto every row.
func (c Collection[T]) LoadCount(ctx context.Context, g auth.Grant, relations ...string) error {
	return c.LoadAggregate(ctx, g, relations, "*", "count")
}

// LoadMax loads the max of column over each relation onto every row.
func (c Collection[T]) LoadMax(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "max")
}

// LoadMin loads the min of column over each relation onto every row.
func (c Collection[T]) LoadMin(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "min")
}

// LoadSum loads the sum of column over each relation onto every row.
func (c Collection[T]) LoadSum(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "sum")
}

// LoadAvg loads the average of column over each relation onto every row.
func (c Collection[T]) LoadAvg(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "avg")
}

// LoadExists loads whether each relation exists onto every row.
func (c Collection[T]) LoadExists(ctx context.Context, g auth.Grant, relations ...string) error {
	return c.LoadAggregate(ctx, g, relations, "*", "exists")
}

// Fresh returns the same rows, read again.
//
// A row that has since been deleted drops out of the result.
func (c Collection[T]) Fresh(ctx context.Context, g auth.Grant, with ...string) (Collection[T], error) {
	fresh, err := freshModels[T](ctx, g, rowModels(c), with...)
	if err != nil {
		return nil, err
	}
	return entitiesOf[T](fresh), nil
}

// Diff returns the rows that are not in items.
func (c Collection[T]) Diff(items Collection[T]) Collection[T] {
	return entitiesOf[T](rowModels(c).Diff(rowModels(items)))
}

// Intersect returns the rows that are also in items.
func (c Collection[T]) Intersect(items Collection[T]) Collection[T] {
	return entitiesOf[T](rowModels(c).Intersect(rowModels(items)))
}

// Unique returns one row per key, the first one seen.
func (c Collection[T]) Unique() Collection[T] { return entitiesOf[T](rowModels(c).Unique()) }

// Only returns the rows with these keys.
func (c Collection[T]) Only(keys ...any) Collection[T] {
	return entitiesOf[T](rowModels(c).Only(keys...))
}

// Except returns the rows without these keys.
func (c Collection[T]) Except(keys ...any) Collection[T] {
	return entitiesOf[T](rowModels(c).Except(keys...))
}

// MakeVisible calls Model.MakeVisible on every row.
func (c Collection[T]) MakeVisible(attributes ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).MakeVisible(attributes...)
	}
	return c
}

// MakeHidden calls Model.MakeHidden on every row.
func (c Collection[T]) MakeHidden(attributes ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).MakeHidden(attributes...)
	}
	return c
}

// SetVisible calls Model.SetVisible on every row.
func (c Collection[T]) SetVisible(visible ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).SetVisible(visible...)
	}
	return c
}

// SetHidden calls Model.SetHidden on every row.
func (c Collection[T]) SetHidden(hidden ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).SetHidden(hidden...)
	}
	return c
}

// Append calls Model.Append on every row.
func (c Collection[T]) Append(attributes ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).Append(attributes...)
	}
	return c
}

// SetAppends calls Model.SetAppends on every row.
func (c Collection[T]) SetAppends(appends ...string) Collection[T] {
	for _, model := range rowModels(c) {
		model.(*Model[T]).SetAppends(appends...)
	}
	return c
}

// ToQuery returns a query over exactly these rows.
//
// A Go collection cannot hold two model types, so the only refusal is the
// empty one -- with no row there is no table to query.
func (c Collection[T]) ToQuery() (*Builder[T], error) { return modelsToQuery[T](rowModels(c)) }

// ToArray returns every row, serialised.
func (c Collection[T]) ToArray() []map[string]any { return rowModels(c).ToArray() }

// Push calls Model.Push on every row, which is what makes a loaded
// relation pushable.
func (c Collection[T]) Push(ctx context.Context, g auth.Grant) (bool, error) {
	return rowModels(c).Push(ctx, g)
}

// Flatten returns the same rows as a collection of any.
//
// The rows are the leaves -- a row is not a list -- so flattening them
// changes only the element type. The depth is optional and unlimited when
// omitted.
func (c Collection[T]) Flatten(depth ...int) collections.Collection[any] {
	items := make(collections.Collection[any], 0, len(c))
	for _, entity := range c {
		items = append(items, entity)
	}
	return collections.Flatten(items, depth...)
}

// Flip returns the rows as keys and their positions as values.
//
// The keys of a Collection[T] are positions, so flipping gives row to
// position, and a row that repeats keeps the last position.
//
// It is written out rather than handed to collections.Flip. That function
// constrains its element to comparable, and a pointer under a comparable
// constraint is compiled once per pointed-to type rather than once for every
// pointer: each model would then carry its own copy of the whole
// collections.Collection method set, in every package that names the model.
func (c Collection[T]) Flip() map[*T]int {
	out := make(map[*T]int, len(c))
	for i, row := range c {
		out[row] = i
	}
	return out
}

// Pad returns the rows padded with value to size elements.
//
// A positive size pads on the right, a negative size on the left, and a
// size no larger than the count returns the rows unchanged.
func (c Collection[T]) Pad(size int, value *T) collections.Collection[*T] {
	return c.ToBase().Pad(size, value)
}

// Partition returns the rows passing callback, then the ones failing it.
func (c Collection[T]) Partition(callback func(row *T, key int) bool) (passed, failed collections.Collection[*T]) {
	return c.ToBase().Partition(callback)
}

// entityOf returns the row a model holds, and nil for no model.
//
// It is the one place the nil check lives, because a terminal that matched
// nothing answers nil and every one of them converts the same way.
func entityOf[T any](m *Model[T]) *T {
	if m == nil {
		return nil
	}
	return m.Entity
}

// entityOfRow is entityOf for a model held in the list, where no model is the
// nil interface.
func entityOfRow[T any](m row) *T {
	if m == nil {
		return nil
	}
	return m.(*Model[T]).Entity
}

// First returns the first model, or nil when there is none.
func (ms models) First() row {
	if len(ms) == 0 {
		return nil
	}
	return ms[0]
}

// ModelKeys returns the primary key of every model.
func (ms models) ModelKeys() []any {
	out := make([]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.GetKey())
	}
	return out
}

// Find returns the model with this key, out of the ones already in hand.
func (ms models) Find(key any) row {
	for _, model := range ms {
		if reflect.DeepEqual(model.GetKey(), key) {
			return model
		}
	}
	return nil
}

// FindOrFail returns the model with this key, or an error when none matches.
func (ms models) FindOrFail(key any) (row, error) {
	if model := ms.Find(key); model != nil {
		return model, nil
	}
	table := ""
	if first := ms.First(); first != nil {
		table = first.GetTable()
	}
	return nil, modelNotFound(table, key)
}

// containsModel reports whether one of the models is the same row as model:
// the same key, on the same table, on the same connection.
func (ms models) containsModel(model row) bool {
	for _, candidate := range ms {
		if sameRow(candidate, model) {
			return true
		}
	}
	return false
}

// sameRow is the comparison Model.Is makes: the same key, on the same table,
// on the same connection.
func sameRow(a, b row) bool {
	return reflect.DeepEqual(a.GetKey(), b.GetKey()) &&
		a.GetTable() == b.GetTable() &&
		a.GetConnectionName() == b.GetConnectionName()
}

// Pluck returns one attribute of every model, as a slice of any: the value
// is whatever that column holds.
func (ms models) Pluck(column string) []any {
	out := make([]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.GetAttribute(column))
	}
	return out
}

// Merge returns the other models added, with a key that is already here
// replaced rather than repeated.
func (ms models) Merge(items models) models {
	out := make(models, 0, len(ms)+len(items))
	out = append(out, ms...)
	for _, model := range items {
		if existing := out.Find(model.GetKey()); existing != nil {
			for i, candidate := range out {
				if candidate == existing {
					out[i] = model
				}
			}
			continue
		}
		out = append(out, model)
	}
	return out
}

// loadModels eager loads these relations onto every model.
func loadModels[T any](ctx context.Context, g auth.Grant, ms models, relations ...string) error {
	if len(ms) == 0 || len(relations) == 0 {
		return nil
	}
	q := ms.First().(*Model[T]).NewQueryWithoutRelationships().With(relations...)
	return eagerLoadRelations(q, ctx, g, ms)
}

// loadMissingModels eager loads these relations onto every model, skipping
// the ones already loaded.
func loadMissingModels[T any](ctx context.Context, g auth.Grant, ms models, relations ...string) error {
	if len(ms) == 0 {
		return nil
	}
	for _, relation := range relations {
		missing := make(models, 0, len(ms))
		for _, model := range ms {
			if !model.RelationLoaded(relation) {
				missing = append(missing, model)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if err := loadModels[T](ctx, g, missing, relation); err != nil {
			return err
		}
	}
	return nil
}

// loadAggregateModels loads function over column of each relation onto every
// model.
//
// It reads the aggregate columns for the keys already in hand and force
// fills them onto the models. The columns are not declared on the entity,
// so they land as raw attributes.
func loadAggregateModels[T any](ctx context.Context, g auth.Grant, ms models, relations []string, column, function string) error {
	if len(ms) == 0 || len(relations) == 0 {
		return nil
	}
	first := ms.First().(*Model[T])
	aggregates, err := aggregateQuery(first.NewModelQuery(), ctx, g, ms.ModelKeys(), relations, column, function)
	if err != nil {
		return err
	}

	byKey := map[any]*Model[T]{}
	for _, aggregate := range aggregates {
		byKey[aggregate.GetKey()] = aggregate.(*Model[T])
	}

	keyName := first.GetKeyName()
	for _, held := range ms {
		model := held.(*Model[T])
		aggregate, ok := byKey[model.GetKey()]
		if !ok {
			continue
		}
		extra := map[string]any{}
		names := make([]string, 0)
		for key, value := range aggregate.GetAttributes() {
			if key == keyName {
				continue
			}
			extra[key] = value
			names = append(names, key)
		}
		if err := model.ForceFill(extra); err != nil {
			return err
		}
		slices.Sort(names)
		model.SyncOriginalAttributes(names...)
	}
	return nil
}

// freshModels returns the same rows, read again.
//
// A model that has since been deleted drops out of the result.
func freshModels[T any](ctx context.Context, g auth.Grant, ms models, with ...string) (models, error) {
	if len(ms) == 0 {
		return models{}, nil
	}
	first := ms.First().(*Model[T])
	fresh, err := first.NewQueryWithoutScopes().
		With(with...).
		WhereKey(ms.ModelKeys()).
		get(ctx, g)
	if err != nil {
		return nil, err
	}

	byKey := map[any]row{}
	for _, model := range fresh {
		byKey[model.GetKey()] = model
	}

	out := make(models, 0, len(ms))
	for _, model := range ms {
		if reloaded, ok := byKey[model.GetKey()]; ok && model.(*Model[T]).Exists {
			out = append(out, reloaded)
		}
	}
	return out, nil
}

// Diff returns the models that are not in items.
func (ms models) Diff(items models) models {
	out := models{}
	for _, model := range ms {
		if items.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Intersect returns the models that are also in items.
func (ms models) Intersect(items models) models {
	out := models{}
	if len(items) == 0 {
		return out
	}
	for _, model := range ms {
		if items.Find(model.GetKey()) != nil {
			out = append(out, model)
		}
	}
	return out
}

// Unique returns one model per key, the first one seen.
func (ms models) Unique() models {
	out := models{}
	for _, model := range ms {
		if out.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Only returns the models with these keys.
func (ms models) Only(keys ...any) models {
	out := models{}
	for _, model := range ms {
		if containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// Except returns the models without these keys.
func (ms models) Except(keys ...any) models {
	out := models{}
	for _, model := range ms {
		if !containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// modelsToQuery returns a query over exactly these rows.
//
// A Go collection cannot hold two model types, so the only refusal is the
// empty one -- with no model there is no table to query.
func modelsToQuery[T any](ms models) (*Builder[T], error) {
	first := ms.First()
	if first == nil {
		return nil, ErrEmptyCollection
	}
	return first.(*Model[T]).NewModelQuery().WhereKey(ms.ModelKeys()), nil
}

// ToArray returns every model, serialised.
func (ms models) ToArray() []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.ToArray())
	}
	return out
}

// Push calls Model.Push on every model, which is what makes a loaded
// relation pushable.
func (ms models) Push(ctx context.Context, g auth.Grant) (bool, error) {
	for _, model := range ms {
		pushed, err := model.Push(ctx, g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// NewCollection builds the Collection a query's rows are handed back in.
//
// There is one collection type and no automatic relation loading, so this is the
// construction and nothing else.
func (m *Model[T]) NewCollection(rows ...*T) Collection[T] {
	return Collection[T](rows)
}

// CountBy counts the rows by the key countBy returns for each one.
//
// It is a function rather than a method because the result is a map keyed
// by K, not a Collection[T]: the key type is the caller's, and Go names it
// in the signature rather than discovering it at run time.
func CountBy[T any, K comparable](c Collection[T], countBy func(row *T, key int) K) map[K]int {
	return collections.CountBy(c.ToBase(), countBy)
}

// Map returns the result of calling callback on every row, as a
// collections.Collection[R].
//
// The compiler decides the result type: a callback returning *T gives back
// exactly what Collection[T] holds, and any other R gives a collection of R.
func Map[T, R any](c Collection[T], callback func(row *T, key int) R) collections.Collection[R] {
	return collections.Map(c.ToBase(), callback)
}

// MapWithKeys returns the key/value pairs callback returns for every row,
// as a map. See Map for how the value type is decided.
func MapWithKeys[T any, K comparable, V any](c Collection[T], callback func(row *T, key int) (K, V)) map[K]V {
	return collections.MapWithKeys(c.ToBase(), callback)
}

// Zip pairs up c with each of items, position by position.
//
// It is a function and not a method for the reason collections.Zip is one:
// the result no longer holds rows, which the return type already says.
func Zip[T any](c Collection[T], items ...[]*T) collections.Collection[collections.Collection[*T]] {
	return collections.Zip(c.ToBase(), items...)
}

func containsValue(values []any, value any) bool {
	for _, candidate := range values {
		if reflect.DeepEqual(candidate, value) {
			return true
		}
	}
	return false
}
