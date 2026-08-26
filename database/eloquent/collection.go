package eloquent

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

// ToBase returns the models as a collections.Collection.
func (c Collection[T]) ToBase() collections.Collection[*Model[T]] {
	return collections.Collect([]*Model[T](c))
}

// All returns the models as a plain slice.
func (c Collection[T]) All() []*Model[T] { return []*Model[T](c) }

// Count returns the number of models.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty reports whether there are no models.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// IsNotEmpty reports the opposite of IsEmpty.
func (c Collection[T]) IsNotEmpty() bool { return len(c) > 0 }

// First returns the first model, or nil when there is none.
func (c Collection[T]) First() *Model[T] {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// ModelKeys returns the primary key of every model.
func (c Collection[T]) ModelKeys() []any {
	out := make([]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.GetKey())
	}
	return out
}

// Find returns the model with this key, out of the ones already in hand.
func (c Collection[T]) Find(key any) *Model[T] {
	for _, model := range c {
		if reflect.DeepEqual(model.GetKey(), key) {
			return model
		}
	}
	return nil
}

// FindOrFail returns the model with this key, or an error when none matches.
func (c Collection[T]) FindOrFail(key any) (*Model[T], error) {
	if model := c.Find(key); model != nil {
		return model, nil
	}
	table := ""
	if first := c.First(); first != nil {
		table = first.GetTable()
	}
	return nil, modelNotFound(table, key)
}

// Contains reports whether key -- a key value, or a *Model[T] to compare by
// key -- matches one of the models.
func (c Collection[T]) Contains(key any) bool {
	if model, ok := key.(*Model[T]); ok {
		for _, candidate := range c {
			if candidate.Is(model) {
				return true
			}
		}
		return false
	}
	return c.Find(key) != nil
}

// DoesntContain reports the opposite of Contains.
func (c Collection[T]) DoesntContain(key any) bool { return !c.Contains(key) }

// Pluck returns one attribute of every model, as a slice of any: the value
// is whatever that column holds.
func (c Collection[T]) Pluck(column string) []any {
	out := make([]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.GetAttribute(column))
	}
	return out
}

// GetDictionary returns the models keyed by their key, which is how every
// set operation here compares them.
func (c Collection[T]) GetDictionary() map[any]*Model[T] {
	out := make(map[any]*Model[T], len(c))
	for _, model := range c {
		out[model.GetKey()] = model
	}
	return out
}

// Merge returns the other models added, with a key that is already here
// replaced rather than repeated.
func (c Collection[T]) Merge(items Collection[T]) Collection[T] {
	out := make(Collection[T], 0, len(c)+len(items))
	out = append(out, c...)
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
func (c Collection[T]) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(c) == 0 || len(relations) == 0 {
		return nil
	}
	q := c.First().NewQueryWithoutRelationships().With(relations...)
	return q.EagerLoadRelations(ctx, g, c)
}

// LoadMissing eager loads these relations onto every model, skipping the
// ones already loaded.
func (c Collection[T]) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(c) == 0 {
		return nil
	}
	for _, relation := range relations {
		missing := make(Collection[T], 0, len(c))
		for _, model := range c {
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
func (c Collection[T]) LoadAggregate(ctx context.Context, g auth.Grant, relations []string, column, function string) error {
	if len(c) == 0 || len(relations) == 0 {
		return nil
	}
	first := c.First()
	rows, err := first.NewModelQuery().loadAggregateModels(ctx, g, c.ModelKeys(), relations, column, function)
	if err != nil {
		return err
	}

	byKey := map[any]*Model[T]{}
	for _, row := range rows {
		byKey[row.GetKey()] = row
	}

	keyName := first.GetKeyName()
	for _, model := range c {
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
	if len(c) == 0 {
		return Collection[T]{}, nil
	}
	first := c.First()
	fresh, err := first.NewQueryWithoutScopes().
		With(with...).
		WhereKey(c.ModelKeys()).
		Get(ctx, g)
	if err != nil {
		return nil, err
	}

	byKey := map[any]*Model[T]{}
	for _, model := range fresh {
		byKey[model.GetKey()] = model
	}

	out := make(Collection[T], 0, len(c))
	for _, model := range c {
		if reloaded, ok := byKey[model.GetKey()]; ok && model.Exists {
			out = append(out, reloaded)
		}
	}
	return out, nil
}

// Diff returns the models that are not in items.
func (c Collection[T]) Diff(items Collection[T]) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if items.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Intersect returns the models that are also in items.
func (c Collection[T]) Intersect(items Collection[T]) Collection[T] {
	out := Collection[T]{}
	if len(items) == 0 {
		return out
	}
	for _, model := range c {
		if items.Find(model.GetKey()) != nil {
			out = append(out, model)
		}
	}
	return out
}

// Unique returns one model per key, the first one seen.
func (c Collection[T]) Unique() Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if out.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Only returns the models with these keys.
func (c Collection[T]) Only(keys ...any) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// Except returns the models without these keys.
func (c Collection[T]) Except(keys ...any) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if !containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// MakeVisible calls Model.MakeVisible on every model.
func (c Collection[T]) MakeVisible(attributes ...string) Collection[T] {
	for _, model := range c {
		model.MakeVisible(attributes...)
	}
	return c
}

// MakeHidden calls Model.MakeHidden on every model.
func (c Collection[T]) MakeHidden(attributes ...string) Collection[T] {
	for _, model := range c {
		model.MakeHidden(attributes...)
	}
	return c
}

// SetVisible calls Model.SetVisible on every model.
func (c Collection[T]) SetVisible(visible ...string) Collection[T] {
	for _, model := range c {
		model.SetVisible(visible...)
	}
	return c
}

// SetHidden calls Model.SetHidden on every model.
func (c Collection[T]) SetHidden(hidden ...string) Collection[T] {
	for _, model := range c {
		model.SetHidden(hidden...)
	}
	return c
}

// Append calls Model.Append on every model.
func (c Collection[T]) Append(attributes ...string) Collection[T] {
	for _, model := range c {
		model.Append(attributes...)
	}
	return c
}

// SetAppends calls Model.SetAppends on every model.
func (c Collection[T]) SetAppends(appends ...string) Collection[T] {
	for _, model := range c {
		model.SetAppends(appends...)
	}
	return c
}

// ToQuery returns a query over exactly these rows.
//
// A Go collection cannot hold two model types, so the only refusal is the
// empty one -- with no model there is no table to query.
func (c Collection[T]) ToQuery() (*Builder[T], error) {
	first := c.First()
	if first == nil {
		return nil, ErrEmptyCollection
	}
	return first.NewModelQuery().WhereKey(c.ModelKeys()), nil
}

// ToArray returns every model, serialised.
func (c Collection[T]) ToArray() []map[string]any {
	out := make([]map[string]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.ToArray())
	}
	return out
}

// Push calls Model.Push on every model, which is what makes a loaded
// relation pushable.
func (c Collection[T]) Push(ctx context.Context, g auth.Grant) (bool, error) {
	for _, model := range c {
		pushed, err := model.Push(ctx, g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

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

// Flatten returns the same models as a collection of any.
//
// The models are the leaves -- a model is not a list -- so flattening them
// changes only the element type. The depth is optional and unlimited when
// omitted.
func (c Collection[T]) Flatten(depth ...int) collections.Collection[any] {
	items := make(collections.Collection[any], 0, len(c))
	for _, model := range c {
		items = append(items, model)
	}
	return collections.Flatten(items, depth...)
}

// Flip returns the models as keys and their positions as values.
//
// The keys of a Collection[T] are positions, so flipping gives model to
// position, and a model that repeats keeps the last position.
func (c Collection[T]) Flip() map[*Model[T]]int {
	return collections.Flip(c.ToBase())
}

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

func containsValue(values []any, value any) bool {
	for _, candidate := range values {
		if reflect.DeepEqual(candidate, value) {
			return true
		}
	}
	return false
}
