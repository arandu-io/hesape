package eloquent

import (
	"reflect"

	"github.com/arandu-io/hesape/auth"
)

// GetMorphClass is Model::getMorphClass: the name a polymorphic column writes
// down for a row of this model.
//
// The PHP consults Relation::morphMap first, so that a table can store a short
// alias instead of a class name. That map lives in eloquent/relations, which
// imports this package; the alias is applied there, and this is the unaliased
// name -- the type of T, which is what get_class answers before the map.
func (m *Model[T]) GetMorphClass() string { return reflect.TypeFor[T]().Name() }

// MorphLoadable is the loaded value of a polymorphic relation: a row of
// whatever model the morph column named.
//
// PHP reads get_class off it and looks the class up in the relations map.
// Nothing in Go can turn that name back into a type, so the value answers for
// itself: *Model[R] satisfies this for every R, and the map is keyed by
// GetMorphClass exactly as it is keyed by the class name there.
type MorphLoadable interface {
	// GetMorphClass is Model::getMorphClass.
	GetMorphClass() string
	// Load is Model::load.
	Load(g auth.Grant, relations ...string) error
	// LoadAggregate is Model::loadAggregate.
	LoadAggregate(g auth.Grant, relations []string, column, function string) error
}

// LoadMorph is Model::loadMorph: eager load, on the row a polymorphic relation
// points at, the relations named for that row's own type.
//
// relation is the morph relation on this model; relations maps a morph class to
// what to load on it, which is the array keyed by class name there. A morph
// class with no entry loads nothing, as there.
func (m *Model[T]) LoadMorph(g auth.Grant, relation string, relations map[string][]string) error {
	loadable, ok := m.morphTarget(relation)
	if !ok {
		return nil
	}
	return loadable.Load(g, relations[loadable.GetMorphClass()]...)
}

// LoadMorphAggregate is Model::loadMorphAggregate.
func (m *Model[T]) LoadMorphAggregate(g auth.Grant, relation string, relations map[string][]string, column, function string) error {
	loadable, ok := m.morphTarget(relation)
	if !ok {
		return nil
	}
	return loadable.LoadAggregate(g, relations[loadable.GetMorphClass()], column, function)
}

// LoadMorphCount is Model::loadMorphCount.
func (m *Model[T]) LoadMorphCount(g auth.Grant, relation string, relations map[string][]string) error {
	return m.LoadMorphAggregate(g, relation, relations, "*", "count")
}

// LoadMorphMax is Model::loadMorphMax.
func (m *Model[T]) LoadMorphMax(g auth.Grant, relation string, relations map[string][]string, column string) error {
	return m.LoadMorphAggregate(g, relation, relations, column, "max")
}

// LoadMorphMin is Model::loadMorphMin.
func (m *Model[T]) LoadMorphMin(g auth.Grant, relation string, relations map[string][]string, column string) error {
	return m.LoadMorphAggregate(g, relation, relations, column, "min")
}

// LoadMorphSum is Model::loadMorphSum.
func (m *Model[T]) LoadMorphSum(g auth.Grant, relation string, relations map[string][]string, column string) error {
	return m.LoadMorphAggregate(g, relation, relations, column, "sum")
}

// LoadMorphAvg is Model::loadMorphAvg.
func (m *Model[T]) LoadMorphAvg(g auth.Grant, relation string, relations map[string][]string, column string) error {
	return m.LoadMorphAggregate(g, relation, relations, column, "avg")
}

// morphTarget answers the PHP's `$this->{$relation}` guard: the loaded row a
// morph relation points at, or nothing when it points at nothing.
func (m *Model[T]) morphTarget(relation string) (MorphLoadable, bool) {
	value, ok := m.GetRelation(relation)
	if !ok || value == nil {
		return nil, false
	}
	loadable, ok := value.(MorphLoadable)
	return loadable, ok
}

// LoadMorph is Collection::loadMorph.
//
// PHP groups the morphed rows by class and eager loads each group in one query.
// A []MorphLoadable cannot become a Collection[R] -- R is only known at run time
// -- so the load runs per row instead: the same rows, more queries. A caller on
// a hot path loads the concrete relation by its type, where the grouping is the
// compiler's.
func (c Collection[T]) LoadMorph(g auth.Grant, relation string, relations map[string][]string) error {
	for _, model := range c {
		if err := model.LoadMorph(g, relation, relations); err != nil {
			return err
		}
	}
	return nil
}

// LoadMorphCount is Collection::loadMorphCount. See LoadMorph for why it is one
// query per row rather than one per class.
func (c Collection[T]) LoadMorphCount(g auth.Grant, relation string, relations map[string][]string) error {
	for _, model := range c {
		if err := model.LoadMorphCount(g, relation, relations); err != nil {
			return err
		}
	}
	return nil
}
