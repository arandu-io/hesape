package model

import (
	"errors"
	"reflect"
)

// Entity is a row type: the application's own struct, embedding Model.
//
// The method is unexported, so the only way to satisfy the interface is to
// embed Model and have Go promote it: an Entity is a struct the framework can
// reach the model of, and nothing else is.
type Entity interface {
	base() *Model
}

// Model is the per-row core, embedded first in every entity:
//
//	type User struct {
//		model.Model
//
//		ID   string `db:"id"`
//		Name string `db:"name"`
//	}
//
// It is one pointer. Copying an entity copies the pointer, which is cheap, and
// a struct literal holds a nil one, which is visibly a row nothing wired: every
// write on it answers ErrUnwired rather than reaching a database it has no
// connection to.
//
// The methods below are promoted onto every entity, so user.Save(ctx, g) is the
// row saving itself. Their names are the only names a model takes from an
// entity, and a field of the entity with the same name wins -- the method is
// still there, as user.Model.Save.
type Model struct {
	r *row
}

// base is how an Entity hands back its model; see Entity.
func (m *Model) base() *Model { return m }

// row is everything a model knows about the row it is: the table and the
// connection it came from, the entity it is inside, and the bookkeeping of what
// changed.
type row struct {
	table *Table
	conn  *conn

	// self is the entity this model is embedded in. It is how the model reaches
	// the columns -- the entity's fields -- and how a write recognises a copy:
	// the model of a copied entity is not self's.
	self Entity

	// attributes holds the columns no field stands behind: a withCount alias, a
	// column a migration added and the struct has not caught up with.
	attributes map[string]any
	original   map[string]any
	changes    map[string]any
	previous   map[string]any
	relations  map[string]any

	// hidden, visible and appends are this row's own serialisation lists, and
	// nil means the table's.
	hidden   []string
	visible  []string
	appends  []string
	ownLists bool

	// tableName is the name a relation gave the table to join it to itself,
	// and empty means the table's own.
	tableName string

	exists        bool
	recent        bool
	muted         bool
	forceDeleting bool
	noTimestamps  bool

	// prototype says this model stands for no row: it is the model a query runs
	// through, and the relation factories build an unconstrained relation from
	// it. See RelationFunc.
	prototype bool

	// ref is this model seen through the interface a relation asks for, kept so
	// that two calls answer the same value.
	ref *modelRef
}

// ErrUnwired is a write on an entity the framework did not build.
//
// A struct written as a literal holds a nil model: no connection, no table, no
// way back to itself. A value copy of a row holds the model of the row it was
// copied from, and writing through it would write that other row. Both are
// refused with this error, and the message says what to do rather than what went
// wrong, because in PHP $this is free and here it is not.
//
// The three it names are the three ways to get a wired row: an empty one from
// the table, a stored one from the query, and one read back.
var ErrUnwired = errors.New("model: this value was not built by the framework, so it has no connection to save through -- make one with Table.New, insert one with Builder.Create, or read one back with Find, First or Get")

// wired reports whether this model can write, and says why not.
//
// The last test is the copy: self is the entity the row was built inside, and
// the model of a value copied out of it is a different address from the one
// self hands back.
func wired(m *Model) error {
	if m == nil || m.r == nil || m.r.conn == nil || m.r.conn.connection == nil || m.r.self.base() != m {
		return ErrUnwired
	}
	return nil
}

// live reports whether this model has a row to read: a literal has none.
func live(m *Model) bool { return m != nil && m.r != nil }

// entity is the entity's struct as a reflect.Value, and whether there is one.
func entityOf(m *Model) (reflect.Value, bool) {
	if !live(m) {
		return reflect.Value{}, false
	}
	return reflect.ValueOf(m.r.self).Elem(), true
}

// modelOf returns the model of e, or nil for no entity.
func modelOf(e Entity) *Model {
	if e == nil {
		return nil
	}
	return e.base()
}
