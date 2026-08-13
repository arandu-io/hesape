package eloquent

import (
	"errors"
	"reflect"
	"slices"
)

// ErrMixedQueueableConnections answers the LogicException
// Collection::getQueueableConnection throws: a queued collection whose models
// are not all on one connection cannot be restored, because the job records one
// connection name.
var ErrMixedQueueableConnections = errors.New("eloquent: queueing collections with multiple model connections is not supported")

// Queueable is what GetQueueableRelations recurses into: a value hanging off a
// loaded relation that can name its own.
//
// It is Illuminate\Contracts\Queue\QueueableEntity and QueueableCollection
// narrowed to the one method this walk asks of them. The contracts package
// itself is a written decision rather than a package here (ADR 0002); an
// interface with one method, declared where it is consumed, is what Go does with
// the rest of it.
type Queueable interface {
	// GetQueueableRelations is QueueableEntity::getQueueableRelations.
	GetQueueableRelations() []string
}

// GetQueueableID is Model::getQueueableId: what a queued job writes down so it
// can find this row again. The PHP spells it getQueueableId.
func (m *Model[T]) GetQueueableID() any { return m.GetKey() }

// GetQueueableConnection is Model::getQueueableConnection.
func (m *Model[T]) GetQueueableConnection() string { return m.GetConnectionName() }

// GetQueueableRelations is Model::getQueueableRelations: the loaded relations a
// job restores along with the row.
//
// The PHP skips a loaded key with no method behind it, because there anything
// can be put in $relations; here the equivalent is a name the model registered a
// resolver for, since a relation with no resolver cannot be loaded again on the
// other side of the queue.
//
// The order is sorted rather than insertion order: a Go map has none, and a job
// payload that differs between two runs over the same row is a payload nobody
// can diff.
func (m *Model[T]) GetQueueableRelations() []string {
	out := []string{}
	for _, name := range sortedKeys(m.relations) {
		if _, declared := m.RelationResolvers[name]; !declared {
			continue
		}
		out = append(out, name)
		nested, ok := m.relations[name].(Queueable)
		if !ok {
			continue
		}
		for _, child := range nested.GetQueueableRelations() {
			out = append(out, name+"."+child)
		}
	}
	return out
}

// GetQueueableClass is Collection::getQueueableClass: the type of the models
// being queued.
//
// The PHP also refuses a collection holding more than one class, which is the
// LogicException it throws. A Collection[T] cannot hold two, so the refusal has
// nothing left to refuse and the check is gone rather than always passing.
//
// It answers the empty string for an empty collection, where the PHP returns
// null: there is no model to take the class from either way.
func (c Collection[T]) GetQueueableClass() string {
	if c.IsEmpty() {
		return ""
	}
	return reflect.TypeFor[T]().Name()
}

// GetQueueableIDs is Collection::getQueueableIds. The PHP spells it
// getQueueableIds.
func (c Collection[T]) GetQueueableIDs() []any {
	if c.IsEmpty() {
		return []any{}
	}
	out := make([]any, 0, len(c))
	for _, model := range c {
		out = append(out, model.GetQueueableID())
	}
	return out
}

// GetQueueableRelations is Collection::getQueueableRelations: the relations
// every model in the collection has loaded.
//
// It is the intersection and not the union, as there: a relation loaded on one
// row and not on another cannot be restored for the whole collection.
func (c Collection[T]) GetQueueableRelations() []string {
	if c.IsEmpty() {
		return []string{}
	}
	shared := c.First().GetQueueableRelations()
	for _, model := range c[1:] {
		relations := model.GetQueueableRelations()
		shared = slices.DeleteFunc(shared, func(name string) bool {
			return !slices.Contains(relations, name)
		})
	}
	return shared
}

// GetQueueableConnection is Collection::getQueueableConnection.
//
// The PHP throws a LogicException when the models disagree; here that is
// ErrMixedQueueableConnections, and the empty collection answers the empty
// string where the PHP returns null.
func (c Collection[T]) GetQueueableConnection() (string, error) {
	if c.IsEmpty() {
		return "", nil
	}
	connection := c.First().GetConnectionName()
	for _, model := range c {
		if model.GetConnectionName() != connection {
			return "", ErrMixedQueueableConnections
		}
	}
	return connection, nil
}
