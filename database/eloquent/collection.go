package eloquent

import (
	"reflect"
	"slices"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/collections"
)

// Collection answers Illuminate\Database\Eloquent\Collection: the models a query
// came back with.
//
// The PHP class extends Support\Collection and inherits a hundred methods. Go
// has no inheritance, so what it inherits lives in hesape/collections and ToBase
// converts to it -- which is the same method toBase() has there, doing the same
// job for a different reason.
//
// Only the methods Eloquent adds or overrides are here. They are the ones that
// know the items are models: keyed by their key, reloaded from their table,
// hidden and appended per model.
type Collection[T any] []*Model[T]

// ToBase answers Collection::toBase.
func (c Collection[T]) ToBase() collections.Collection[*Model[T]] {
	return collections.Collect([]*Model[T](c))
}

// All answers Collection::all.
func (c Collection[T]) All() []*Model[T] { return []*Model[T](c) }

// Count answers Collection::count.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty answers Collection::isEmpty.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// IsNotEmpty answers Collection::isNotEmpty.
func (c Collection[T]) IsNotEmpty() bool { return len(c) > 0 }

// First answers Collection::first: the first model, or nil when there is none.
func (c Collection[T]) First() *Model[T] {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// ModelKeys answers Collection::modelKeys.
func (c Collection[T]) ModelKeys() []any {
	out := make([]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.GetKey())
	}
	return out
}

// Find answers Collection::find: the model with this key, out of the ones
// already in hand.
func (c Collection[T]) Find(key any) *Model[T] {
	for _, model := range c {
		if reflect.DeepEqual(model.GetKey(), key) {
			return model
		}
	}
	return nil
}

// FindOrFail answers Collection::findOrFail.
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

// Contains answers Collection::contains, in its key and model forms.
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

// DoesntContain answers Collection::doesntContain.
func (c Collection[T]) DoesntContain(key any) bool { return !c.Contains(key) }

// Pluck answers Collection::pluck: one attribute of every model.
//
// PHP hands back a Support\Collection of mixed; here it is a slice of any, for
// the same reason: the value is whatever that column holds.
func (c Collection[T]) Pluck(column string) []any {
	out := make([]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.GetAttribute(column))
	}
	return out
}

// GetDictionary answers Collection::getDictionary: the models keyed by their
// key, which is how every set operation here compares them.
func (c Collection[T]) GetDictionary() map[any]*Model[T] {
	out := make(map[any]*Model[T], len(c))
	for _, model := range c {
		out[model.GetKey()] = model
	}
	return out
}

// Merge answers Collection::merge: the other models added, with a key that is
// already here replaced rather than repeated.
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

// Load answers Collection::load: eager load these relations onto every model.
func (c Collection[T]) Load(g auth.Grant, relations ...string) error {
	if len(c) == 0 || len(relations) == 0 {
		return nil
	}
	q := c.First().NewQueryWithoutRelationships().With(relations...)
	return q.EagerLoadRelations(g, c)
}

// LoadMissing answers Collection::loadMissing: the same, minus what is loaded.
func (c Collection[T]) LoadMissing(g auth.Grant, relations ...string) error {
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
		if err := missing.Load(g, relation); err != nil {
			return err
		}
	}
	return nil
}

// LoadAggregate answers Collection::loadAggregate.
//
// It reads the aggregate columns for the keys already in hand and force fills
// them onto the models, which is what the PHP does -- the columns are not
// declared on the entity, so they land as raw attributes.
func (c Collection[T]) LoadAggregate(g auth.Grant, relations []string, column, function string) error {
	if len(c) == 0 || len(relations) == 0 {
		return nil
	}
	first := c.First()
	rows, err := first.NewModelQuery().loadAggregateModels(g, c.ModelKeys(), relations, column, function)
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

// LoadCount answers Collection::loadCount.
func (c Collection[T]) LoadCount(g auth.Grant, relations ...string) error {
	return c.LoadAggregate(g, relations, "*", "count")
}

// LoadMax answers Collection::loadMax.
func (c Collection[T]) LoadMax(g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(g, relations, column, "max")
}

// LoadMin answers Collection::loadMin.
func (c Collection[T]) LoadMin(g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(g, relations, column, "min")
}

// LoadSum answers Collection::loadSum.
func (c Collection[T]) LoadSum(g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(g, relations, column, "sum")
}

// LoadAvg answers Collection::loadAvg.
func (c Collection[T]) LoadAvg(g auth.Grant, relations []string, column string) error {
	return c.LoadAggregate(g, relations, column, "avg")
}

// LoadExists answers Collection::loadExists.
func (c Collection[T]) LoadExists(g auth.Grant, relations ...string) error {
	return c.LoadAggregate(g, relations, "*", "exists")
}

// Fresh answers Collection::fresh: the same rows, read again.
//
// A model that has since been deleted drops out of the result, as it does there.
func (c Collection[T]) Fresh(g auth.Grant, with ...string) (Collection[T], error) {
	if len(c) == 0 {
		return Collection[T]{}, nil
	}
	first := c.First()
	fresh, err := first.NewQueryWithoutScopes().
		With(with...).
		WhereKey(c.ModelKeys()).
		Get(g)
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

// Diff answers Collection::diff: the models that are not in the other set.
func (c Collection[T]) Diff(items Collection[T]) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if items.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Intersect answers Collection::intersect.
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

// Unique answers Collection::unique: one model per key, the first one seen.
func (c Collection[T]) Unique() Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if out.Find(model.GetKey()) == nil {
			out = append(out, model)
		}
	}
	return out
}

// Only answers Collection::only: the models with these keys.
func (c Collection[T]) Only(keys ...any) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// Except answers Collection::except: the models without these keys.
func (c Collection[T]) Except(keys ...any) Collection[T] {
	out := Collection[T]{}
	for _, model := range c {
		if !containsValue(keys, model.GetKey()) {
			out = append(out, model)
		}
	}
	return out
}

// MakeVisible answers Collection::makeVisible, on every model.
func (c Collection[T]) MakeVisible(attributes ...string) Collection[T] {
	for _, model := range c {
		model.MakeVisible(attributes...)
	}
	return c
}

// MakeHidden answers Collection::makeHidden, on every model.
func (c Collection[T]) MakeHidden(attributes ...string) Collection[T] {
	for _, model := range c {
		model.MakeHidden(attributes...)
	}
	return c
}

// SetVisible answers Collection::setVisible.
func (c Collection[T]) SetVisible(visible ...string) Collection[T] {
	for _, model := range c {
		model.SetVisible(visible...)
	}
	return c
}

// SetHidden answers Collection::setHidden.
func (c Collection[T]) SetHidden(hidden ...string) Collection[T] {
	for _, model := range c {
		model.SetHidden(hidden...)
	}
	return c
}

// Append answers Collection::append, on every model.
func (c Collection[T]) Append(attributes ...string) Collection[T] {
	for _, model := range c {
		model.Append(attributes...)
	}
	return c
}

// SetAppends answers Collection::setAppends.
func (c Collection[T]) SetAppends(appends ...string) Collection[T] {
	for _, model := range c {
		model.SetAppends(appends...)
	}
	return c
}

// ToQuery answers Collection::toQuery: a query over exactly these rows.
//
// The PHP also refuses a collection of mixed classes; a Go collection cannot
// hold two model types, so the only refusal left is the empty one -- with no
// model there is no table to query.
func (c Collection[T]) ToQuery() (*Builder[T], error) {
	first := c.First()
	if first == nil {
		return nil, ErrEmptyCollection
	}
	return first.NewModelQuery().WhereKey(c.ModelKeys()), nil
}

// ToArray answers Collection::toArray.
func (c Collection[T]) ToArray() []map[string]any {
	out := make([]map[string]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.ToArray())
	}
	return out
}

// Push answers Model::push for every model, which is what makes a loaded
// relation pushable. See Model.Push.
func (c Collection[T]) Push(g auth.Grant) (bool, error) {
	for _, model := range c {
		pushed, err := model.Push(g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// NewCollection is HasCollection::newCollection.
//
// The PHP body resolves the collection class -- from a #[CollectedBy] attribute
// or static::$collectionClass -- and then switches relationship autoloading on
// when the strict flag asks for it. Go resolves no type from a name, and there
// is no autoloading here (see the package comment on RULE 17), so what is left
// of the body is the construction.
func (m *Model[T]) NewCollection(models ...*Model[T]) Collection[T] {
	return Collection[T](models)
}

// CountBy is Collection::countBy.
//
// The PHP override exists to change the return type: counting models by
// something gives a collection of counts, not of models. Here that is the type
// of the result, so this is a function -- the key is the caller's, and Go names
// it in the signature rather than discovering it at run time.
func CountBy[T any, K comparable](c Collection[T], countBy func(model *Model[T], key int) K) map[K]int {
	return collections.CountBy(c.ToBase(), countBy)
}

// Map is Collection::map.
//
// The PHP answers a base collection when the callback returned anything that is
// not a Model, and the Eloquent collection when everything it returned was one.
// The compiler makes that choice here: a callback returning *Model[T] gives back
// exactly what Collection[T] holds, and any other R gives a collection of R.
func Map[T, R any](c Collection[T], callback func(model *Model[T], key int) R) collections.Collection[R] {
	return collections.Map(c.ToBase(), callback)
}

// MapWithKeys is Collection::mapWithKeys.
//
// PHP returns a single-entry array from the callback; Go returns the pair, which
// is what hesape/collections does with the same method. See Map for the return
// type.
func MapWithKeys[T any, K comparable, V any](c Collection[T], callback func(model *Model[T], key int) (K, V)) map[K]V {
	return collections.MapWithKeys(c.ToBase(), callback)
}

// Zip is Collection::zip.
//
// It is a function and not a method for the reason collections.Zip is one, and
// because the PHP override is there only to say the result no longer holds
// models -- which the type says here.
func Zip[T any](c Collection[T], items ...[]*Model[T]) collections.Collection[collections.Collection[*Model[T]]] {
	return collections.Zip(c.ToBase(), items...)
}

// Flatten is Collection::flatten.
//
// The models are the leaves -- a model is not a list -- so this is the same
// models as a collection of any, which is what Arr::flatten answers there for
// the same input. The depth is optional and unlimited when omitted, as in PHP.
func (c Collection[T]) Flatten(depth ...int) collections.Collection[any] {
	items := make(collections.Collection[any], 0, len(c))
	for _, model := range c {
		items = append(items, model)
	}
	return collections.Flatten(items, depth...)
}

// Flip is Collection::flip: the models become the keys and their positions
// become the values.
//
// The keys of a Collection[T] are positions, so flipping gives model to
// position, and a model that repeats keeps the last position -- which is what
// array_flip does there.
func (c Collection[T]) Flip() map[*Model[T]]int {
	return collections.Flip(c.ToBase())
}

// Pad is Collection::pad.
//
// A positive size pads on the right, a negative size on the left, and a size no
// larger than the count returns the models unchanged. That is array_pad.
func (c Collection[T]) Pad(size int, value *Model[T]) collections.Collection[*Model[T]] {
	return c.ToBase().Pad(size, value)
}

// Partition is Collection::partition: the models passing the test, then the ones
// failing it.
//
// The PHP returns a collection holding two collections; Go returns them as two
// results, which is the same pair without the indexing.
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
