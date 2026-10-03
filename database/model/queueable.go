package model

import (
	"errors"
	"slices"
)

// ErrMixedQueueableConnections is what GetQueueableConnection returns for
// queued rows that are not all on one connection: they cannot be restored,
// because the job records one connection name.
var ErrMixedQueueableConnections = errors.New("model: queueing collections with multiple model connections is not supported")

// Queueable is what the queueable relations recurse into: a value hanging off a
// loaded relation that can name its own.
//
// It is one method, declared where it is consumed.
type Queueable interface {
	// GetQueueableRelations returns the names of this value's own loaded
	// relations that a queued job restores along with it.
	GetQueueableRelations() []string
}

// queueableRelations returns the loaded relations a job restores along with the
// row.
//
// A loaded relation with no registered relation behind it is skipped, since it
// cannot be loaded again on the other side of the queue.
//
// The order is sorted rather than insertion order: a Go map has none, and a job
// payload that differs between two runs over the same row is a payload nobody
// can diff.
func queueableRelations(m *Model) []string {
	out := []string{}
	for _, name := range sortedKeys(m.r.relations) {
		if _, declared := m.r.table.relation(name); !declared {
			continue
		}
		out = append(out, name)
		nested, ok := m.r.relations[name].(Queueable)
		if !ok {
			continue
		}
		for _, child := range nested.GetQueueableRelations() {
			out = append(out, name+"."+child)
		}
	}
	return out
}

// GetQueueableClass returns the type name of the rows being queued, and the
// empty string when there are none.
func (rows Rows) GetQueueableClass() string {
	first := rows.firstModel()
	if first == nil {
		return ""
	}
	return first.r.table.morphClass
}

// GetQueueableIDs returns the key of every row: what a queued job writes down
// so it can find them again.
func (rows Rows) GetQueueableIDs() []any {
	out := make([]any, 0, len(rows))
	for _, m := range rows.models() {
		out = append(out, m.GetKey())
	}
	return out
}

// GetQueueableRelations returns the relations every row has loaded.
//
// It is the intersection and not the union: a relation loaded on one row and
// not on another cannot be restored for the whole collection.
func (rows Rows) GetQueueableRelations() []string {
	found := rows.models()
	if len(found) == 0 {
		return []string{}
	}
	shared := queueableRelations(found[0])
	for _, m := range found[1:] {
		relations := queueableRelations(m)
		shared = slices.DeleteFunc(shared, func(name string) bool {
			return !slices.Contains(relations, name)
		})
	}
	return shared
}

// GetQueueableConnection returns the connection name shared by every row, or
// ErrMixedQueueableConnections when they disagree. No rows is the empty string.
func (rows Rows) GetQueueableConnection() (string, error) {
	found := rows.models()
	if len(found) == 0 {
		return "", nil
	}
	connection := found[0].r.conn.name
	for _, m := range found {
		if m.r.conn.name != connection {
			return "", ErrMixedQueueableConnections
		}
	}
	return connection, nil
}
