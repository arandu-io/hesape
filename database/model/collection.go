package model

import (
	"context"
	"reflect"
	"slices"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/collections"
)

// Collection is the models a query came back with.
//
// The general collection vocabulary lives in hesape/collections, and ToBase
// converts to it. Only the methods that know the items are models are here:
// keyed by their key, reloaded from their table, hidden and appended per model.
type Collection[T any] []*Model[T]

// models is the same list, held as models rather than as what a caller reads.
//
// It carries the implementation, and Collection is one line per method over it.
// The split is not a duplication: this side is what the package hands itself --
// eager loading, Fresh, the load of an aggregate -- and it keeps working for a T
// that does not embed Model[T], where a row has no way back to the model that
// hydrated it. See embed.go for the two shapes.
type models[T any] []*Model[T]

// ToBase returns the models as a collections.Collection.
func (c Collection[T]) ToBase() collections.Collection[*Model[T]] { return models[T](c).ToBase() }

// All returns the models as a plain slice.
func (c Collection[T]) All() []*Model[T] { return models[T](c).All() }

// Count returns the number of models.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty reports whether there are no models.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// IsNotEmpty reports the opposite of IsEmpty.
func (c Collection[T]) IsNotEmpty() bool { return len(c) > 0 }

// First returns the first model, or nil when there is none.
func (c Collection[T]) First() *Model[T] { return models[T](c).First() }

// ModelKeys returns the primary key of every model.
func (c Collection[T]) ModelKeys() []any { return models[T](c).ModelKeys() }

// Find returns the model with this key, out of the ones already in hand.
func (c Collection[T]) Find(key any) *Model[T] { return models[T](c).Find(key) }

// FindOrFail returns the model with this key, or an error when none matches.
func (c Collection[T]) FindOrFail(key any) (*Model[T], error) { return models[T](c).FindOrFail(key) }

// Contains reports whether key -- a key value, or a *Model[T] to compare by
// key -- matches one of the models.
func (c Collection[T]) Contains(key any) bool { return models[T](c).Contains(key) }

// DoesntContain reports the opposite of Contains.
func (c Collection[T]) DoesntContain(key any) bool { return !c.Contains(key) }

// Pluck returns one attribute of every model, as a slice of any: the value
// is whatever that column holds.
func (c Collection[T]) Pluck(column string) []any { return models[T](c).Pluck(column) }

// GetDictionary returns the models keyed by their key, which is how every
// set operation here compares them.
func (c Collection[T]) GetDictionary() map[any]*Model[T] { return models[T](c).GetDictionary() }

// Merge returns the other models added, with a key that is already here
// replaced rather than repeated.
func (c Collection[T]) Merge(items Collection[T]) Collection[T] {
	return Collection[T](models[T](c).Merge(models[T](items)))
}

// Load eager loads these relations onto every model.
func (c Collection[T]) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	return models[T](c).Load(ctx, g, relations...)
}

// LoadMissing eager loads these relations onto every model, skipping the
// ones already loaded.
func (c Collection[T]) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	return models[T](c).LoadMissing(ctx, g, relations...)
}

// LoadAggregate loads function over column of each relation onto every
// model.
func (c Collection[T]) LoadAggregate(ctx context.Context, g auth.Grant, relations []string, column, function string) error {
	return models[T](c).LoadAggregate(ctx, g, relations, column, function)
}

// LoadCount loads the count of each relation onto every model.
func (c Collection[T]) LoadCount(ctx context.Context, g auth.Grant, relations ...string) error {
	return c.LoadAggregate(ctx, g, relations, "*", "count")
}

// LoadMax loads the max of column over each relation onto every model.
func (c Collection[T]) LoadMax(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "max")
}

// LoadMin loads the min of column over each relation onto every model.
func (c Collection[T]) LoadMin(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "min")
}

// LoadSum loads the sum of column over each relation onto every model.
func (c Collection[T]) LoadSum(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "sum")
}

// LoadAvg loads the average of column over each relation onto every model.
func (c Collection[T]) LoadAvg(ctx context.Context, g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(ctx, g, relations, column, "avg")
}

// LoadExists loads whether each relation exists onto every model.
func (c Collection[T]) LoadExists(ctx context.Context, g auth.Grant, relations ...string) error {
	return c.LoadAggregate(ctx, g, relations, "*", "exists")
}

// Fresh returns the same rows, read again.
//
// A model that has since been deleted drops out of the result.
func (c Collection[T]) Fresh(ctx context.Context, g auth.Grant, with ...string) (Collection[T], error) {
	fresh, err := models[T](c).Fresh(ctx, g, with...)
	return Collection[T](fresh), err
}

// Diff returns the models that are not in items.
func (c Collection[T]) Diff(items Collection[T]) Collection[T] {
	return Collection[T](models[T](c).Diff(models[T](items)))
}

// Intersect returns the models that are also in items.
func (c Collection[T]) Intersect(items Collection[T]) Collection[T] {
	return Collection[T](models[T](c).Intersect(models[T](items)))
}

// Unique returns one model per key, the first one seen.
func (c Collection[T]) Unique() Collection[T] { return Collection[T](models[T](c).Unique()) }

// Only returns the models with these keys.
func (c Collection[T]) Only(keys ...any) Collection[T] {
	return Collection[T](models[T](c).Only(keys...))
}

// Except returns the models without these keys.
func (c Collection[T]) Except(keys ...any) Collection[T] {
	return Collection[T](models[T](c).Except(keys...))
}

// MakeVisible calls Model.MakeVisible on every model.
func (c Collection[T]) MakeVisible(attributes ...string) Collection[T] {
	models[T](c).MakeVisible(attributes...)
	return c
}

// MakeHidden calls Model.MakeHidden on every model.
func (c Collection[T]) MakeHidden(attributes ...string) Collection[T] {
	models[T](c).MakeHidden(attributes...)
	return c
}

// SetVisible calls Model.SetVisible on every model.
func (c Collection[T]) SetVisible(visible ...string) Collection[T] {
	models[T](c).SetVisible(visible...)
	return c
}

// SetHidden calls Model.SetHidden on every model.
func (c Collection[T]) SetHidden(hidden ...string) Collection[T] {
	models[T](c).SetHidden(hidden...)
	return c
}

// Append calls Model.Append on every model.
func (c Collection[T]) Append(attributes ...string) Collection[T] {
	models[T](c).Append(attributes...)
	return c
}

// SetAppends calls Model.SetAppends on every model.
func (c Collection[T]) SetAppends(appends ...string) Collection[T] {
	models[T](c).SetAppends(appends...)
	return c
}

// ToQuery returns a query over exactly these rows.
//
// A Go collection cannot hold two model types, so the only refusal is the
// empty one -- with no model there is no table to query.
func (c Collection[T]) ToQuery() (*Builder[T], error) { return models[T](c).ToQuery() }

// ToArray returns every model, serialised.
func (c Collection[T]) ToArray() []map[string]any { return models[T](c).ToArray() }

// Push calls Model.Push on every model, which is what makes a loaded
// relation pushable.
func (c Collection[T]) Push(ctx context.Context, g auth.Grant) (bool, error) {
	return models[T](c).Push(ctx, g)
}

// Flatten returns the same models as a collection of any.
//
// The models are the leaves -- a model is not a list -- so flattening them
// changes only the element type. The depth is optional and unlimited when
// omitted.
func (c Collection[T]) Flatten(depth ...int) collections.Collection[any] {
	return models[T](c).Flatten(depth...)
}

// Flip returns the models as keys and their positions as values.
//
// The keys of a Collection[T] are positions, so flipping gives model to
// position, and a model that repeats keeps the last position.
func (c Collection[T]) Flip() map[*Model[T]]int { return models[T](c).Flip() }

// Pad returns the models padded with value to size elements.
//
// A positive size pads on the right, a negative size on the left, and a
// size no larger than the count returns the models unchanged.
func (c Collection[T]) Pad(size int, value *Model[T]) collections.Collection[*Model[T]] {
	return c.ToBase().Pad(size, value)
}

// Partition returns the models passing callback, then the ones failing it.
func (c Collection[T]) Partition(callback func(model *Model[T], key int) bool) (passed, failed collections.Collection[*Model[T]]) {
	return c.ToBase().Partition(callback)
}

// ToBase returns the models as a collections.Collection.
func (ms models[T]) ToBase() collections.Collection[*Model[T]] {
	return collections.Collect([]*Model[T](ms))
}

// All returns the models as a plain slice.
func (ms models[T]) All() []*Model[T] { return []*Model[T](ms) }

// IsEmpty reports whether there are no models.
func (ms models[T]) IsEmpty() bool { return len(ms) == 0 }

// First returns the first model, or nil when there is none.
func (ms models[T]) First() *Model[T] {
	if len(ms) == 0 {
		return nil
	}
	return ms[0]
}

// ModelKeys returns the primary key of every model.
func (ms models[T]) ModelKeys() []any {
	out := make([]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.GetKey())
	}
	return out
}

// Find returns the model with this key, out of the ones already in hand.
func (ms models[T]) Find(key any) *Model[T] {
	for _, model := range ms {
		if reflect.DeepEqual(model.GetKey(), key) {
			return model
		}
	}
	return nil
}

// FindOrFail returns the model with this key, or an error when none matches.
func (ms models[T]) FindOrFail(key any) (*Model[T], error) {
	if model := ms.Find(key); model != nil {
		return model, nil
	}
	table := ""
	if first := ms.First(); first != nil {
		table = first.GetTable()
	}
	return nil, modelNotFound(table, key)
}

// Contains reports whether key -- a key value, or a *Model[T] to compare by
// key -- matches one of the models.
func (ms models[T]) Contains(key any) bool {
	if model, ok := key.(*Model[T]); ok {
		for _, candidate := range ms {
			if candidate.Is(model) {
				return true
			}
		}
		return false
	}
	return ms.Find(key) != nil
}

// Pluck returns one attribute of every model, as a slice of any: the value
// is whatever that column holds.
func (ms models[T]) Pluck(column string) []any {
	out := make([]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.GetAttribute(column))
	}
	return out
}

// GetDictionary returns the models keyed by their key, which is how every
// set operation here compares them.
func (ms models[T]) GetDictionary() map[any]*Model[T] {
	out := make(map[any]*Model[T], len(ms))
	for _, model := range ms {
		out[model.GetKey()] = model
	}
	return out
}

// Merge returns the other models added, with a key that is already here
// replaced rather than repeated.
func (ms models[T]) Merge(items models[T]) models[T] {
	out := make(models[T], 0, len(ms)+len(items))
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

// Load eager loads these relations onto every model.
func (ms models[T]) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(ms) == 0 || len(relations) == 0 {
		return nil
	}
	q := ms.First().NewQueryWithoutRelationships().With(relations...)
	return q.eagerLoadRelations(ctx, g, ms)
}

// LoadMissing eager loads these relations onto every model, skipping the
// ones already loaded.
func (ms models[T]) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(ms) == 0 {
		return nil
	}
	for _, relation := range relations {
		missing := make(models[T], 0, len(ms))
		for _, model := range ms {
			if !model.RelationLoaded(relation) {
				missing = append(missing, model)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if err := missing.Load(ctx, g, relation); err != nil {
			return err
		}
	}
	return nil
}

// LoadAggregate loads function over column of each relation onto every
// model.
//
// It reads the aggregate columns for the keys already in hand and force
// fills them onto the models. The columns are not declared on the entity,
// so they land as raw attributes.
func (ms models[T]) LoadAggregate(ctx context.Context, g auth.Grant, relations []string, column, function string) error {
	if len(ms) == 0 || len(relations) == 0 {
		return nil
	}
	first := ms.First()
	rows, err := first.NewModelQuery().loadAggregateModels(ctx, g, ms.ModelKeys(), relations, column, function)
	if err != nil {
		return err
	}

	byKey := map[any]*Model[T]{}
	for _, row := range rows {
		byKey[row.GetKey()] = row
	}

	keyName := first.GetKeyName()
	for _, model := range ms {
		row, ok := byKey[model.GetKey()]
		if !ok {
			continue
		}
		extra := map[string]any{}
		names := make([]string, 0)
		for key, value := range row.GetAttributes() {
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

// LoadCount loads the count of each relation onto every model.
func (ms models[T]) LoadCount(ctx context.Context, g auth.Grant, relations ...string) error {
	return ms.LoadAggregate(ctx, g, relations, "*", "count")
}

// Fresh returns the same rows, read again.
//
// A model that has since been deleted drops out of the result.
func (ms models[T]) Fresh(ctx context.Context, g auth.Grant, with ...string) (models[T], error) {
	if len(ms) == 0 {
		return models[T]{}, nil
	}
	first := ms.First()
	fresh, err := first.NewQueryWithoutScopes().
		With(with...).
		WhereKey(ms.ModelKeys()).
		get(ctx, g)
	if err != nil {
		return nil, err
	}

	byKey := map[any]*Model[T]{}
	for _, model := range fresh {
		byKey[model.GetKey()] = model
	}

	out := make(models[T], 0, len(ms))
	for _, model := range ms {
		if reloaded, ok := byKey[model.GetKey()]; ok && model.Exists {
			out = append(out, reloaded)
		}
	}
	return out, nil
}

// Diff returns the models that are not in items.
func (ms models[T]) Diff(items models[T]) models[T] {
	out := models[T]{}
	for _, model := range ms {
		if items.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Intersect returns the models that are also in items.
func (ms models[T]) Intersect(items models[T]) models[T] {
	out := models[T]{}
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
func (ms models[T]) Unique() models[T] {
	out := models[T]{}
	for _, model := range ms {
		if out.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Only returns the models with these keys.
func (ms models[T]) Only(keys ...any) models[T] {
	out := models[T]{}
	for _, model := range ms {
		if containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// Except returns the models without these keys.
func (ms models[T]) Except(keys ...any) models[T] {
	out := models[T]{}
	for _, model := range ms {
		if !containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// MakeVisible calls Model.MakeVisible on every model.
func (ms models[T]) MakeVisible(attributes ...string) {
	for _, model := range ms {
		model.MakeVisible(attributes...)
	}
}

// MakeHidden calls Model.MakeHidden on every model.
func (ms models[T]) MakeHidden(attributes ...string) {
	for _, model := range ms {
		model.MakeHidden(attributes...)
	}
}

// SetVisible calls Model.SetVisible on every model.
func (ms models[T]) SetVisible(visible ...string) {
	for _, model := range ms {
		model.SetVisible(visible...)
	}
}

// SetHidden calls Model.SetHidden on every model.
func (ms models[T]) SetHidden(hidden ...string) {
	for _, model := range ms {
		model.SetHidden(hidden...)
	}
}

// Append calls Model.Append on every model.
func (ms models[T]) Append(attributes ...string) {
	for _, model := range ms {
		model.Append(attributes...)
	}
}

// SetAppends calls Model.SetAppends on every model.
func (ms models[T]) SetAppends(appends ...string) {
	for _, model := range ms {
		model.SetAppends(appends...)
	}
}

// ToQuery returns a query over exactly these rows.
//
// A Go collection cannot hold two model types, so the only refusal is the
// empty one -- with no model there is no table to query.
func (ms models[T]) ToQuery() (*Builder[T], error) {
	first := ms.First()
	if first == nil {
		return nil, ErrEmptyCollection
	}
	return first.NewModelQuery().WhereKey(ms.ModelKeys()), nil
}

// ToArray returns every model, serialised.
func (ms models[T]) ToArray() []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, model := range ms {
		out = append(out, model.ToArray())
	}
	return out
}

// Push calls Model.Push on every model, which is what makes a loaded
// relation pushable.
func (ms models[T]) Push(ctx context.Context, g auth.Grant) (bool, error) {
	for _, model := range ms {
		pushed, err := model.Push(ctx, g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// Flatten returns the same models as a collection of any.
func (ms models[T]) Flatten(depth ...int) collections.Collection[any] {
	items := make(collections.Collection[any], 0, len(ms))
	for _, model := range ms {
		items = append(items, model)
	}
	return collections.Flatten(items, depth...)
}

// Flip returns the models as keys and their positions as values.
func (ms models[T]) Flip() map[*Model[T]]int { return collections.Flip(ms.ToBase()) }

// NewCollection builds the Collection a query's models are handed back in.
//
// There is one collection type and no automatic relation loading, so this is the
// construction and nothing else.
func (m *Model[T]) NewCollection(models ...*Model[T]) Collection[T] {
	return Collection[T](models)
}

// CountBy counts the models by the key countBy returns for each one.
//
// It is a function rather than a method because the result is a map keyed
// by K, not a Collection[T]: the key type is the caller's, and Go names it
// in the signature rather than discovering it at run time.
func CountBy[T any, K comparable](c Collection[T], countBy func(model *Model[T], key int) K) map[K]int {
	return collections.CountBy(c.ToBase(), countBy)
}

// Map returns the result of calling callback on every model, as a
// collections.Collection[R].
//
// The compiler decides the result type: a callback returning *Model[T]
// gives back exactly what Collection[T] holds, and any other R gives a
// collection of R.
func Map[T, R any](c Collection[T], callback func(model *Model[T], key int) R) collections.Collection[R] {
	return collections.Map(c.ToBase(), callback)
}

// MapWithKeys returns the key/value pairs callback returns for every model,
// as a map. See Map for how the value type is decided.
func MapWithKeys[T any, K comparable, V any](c Collection[T], callback func(model *Model[T], key int) (K, V)) map[K]V {
	return collections.MapWithKeys(c.ToBase(), callback)
}

// Zip pairs up c with each of items, position by position.
//
// It is a function and not a method for the reason collections.Zip is one:
// the result no longer holds models, which the return type already says.
func Zip[T any](c Collection[T], items ...[]*Model[T]) collections.Collection[collections.Collection[*Model[T]]] {
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
