package model

import "github.com/arandu-io/hesape/database/model/relations/concerns"

// Related reads a loaded relation off the row, as rows.
//
// The relation was loaded as rows of another table, and Rows is how this
// package holds rows of any table; the generated accessor beside the entity
// converts them to the related type. A relation that matched one row reads as
// one row, and one that matched none as nothing.
//
// It reports false when the relation was not loaded, when it holds something
// that is not a row of a table, and when it was loaded empty on a relation that
// holds at most one row. A name the table declares a relation for, asked for
// before anything loaded it, is the lazy load this framework does not do: it
// reports false rather than running a query, and PreventLazyLoading is what
// makes that silence loud.
func (m *Model) Related(name string) (Rows, bool) {
	if !live(m) {
		return nil, false
	}
	value, ok := getRelation(m, name)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case Rows:
		return typed, true
	case Entity:
		return Rows{typed}, true

	// What a relation loads is erased -- the relations tree holds the narrow
	// interface, not the model -- so the way back is through the adapter. A
	// value that is not one of this package's models, a pivot row, reads as not
	// loaded rather than as a panic.
	case []concerns.Model:
		out := make(Rows, 0, len(typed))
		for _, one := range typed {
			related, ok := unref(one)
			if !ok {
				return nil, false
			}
			out = append(out, related.r.self)
		}
		return out, true
	case concerns.Model:
		related, ok := unref(typed)
		if !ok {
			return nil, false
		}
		return Rows{related.r.self}, true
	}
	return nil, false
}

// SetRelation records value as the loaded relation named name: rows, a row, or
// whatever a relation loads.
func (m *Model) SetRelation(name string, value any) {
	if live(m) {
		setRelation(&m.r.relations, name, value)
	}
}

// RelationLoaded reports whether name has a loaded value.
func (m *Model) RelationLoaded(name string) bool { return live(m) && relationLoaded(m, name) }

// relationLoaded is RelationLoaded on a row known to be live.
func relationLoaded(m *Model, name string) bool {
	_, ok := m.r.relations[name]
	return ok
}

// getRelation returns the value loaded for a relation, and whether it was
// loaded at all, reporting a lazy-loading violation for a declared relation that
// was not.
func getRelation(m *Model, name string) (any, bool) {
	value, ok := m.r.relations[name]
	if !ok {
		if _, declared := m.r.table.relation(name); declared {
			handleLazyLoadingViolation(m, name)
		}
	}
	return value, ok
}

// unsetRelation removes the loaded relation named name.
func unsetRelation(m *Model, name string) { delete(m.r.relations, name) }

// isRelation reports whether key names a relation the table declares.
//
// It reads the registrations rather than the loaded values, so it answers true
// for a relation that has not been loaded yet -- which is the question being
// asked: "is this a relation" and not "is this relation here".
func isRelation(m *Model, key string) bool {
	_, declared := m.r.table.relation(key)
	return declared
}

// setRelation records value under name in the relations map relations points
// at, making the map first when there is none.
func setRelation(relations *map[string]any, name string, value any) {
	if *relations == nil {
		*relations = map[string]any{}
	}
	(*relations)[name] = value
}
